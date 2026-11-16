package signals

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"opscope/internal/bus"
	"opscope/internal/collector"
	"opscope/internal/resources"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeFeed stands in for the collectors, as in the graph's tests: a real
// bus, and per cluster the modes and caches a test sets.
type fakeFeed struct {
	bus *bus.Bus

	mu     sync.Mutex
	status map[string]collector.Status
	caches map[string]map[string][]runtime.Object
}

func newFakeFeed() *fakeFeed {
	return &fakeFeed{bus: bus.New(), status: map[string]collector.Status{}, caches: map[string]map[string][]runtime.Object{}}
}

func (f *fakeFeed) Subscribe(buffer int) *bus.Subscription { return f.bus.Subscribe(buffer) }
func (f *fakeFeed) Publish(e bus.Event)                    { f.bus.Publish(e) }

func (f *fakeFeed) Modes(cluster string) (collector.Status, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.status[cluster]
	return s, ok
}

func (f *fakeFeed) Cached(cluster, resource string) ([]runtime.Object, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range f.status[cluster].Kinds {
		if k.Resource == resource && k.Mode == collector.ModeWatch && k.Synced {
			return slices.Clone(f.caches[cluster][resource]), true
		}
	}
	return nil, false
}

// cluster sets a cluster up with every kind watched, except those in
// modes, and these objects in its caches.
func (f *fakeFeed) cluster(id string, objects map[string][]runtime.Object, modes map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := collector.Status{Enabled: true, State: "live"}
	for _, k := range resources.Kinds() {
		mode, synced := collector.ModeWatch, true
		if m, ok := modes[k.Resource]; ok {
			mode, synced = m, m == collector.ModeWatch
		}
		status.Kinds = append(status.Kinds, collector.KindStatus{Resource: k.Resource, Mode: mode, Synced: synced})
	}
	f.status[id] = status
	f.caches[id] = objects
}

// change sets an object in a cluster's cache and publishes it.
func (f *fakeFeed) change(cluster, resource string, kind bus.ChangeType, obj runtime.Object) {
	m := obj.(metav1.Object)
	f.mu.Lock()
	list := slices.DeleteFunc(f.caches[cluster][resource], func(o runtime.Object) bool {
		return o.(metav1.Object).GetUID() == m.GetUID()
	})
	if kind != bus.Deleted {
		list = append(list, obj)
	}
	f.caches[cluster][resource] = list
	f.mu.Unlock()
	ev := bus.ObjectChanged{Cluster: cluster, Resource: resource, Type: kind, UID: string(m.GetUID()),
		Namespace: m.GetNamespace(), Name: m.GetName(), Seen: time.Now()}
	if kind == bus.Deleted {
		ev.Old = obj
	} else {
		ev.New = obj
	}
	f.bus.Publish(ev)
}

func startEngine(t *testing.T, f *fakeFeed) *Engine {
	t.Helper()
	e := NewEngine(f, Default, quiet)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return e
}

// eventually retries check until it passes or a second has gone.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func readSignals(t *testing.T, e *Engine, cluster string, src resources.Source) ([]Signal, Info) {
	t.Helper()
	var out []Signal
	var info Info
	if err := e.Read(context.Background(), cluster, src, "", func(v View, i Info) error {
		out, info = v.Signals(Filter{State: Open}), i
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out, info
}

func TestEngineFollowsTheBus(t *testing.T) {
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{"pods": {running("api-1")}}, nil)
	heard := f.bus.Subscribe(64)
	defer heard.Close()
	e := startEngine(t, f)

	// The first read takes the cluster from its cache.
	got, info := readSignals(t, e, "lab", nil)
	if len(got) != 0 || info.Built != BuiltLive || info.HistorySince.IsZero() {
		t.Fatalf("signals %v, info %+v", got, info)
	}
	if h := e.HealthOf("lab", "pods", "uid-api-1"); h == nil || h.State != Healthy {
		t.Errorf("health before = %+v", h)
	}
	if h := e.HealthOf("lab", "configmaps", "uid-cfg"); h != nil {
		t.Errorf("a kind no detector reads has health %+v", h)
	}

	f.change("lab", "pods", bus.Updated, crashing("api-1", 2))
	eventually(t, "the signal", func() bool {
		got, _ := readSignals(t, e, "lab", nil)
		return len(got) == 1 && got[0].Type == "crash_loop"
	})
	if h := e.HealthOf("lab", "pods", "uid-api-1"); h == nil || h.State != Failing || len(h.Signals) != 1 {
		t.Errorf("health after = %+v", h)
	}

	// The engine told the bus.
	var raised, changed bool
	eventually(t, "the bus events", func() bool {
		select {
		case ev := <-heard.C:
			switch v := ev.(type) {
			case bus.SignalRaised:
				raised = v.Type == "crash_loop" && v.UID == "uid-api-1"
			case bus.HealthChanged:
				changed = v.To == Failing
			}
		default:
		}
		return raised && changed
	})

	// A gap for pods leaves the signal open, and the relist reads it again.
	f.bus.Publish(bus.CollectorGap{Cluster: "lab", Resource: "pods", Reason: bus.GapRelist})
	eventually(t, "the relist", func() bool {
		got, _ := readSignals(t, e, "lab", nil)
		return len(got) == 1 && !got[0].Stale
	})

	// A stopped collector takes the cluster's signals and health with it.
	f.bus.Publish(bus.CollectorGap{Cluster: "lab", Reason: bus.GapStopped})
	eventually(t, "the drop", func() bool { return e.HealthOf("lab", "pods", "uid-api-1") == nil })
}

func TestEngineNamesKindsItCantRead(t *testing.T) {
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{"pods": {crashing("api-1", 1)}},
		map[string]string{"daemonsets": collector.ModeForbidden})
	e := startEngine(t, f)
	got, info := readSignals(t, e, "lab", nil)
	if len(got) != 1 || info.Built != BuiltLive {
		t.Fatalf("signals %v, info %+v", got, info)
	}
	if len(info.Partial) != 1 || info.Partial[0].Resource != "daemonsets" || info.Partial[0].Code != "forbidden" {
		t.Errorf("partial = %+v", info.Partial)
	}
}

func TestEngineWorksItOutOnRequest(t *testing.T) {
	// No collector (live data off): the answer is worked out from the
	// pages' Source, with no history.
	f := newFakeFeed()
	e := startEngine(t, f)
	kube := fake.NewClientset(crashing("api-1", 4), running("ok"), deployment("web", 2, 1))
	src := resources.Direct(resources.Clients{Kube: kube})

	got, info := readSignals(t, e, "lab", src)
	if info.Built != BuiltOnRequest || !info.HistorySince.IsZero() {
		t.Errorf("info = %+v", info)
	}
	// The Deployment's wait is taken as over: it doesn't say since when,
	// and there's no history to tell.
	if want := []string{"api-1:crash_loop:open", "web:replicas_unavailable:open"}; !slices.Equal(types_(got), want) {
		t.Errorf("signals = %v, want %v", types_(got), want)
	}
	// Gateway API isn't in the fakes: no detector reads it yet, so nothing
	// is partial.
	if len(info.Partial) != 0 {
		t.Errorf("partial = %+v", info.Partial)
	}
	if e.HealthOf("lab", "pods", "uid-api-1") != nil {
		t.Error("health from an answer built on request is shared")
	}
}

func TestEngineWaitsForACompleteCluster(t *testing.T) {
	// Pods still on their first list: the live world isn't complete, so
	// answers come from the Source and health isn't claimed.
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{}, map[string]string{"pods": collector.ModePending})
	e := startEngine(t, f)
	src := resources.Direct(resources.Clients{Kube: fake.NewClientset(crashing("api-1", 1))})
	got, info := readSignals(t, e, "lab", src)
	if info.Built != BuiltOnRequest || len(got) != 1 {
		t.Errorf("signals %v, info %+v", got, info)
	}
	if h := e.HealthOf("lab", "deployments", "uid-web"); h != nil {
		t.Errorf("health claimed before the cluster is read: %+v", h)
	}
}

// Every object the engine holds can reach it: events come with the
// detectors' kinds.
func TestRegistryKinds(t *testing.T) {
	kinds := Default.Kinds()
	for _, want := range []string{"events", "pods", "deployments", "statefulsets", "daemonsets", "replicasets"} {
		if !slices.Contains(kinds, want) {
			t.Errorf("%s isn't held: %v", want, kinds)
		}
	}
	for _, k := range kinds {
		if _, ok := resources.Lookup(k); !ok {
			t.Errorf("%s isn't a kind Opscope knows", k)
		}
	}
}

func TestRegistryRefusesMistakes(t *testing.T) {
	cases := map[string]Detector{
		"no kinds":     {Name: "x", Types: []string{"crash_loop"}, Detect: podContainers},
		"unknown type": {Name: "x", Kinds: []string{"pods"}, Types: []string{"sad_pod"}, Detect: podContainers},
		"reserved":     {Name: "x", Kinds: []string{"pods"}, Types: []string{"dependency_unreachable"}, Detect: podContainers},
		"no function":  {Name: "x", Kinds: []string{"pods"}, Types: []string{"crash_loop"}},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("registered")
				}
			}()
			(&Registry{}).Register(d)
		})
	}
}

func TestVocabulary(t *testing.T) {
	seen := map[string]bool{}
	for _, typ := range Vocabulary {
		if seen[typ.Name] {
			t.Errorf("%s twice", typ.Name)
		}
		seen[typ.Name] = true
		if severityRank(typ.Severity) == 0 || typ.About == "" {
			t.Errorf("%s: severity %q, about %q", typ.Name, typ.Severity, typ.About)
		}
	}
	// The design record's table: 28 types and one reserved word.
	if len(Vocabulary) != 29 || !seen["dependency_unreachable"] {
		t.Errorf("%d types", len(Vocabulary))
	}
	// Every type a detector raises is in it (Register checks), and every
	// detector's kinds are kinds Opscope knows.
	for _, d := range Default.Detectors() {
		for _, k := range d.Kinds {
			if _, ok := resources.Lookup(k); !ok {
				t.Errorf("%s reads %s", d.Name, k)
			}
		}
	}
}
