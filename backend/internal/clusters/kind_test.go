package clusters

import (
	"context"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

func TestKindRouteFor(t *testing.T) {
	tests := []struct {
		name, cluster, context, server string
		wantNode                       string // "" for no route
	}{
		{"kind's own kubeconfig", "kind-dev", "kind-dev", "https://127.0.0.1:56974", "dev-control-plane:6443"},
		{"renamed context", "kind-dev", "lab", "https://127.0.0.1:56974", "dev-control-plane:6443"},
		{"renamed cluster entry", "lab", "kind-dev", "https://127.0.0.1:56974", "dev-control-plane:6443"},
		{"localhost", "kind-dev", "kind-dev", "https://localhost:56974", "dev-control-plane:6443"},
		{"IPv6 loopback", "kind-dev", "kind-dev", "https://[::1]:56974", "dev-control-plane:6443"},
		{"all addresses", "kind-dev", "kind-dev", "https://0.0.0.0:56974", "dev-control-plane:6443"},
		{"kind's internal kubeconfig", "kind-dev", "kind-dev", "https://dev-control-plane:6443", ""},
		{"kind on a LAN address", "kind-dev", "kind-dev", "https://192.168.1.20:6443", ""},
		{"not kind", "lab", "lab", "https://127.0.0.1:6443", ""},
		{"no name after the prefix", "kind-", "kind-", "https://127.0.0.1:6443", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := kindRouteFor(kindConfig(tt.cluster, tt.context, tt.server, nil))
			switch {
			case tt.wantNode == "" && route != nil:
				t.Errorf("got a route to %s, want none", route.node)
			case tt.wantNode != "" && route == nil:
				t.Errorf("got no route, want one to %s", tt.wantNode)
			case route != nil && route.node != tt.wantNode:
				t.Errorf("node = %s, want %s", route.node, tt.wantNode)
			}
		})
	}
}

func TestKindClusterReachedThroughItsNode(t *testing.T) {
	api, ca := fakeTLSAPIServer(t)
	dialed := fakeKindNetwork(t, api.Listener.Addr().String())

	// The kubeconfig's address has nothing behind it, as inside a container.
	// TLS still checks the certificate for 127.0.0.1, which it lists.
	cluster, err := connect(t, kindConfig("kind-test", "kind-test", "https://"+closedPort(t), ca))
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got := cluster.Via(); got != "test-control-plane:6443" {
		t.Errorf("Via = %q, want test-control-plane:6443", got)
	}
	if !slices.Contains(*dialed, "test-control-plane:6443") {
		t.Errorf("dialed %v, want the node too", *dialed)
	}
}

func TestKindClusterReachedAtItsOwnAddress(t *testing.T) {
	api, ca := fakeTLSAPIServer(t)
	dialed := fakeKindNetwork(t, "")

	// Outside a container, kind's address works and the node is never tried.
	cluster, err := connect(t, kindConfig("kind-test", "kind-test", api.URL, ca))
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got := cluster.Via(); got != "" {
		t.Errorf("Via = %q, want empty", got)
	}
	if slices.Contains(*dialed, "test-control-plane:6443") {
		t.Errorf("dialed %v, want only the kubeconfig's address", *dialed)
	}
}

func TestExplainUnreachableKindCluster(t *testing.T) {
	server := "https://" + closedPort(t)

	t.Run("in a container, off kind's network", func(t *testing.T) {
		fakeKindNetwork(t, "") // the node's name isn't known
		setInContainer(t, true)
		_, err := connect(t, kindConfig("kind-test", "kind-test", server, nil))
		got := Explain(err, server)
		for _, want := range []string{`kind cluster "test"`, "127.0.0.1 is the container itself", "--network kind", "test-control-plane:6443"} {
			if !strings.Contains(got, want) {
				t.Errorf("explanation %q doesn't mention %q", got, want)
			}
		}
	})

	t.Run("in a container, on kind's network, cluster down", func(t *testing.T) {
		fakeKindNetwork(t, closedPort(t)) // the node is known but doesn't answer
		setInContainer(t, true)
		_, err := connect(t, kindConfig("kind-test", "kind-test", server, nil))
		if got := Explain(err, server); !strings.Contains(got, "Check that the cluster is running") || strings.Contains(got, "--network") {
			t.Errorf("explanation = %q", got)
		}
	})

	t.Run("outside a container", func(t *testing.T) {
		fakeKindNetwork(t, "")
		setInContainer(t, false)
		_, err := connect(t, kindConfig("kind-test", "kind-test", server, nil))
		if got := Explain(err, server); !strings.HasPrefix(got, "Couldn't reach the API server at "+server) {
			t.Errorf("explanation = %q", got)
		}
	})

	t.Run("other loopback addresses in a container", func(t *testing.T) {
		setInContainer(t, true)
		_, err := connect(t, kindConfig("lab", "lab", server, nil))
		if got := Explain(err, server); !strings.Contains(got, "is the container itself") || strings.Contains(got, "kind") {
			t.Errorf("explanation = %q", got)
		}
	})

	t.Run("a working connection that dropped, in a container", func(t *testing.T) {
		// As when Opscope leaves kind's network: requests on the open
		// connection fail before the next dial. The address isn't to blame.
		setInContainer(t, true)
		err := &url.Error{Op: "Get", URL: server + "/version", Err: errors.New("http2: client connection lost")}
		if got := Explain(err, server); !strings.HasPrefix(got, "Couldn't reach the API server at "+server) || strings.Contains(got, "container itself") {
			t.Errorf("explanation = %q", got)
		}
	})
}

// kindConfig builds a one-context kubeconfig. caPEM may be nil.
func kindConfig(cluster, context, server string, caPEM []byte) *clientcmdapi.Config {
	config := clientcmdapi.NewConfig()
	config.Clusters[cluster] = &clientcmdapi.Cluster{Server: server, CertificateAuthorityData: caPEM}
	config.AuthInfos["user"] = &clientcmdapi.AuthInfo{Token: "token"}
	config.Contexts[context] = &clientcmdapi.Context{Cluster: cluster, AuthInfo: "user"}
	config.CurrentContext = context
	return config
}

// connect builds the clients for config and asks the cluster for its version.
func connect(t *testing.T, config *clientcmdapi.Config) (*Cluster, error) {
	t.Helper()
	clients, server, err := newClients(config)
	if err != nil {
		t.Fatal(err)
	}
	cluster := &Cluster{Server: server, Client: clients.kube, route: clients.route}
	_, err = cluster.Version()
	return cluster, err
}

// fakeTLSAPIServer is fakeAPIServer over HTTPS. It returns the server and its
// certificate as PEM, for certificate-authority-data. httptest's certificate
// lists 127.0.0.1, as kind's does.
func fakeTLSAPIServer(t *testing.T) (*httptest.Server, []byte) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"major":"1","minor":"35","gitVersion":"v1.35.0"}`)
	}))
	t.Cleanup(server.Close)
	return server, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}

// fakeKindNetwork stands in for Docker's DNS on kind's network: the node name
// test-control-plane leads to nodeAddr, or isn't known when nodeAddr is empty.
// It returns every address dialed.
func fakeKindNetwork(t *testing.T, nodeAddr string) *[]string {
	var mu sync.Mutex
	dialed := []string{}
	real := dialer
	t.Cleanup(func() { dialer = real })
	dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		if address == "test-control-plane:6443" {
			if nodeAddr == "" {
				return nil, &net.OpError{Op: "dial", Net: network, Err: &net.DNSError{
					Err: "no such host", Name: "test-control-plane", IsNotFound: true,
				}}
			}
			address = nodeAddr
		}
		return real(ctx, network, address)
	}
	return &dialed
}

func setInContainer(t *testing.T, value bool) {
	old := inContainer
	t.Cleanup(func() { inContainer = old })
	inContainer = value
}

// closedPort returns a 127.0.0.1 address that nothing listens on.
func closedPort(t *testing.T) string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	return addr
}
