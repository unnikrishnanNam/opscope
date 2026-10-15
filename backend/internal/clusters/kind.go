package clusters

// Reaching kind clusters when Opscope runs in a container.
//
// kind publishes each cluster's API server on the host at 127.0.0.1 and a
// random port, and that's the address in the kubeconfig it writes. Inside a
// container, 127.0.0.1 is the container itself, so the address leads nowhere.
// kind's node containers sit on a Docker network called "kind", where Docker's
// DNS knows each one by its container name and the API server listens on its
// own port, 6443. A container on that network reaches the cluster at
// <name>-control-plane:6443.
//
// So for a kubeconfig kind wrote (its cluster or context is "kind-<name>")
// with a loopback address, Opscope dials the address as written and, only if
// that fails, the control-plane node by its name. Outside a container, or with
// --network host, the first dial works and nothing changes. The kubeconfig is
// saved as it is, and TLS still checks the certificate against the address in
// the kubeconfig: kind's API server certificate lists 127.0.0.1 and localhost.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// kindAPIPort is the port kind's API server listens on inside its node.
const kindAPIPort = "6443"

// dialer opens TCP connections, with the settings client-go uses when no dial
// function is set. Tests replace it.
var dialer = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext

// inContainer is true when Opscope runs in a Docker or Podman container,
// which mark their containers with these files. It only changes how a failed
// connection is explained.
var inContainer = fileExists("/.dockerenv") || fileExists("/run/.containerenv")

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// kindRoute is the dial function for one kind cluster. It remembers whether
// the last connection went to the node, for Cluster.Via.
type kindRoute struct {
	cluster     string // kind's name for the cluster ("opscope-test")
	direct      string // host:port from the kubeconfig ("127.0.0.1:56974")
	node        string // the control-plane node on kind's network ("opscope-test-control-plane:6443")
	throughNode atomic.Bool
}

// kindRouteFor returns a route for the current context of config when it
// looks like a kind cluster at a loopback address, and nil otherwise.
func kindRouteFor(config *clientcmdapi.Config) *kindRoute {
	ctx, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return nil
	}
	cluster, ok := config.Clusters[ctx.Cluster]
	if !ok {
		return nil
	}
	name := kindName(ctx.Cluster, config.CurrentContext)
	if name == "" {
		return nil
	}
	direct, ok := loopbackAddress(cluster.Server)
	if !ok {
		return nil
	}
	return &kindRoute{
		cluster: name,
		direct:  direct,
		node:    net.JoinHostPort(name+"-control-plane", kindAPIPort),
	}
}

// kindName returns <name> from the first of names that is "kind-<name>", the
// way kind names the cluster, context and user it writes. The cluster entry
// comes first, since people rename contexts more often.
func kindName(names ...string) string {
	for _, n := range names {
		if name, ok := strings.CutPrefix(n, "kind-"); ok && name != "" {
			return name
		}
	}
	return ""
}

// loopbackAddress returns the host:port a server URL points to, if its host
// means "this machine": localhost, 127.0.0.0/8, ::1 or 0.0.0.0.
func loopbackAddress(server string) (string, bool) {
	u, err := url.Parse(server)
	if err != nil {
		return "", false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !(ip.IsLoopback() || ip.IsUnspecified())) {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = "443"
		if u.Scheme == "http" {
			port = "80"
		}
	}
	return net.JoinHostPort(host, port), true
}

// dial is the cluster's rest.Config.Dial. Only connections to the address in
// the kubeconfig are rerouted; anything else (a proxy, say) is dialed as is.
func (r *kindRoute) dial(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := dialer(ctx, network, address)
	if address != r.direct {
		return conn, err
	}
	if err == nil {
		r.throughNode.Store(false)
		return conn, nil
	}
	nodeConn, nodeErr := dialer(ctx, network, r.node)
	if nodeErr != nil {
		return nil, &kindDialError{route: r, err: err, nodeErr: nodeErr}
	}
	r.throughNode.Store(true)
	return nodeConn, nil
}

// Via returns where the last connection to the cluster went when that isn't
// the address in Server: a kind node on kind's Docker network. It is empty
// for every other cluster, and before the first connection.
func (c *Cluster) Via() string {
	if c.route == nil || !c.route.throughNode.Load() {
		return ""
	}
	return c.route.node
}

// kindDialError means neither the kubeconfig's address nor the node on kind's
// network answered. It unwraps to the first error, so it still counts as a
// network error.
type kindDialError struct {
	route   *kindRoute
	err     error // dialing the address in the kubeconfig
	nodeErr error // dialing the node
}

func (e *kindDialError) Error() string {
	return fmt.Sprintf("%v (and on kind's network: %v)", e.err, e.nodeErr)
}

func (e *kindDialError) Unwrap() error { return e.err }

// explainUnreachable explains a connection that didn't get through. In a
// container, an address on "this machine" means the container itself, which
// gets its own advice.
func explainUnreachable(err error, server string) string {
	generic := "Couldn't reach the API server at " + server + ". Check the address and that this machine " +
		"(or container) can reach it."
	if !inContainer {
		return generic
	}
	host := server
	if u, err := url.Parse(server); err == nil {
		host = u.Hostname()
	}

	var kindErr *kindDialError
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &kindErr) && errors.As(kindErr.nodeErr, &dnsErr) && dnsErr.IsNotFound:
		// The node's name isn't known: Opscope isn't on kind's network, or
		// the cluster is stopped (Docker's DNS only knows running containers).
		return fmt.Sprintf("Couldn't reach the kind cluster %q. Opscope runs in a container, where %s is the container "+
			"itself, not your machine. Put the container on kind's Docker network (`--network kind`, or "+
			"`docker network connect kind <container>`) and Opscope connects to %s instead. If it's on that network "+
			"already, check that the cluster is running.", kindErr.route.cluster, host, kindErr.route.node)
	case errors.As(err, &kindErr):
		return fmt.Sprintf("Couldn't reach the kind cluster %q at %s on kind's Docker network. Check that the "+
			"cluster is running.", kindErr.route.cluster, kindErr.route.node)
	}
	// Only a failed dial says nothing listens there. A connection that was
	// working and dropped (a kind cluster reached through its node) isn't that.
	var opErr *net.OpError
	if _, local := loopbackAddress(server); local && errors.As(err, &opErr) && opErr.Op == "dial" {
		return "Couldn't reach the API server at " + server + ". Opscope runs in a container, where " + host +
			" is the container itself, not your machine. Use an address the container can reach."
	}
	return generic
}
