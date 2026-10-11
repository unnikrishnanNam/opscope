package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// newTopologyServer has one cluster, "lab", whose snapshot comes from load
// instead of a real cluster. Its kubeconfig points nowhere; it's never used.
func newTopologyServer(t *testing.T, load func(context.Context, *clusters.Cluster) (*snapshot.Snapshot, error)) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
clusters: [{name: lab, cluster: {server: "https://127.0.0.1:1"}}]
users: [{name: lab, user: {token: x}}]
contexts: [{name: lab, context: {cluster: lab, user: lab}}]
current-context: lab
`), 0o600)
	manager := clusters.NewManager(t.TempDir())
	if _, err := manager.LoadFromFile(path, "", "lab"); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{StaticDir: t.TempDir()}, manager, metrics.NewHistory(), snapshot.NewStoreReading(load), logger)
}

// shopSnapshot is a namespace with a pod that can't start because it needs
// a ConfigMap that doesn't exist.
func shopSnapshot(context.Context, *clusters.Cluster) (*snapshot.Snapshot, error) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "shop", Name: "web", UID: "uid-web"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app", EnvFrom: []corev1.EnvFromSource{
			{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}}},
		}}}},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: corev1.ContainerState{
			Waiting: &corev1.ContainerStateWaiting{Reason: "CreateContainerConfigError", Message: `configmap "settings" not found`},
		}}}},
	}
	s := &snapshot.Snapshot{Taken: time.Now(), Pods: []corev1.Pod{pod}}
	s.BuildIndex()
	return s, nil
}

func TestTopology(t *testing.T) {
	server := newTopologyServer(t, shopSnapshot)

	rec := get(server, "/api/clusters/lab/topology?namespace=shop")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Nodes    []struct{ ID, State string }
		Edges    []struct{ From, To, Type string }
		Apps     []struct{ Name string }
		Snapshot struct {
			Taken   time.Time
			Skipped []any
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Nodes) != 2 || body.Nodes[0].ID != "configmaps/shop/settings" || body.Nodes[0].State != "missing" {
		t.Errorf("nodes = %+v", body.Nodes)
	}
	if len(body.Edges) != 1 || body.Edges[0].Type != "uses" {
		t.Errorf("edges = %+v", body.Edges)
	}
	if body.Snapshot.Taken.IsZero() || body.Snapshot.Skipped == nil {
		t.Errorf("snapshot = %+v", body.Snapshot)
	}

	if rec := get(server, "/api/clusters/lab/topology?focus=pods/shop/web&depth=1"); rec.Code != http.StatusOK {
		t.Errorf("focus: status %d: %s", rec.Code, rec.Body.String())
	}
	if rec := get(server, "/api/clusters/lab/topology?namespace=shop&app=web"); rec.Code != http.StatusOK {
		t.Errorf("app: status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTopologyErrors(t *testing.T) {
	server := newTopologyServer(t, shopSnapshot)
	tests := []struct {
		path string
		want int
		text string
	}{
		{"/api/clusters/lab/topology", http.StatusBadRequest, "give a namespace"},
		{"/api/clusters/lab/topology?app=web", http.StatusBadRequest, "within a namespace"},
		{"/api/clusters/lab/topology?focus=pods/shop/web&depth=9", http.StatusBadRequest, "depth"},
		{"/api/clusters/lab/topology?focus=pods/shop/nope", http.StatusNotFound, "no object pods/shop/nope"},
		{"/api/clusters/nope/topology?namespace=shop", http.StatusNotFound, "no cluster"},
	}
	for _, tt := range tests {
		rec := get(server, tt.path)
		if rec.Code != tt.want || !strings.Contains(rec.Body.String(), tt.text) {
			t.Errorf("%s: got %d %s, want %d containing %q", tt.path, rec.Code, rec.Body.String(), tt.want, tt.text)
		}
	}
}

func TestTopologyUnreachableCluster(t *testing.T) {
	server := newTopologyServer(t, func(context.Context, *clusters.Cluster) (*snapshot.Snapshot, error) {
		return nil, errors.New("connection refused")
	})
	if rec := get(server, "/api/clusters/lab/topology?namespace=shop"); rec.Code != http.StatusBadGateway {
		t.Errorf("got %d, want 502", rec.Code)
	}
}

func TestDiagnosis(t *testing.T) {
	server := newTopologyServer(t, shopSnapshot)

	rec := get(server, "/api/clusters/lab/diagnosis/configmaps/shop/settings")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Object struct{ ID, State string }
		Steps  []struct {
			Rule, Title string
			Root        bool
		}
		Impact   []struct{ ID string }
		Snapshot struct{ Taken time.Time }
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object.ID != "configmaps/shop/settings" || body.Object.State != "missing" {
		t.Errorf("object = %+v", body.Object)
	}
	if len(body.Steps) != 1 || body.Steps[0].Rule != "missing" || !body.Steps[0].Root {
		t.Errorf("steps = %+v", body.Steps)
	}
	if len(body.Impact) != 1 || body.Impact[0].ID != "pods/shop/web" {
		t.Errorf("impact = %+v", body.Impact)
	}
	if body.Snapshot.Taken.IsZero() {
		t.Error("snapshot time missing")
	}

	if rec := get(server, "/api/clusters/lab/diagnosis/pods/shop/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown object: got %d", rec.Code)
	}
	// The detail page's route for an object is still there next to it.
	if rec := get(server, "/api/clusters/nope/pods/shop/web"); rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no cluster") {
		t.Errorf("detail route: got %d %s", rec.Code, rec.Body.String())
	}
}

// Lists in the diagnosis are never null, so the page can read them as lists.
func TestDiagnosisListsAreNeverNull(t *testing.T) {
	body := get(newTopologyServer(t, shopSnapshot), "/api/clusters/lab/diagnosis/configmaps/shop/settings").Body.String()
	if strings.Contains(body, "null") {
		t.Errorf("found null in %s", body)
	}
}
