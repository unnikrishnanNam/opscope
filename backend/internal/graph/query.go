package graph

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"opscope/internal/resources"
)

// Query says how far a walk goes and what it follows. Every query takes a
// time (contract 2): zero means now, and anything else is
// resources.ErrNoHistory until the Time machine (v1.8).
type Query struct {
	At    time.Time
	Depth int      // steps out; 0 means DefaultDepth, and more than MaxDepth is MaxDepth
	Kinds []string // edge kinds to follow; empty means every kind
}

const (
	DefaultDepth = 2
	MaxDepth     = 10
)

// Directions for Subgraph: up follows edges forward, to what a node
// depends on; down follows them backward, to what depends on it.
const (
	Up   = "up"
	Down = "down"
	Both = "both"
)

// Subgraph is what a query returns: some nodes, every edge between them
// that the query follows, and the node it was about.
type Subgraph struct {
	Focus   string   `json:"focus,omitempty"`
	Nodes   []Node   `json:"nodes"`
	Edges   []Edge   `json:"edges"`
	Matches []string `json:"matches,omitempty"` // search only: the nodes that answer it
}

var (
	// ErrNotFound means the graph has no such node.
	ErrNotFound = errors.New("no such object in the graph")
	// ErrNoPath means no edges join two nodes, short of going through a
	// node many objects share.
	ErrNoPath = errors.New("nothing connects these two objects")
)

// hubs are kinds many objects share. A walk reaches them but doesn't go on
// through them, unless it starts there: two steps out through a node would
// reach every pod on that node, which says nothing about the first one.
var hubs = map[string]bool{"nodes": true, "storageclasses": true, "gatewayclasses": true}

func (q Query) check() error {
	if !q.At.IsZero() {
		return resources.ErrNoHistory
	}
	return nil
}

func (q Query) depth() int {
	switch {
	case q.Depth <= 0:
		return DefaultDepth
	case q.Depth > MaxDepth:
		return MaxDepth
	}
	return q.Depth
}

func (q Query) follows(kind string) bool {
	return len(q.Kinds) == 0 || slices.Contains(q.Kinds, kind)
}

// Node returns one node by its ID.
func (g *Graph) Node(id string, q Query) (Node, error) {
	if err := q.check(); err != nil {
		return Node{}, err
	}
	if _, ok := g.nodes[id]; !ok {
		return Node{}, ErrNotFound
	}
	return g.node(id), nil
}

// Find returns the node for an object named the way pages name it. An
// object that's referred to but doesn't exist, or can't be read, is found
// as its placeholder.
func (g *Graph) Find(resource, namespace, name string, q Query) (Node, error) {
	if err := q.check(); err != nil {
		return Node{}, err
	}
	if k, ok := resources.Lookup(resource); ok && !k.Namespaced {
		namespace = ""
	}
	key := objectKey(resource, namespace, name)
	if id, ok := g.byKey[key]; ok {
		return g.node(id), nil
	}
	for _, presence := range []string{Missing, Unknown} {
		if _, ok := g.nodes[presence+":"+key]; ok {
			return g.node(presence + ":" + key), nil
		}
	}
	return Node{}, ErrNotFound
}

// Neighbors is a node and everything one edge away, either way.
func (g *Graph) Neighbors(id string, q Query) (Subgraph, error) {
	q.Depth = 1
	return g.Subgraph(id, Both, q)
}

// Upstream is what a node depends on, up to q.Depth edges away.
func (g *Graph) Upstream(id string, q Query) (Subgraph, error) {
	return g.Subgraph(id, Up, q)
}

// Downstream is what depends on a node, up to q.Depth edges away.
func (g *Graph) Downstream(id string, q Query) (Subgraph, error) {
	return g.Subgraph(id, Down, q)
}

// Subgraph walks out from a node in a direction, up to q.Depth edges,
// stopping at hubs, and returns what it reached with the edges between.
func (g *Graph) Subgraph(id, direction string, q Query) (Subgraph, error) {
	if err := q.check(); err != nil {
		return Subgraph{}, err
	}
	if _, ok := g.nodes[id]; !ok {
		return Subgraph{}, ErrNotFound
	}
	if direction != Up && direction != Down && direction != Both {
		return Subgraph{}, fmt.Errorf("direction %q: up, down or both", direction)
	}
	reached := map[string]bool{id: true}
	frontier := []string{id}
	for step := range q.depth() {
		var next []string
		for _, n := range frontier {
			if step > 0 && g.isHub(n) {
				continue
			}
			for _, other := range g.adjacent(n, direction, q) {
				if !reached[other] {
					reached[other] = true
					next = append(next, other)
				}
			}
		}
		frontier = next
	}
	s := g.induced(reached, q)
	s.Focus = id
	return s, nil
}

// Path is the shortest chain of edges between two nodes, in either
// direction, not going through a hub.
func (g *Graph) Path(from, to string, q Query) (Subgraph, error) {
	if err := q.check(); err != nil {
		return Subgraph{}, err
	}
	for _, id := range []string{from, to} {
		if _, ok := g.nodes[id]; !ok {
			return Subgraph{}, ErrNotFound
		}
	}
	// Breadth first, keeping the edge each node was reached by.
	via := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 && via[to] == "" && from != to {
		n := queue[0]
		queue = queue[1:]
		if n != from && g.isHub(n) {
			continue
		}
		for _, edgeID := range g.edgesAt(n, Both, q) {
			es := g.edges[edgeID]
			other := es.to
			if other == n {
				other = es.from
			}
			if _, seen := via[other]; !seen {
				via[other] = edgeID
				queue = append(queue, other)
			}
		}
	}
	if _, ok := via[to]; !ok {
		return Subgraph{}, ErrNoPath
	}
	s := Subgraph{Focus: from, Nodes: []Node{}, Edges: []Edge{}}
	for n := to; ; {
		s.Nodes = append(s.Nodes, g.node(n))
		edgeID := via[n]
		if edgeID == "" {
			break
		}
		s.Edges = append(s.Edges, g.edge(edgeID))
		es := g.edges[edgeID]
		if es.to == n {
			n = es.from
		} else {
			n = es.to
		}
	}
	slices.Reverse(s.Nodes)
	slices.Reverse(s.Edges)
	return s, nil
}

// Namespace is everything in a namespace, and what it's joined to outside
// it one edge away (its nodes, volumes, classes, objects in other
// namespaces), with the edges between them.
func (g *Graph) Namespace(namespace string, q Query) (Subgraph, error) {
	if err := q.check(); err != nil {
		return Subgraph{}, err
	}
	set := map[string]bool{}
	for id, n := range g.nodes {
		if n.Namespace == namespace {
			set[id] = true
		}
	}
	for id := range maps.Clone(set) {
		for _, other := range g.adjacent(id, Both, q) {
			set[other] = true
		}
	}
	return g.induced(set, q), nil
}

func (g *Graph) isHub(id string) bool {
	n, ok := g.nodes[id]
	return ok && hubs[n.Resource]
}

// edgesAt lists the IDs of the edges at a node in a direction, sorted.
func (g *Graph) edgesAt(id, direction string, q Query) []string {
	var out []string
	add := func(index map[string]map[string]bool) {
		for edgeID := range index[id] {
			if q.follows(g.edges[edgeID].kind) {
				out = append(out, edgeID)
			}
		}
	}
	if direction == Up || direction == Both {
		add(g.out)
	}
	if direction == Down || direction == Both {
		add(g.in)
	}
	slices.Sort(out)
	return out
}

// adjacent lists the nodes one edge away from a node in a direction.
func (g *Graph) adjacent(id, direction string, q Query) []string {
	var out []string
	for _, edgeID := range g.edgesAt(id, direction, q) {
		es := g.edges[edgeID]
		if es.from == id {
			out = append(out, es.to)
		} else {
			out = append(out, es.from)
		}
	}
	return out
}

// induced is a set of nodes with every edge between them that q follows.
func (g *Graph) induced(set map[string]bool, q Query) Subgraph {
	s := Subgraph{Nodes: make([]Node, 0, len(set)), Edges: []Edge{}}
	for _, id := range slices.Sorted(maps.Keys(set)) {
		s.Nodes = append(s.Nodes, g.node(id))
	}
	var edgeIDs []string
	for id := range set {
		for edgeID := range g.out[id] {
			if es := g.edges[edgeID]; set[es.to] && q.follows(es.kind) {
				edgeIDs = append(edgeIDs, edgeID)
			}
		}
	}
	slices.Sort(edgeIDs)
	for _, edgeID := range edgeIDs {
		s.Edges = append(s.Edges, g.edge(edgeID))
	}
	return s
}

// ---- Search ----

// Search is a graph search from the command palette: "pods using
// db-creds" or "what runs on node-3".
type Search struct {
	Resource string // the kind to answer with ("pods"); "" for every kind
	Verb     string // "using" or "on"
	Name     string // the object it's about, by name
}

// fillers are words before "using" or "on" that don't name a kind.
var fillers = map[string]bool{"what": true, "which": true, "everything": true, "anything": true, "all": true,
	"show": true, "find": true, "runs": true, "run": true, "is": true, "are": true, "that": true}

// ErrSearch explains what a search can say, for one that doesn't parse.
var ErrSearch = errors.New(`search says "using <name>" or "on <node>", with an optional kind first: "pods using db-creds", "what runs on node-3"`)

// ParseSearch reads a search phrase.
func ParseSearch(phrase string) (Search, error) {
	words := strings.Fields(phrase)
	at := slices.IndexFunc(words, func(w string) bool {
		w = strings.ToLower(w)
		return w == "using" || w == "on" || w == "uses"
	})
	if at < 0 || at != len(words)-2 {
		return Search{}, ErrSearch
	}
	s := Search{Verb: strings.ToLower(words[at]), Name: words[at+1]}
	if s.Verb == "uses" {
		s.Verb = "using"
	}
	for _, w := range words[:at] {
		w = strings.ToLower(w)
		if fillers[w] {
			continue
		}
		resource, ok := kindWord(w)
		if !ok || s.Resource != "" {
			return Search{}, fmt.Errorf("%q isn't a kind: %w", w, ErrSearch)
		}
		s.Resource = resource
	}
	return s, nil
}

// kindWord finds a kind the graph holds from how people say it: "pods",
// "pod", "Pod", "deploy".
func kindWord(w string) (string, bool) {
	short := map[string]string{"po": "pods", "deploy": "deployments", "sts": "statefulsets", "ds": "daemonsets",
		"rs": "replicasets", "svc": "services", "ing": "ingresses", "cm": "configmaps", "pvc": "persistentvolumeclaims",
		"pv": "persistentvolumes", "sa": "serviceaccounts", "cj": "cronjobs", "no": "nodes", "sc": "storageclasses"}
	if r, ok := short[w]; ok {
		return r, true
	}
	for _, r := range NodeKinds {
		k, _ := resources.Lookup(r)
		if w == r || w+"s" == r || w+"es" == r || w == strings.ToLower(k.KindName) {
			return r, true
		}
	}
	return "", false
}

// Search answers a graph search. "using <name>" finds what has an edge to
// any object of that name; "on <node>" finds what runs on that node.
// namespace, when set, keeps answers to that namespace and cluster-wide
// objects. The answer holds the matches, what they matched, and the edges
// between.
func (g *Graph) Search(s Search, namespace string, q Query) (Subgraph, error) {
	if err := q.check(); err != nil {
		return Subgraph{}, err
	}
	targetNS, name := "", s.Name
	if ns, n, ok := strings.Cut(s.Name, "/"); ok {
		targetNS, name = ns, n
	}
	set, matches := map[string]bool{}, map[string]bool{}
	var edgeIDs []string
	for id, t := range g.nodes {
		if t.Name != name || (targetNS != "" && t.Namespace != targetNS) || (s.Verb == "on" && t.Resource != "nodes") {
			continue
		}
		for edgeID := range g.in[id] {
			es := g.edges[edgeID]
			if s.Verb == "on" && es.kind != RunsOn || !q.follows(es.kind) {
				continue
			}
			src := g.nodes[es.from]
			if s.Resource != "" && src.Resource != s.Resource {
				continue
			}
			if namespace != "" && src.Namespace != "" && src.Namespace != namespace {
				continue
			}
			set[id], set[es.from], matches[es.from] = true, true, true
			edgeIDs = append(edgeIDs, edgeID)
		}
	}
	out := Subgraph{Nodes: []Node{}, Edges: []Edge{}, Matches: slices.Sorted(maps.Keys(matches))}
	for _, id := range slices.Sorted(maps.Keys(set)) {
		out.Nodes = append(out.Nodes, g.node(id))
	}
	slices.Sort(edgeIDs)
	for _, edgeID := range edgeIDs {
		out.Edges = append(out.Edges, g.edge(edgeID))
	}
	return out, nil
}
