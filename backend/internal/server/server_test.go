package server

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
)

// newTestServer builds the server with a temporary static folder that
// contains a fake index.html.
func newTestServer(t *testing.T) http.Handler {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<html>app</html>"), 0o644)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(Config{Version: "test", StaticDir: dir}, clusters.NewManager(t.TempDir()), metrics.NewHistory(), logger)
}

// get sends a GET request to the handler and returns the recorded response.
func get(handler http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealth(t *testing.T) {
	rec := get(newTestServer(t), "/api/health")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	want := `{"status":"ok","version":"test"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestUnknownAPIRouteIsJSON404(t *testing.T) {
	rec := get(newTestServer(t), "/api/nope")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
}

func TestFrontendRoutesFallBackToIndex(t *testing.T) {
	rec := get(newTestServer(t), "/workloads/pods")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "app") {
		t.Fatalf("expected index.html, got %q", rec.Body.String())
	}
}

func TestWebManifestType(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "site.webmanifest"), []byte(`{"name":"Opscope"}`), 0o644)

	rec := get(staticHandler(dir), "/site.webmanifest")
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Fatalf("content-type = %q, want application/manifest+json", ct)
	}
}

func TestUnknownResourceIs404(t *testing.T) {
	rec := get(newTestServer(t), "/api/clusters/any/widgets")

	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "unknown resource type") {
		t.Fatalf("got %d %s, want 404 unknown resource type", rec.Code, rec.Body.String())
	}
}

// /names has its own handler; it isn't taken for a resource type called "names".
func TestNamesRoute(t *testing.T) {
	rec := get(newTestServer(t), "/api/clusters/any/names")

	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "no cluster with id any") {
		t.Fatalf("got %d %s, want 404 for the unknown cluster", rec.Code, rec.Body.String())
	}
}

func TestClusterErrorsKeepTheirMeaning(t *testing.T) {
	cluster := &clusters.Cluster{Server: "https://example:6443"}
	tests := []struct {
		err  error
		want int
	}{
		{apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("RBAC")), http.StatusForbidden},
		{apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, "web"), http.StatusNotFound},
		{errors.New("connection refused"), http.StatusBadGateway},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		writeClusterError(rec, tt.err, cluster)
		if rec.Code != tt.want {
			t.Errorf("%v: status %d, want %d", tt.err, rec.Code, tt.want)
		}
	}
}
