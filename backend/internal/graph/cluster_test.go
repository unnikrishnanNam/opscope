package graph

import (
	"slices"
	"testing"

	"opscope/internal/snapshot"
)

// shopAndCalm is the shop, and a namespace with nothing wrong in it.
func shopAndCalm() *snapshot.Snapshot {
	s := shop()
	s.Pods = append(s.Pods, readyPod(meta("calm", "solo", "app", "solo"), "worker-1"))
	return prepared(s)
}

func TestClusterView(t *testing.T) {
	g := Of(shopAndCalm())
	v := g.Cluster(Options{})

	for _, id := range []string{
		"apps/shop/storefront", "apps/shop/api", "apps/shop/legacy", // shop has problems: its apps are shown
		"apps/other/x",    // so does other: its Service selects no pods
		"namespaces/calm", // nothing wrong in it: one card
		"gateways/infra/edge", "gatewayclasses/nginx",
		"nodes/worker-1", "nodes/worker-2",
	} {
		if !hasID(v.Nodes, id) {
			t.Errorf("missing %s; got %v", id, ids(v.Nodes))
		}
	}
	if len(v.Nodes) != 9 {
		t.Errorf("got %v", ids(v.Nodes))
	}
	if len(v.Groups) != 2 || v.Groups[1].ID != "namespaces/shop" || v.Groups[1].State != Failing || v.Groups[1].Apps != 3 {
		t.Errorf("groups = %+v", v.Groups)
	}
	if n := findNode(v.Nodes, "apps/shop/api"); n.Group != "namespaces/shop" || n.State != Failing || n.Status != "1 workload, 1 degraded" {
		t.Errorf("api = %+v", n)
	}
	if n := findNode(v.Nodes, "namespaces/calm"); n.State != OK || n.Status != "1 app" || n.Group != "" {
		t.Errorf("calm = %+v", n)
	}
	if !slices.Contains(v.Members("apps/shop/api"), "secrets/shop/stripe") {
		t.Errorf("the api app holds its missing Secret: %v", v.Members("apps/shop/api"))
	}

	// The route joins its app to the Gateway, and to the other app it
	// sends traffic to; the Gateway to its class.
	want := map[string]string{
		"apps/shop/storefront>gateways/infra/edge": "attaches",
		"apps/shop/storefront>apps/shop/api":       "routes-to",
		"gateways/infra/edge>gatewayclasses/nginx": "attaches",
	}
	for _, e := range v.Edges {
		if want[e.From+">"+e.To] == e.Type {
			delete(want, e.From+">"+e.To)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing edges %v; got %v", want, v.Edges)
	}
	for _, e := range v.Edges {
		if e.From == "apps/shop/storefront" && e.To == "gateways/infra/edge" && !slices.Equal(e.Via, []string{"HTTPRoute storefront"}) {
			t.Errorf("via = %v", e.Via)
		}
	}
}

func TestClusterViewExpandAndProblems(t *testing.T) {
	g := Of(shopAndCalm())

	v := g.Cluster(Options{Expand: map[string]bool{"namespaces/calm": true}})
	if !hasID(v.Nodes, "apps/calm/solo") || hasID(v.Nodes, "namespaces/calm") || len(v.Groups) != 3 {
		t.Errorf("expanded: %v, groups %v", ids(v.Nodes), v.Groups)
	}

	v = g.Cluster(Options{Problems: true})
	for _, id := range []string{"apps/shop/api", "apps/shop/legacy", "nodes/worker-2", "apps/shop/storefront"} {
		if !hasID(v.Nodes, id) {
			t.Errorf("problems only: missing %s; got %v", id, ids(v.Nodes))
		}
	}
	for _, id := range []string{"namespaces/calm", "nodes/worker-1", "gatewayclasses/nginx"} {
		if hasID(v.Nodes, id) {
			t.Errorf("problems only: %s is fine and joined to no problem", id)
		}
	}
}

// A pod that runs again between restarts is "ok" on its own; the problem
// the diagnosis found in it still counts toward its app and namespace.
func TestClusterViewCountsTroubledObjects(t *testing.T) {
	v := Of(shopAndCalm()).Cluster(Options{Troubled: map[string]State{"pods/calm/solo": Degraded}})
	if hasID(v.Nodes, "namespaces/calm") {
		t.Fatal("a namespace with a problem isn't folded away")
	}
	if n := findNode(v.Nodes, "apps/calm/solo"); n == nil || n.State != Degraded {
		t.Errorf("solo = %+v", n)
	}
}
