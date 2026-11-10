package server

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"opscope/internal/bus"
	"opscope/internal/clusters"
	"opscope/internal/collector"
	"opscope/internal/graph"
)

// fakeFeed stands in for the collectors: a real bus, and a status the test
// sets.
type fakeFeed struct {
	bus  *bus.Bus
	done chan struct{}

	mu     sync.Mutex
	status collector.Status
	known  bool
}

func newFakeFeed() *fakeFeed {
	return &fakeFeed{
		bus:    bus.New(),
		done:   make(chan struct{}),
		known:  true,
		status: collector.Status{Enabled: true, State: "live", Kinds: []collector.KindStatus{{Resource: "pods", Mode: "watch"}}},
	}
}

func (f *fakeFeed) Enabled() bool { return true }

func (f *fakeFeed) Modes(string) (collector.Status, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.known
}

func (f *fakeFeed) Subscribe(buffer int) *bus.Subscription { return f.bus.Subscribe(buffer) }
func (f *fakeFeed) Done() <-chan struct{}                  { return f.done }

func (f *fakeFeed) set(fn func(*fakeFeed)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// managerWithCluster knows one cluster, "lab", which is never contacted.
func managerWithCluster(t *testing.T) *clusters.Manager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	os.WriteFile(path, []byte(`apiVersion: v1
kind: Config
current-context: lab
clusters:
- name: lab
  cluster: {server: "https://lab.example:6443"}
contexts:
- name: lab
  context: {cluster: lab, user: lab}
users:
- name: lab
  user: {token: t}
`), 0o600)
	manager := clusters.NewManager(t.TempDir())
	if _, err := manager.LoadFromFile(path, "", "lab"); err != nil {
		t.Fatal(err)
	}
	return manager
}

type sse struct {
	event string
	data  string
}

// openStream starts the handler on a real server and returns its messages
// as they arrive, heartbeats left out.
func openStream(t *testing.T, handler http.Handler, path string) (*http.Response, <-chan sse) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })

	messages := make(chan sse, 100)
	go func() {
		defer close(messages)
		scanner := bufio.NewScanner(resp.Body)
		var m sse
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				m.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				m.data = strings.TrimPrefix(line, "data: ")
			case line == "" && m.event != "":
				if m.event != "heartbeat" {
					messages <- m
				}
				m = sse{}
			}
		}
	}()
	return resp, messages
}

func next(t *testing.T, messages <-chan sse) sse {
	t.Helper()
	select {
	case m, ok := <-messages:
		if !ok {
			t.Fatal("the stream ended")
		}
		return m
	case <-time.After(3 * time.Second):
		t.Fatal("no message within 3 seconds")
	}
	return sse{}
}

func expect(t *testing.T, messages <-chan sse, event, data string) {
	t.Helper()
	if m := next(t, messages); m.event != event || m.data != data {
		t.Fatalf("got %s %s, want %s %s", m.event, m.data, event, data)
	}
}

func TestStreamSendsChangesAndStatus(t *testing.T) {
	feed := newFakeFeed()
	handler := streamLive(managerWithCluster(t), feed)
	mux := http.NewServeMux()
	mux.Handle("GET /api/clusters/{id}/live/stream", handler)
	resp, messages := openStream(t, mux, "/api/clusters/lab/live/stream")

	for header, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s: %q, want %q", header, got, want)
		}
	}
	expect(t, messages, "status", `{"state":"live","kinds":[{"resource":"pods","mode":"watch"}]}`)

	// Changes in two namespaces are sent as one message; another cluster's
	// are left out; a cluster-wide kind names no namespace.
	feed.bus.Publish(bus.ObjectChanged{Cluster: "lab", Resource: "pods", Namespace: "shop"})
	feed.bus.Publish(bus.ObjectChanged{Cluster: "lab", Resource: "pods", Namespace: "default"})
	feed.bus.Publish(bus.ObjectChanged{Cluster: "other", Resource: "services", Namespace: "default"})
	feed.bus.Publish(bus.ObjectChanged{Cluster: "lab", Resource: "nodes"})
	got := []sse{next(t, messages), next(t, messages)}
	want := []sse{
		{"changed", `{"resource":"nodes"}`},
		{"changed", `{"resource":"pods","namespaces":["default","shop"]}`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// A mode that changes is sent as a new status.
	feed.set(func(f *fakeFeed) {
		f.status = collector.Status{Enabled: true, State: "partial", Kinds: []collector.KindStatus{
			{Resource: "pods", Mode: "poll", Reason: "watch isn't allowed", Objects: 3},
		}}
	})
	expect(t, messages, "status", `{"state":"partial","kinds":[{"resource":"pods","mode":"poll","reason":"watch isn't allowed"}]}`)

	// A subscriber that lost events asks the browser to fetch everything.
	feed.bus.Publish(bus.CollectorGap{Reason: bus.GapLagged})
	expect(t, messages, "resync", `{}`)
	// Other gaps don't: a relist's changes come as changes.
	feed.bus.Publish(bus.CollectorGap{Cluster: "lab", Resource: "pods", Reason: bus.GapRelist})
	feed.bus.Publish(bus.ObjectChanged{Cluster: "lab", Resource: "services", Namespace: "a"})
	expect(t, messages, "changed", `{"resource":"services","namespaces":["a"]}`)

	// Shutting down ends the stream.
	close(feed.done)
	select {
	case _, open := <-messages:
		if open {
			t.Fatal("got a message after shutdown")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream didn't end on shutdown")
	}
}

func TestStreamEndsWhenTheClusterIsRemoved(t *testing.T) {
	feed := newFakeFeed()
	mux := http.NewServeMux()
	mux.Handle("GET /api/clusters/{id}/live/stream", streamLive(managerWithCluster(t), feed))
	_, messages := openStream(t, mux, "/api/clusters/lab/live/stream")
	next(t, messages)

	feed.set(func(f *fakeFeed) { f.known = false })
	select {
	case _, open := <-messages:
		if open {
			t.Fatal("got a message for a removed cluster")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream didn't end")
	}
}

func TestStreamAnswers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := managerWithCluster(t)
	off := collector.NewSet(t.Context(), false, bus.New(), logger)
	on := collector.NewSet(t.Context(), true, bus.New(), logger) // not following the manager: no collector

	tests := []struct {
		name string
		live *collector.Set
		path string
		want int
	}{
		{"collectors off: don't reconnect", off, "/api/clusters/lab/live/stream", http.StatusNoContent},
		{"unknown cluster", on, "/api/clusters/nope/live/stream", http.StatusNotFound},
		{"collector not started yet", on, "/api/clusters/lab/live/stream", http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		handler := New(Config{}, manager, tt.live, graph.NewEngine(tt.live, logger), nil, logger)
		if rec := get(handler, tt.path); rec.Code != tt.want {
			t.Errorf("%s: %d %s, want %d", tt.name, rec.Code, rec.Body.String(), tt.want)
		}
	}
}

func TestChangesAreSentAtMostOnceASecond(t *testing.T) {
	g := newChangeGatherer("lab", time.Second)
	t0 := time.Now()
	change := func(ns string) { g.add(bus.ObjectChanged{Cluster: "lab", Resource: "pods", Namespace: ns}) }
	resources := func(cs []changed) string {
		b, _ := json.Marshal(cs)
		return string(b)
	}

	change("a")
	if got := resources(g.due(t0)); got != `[{"resource":"pods","namespaces":["a"]}]` {
		t.Fatalf("first change: %s", got)
	}
	change("b")
	change("c")
	if got := g.due(t0.Add(500 * time.Millisecond)); got != nil {
		t.Fatalf("half a second later: %v, want nothing yet", got)
	}
	if got := resources(g.due(t0.Add(time.Second))); got != `[{"resource":"pods","namespaces":["b","c"]}]` {
		t.Fatalf("a second later: %s", got)
	}
	if got := g.due(t0.Add(5 * time.Second)); got != nil {
		t.Fatalf("nothing changed: %v", got)
	}
}
