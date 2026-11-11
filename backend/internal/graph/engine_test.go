package graph

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"opscope/internal/bus"
	"opscope/internal/collector"
	"opscope/internal/resources"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeFeed stands in for the collectors: a real bus, and per cluster the
// modes and caches a test sets.
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

func (f *fakeFeed) Modes(cluster string) (collector.Status, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.status[cluster]
	return s, ok
}

func (f *fakeFeed) Cached(cluster, resource string) ([]runtime.Object, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.status[cluster]
	if !ok {
		return nil, false
	}
	for _, k := range s.Kinds {
		if k.Resource == resource && (k.Mode == collector.ModeWatch || k.Mode == collector.ModePoll) && k.Synced {
			return slices.Clone(f.caches[cluster][resource]), true
		}
	}
	return nil, false
}

// cluster sets a cluster up with every kind watched and these objects in
// its caches, except for the kinds in modes, which get that mode.
func (f *fakeFeed) cluster(id string, objects []Object, modes map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	status := collector.Status{Enabled: true, State: "live"}
	for _, k := range resources.Kinds() {
		mode, synced := collector.ModeWatch, true
		if m, ok := modes[k.Resource]; ok {
			mode, synced = m, m == collector.ModeWatch || m == collector.ModePoll
		}
		status.Kinds = append(status.Kinds, collector.KindStatus{Resource: k.Resource, Mode: mode, Synced: synced})
	}
	f.status[id] = status
	f.caches[id] = map[string][]runtime.Object{}
	for _, o := range objects {
		f.caches[id][o.Resource] = append(f.caches[id][o.Resource], o.Object)
	}
}

// change applies a change to a cluster's cache and, unless silent,
// publishes it as a collector would.
func (f *fakeFeed) change(cluster string, kind bus.ChangeType, o Object, silent bool) {
	m := o.Object.(metav1.Object)
	f.mu.Lock()
	list := slices.DeleteFunc(f.caches[cluster][o.Resource], func(c runtime.Object) bool {
		return c.(metav1.Object).GetUID() == m.GetUID()
	})
	if kind != bus.Deleted {
		list = append(list, o.Object)
	}
	f.caches[cluster][o.Resource] = list
	f.mu.Unlock()
	if silent {
		return
	}
	ev := bus.ObjectChanged{Cluster: cluster, Resource: o.Resource, Type: kind, UID: string(m.GetUID()),
		Namespace: m.GetNamespace(), Name: m.GetName(), ResourceVersion: m.GetResourceVersion(), Seen: time.Now()}
	if kind == bus.Deleted {
		ev.Old = o.Object
	} else {
		ev.New = o.Object
	}
	f.bus.Publish(ev)
}

func (f *fakeFeed) allObjects(cluster string) []Object {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Object
	for resource, list := range f.caches[cluster] {
		for _, o := range list {
			out = append(out, Object{Resource: resource, Object: o, Seen: seen})
		}
	}
	return out
}

func startEngine(t *testing.T, f *fakeFeed) *Engine {
	t.Helper()
	e := NewEngine(f, quiet)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return e
}

// read returns a cluster's graph as a snapshot with every evidence time
// set to the tests' own, since the engine stamps what it reads with now.
func read(t *testing.T, e *Engine, cluster string, src resources.Source) (Snapshot, Info) {
	t.Helper()
	var s Snapshot
	var info Info
	err := e.Read(context.Background(), cluster, src, "", func(g *Graph, i Info) error {
		s, info = g.Snapshot(), i
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := range s.Edges {
		for j := range s.Edges[i].Evidence {
			s.Edges[i].Evidence[j].Seen = seen
		}
	}
	return s, info
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func sameAs(t *testing.T, s Snapshot, objects []Object) bool {
	t.Helper()
	want := Build(nil, objects).Snapshot()
	a, _ := json.Marshal(s)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}

func TestEngineReadsTheCache(t *testing.T) {
	f := newFakeFeed()
	f.cluster("c1", shop(), nil)
	e := startEngine(t, f)

	s, info := read(t, e, "c1", nil)
	if info.Built != BuiltLive || len(info.Partial) != 0 {
		t.Errorf("info %+v", info)
	}
	if !sameAs(t, s, shop()) {
		t.Error("the live graph differs from one built from the same objects")
	}
}

// After any mix of changes on the bus, the live graph is the graph of
// what the cache holds.
func TestEngineFollowsTheBus(t *testing.T) {
	f := newFakeFeed()
	f.cluster("c1", shop(), nil)
	f.cluster("c2", shop()[:5], nil)
	e := startEngine(t, f)
	read(t, e, "c1", nil) // the graph exists before the changes

	f.change("c1", bus.Added, secret("shop", "stripe-key"), false)
	f.change("c1", bus.Deleted, configMap("shop", "app-config"), false)
	relabelled := shop()[16] // pods/shop/api-7d9f-x1
	p := relabelled.Object.DeepCopyObject().(metav1.Object)
	p.SetLabels(map[string]string{"app": "nothing"})
	p.SetResourceVersion("2")
	f.change("c1", bus.Updated, Object{Resource: "pods", Object: p.(runtime.Object)}, false)
	f.change("c2", bus.Added, secret("other", "only-in-c2"), false)

	eventually(t, "c1 to match its cache", func() bool {
		s, _ := read(t, e, "c1", nil)
		return sameAs(t, s, f.allObjects("c1"))
	})
	s, _ := read(t, e, "c1", nil)
	findEdge(t, s, "pods/shop/api-7d9f-x1", "secrets/shop/stripe-key", References)
	findEdge(t, s, "services/shop/ghost", "pods/shop/api-7d9f-x1", Selects)
	eventually(t, "c2 to match its cache", func() bool {
		s, _ := read(t, e, "c2", nil)
		return sameAs(t, s, f.allObjects("c2"))
	})
}

// A gap means changes may be missing: the kind is read from the cache again.
func TestEngineRereadsAfterAGap(t *testing.T) {
	for _, gap := range []bus.CollectorGap{
		{Cluster: "c1", Resource: "secrets", Reason: bus.GapRelist},
		{Cluster: "c1", Reason: bus.GapUnreachable},
		{Reason: bus.GapLagged},
	} {
		t.Run(gap.Reason, func(t *testing.T) {
			f := newFakeFeed()
			f.cluster("c1", shop(), nil)
			e := startEngine(t, f)
			read(t, e, "c1", nil)

			f.change("c1", bus.Deleted, secret("shop", "db-creds"), true)
			f.change("c1", bus.Added, secret("shop", "stripe-key"), true)
			f.bus.Publish(gap)
			f.bus.Publish(bus.ObjectChanged{Cluster: "c1", Resource: "events"}) // a gap comes with the next event
			eventually(t, "the missed changes", func() bool {
				s, _ := read(t, e, "c1", nil)
				return sameAs(t, s, f.allObjects("c1"))
			})
		})
	}
}

// A stopped collector's graph is dropped; one that starts again under the
// same name is read from its own cache.
func TestEngineDropsAStoppedCluster(t *testing.T) {
	f := newFakeFeed()
	f.cluster("c1", shop(), nil)
	e := startEngine(t, f)
	read(t, e, "c1", nil)

	f.cluster("c1", shop()[:3], nil) // the new collector's cache
	f.bus.Publish(bus.CollectorGap{Cluster: "c1", Reason: bus.GapStopped})
	f.bus.Publish(bus.ObjectChanged{Cluster: "c1", Resource: "events"})
	eventually(t, "the new cache", func() bool {
		s, _ := read(t, e, "c1", nil)
		return sameAs(t, s, shop()[:3])
	})
}

// Kinds that can't be read are Unknown and named; a kind whose mode
// changes is read in at the next check.
func TestEngineKindsItCantRead(t *testing.T) {
	f := newFakeFeed()
	f.cluster("c1", shop(), map[string]string{"secrets": collector.ModeForbidden, "gateways": collector.ModeNotInstalled,
		"serviceaccounts": collector.ModeDirect})
	e := startEngine(t, f)

	s, info := read(t, e, "c1", nil)
	if info.Built != BuiltLive {
		t.Errorf("built %s", info.Built)
	}
	want := []PartialKind{{Resource: "gateways", Code: PartialNotInstalled, Reason: "not installed"}, {Resource: "secrets", Code: PartialForbidden, Reason: "not allowed"},
		{Resource: "serviceaccounts", Code: PartialForbidden, Reason: "not allowed"}}
	if !slices.Equal(info.Partial, want) {
		t.Errorf("partial %+v, want %+v", info.Partial, want)
	}
	findEdge(t, s, "pods/shop/api-7d9f-x1", "unknown:secrets/shop/db-creds", References)
	findEdge(t, s, "pods/shop/api-7d9f-x1", "unknown:serviceaccounts/shop/api", References)

	f.cluster("c1", shop(), map[string]string{"gateways": collector.ModeNotInstalled}) // Secrets allowed now
	eventually(t, "Secrets read in", func() bool {
		s, _ := read(t, e, "c1", nil)
		return hasEdge(s, "pods/shop/api-7d9f-x1", "secrets/shop/db-creds", References)
	})
}

// countingSource is a Source that counts its calls.
type countingSource struct {
	objects []Object
	errs    map[string]error
	calls   atomic.Int32
}

func (c *countingSource) List(_ context.Context, resource string, q resources.Query) ([]runtime.Object, error) {
	c.calls.Add(1)
	if err := c.errs[resource]; err != nil {
		return nil, err
	}
	var out []runtime.Object
	for _, o := range c.objects {
		if o.Resource == resource && (q.Namespace == "" || o.Object.(metav1.Object).GetNamespace() == q.Namespace ||
			o.Object.(metav1.Object).GetNamespace() == "") {
			out = append(out, o.Object)
		}
	}
	return out, nil
}

// When the graph can't be kept live, a request builds one from the pages'
// Source, and reuses it for a few seconds.
func TestEngineBuildsOnRequest(t *testing.T) {
	cases := map[string]func(f *fakeFeed){
		"live data off": func(f *fakeFeed) { f.status["c1"] = collector.Status{Enabled: false, State: "off"} },
		"no collector":  func(f *fakeFeed) {},
		"pods read per namespace": func(f *fakeFeed) {
			f.cluster("c1", shop(), map[string]string{"pods": collector.ModeDirect})
		},
		"a kind not probed yet": func(f *fakeFeed) {
			f.cluster("c1", shop(), map[string]string{"pods": collector.ModePending})
		},
		"unreachable": func(f *fakeFeed) {
			f.cluster("c1", shop(), nil)
			s := f.status["c1"]
			s.State = "unreachable"
			f.status["c1"] = s
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFakeFeed()
			setup(f)
			e := startEngine(t, f)
			clock := time.Now()
			e.now = func() time.Time { return clock }
			src := &countingSource{objects: shop(), errs: map[string]error{
				"secrets":  apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "", errors.New("no")),
				"gateways": resources.ErrGatewayAPINotInstalled,
			}}

			s, info := read(t, e, "c1", src)
			if info.Built != BuiltOnRequest {
				t.Fatalf("built %s", info.Built)
			}
			want := []PartialKind{{Resource: "gateways", Code: PartialNotInstalled, Reason: "not installed"}, {Resource: "secrets", Code: PartialForbidden, Reason: "not allowed"}}
			if !slices.Equal(info.Partial, want) {
				t.Errorf("partial %+v", info.Partial)
			}
			findEdge(t, s, "pods/shop/api-7d9f-x1", "unknown:secrets/shop/db-creds", References)
			calls := src.calls.Load()
			if calls != int32(len(NodeKinds)) {
				t.Errorf("%d lists for %d kinds", calls, len(NodeKinds))
			}

			read(t, e, "c1", src)
			if src.calls.Load() != calls {
				t.Error("read the cluster again within a few seconds")
			}
			clock = clock.Add(onRequestFor)
			read(t, e, "c1", src)
			if src.calls.Load() == calls {
				t.Error("kept a graph built on request for too long")
			}
		})
	}
}

func TestEngineOnRequestFailsClearly(t *testing.T) {
	e := startEngine(t, newFakeFeed())
	src := &countingSource{errs: map[string]error{"pods": errors.New("connection refused")}}
	err := e.Read(context.Background(), "c1", src, "", func(*Graph, Info) error { return nil })
	if err == nil || err.Error() != "reading pods: connection refused" {
		t.Errorf("err = %v", err)
	}
}

func TestEveryNodeKindIsCollected(t *testing.T) {
	collected := map[string]bool{}
	for _, k := range resources.Kinds() {
		collected[k.Resource] = true
	}
	for _, k := range NodeKinds {
		if !collected[k] {
			t.Errorf("%s isn't collected, so the graph can never be live", k)
		}
	}
}
