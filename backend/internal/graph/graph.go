// Package graph turns a cluster snapshot into a graph: every object is a
// node, and an edge joins each object to something it needs. A Deployment
// needs its pods, a pod needs its node and its Secrets, a route needs its
// Service and its Gateway.
//
// Edges always point from the dependent to its dependency ("From needs To").
// Following them forward finds what can break an object; following them
// backward finds what an object's failure affects.
//
// Objects that are referred to but don't exist are nodes too, in the
// "missing" state, and ones Opscope isn't allowed to read are "unknown".
//
// The graph is built once per snapshot (see Of). Views of it (a namespace,
// an app, one object's neighbourhood) are in views.go.
package graph

import (
	"slices"
	"sort"
	"time"

	"opscope/internal/kube"
	"opscope/internal/snapshot"
)

// State is how an object is doing.
type State string

const (
	OK          State = "ok"
	Progressing State = "progressing" // starting, rolling out or scaling, within its deadline
	Degraded    State = "degraded"    // working, but not fully: 2 of 3 ready, a node under pressure
	Failing     State = "failing"
	Missing     State = "missing" // referred to, but doesn't exist
	Unknown     State = "unknown" // referred to, but Opscope can't read that kind
)

// severity orders states from fine to worst, for "the worst of these".
var severity = map[State]int{OK: 0, Progressing: 1, Degraded: 2, Unknown: 3, Missing: 4, Failing: 5}

// Worse returns the worse of two states.
func Worse(a, b State) State {
	if severity[b] > severity[a] {
		return b
	}
	return a
}

// Node is one object.
type Node struct {
	ID        string    `json:"id"`       // "pods/shop/web-1": resource/namespace/name, as in URLs
	Resource  string    `json:"resource"` // "pods"
	Kind      string    `json:"kind"`     // "Pod"
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name"`
	App       string    `json:"app,omitempty"` // the app it belongs to, within its namespace
	State     State     `json:"state"`
	Status    string    `json:"status"`            // what its list page shows: "CrashLoopBackOff", "2/3"
	Message   string    `json:"message,omitempty"` // why, when something is wrong
	Pods      *PodGroup `json:"pods,omitempty"`    // set when this node stands for several pods (views only)
	Group     string    `json:"group,omitempty"`   // the cluster view's namespace group the card is in
	Members   []string  `json:"members,omitempty"` // cluster view: the objects whose findings the card shows

	labels map[string]string
}

// Edge types. Each says how From needs To.
const (
	Owns     = "owns"      // Deployment → ReplicaSet → Pod; CronJob → Job → Pod
	RunsOn   = "runs-on"   // Pod → Node
	Uses     = "uses"      // Pod → ConfigMap, Secret, PVC, ServiceAccount; PVC → PV → StorageClass; ...
	Selects  = "selects"   // Service → Pod; PodDisruptionBudget → Pod
	RoutesTo = "routes-to" // Ingress → Service; HTTPRoute → Service
	Attaches = "attaches"  // HTTPRoute → Gateway → GatewayClass
	Permits  = "permits"   // HTTPRoute → the ReferenceGrant that lets it reach a Service in another namespace
	Scales   = "scales"    // HorizontalPodAutoscaler → Deployment or StatefulSet

	// Blames is only in views: the diagnosis says From fails because of To,
	// and no other edge joins them (a ResourceQuota in a ReplicaSet's way).
	Blames = "blames"
)

// Edge says that From needs To.
type Edge struct {
	From     string   `json:"from"`
	To       string   `json:"to"`
	Type     string   `json:"type"`
	Via      []string `json:"via,omitempty"`      // where the references are: "container api: env DB_URL", "rule 1"
	Optional bool     `json:"optional,omitempty"` // every reference is optional, so a missing To breaks nothing
}

type edgeKey struct{ from, to, typ string }

// Graph is a whole cluster. It is read-only once built, so many requests
// can use it at once.
type Graph struct {
	Taken   time.Time
	Skipped []kube.Skipped

	nodes map[string]*Node
	edges map[edgeKey]*Edge
	out   map[string][]*Edge // by From
	in    map[string][]*Edge // by To

	cantCreatePods map[string]bool // ReplicaSets whose problem is their own: they can't create pods
}

// Of returns the graph of a snapshot, building it the first time.
func Of(s *snapshot.Snapshot) *Graph {
	return s.Derived("graph", func() any { return Build(s) }).(*Graph)
}

// Build makes the graph of a snapshot: the nodes, then the edges between
// them (adding missing and unknown nodes as references are found), then
// each node's state, which for some kinds depends on the edges, and last the
// apps, which depend on both.
func Build(s *snapshot.Snapshot) *Graph {
	g := &Graph{
		Taken:   s.Taken,
		Skipped: s.Skipped,
		nodes:   map[string]*Node{},
		edges:   map[edgeKey]*Edge{},
		out:     map[string][]*Edge{},
		in:      map[string][]*Edge{},

		cantCreatePods: map[string]bool{},
	}
	b := &builder{s: s, g: g, now: s.Taken}
	b.addNodes()
	b.addEdges()
	b.addStates()
	b.addApps()

	for _, edges := range g.out {
		SortEdges(edges)
	}
	for _, edges := range g.in {
		SortEdges(edges)
	}
	return g
}

// Node returns one node by ID.
func (g *Graph) Node(id string) (*Node, bool) {
	n, ok := g.nodes[id]
	return n, ok
}

// Out lists the edges from a node: what it needs.
func (g *Graph) Out(id string) []*Edge { return g.out[id] }

// In lists the edges to a node: what needs it.
func (g *Graph) In(id string) []*Edge { return g.in[id] }

// Nodes lists every node, sorted by ID.
func (g *Graph) Nodes() []*Node {
	nodes := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// addEdge adds an edge, or merges into the same one: a pod that reads three
// keys of one Secret has one edge to it, with three places in Via.
func (g *Graph) addEdge(from, to, typ, via string, optional bool) {
	if from == to {
		return
	}
	key := edgeKey{from, to, typ}
	e, ok := g.edges[key]
	if !ok {
		e = &Edge{From: from, To: to, Type: typ, Optional: optional}
		g.edges[key] = e
		g.out[from] = append(g.out[from], e)
		g.in[to] = append(g.in[to], e)
	}
	e.Optional = e.Optional && optional // one required reference makes the edge required
	if via != "" && !slices.Contains(e.Via, via) {
		e.Via = append(e.Via, via)
	}
}

// SortEdges sorts edges by From, then To, then type.
func SortEdges(edges []*Edge) {
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Type < b.Type
	})
}
