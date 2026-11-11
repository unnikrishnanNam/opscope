package graph

import (
	"errors"
	"slices"
	"testing"
	"time"

	"opscope/internal/resources"
)

// names lists a subgraph's nodes the way tests name them.
func names(s Subgraph) []string {
	var out []string
	for _, n := range s.Nodes {
		key := objectKey(n.Resource, n.Namespace, n.Name)
		if n.Presence != Present {
			key = n.ID
		}
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func idOf(t *testing.T, g *Graph, resource, namespace, name string) string {
	t.Helper()
	n, err := g.Find(resource, namespace, name, Query{})
	if err != nil {
		t.Fatalf("%s %s/%s: %v", resource, namespace, name, err)
	}
	return n.ID
}

func TestFind(t *testing.T) {
	g := Build(nil, shop())
	if id := idOf(t, g, "pods", "shop", "api-7d9f-x1"); id != string(uidOf("pods", "api-7d9f-x1")) {
		t.Errorf("pod's ID %s", id)
	}
	if id := idOf(t, g, "secrets", "shop", "stripe-key"); id != "missing:secrets/shop/stripe-key" {
		t.Errorf("a missing Secret is found as %s", id)
	}
	if id := idOf(t, g, "nodes", "ignored", "node-1"); id != string(uidOf("nodes", "node-1")) {
		t.Errorf("a cluster-wide kind's namespace isn't ignored: %s", id)
	}
	if _, err := g.Find("pods", "shop", "nope", Query{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v", err)
	}
	n, _ := g.Find("deployments", "shop", "api", Query{})
	if n.Status != "0/1" {
		t.Errorf("a Deployment's status is %q, as its list shows it", n.Status)
	}
}

func TestWalks(t *testing.T) {
	g := Build(nil, shop())
	deployment := idOf(t, g, "deployments", "shop", "api")
	pod := idOf(t, g, "pods", "shop", "api-7d9f-x1")
	node := idOf(t, g, "nodes", "", "node-1")

	cases := []struct {
		name string
		walk func() (Subgraph, error)
		want []string
		not  []string
	}{
		{"what a Deployment needs, two steps", func() (Subgraph, error) { return g.Upstream(deployment, Query{}) },
			[]string{"deployments/shop/api", "replicasets/shop/api-7d9f", "pods/shop/api-7d9f-x1", "secrets/shop/db-creds", "missing:secrets/shop/stripe-key"},
			[]string{"nodes/node-1"}}, // three steps away
		{"only owns", func() (Subgraph, error) { return g.Upstream(deployment, Query{Depth: 3, Kinds: []string{Owns}}) },
			[]string{"replicasets/shop/api-7d9f", "pods/shop/api-7d9f-x2"},
			[]string{"secrets/shop/db-creds"}},
		{"what depends on a Secret", func() (Subgraph, error) {
			return g.Downstream(idOf(t, g, "secrets", "shop", "db-creds"), Query{Depth: 3})
		}, []string{"pods/shop/api-7d9f-x1", "deployments/shop/api", "services/shop/api", "ingresses/shop/web", "httproutes/shop/store"},
			[]string{"nodes/node-1"}},
		{"a pod's neighbours", func() (Subgraph, error) { return g.Neighbors(pod, Query{Depth: 5}) },
			[]string{"replicasets/shop/api-7d9f", "nodes/node-1", "services/shop/api", "endpointslices/shop/api-abcde", "secrets/shop/db-creds"},
			[]string{"deployments/shop/api"}},
		{"a walk doesn't go through a node", func() (Subgraph, error) { return g.Subgraph(pod, Both, Query{Depth: 2}) },
			[]string{"nodes/node-1"},
			[]string{"pods/shop/db-0", "pods/kube-system/etcd-node-1"}},
		{"unless it starts there", func() (Subgraph, error) { return g.Subgraph(node, Both, Query{Depth: 1}) },
			[]string{"pods/shop/db-0", "pods/kube-system/etcd-node-1", "pods/shop/api-7d9f-x1"}, nil},
	}
	for _, c := range cases {
		s, err := c.walk()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := names(s)
		for _, w := range c.want {
			if !slices.Contains(got, w) {
				t.Errorf("%s: no %s in %v", c.name, w, got)
			}
		}
		for _, w := range c.not {
			if slices.Contains(got, w) {
				t.Errorf("%s: %s shouldn't be there", c.name, w)
			}
		}
		// Every edge is between two of its nodes.
		ids := map[string]bool{}
		for _, n := range s.Nodes {
			ids[n.ID] = true
		}
		for _, e := range s.Edges {
			if !ids[e.From] || !ids[e.To] {
				t.Errorf("%s: edge %s → %s leaves the subgraph", c.name, e.From, e.To)
			}
		}
	}
}

func TestPath(t *testing.T) {
	g := Build(nil, shop())
	ingress := idOf(t, g, "ingresses", "shop", "web")
	pod := idOf(t, g, "pods", "shop", "api-7d9f-x1")
	s, err := g.Path(ingress, pod, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(s); len(s.Edges) != 2 || !slices.Contains(got, "services/shop/api") {
		t.Errorf("path %v with %d edges", got, len(s.Edges))
	}
	if s.Nodes[0].ID != ingress || s.Nodes[2].ID != pod || s.Edges[0].Kind != Routes || s.Edges[1].Kind != Selects {
		t.Errorf("path in the wrong order: %+v", s)
	}
	// db-0 shares only the node with the api pod.
	if _, err := g.Path(pod, idOf(t, g, "pods", "shop", "db-0"), Query{}); !errors.Is(err, ErrNoPath) {
		t.Errorf("a path through a node: %v", err)
	}
}

func TestNamespaceView(t *testing.T) {
	g := Build(nil, shop())
	s, err := g.Namespace("shop", Query{})
	if err != nil {
		t.Fatal(err)
	}
	got := names(s)
	for _, w := range []string{"pods/shop/api-7d9f-x1", "nodes/node-1", "persistentvolumes/pv-1", "gatewayclasses/nginx", "missing:storageclasses/slow"} {
		if !slices.Contains(got, w) {
			t.Errorf("no %s", w)
		}
	}
	if slices.Contains(got, "pods/kube-system/etcd-node-1") {
		t.Error("another namespace's pod came in through the node")
	}
}

func TestQueriesTakeATime(t *testing.T) {
	g := Build(nil, shop())
	pod := idOf(t, g, "pods", "shop", "api-7d9f-x1")
	past := Query{At: time.Now().Add(-time.Hour)}
	for name, err := range map[string]error{
		"node":      func() error { _, err := g.Node(pod, past); return err }(),
		"find":      func() error { _, err := g.Find("pods", "shop", "api-7d9f-x1", past); return err }(),
		"subgraph":  func() error { _, err := g.Subgraph(pod, Both, past); return err }(),
		"path":      func() error { _, err := g.Path(pod, pod, past); return err }(),
		"namespace": func() error { _, err := g.Namespace("shop", past); return err }(),
		"search":    func() error { _, err := g.Search(Search{Verb: "on", Name: "node-1"}, "", past); return err }(),
	} {
		if !errors.Is(err, resources.ErrNoHistory) {
			t.Errorf("%s at an earlier time: %v", name, err)
		}
	}
}

func TestSearch(t *testing.T) {
	g := Build(nil, shop())
	cases := []struct {
		phrase, namespace string
		want              []string
	}{
		{"pods using db-creds", "", []string{"pods/shop/api-7d9f-x1", "pods/shop/api-7d9f-x2"}},
		{"using db-creds", "", []string{"deployments/shop/api", "pods/shop/api-7d9f-x1", "pods/shop/api-7d9f-x2"}},
		{"what uses stripe-key", "", []string{"deployments/shop/api", "pods/shop/api-7d9f-x1", "pods/shop/api-7d9f-x2"}},
		{"what runs on node-1", "", []string{"pods/kube-system/etcd-node-1", "pods/shop/api-7d9f-x1", "pods/shop/api-7d9f-x2", "pods/shop/db-0"}},
		{"what runs on node-1", "shop", []string{"pods/shop/api-7d9f-x1", "pods/shop/api-7d9f-x2", "pods/shop/db-0"}},
		{"deploy using shop/tls-cert", "", nil},
		{"ingress using shop/tls-cert", "", []string{"ingresses/shop/web"}},
	}
	for _, c := range cases {
		s, err := ParseSearch(c.phrase)
		if err != nil {
			t.Fatalf("%q: %v", c.phrase, err)
		}
		sub, err := g.Search(s, c.namespace, Query{})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, n := range sub.Nodes {
			if slices.Contains(sub.Matches, n.ID) {
				got = append(got, objectKey(n.Resource, n.Namespace, n.Name))
			}
		}
		slices.Sort(got)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q in %q: %v, want %v", c.phrase, c.namespace, got, c.want)
		}
	}
	for _, bad := range []string{"db-creds", "pods using", "frobs using x", "pods deployments using x", "using a b"} {
		if _, err := ParseSearch(bad); !errors.Is(err, ErrSearch) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
