package signals

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/graph"
	"opscope/internal/resources"
)

// Every place Opscope shows health says the same of the same object: the
// graph's node (Topology, a detail page's links), the object's own health
// (GET /health, a detail page's badge), the Issues feed and the overview's
// counts. They all read one engine; this checks nothing in between bends
// it.
func TestEveryViewAgreesOnHealth(t *testing.T) {
	unavailable := appsv1.DeploymentCondition{Type: appsv1.DeploymentAvailable, Status: corev1.ConditionFalse,
		LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour))}
	f := newFakeFeed()
	f.cluster("lab", map[string][]runtime.Object{
		"pods":        {crashing("api-1", 3), running("web-1")},
		"deployments": {deployment("short", 3, 1, unavailable), deployment("down", 1, 0, unavailable), deployment("full", 2, 2)},
		"configmaps":  {&corev1.ConfigMap{ObjectMeta: objMeta("shop", "cfg")}},
	}, nil)
	signals := startEngine(t, f)
	graphs := graph.NewEngine(f, quiet)
	graphs.SetHealth(func(cluster, resource, uid string) *graph.Health {
		if h := signals.HealthOf(cluster, resource, uid); h != nil {
			return &graph.Health{State: h.State, Since: h.Since, Signals: h.Signals}
		}
		return nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go graphs.Run(ctx)

	want := map[string]string{ // resource/name → health, "" for none
		"pods/api-1": Failing, "pods/web-1": Healthy,
		"deployments/short": Degraded, "deployments/down": Failing, "deployments/full": Healthy,
		"configmaps/cfg": "",
	}

	eventually(t, "the signals", func() bool {
		var groups []Group
		_ = signals.Read(ctx, "lab", nil, resources.Query{}, func(v View, _ Info) error {
			groups = GroupSignals(v.Signals(Filter{State: Open}))
			return nil
		})
		return len(groups) == 3
	})

	for key, health := range want {
		resource, name, _ := strings.Cut(key, "/")
		uid := "uid-" + name

		// The object's own health (GET /health).
		var own string
		var listed bool
		_ = signals.Read(ctx, "lab", nil, resources.Query{}, func(v View, _ Info) error {
			if h, ok := v.Health(uid); ok {
				own = h.State
			}
			listed = slices.ContainsFunc(GroupSignals(v.Signals(Filter{State: Open})), func(g Group) bool {
				return slices.ContainsFunc(g.Signals, func(s Signal) bool { return s.Resource.UID == uid })
			})
			return nil
		})
		if own != health {
			t.Errorf("%s: own health %q, want %q", key, own, health)
		}

		// The graph's node (Topology).
		var node graph.Node
		if err := graphs.Read(ctx, "lab", nil, "", func(g *graph.Graph, _ graph.Info) error {
			n, err := g.Find(resource, "shop", name, graph.Query{})
			node = n
			return err
		}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		graphHealth := ""
		if node.Health != nil {
			graphHealth = node.Health.State
		}
		if graphHealth != health {
			t.Errorf("%s: the graph says %q, want %q", key, graphHealth, health)
		}

		// The Issues feed lists it exactly when it isn't healthy.
		if bad := health == Failing || health == Degraded; listed != bad {
			t.Errorf("%s: in the Issues feed %v, health %q", key, listed, health)
		}
	}

	// The overview's counts: objects of a kind with an open signal.
	_ = signals.Read(ctx, "lab", nil, resources.Query{}, func(v View, _ Info) error {
		if p, d := v.Objects("shop", "pods"), v.Objects("shop", "deployments"); p != 1 || d != 2 {
			t.Errorf("overview counts: %d pods, %d deployments; want 1 and 2", p, d)
		}
		return nil
	})
}
