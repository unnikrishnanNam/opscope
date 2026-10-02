package clusters

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeAPIServer pretends to be a Kubernetes API server that only knows
// GET /version, which is all Add needs for its connection check.
func fakeAPIServer(t *testing.T) *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"major":"1","minor":"31","gitVersion":"v1.31.0"}`)
	}))
	t.Cleanup(server.Close)
	return server
}

// kubeconfig builds a kubeconfig with two contexts: "good" points at
// goodServer, "other" at a second address. userExtra is added to the
// user entry of "good" (used to test exec plugins).
func kubeconfig(goodServer, userExtra string) []byte {
	return []byte(`apiVersion: v1
kind: Config
current-context: good
clusters:
- name: good-cluster
  cluster:
    server: ` + goodServer + `
- name: other-cluster
  cluster:
    server: https://other.example:6443
contexts:
- name: good
  context: {cluster: good-cluster, user: good-user}
- name: other
  context: {cluster: other-cluster, user: other-user}
users:
- name: good-user
  user:
    token: good-token` + userExtra + `
- name: other-user
  user:
    token: secret-other-token
`)
}

func TestListContexts(t *testing.T) {
	contexts, current, err := ListContexts(kubeconfig("https://good.example:6443", ""))
	if err != nil {
		t.Fatal(err)
	}
	if current != "good" {
		t.Errorf("current = %q, want good", current)
	}
	if len(contexts) != 2 || contexts[0].Name != "good" || contexts[1].Server != "https://other.example:6443" {
		t.Errorf("unexpected contexts: %+v", contexts)
	}
}

func TestListContextsRejectsGarbage(t *testing.T) {
	if _, _, err := ListContexts([]byte("hello: [")); err == nil {
		t.Fatal("expected an error for invalid YAML")
	}
}

func TestAddSavesOnlyTheChosenContext(t *testing.T) {
	api := fakeAPIServer(t)
	dataDir := t.TempDir()

	cluster, err := NewManager(dataDir).Add("Test Lab", kubeconfig(api.URL, ""), "good")
	if err != nil {
		t.Fatal(err)
	}
	if cluster.ID != "test-lab" || cluster.Source != SourceUI {
		t.Errorf("unexpected cluster: %+v", cluster)
	}

	// The saved file is private and contains no credentials for the other context.
	file := filepath.Join(dataDir, "clusters", "test-lab.json")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
	data, _ := os.ReadFile(file)
	if strings.Contains(string(data), "secret-other-token") {
		t.Error("saved file contains credentials for a context that wasn't chosen")
	}

	// A fresh manager (like after a restart) loads it back.
	restarted := NewManager(dataDir)
	if errs := restarted.LoadSaved(); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, ok := restarted.Get("test-lab"); !ok {
		t.Error("cluster not loaded after restart")
	}
}

func TestAddRejectsExecPlugins(t *testing.T) {
	api := fakeAPIServer(t)
	execUser := "\n    exec:\n      apiVersion: client.authentication.k8s.io/v1\n      command: /bin/sh"

	_, err := NewManager(t.TempDir()).Add("lab", kubeconfig(api.URL, execUser), "good")
	if err == nil || !strings.Contains(err.Error(), "run a command") {
		t.Fatalf("expected exec plugin to be rejected, got %v", err)
	}
}

func TestAddReportsUnreachableCluster(t *testing.T) {
	// Port 1 on localhost: nothing listens there, so the connection is refused.
	_, err := NewManager(t.TempDir()).Add("lab", kubeconfig("https://127.0.0.1:1", ""), "good")

	var connErr *ConnectionError
	if !errors.As(err, &connErr) {
		t.Fatalf("expected a ConnectionError, got %v", err)
	}
	if !strings.Contains(connErr.Error(), "Couldn't reach") {
		t.Errorf("unexpected explanation: %s", connErr.Error())
	}
}

func TestAddRejectsDuplicateNames(t *testing.T) {
	api := fakeAPIServer(t)
	manager := NewManager(t.TempDir())

	if _, err := manager.Add("lab", kubeconfig(api.URL, ""), "good"); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Add("Lab", kubeconfig(api.URL, ""), "good"); err == nil {
		t.Fatal("expected a duplicate name to be rejected")
	}
}

func TestEnvClusterCannotBeRemoved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kubeconfig")
	os.WriteFile(path, kubeconfig("https://good.example:6443", ""), 0o600)

	manager := NewManager(t.TempDir())
	cluster, err := manager.LoadFromFile(path, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if cluster.Source != SourceEnv || cluster.ID != "good" {
		t.Errorf("unexpected cluster: %+v", cluster)
	}
	if err := manager.Remove(cluster.ID); err == nil {
		t.Fatal("expected removing an environment cluster to fail")
	}
}
