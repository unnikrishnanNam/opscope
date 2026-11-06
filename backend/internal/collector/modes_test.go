package collector

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	clienttesting "k8s.io/client-go/testing"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

func forbidden(resource string) error {
	return apierrors.NewForbidden(schema.GroupResource{Resource: resource}, "", errors.New("RBAC"))
}

// forbid makes the dynamic fake refuse a verb on a resource while *on is
// true. The probe asks through the dynamic client.
func forbid(f *fakeCluster, verb, resource string, on *atomic.Bool) {
	if verb == "watch" {
		f.dyn.PrependWatchReactor(resource, func(clienttesting.Action) (bool, watch.Interface, error) {
			if on.Load() {
				return true, nil, forbidden(resource)
			}
			return false, nil, nil
		})
		return
	}
	f.dyn.PrependReactor(verb, resource, func(clienttesting.Action) (bool, runtime.Object, error) {
		if on.Load() {
			return true, nil, forbidden(resource)
		}
		return false, nil, nil
	})
}

func always() *atomic.Bool {
	var b atomic.Bool
	b.Store(true)
	return &b
}

func statusOf(c *Collector, resource string) KindStatus {
	for _, k := range c.Status().Kinds {
		if k.Resource == resource {
			return k
		}
	}
	return KindStatus{}
}

// Each kind's mode follows what the cluster allows, and the status says why.
func TestModesFollowPermissions(t *testing.T) {
	cluster := newFakeCluster([]runtime.Object{pod("api-1", "1", nil)})
	forbid(cluster, "list", "secrets", always())
	forbid(cluster, "list", "nodes", always())
	forbid(cluster, "watch", "pods", always())
	cluster.dyn.PrependReactor("list", "gateways", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "gateways"}, "")
	})

	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	c := start(t.Context(), "lab", cluster.clients(), b, quiet, fast)
	eventually(t, "every kind settled", func() bool { return c.Status().State != "connecting" })

	for resource, want := range map[string]KindStatus{
		"secrets":    {Mode: ModeDirect, Reason: "listing across all namespaces isn't allowed"},
		"nodes":      {Mode: ModeForbidden, Reason: "listing isn't allowed"},
		"pods":       {Mode: ModePoll, Reason: "watching isn't allowed", Synced: true, Objects: 1},
		"gateways":   {Mode: ModeNotInstalled, Reason: "not installed on this cluster"},
		"configmaps": {Mode: ModeWatch, Synced: true},
	} {
		got := statusOf(c, resource)
		if got.Mode != want.Mode || got.Reason != want.Reason || got.Synced != want.Synced || got.Objects != want.Objects {
			t.Errorf("%s: %+v, want %+v", resource, got, want)
		}
	}
	if s := c.Status().State; s != "partial" {
		t.Errorf("state = %s, want partial", s)
	}

	// Polled pods: a new pod shows up at the next poll, after a gap (changes
	// between two polls arrive collapsed), and in the cache.
	if _, err := cluster.kube.CoreV1().Pods("web").Create(context.Background(), pod("api-2", "1", nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	added := rec.next(t, "pods")
	if added.Type != bus.Added || added.Name != "api-2" || added.Initial {
		t.Errorf("unexpected change: %+v", added)
	}
	if len(rec.gaps) == 0 || rec.gaps[len(rec.gaps)-1].Resource != "pods" || rec.gaps[len(rec.gaps)-1].Reason != bus.GapRelist {
		t.Errorf("want a relist gap for pods before the change, got %+v", rec.gaps)
	}
	src := &cacheSource{collector: c, direct: failingSource{t}, wait: time.Second}
	if pods, err := resources.ListOf[*corev1.Pod](context.Background(), src, "pods", resources.Query{}); err != nil || len(pods) != 2 {
		t.Errorf("polled cache: %d pods, %v", len(pods), err)
	}

	// Kinds the cache doesn't hold are read from the cluster.
	var read []string
	direct := sourceFunc(func(_ context.Context, resource string, _ resources.Query) ([]runtime.Object, error) {
		read = append(read, resource)
		return nil, nil
	})
	src = &cacheSource{collector: c, direct: direct, wait: time.Minute}
	for _, resource := range []string{"secrets", "nodes", "gateways"} {
		src.List(context.Background(), resource, resources.Query{})
	}
	if len(read) != 3 {
		t.Errorf("read from the cluster: %v, want secrets, nodes and gateways", read)
	}
}

// A permission granted later is picked up by the next probe, without a
// restart: polled pods become watched.
func TestReprobePicksUpAGrantedPermission(t *testing.T) {
	cluster := newFakeCluster(nil)
	var noWatch atomic.Bool
	noWatch.Store(true)
	forbid(cluster, "watch", "pods", &noWatch)

	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	c := start(t.Context(), "lab", cluster.clients(), b, quiet, fast)
	eventually(t, "pods polled", func() bool { return statusOf(c, "pods").Mode == ModePoll })

	noWatch.Store(false)
	eventually(t, "pods watched", func() bool {
		s := statusOf(c, "pods")
		return s.Mode == ModeWatch && s.Synced
	})
	// Polling only lists; the typed watch is the new informer's.
	eventually(t, "the pods informer watching", func() bool {
		cluster.mu.Lock()
		defer cluster.mu.Unlock()
		return cluster.watched["pods"] >= 1
	})
	if _, err := cluster.kube.CoreV1().Pods("web").Create(context.Background(), pod("api-1", "1", nil), metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if added := rec.next(t, "pods"); added.Name != "api-1" {
		t.Errorf("unexpected change: %+v", added)
	}
}

// While the cluster doesn't answer, pages read it directly (and get its
// error, as before there was a cache); when it answers again, the time in
// between is published as a gap.
func TestUnreachableClusterLeavesAGap(t *testing.T) {
	cluster := newFakeCluster(nil)
	var down atomic.Bool
	cluster.kube.PrependReactor("get", "version", func(clienttesting.Action) (bool, runtime.Object, error) {
		if down.Load() {
			return true, nil, errors.New("connection refused")
		}
		return false, nil, nil
	})
	clients := cluster.clients()
	clients.Check = cluster.kube

	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	c := start(t.Context(), "lab", clients, b, quiet, fast)
	eventually(t, "live", func() bool { return c.Status().State == "live" })

	down.Store(true)
	eventually(t, "unreachable", func() bool { return c.Status().State == "unreachable" })
	var fromCluster bool
	src := &cacheSource{collector: c, wait: time.Minute, direct: sourceFunc(func(context.Context, string, resources.Query) ([]runtime.Object, error) {
		fromCluster = true
		return nil, errors.New("connection refused")
	})}
	if _, err := src.List(context.Background(), "pods", resources.Query{}); err == nil || !fromCluster {
		t.Error("an unreachable cluster should be read directly, and fail as it does")
	}

	down.Store(false)
	eventually(t, "live again", func() bool { return c.Status().State == "live" })
	eventually(t, "the gap", func() bool {
		for {
			select {
			case e := <-rec.sub.C:
				if g, ok := e.(bus.CollectorGap); ok && g.Reason == bus.GapUnreachable {
					return g.Cluster == "lab" && g.Resource == "" && g.To.After(g.From)
				}
			default:
				return false
			}
		}
	})
}

// A watch that expires makes the informer list again; the collector
// publishes a gap for that kind, since changes in between came collapsed.
func TestRelistLeavesAGap(t *testing.T) {
	cluster := newFakeCluster(nil)
	b := bus.New()
	rec := &recorder{sub: b.Subscribe(1000)}
	c := start(t.Context(), "lab", cluster.clients(), b, quiet, fast)
	cluster.waitForWatches(t)
	eventually(t, "live", func() bool { return c.Status().State == "live" })

	before := time.Now()
	cluster.expireWatch("pods")
	eventually(t, "the relist gap", func() bool {
		for {
			select {
			case e := <-rec.sub.C:
				if g, ok := e.(bus.CollectorGap); ok && g.Resource == "pods" {
					if g.Reason != bus.GapRelist || g.Cluster != "lab" || g.From.After(before) || !g.To.After(before) {
						t.Errorf("unexpected gap %+v", g)
					}
					return true
				}
			default:
				return false
			}
		}
	})
}

// A collector that stops says so with a gap that stays open.
func TestStopLeavesAnOpenGap(t *testing.T) {
	cluster := newFakeCluster(nil)
	b := bus.New()
	sub := b.Subscribe(1000)
	c := start(t.Context(), "lab", cluster.clients(), b, quiet, fast)
	eventually(t, "live", func() bool { return c.Status().State == "live" })
	c.Stop()
	<-c.Done()
	for {
		select {
		case e := <-sub.C:
			if g, ok := e.(bus.CollectorGap); ok && g.Reason == bus.GapStopped {
				if g.Cluster != "lab" || g.Resource != "" || g.From.IsZero() || !g.To.IsZero() {
					t.Errorf("unexpected gap %+v", g)
				}
				return
			}
		default:
			t.Fatal("no stopped gap")
		}
	}
}
