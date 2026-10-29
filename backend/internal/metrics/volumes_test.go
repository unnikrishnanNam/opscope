package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

// podOn is a running pod on a node, with the given volumes.
func podOn(namespace, name, node string, phase corev1.PodPhase, volumes ...corev1.Volume) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:       corev1.PodSpec{NodeName: node, Volumes: volumes},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func claimVolume(name, claim string) corev1.Volume {
	return corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
	}}
}

// stubStats serves canned summaries per node and counts the calls.
type stubStats struct {
	mu      sync.Mutex
	answers map[string]string // node -> summary JSON
	errs    map[string]error
	calls   map[string]int
}

func (s *stubStats) fetch(_ context.Context, _ kubernetes.Interface, node string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[node]++
	if err := s.errs[node]; err != nil {
		return nil, err
	}
	return []byte(s.answers[node]), nil
}

func (s *stubStats) asked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var nodes []string
	for node := range s.calls {
		nodes = append(nodes, node)
	}
	slices.Sort(nodes)
	return nodes
}

func newStubbed(stub *stubStats) *Volumes {
	v := NewVolumes()
	v.fetch = stub.fetch
	return v
}

// The kubelet's summary, trimmed to what Opscope reads.
const summaryA = `{"node": {"nodeName": "a"}, "pods": [
  {"podRef": {"namespace": "data", "name": "db-0"}, "volume": [
    {"name": "data", "pvcRef": {"namespace": "data", "name": "db"},
     "usedBytes": 900, "capacityBytes": 1000, "availableBytes": 100, "inodesUsed": 10, "inodes": 100},
    {"name": "tmp", "usedBytes": 5, "capacityBytes": 50}
  ]},
  {"podRef": {"namespace": "data", "name": "worker"}, "volume": [
    {"name": "scratch", "pvcRef": {"namespace": "data", "name": "worker-scratch"},
     "usedBytes": 1, "capacityBytes": 10, "availableBytes": 9, "inodesUsed": 1, "inodes": 10},
    {"name": "fresh", "pvcRef": {"namespace": "data", "name": "just-mounted"}}
  ]},
  {"podRef": {"namespace": "web", "name": "cache-0"}, "volume": [
    {"name": "cache", "pvcRef": {"namespace": "web", "name": "cache"}, "usedBytes": 1, "capacityBytes": 2}
  ]}
]}`

// The same ReadWriteMany claim, mounted on a second node.
const summaryC = `{"pods": [{"volume": [
  {"name": "data", "pvcRef": {"namespace": "data", "name": "db"}, "usedBytes": 900, "capacityBytes": 1000}
]}]}`

func TestReadVolumes(t *testing.T) {
	scratch := corev1.Volume{Name: "scratch", VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{}}}
	kube := fake.NewClientset(
		podOn("data", "db-0", "a", corev1.PodRunning, claimVolume("data", "db")),
		podOn("data", "worker", "a", corev1.PodRunning, scratch, corev1.Volume{Name: "tmp"}, claimVolume("fresh", "just-mounted")),
		podOn("data", "files", "c", corev1.PodRunning, claimVolume("files", "local-path")),
		podOn("data", "logs-0", "b", corev1.PodRunning, claimVolume("logs", "logs")),
		podOn("data", "reader", "c", corev1.PodRunning, claimVolume("data", "db")),
		podOn("data", "no-claims", "d", corev1.PodRunning, corev1.Volume{Name: "tmp"}),
		podOn("data", "done", "e", corev1.PodSucceeded, claimVolume("data", "db")),
		podOn("data", "unscheduled", "", corev1.PodPending, claimVolume("data", "db")),
		podOn("web", "cache-0", "f", corev1.PodRunning, claimVolume("cache", "cache")),
	)
	stub := &stubStats{
		answers: map[string]string{"a": summaryA, "c": summaryC},
		errs:    map[string]error{"b": errors.New("the node didn't answer")},
		calls:   map[string]int{},
	}

	report, err := newStubbed(stub).Read(context.Background(), "lab", kube, "data")
	if err != nil {
		t.Fatal(err)
	}

	// Only nodes running pods that mount claims in this namespace.
	if got := stub.asked(); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Errorf("asked nodes %v, want a, b and c", got)
	}
	// Claims in other namespaces, volumes without a claim, and volumes not
	// measured yet are left out; the claim on two nodes is there once.
	want := []VolumeUsage{
		{Namespace: "data", Name: "db", Used: 900, Capacity: 1000, Available: 100, InodesUsed: 10, Inodes: 100},
		{Namespace: "data", Name: "worker-scratch", Used: 1, Capacity: 10, Available: 9, InodesUsed: 1, Inodes: 10},
	}
	if !reflect.DeepEqual(report.Volumes, want) {
		t.Errorf("volumes:\n got %+v\nwant %+v", report.Volumes, want)
	}
	wantUnread := []UnreadNode{{Node: "b", Error: "the node didn't answer", Claims: []string{"data/logs"}}}
	if !reflect.DeepEqual(report.Unread, wantUnread) {
		t.Errorf("unread: got %+v, want %+v", report.Unread, wantUnread)
	}
	// Mounted, but the kubelet gave no numbers: not measured yet, or a kind
	// of volume it doesn't measure (like local-path's hostPath folders).
	if want := []string{"data/just-mounted", "data/local-path"}; !slices.Equal(report.Unmeasured, want) {
		t.Errorf("unmeasured: got %v, want %v", report.Unmeasured, want)
	}
}

func TestReadVolumesWithNothingMounted(t *testing.T) {
	stub := &stubStats{calls: map[string]int{}}
	report, err := newStubbed(stub).Read(context.Background(), "lab", fake.NewClientset(), "")
	if err != nil {
		t.Fatal(err)
	}
	// Empty lists, not null, so the page can tell "nothing mounted" from "off".
	if report.Volumes == nil || len(report.Volumes) != 0 || report.Unread == nil || report.Unmeasured == nil ||
		len(stub.calls) != 0 {
		t.Errorf("unexpected report %+v, calls %v", report, stub.calls)
	}
}

func TestVolumeUsageCache(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	Now = func() time.Time { return now }
	defer func() { Now = time.Now }()

	kube := fake.NewClientset(
		podOn("data", "db-0", "a", corev1.PodRunning, claimVolume("data", "db")),
		podOn("data", "logs-0", "b", corev1.PodRunning, claimVolume("logs", "logs")),
	)
	stub := &stubStats{
		answers: map[string]string{"a": summaryA},
		errs:    map[string]error{"b": errors.New("timeout")},
		calls:   map[string]int{},
	}
	v := newStubbed(stub)
	read := func() {
		t.Helper()
		if _, err := v.Read(context.Background(), "lab", kube, ""); err != nil {
			t.Fatal(err)
		}
	}

	read()
	now = now.Add(20 * time.Second)
	read()
	// Failures are remembered too, so a node that's down doesn't slow every refresh.
	if stub.calls["a"] != 1 || stub.calls["b"] != 1 {
		t.Errorf("within the cache time: calls %v, want one each", stub.calls)
	}

	// Another cluster with a node of the same name is asked separately.
	if _, err := v.Read(context.Background(), "other", kube, ""); err != nil {
		t.Fatal(err)
	}
	if stub.calls["a"] != 2 || stub.calls["b"] != 2 {
		t.Errorf("another cluster should have its own cache: calls %v", stub.calls)
	}

	now = now.Add(volumeCacheFor)
	read()
	if stub.calls["a"] != 3 || stub.calls["b"] != 3 {
		t.Errorf("after the cache time: calls %v, want each asked 3 times", stub.calls)
	}
}

func TestVolumeUsageForbidden(t *testing.T) {
	kube := fake.NewClientset(
		podOn("data", "db-0", "a", corev1.PodRunning, claimVolume("data", "db")),
		podOn("data", "logs-0", "b", corev1.PodRunning, claimVolume("logs", "logs")),
	)
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "nodes/proxy"}, "a", errors.New("RBAC"))
	stub := &stubStats{
		answers: map[string]string{"b": summaryA},
		errs:    map[string]error{"a": forbidden},
		calls:   map[string]int{},
	}

	_, err := newStubbed(stub).Read(context.Background(), "lab", kube, "")
	if !errors.Is(err, ErrVolumeUsageForbidden) || !apierrors.IsForbidden(err) {
		t.Fatalf("got %v, want ErrVolumeUsageForbidden wrapping the API's answer", err)
	}
}

func TestReadVolumesWithoutPodAccess(t *testing.T) {
	kube := fake.NewClientset()
	kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("RBAC"))
	})
	_, err := NewVolumes().Read(context.Background(), "lab", kube, "")
	// Not the node proxy's fault: an ordinary "not allowed", like any list.
	if !apierrors.IsForbidden(err) || errors.Is(err, ErrVolumeUsageForbidden) {
		t.Fatalf("got %v, want a plain forbidden error", err)
	}
}

func TestParseSummaryRejectsGarbage(t *testing.T) {
	if _, err := parseSummary([]byte("<html>not json</html>")); err == nil {
		t.Error("expected an error for a summary that isn't JSON")
	}
}

// fetchSummary against a stand-in API server: the path it asks for, and a
// refusal that keeps the API server's message.
func TestFetchSummary(t *testing.T) {
	refusal := `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403,` +
		`"message":"nodes \"b\" is forbidden: User \"viewer\" cannot get resource \"nodes/proxy\""}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/nodes/a/proxy/stats/summary":
			w.Write([]byte(summaryC))
		case "/api/v1/nodes/b/proxy/stats/summary":
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(refusal))
		default:
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte("kubelet unreachable"))
		}
	}))
	defer srv.Close()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}

	body, err := fetchSummary(context.Background(), kube, "a")
	if err != nil || string(body) != summaryC {
		t.Errorf("node a: got %q, %v", body, err)
	}

	_, err = fetchSummary(context.Background(), kube, "b")
	if !apierrors.IsForbidden(err) || err.Error() != `nodes "b" is forbidden: User "viewer" cannot get resource "nodes/proxy"` {
		t.Errorf("node b: got %v", err)
	}

	if _, err = fetchSummary(context.Background(), kube, "c"); err == nil {
		t.Error("node c: expected an error")
	}
}
