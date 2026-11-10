package graph

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/resources"
)

// NodeKinds are the kinds the graph holds. Objects of any other kind
// (events, namespaces) are ignored.
var NodeKinds = []string{
	"configmaps", "cronjobs", "daemonsets", "deployments", "endpointslices", "gatewayclasses",
	"gateways", "httproutes", "ingresses", "jobs", "nodes", "persistentvolumeclaims",
	"persistentvolumes", "pods", "replicasets", "secrets", "serviceaccounts", "services",
	"statefulsets", "storageclasses",
}

// Object is one object handed to the graph, with when Opscope saw it.
type Object struct {
	Resource string // URL name, e.g. "pods"
	Object   runtime.Object
	Seen     time.Time
}

// Graph is one cluster's graph. It's built by adding objects one at a time
// (Upsert) and removing them (Delete); building from all objects at once
// gives the same graph as adding them in any order. Each change touches
// only the object that changed and the objects that refer to it.
//
// A Graph isn't safe for concurrent use; the engine serialises changes.
type Graph struct {
	holds    map[string]bool // NodeKinds
	readable map[string]bool // kinds whose objects the graph is given

	objects map[string]*entry          // present objects, by node ID
	byKey   map[string]string          // objectKey → node ID
	groups  map[string]map[string]bool // "resource/namespace" → node IDs, for matches
	nodes   map[string]*Node           // present and placeholder nodes, by ID
	edges   map[string]*edgeState
	degree  map[string]int // edges touching each placeholder node

	wanting  map[string]map[string]bool // target key → readers that name it
	watching map[string]map[string]bool // "resource/namespace" → readers whose matches look there
}

// entry is a present object and what it contributes to the graph.
type entry struct {
	id, resource, key, group string
	obj                      runtime.Object
	labels                   map[string]string
	rv                       string
	seen                     time.Time

	refs    []Ref
	matched map[int]map[string]bool // for each Match in refs (by index), the node IDs it picks
	wants   []string                // keys this object is registered under in wanting
	watches []string                // groups this object is registered under in watching
	gave    map[string]bool         // edges it contributed evidence to
}

type edgeState struct {
	from, to, kind string
	evidence       map[string][]Evidence // by the node ID of the object it was read from
}

// New makes an empty graph. readable names the kinds whose objects it will
// be given; a reference to an object of any other kind is Unknown rather
// than Missing. nil means every kind in NodeKinds.
func New(readable []string) *Graph {
	if readable == nil {
		readable = NodeKinds
	}
	g := &Graph{
		holds:    map[string]bool{},
		readable: map[string]bool{},
		objects:  map[string]*entry{},
		byKey:    map[string]string{},
		groups:   map[string]map[string]bool{},
		nodes:    map[string]*Node{},
		edges:    map[string]*edgeState{},
		degree:   map[string]int{},
		wanting:  map[string]map[string]bool{},
		watching: map[string]map[string]bool{},
	}
	for _, k := range NodeKinds {
		g.holds[k] = true
	}
	for _, k := range readable {
		if g.holds[k] {
			g.readable[k] = true
		}
	}
	return g
}

// Build makes a graph from a set of objects.
func Build(readable []string, objects []Object) *Graph {
	g := New(readable)
	for _, o := range objects {
		g.Upsert(o)
	}
	return g
}

// Upsert adds an object or updates it. Objects of kinds the graph doesn't
// hold, or isn't told it can read, are ignored.
func (g *Graph) Upsert(o Object) {
	if !g.readable[o.Resource] {
		return
	}
	m, err := meta.Accessor(o.Object)
	if err != nil {
		return
	}
	key := objectKey(o.Resource, m.GetNamespace(), m.GetName())
	id := string(m.GetUID())
	if id == "" {
		id = "object:" + key // only objects made up in tests have no UID
	}
	// Another object under the same name is one that was deleted and made
	// again, its delete missed. It's gone now.
	if other, ok := g.byKey[key]; ok && other != id {
		g.Delete(other)
	}

	e, existed := g.objects[id]
	matchable := !existed || !maps.Equal(e.labels, m.GetLabels())
	if !existed {
		e = &entry{id: id, resource: o.Resource, key: key, group: groupKey(o.Resource, m.GetNamespace())}
		g.objects[id] = e
		g.byKey[key] = id
		addTo(g.groups, e.group, id)
		kindName := ""
		if k, ok := resources.Lookup(o.Resource); ok {
			kindName = k.KindName
		}
		g.nodes[id] = &Node{
			ID: id, Resource: o.Resource, Kind: kindName,
			Namespace: m.GetNamespace(), Name: m.GetName(), Presence: Present,
		}
	}
	e.obj, e.labels, e.rv, e.seen = o.Object, m.GetLabels(), m.GetResourceVersion(), o.Seen
	g.nodes[id].ResourceVersion = e.rv
	e.refs = refsOf(o.Resource, o.Object)
	g.resolve(e)

	if !existed {
		g.resolveAll(g.wanting[key])
	}
	if matchable {
		g.rematch(e, true)
	}
}

// Delete removes an object by its node ID (its UID). What referred to it
// now refers to a missing node.
func (g *Graph) Delete(id string) {
	e, ok := g.objects[id]
	if !ok {
		return
	}
	g.withdraw(e)
	g.rematch(e, false)
	delete(g.objects, id)
	if g.byKey[e.key] == id {
		delete(g.byKey, e.key)
	}
	removeFrom(g.groups, e.group, id)
	delete(g.nodes, id)
	g.resolveAll(g.wanting[e.key])
}

// SetReadable says whether the graph can read a kind. A kind that stops
// being readable loses its objects, and references to them become
// Unknown; one that becomes readable turns Unknown references into
// Missing until its objects are added.
func (g *Graph) SetReadable(resource string, readable bool) {
	if !g.holds[resource] || g.readable[resource] == readable {
		return
	}
	g.readable[resource] = readable
	if !readable {
		for _, id := range slices.Sorted(maps.Keys(g.objects)) {
			if g.objects[id].resource == resource {
				g.Delete(id)
			}
		}
	}
	prefix := resource + "/"
	readers := map[string]bool{}
	for key, ids := range g.wanting {
		if strings.HasPrefix(key, prefix) {
			maps.Copy(readers, ids)
		}
	}
	for group, ids := range g.watching {
		if strings.HasPrefix(group, prefix) {
			maps.Copy(readers, ids)
		}
	}
	g.resolveAll(readers)
}

// Readable reports whether the graph is given a kind's objects.
func (g *Graph) Readable(resource string) bool {
	return g.readable[resource]
}

// ---- Resolving references ----

// refsOf runs every builder for a kind over one object.
func refsOf(resource string, obj runtime.Object) []Ref {
	var out []Ref
	for _, b := range buildersFor(resource) {
		r := &Refs{builder: b.Name}
		b.Refs(obj, r)
		out = append(out, r.list...)
	}
	return out
}

// resolveAll resolves a set of readers again, in a fixed order.
func (g *Graph) resolveAll(ids map[string]bool) {
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		if e, ok := g.objects[id]; ok {
			g.resolve(e)
		}
	}
}

// resolve turns an object's references into edges, replacing the ones it
// gave before.
func (g *Graph) resolve(e *entry) {
	g.withdraw(e)
	e.matched = map[int]map[string]bool{}
	for i, ref := range e.refs {
		switch {
		case ref.Target != nil:
			key := ref.Target.key()
			e.wants = append(e.wants, key)
			addTo(g.wanting, key, e.id)
			from, to := e.id, g.target(*ref.Target)
			if ref.Reverse {
				from, to = to, from
			}
			g.contribute(e, from, to, ref.Kind, evidenceOf(e, ref))
		case ref.Match != nil:
			m := ref.Match
			group := groupKey(m.Resource, m.Namespace)
			e.watches = append(e.watches, group)
			addTo(g.watching, group, e.id)
			picked := map[string]bool{}
			for id := range g.groups[group] {
				if id != e.id && m.Selects(g.objects[id].obj) {
					g.contribute(e, e.id, id, ref.Kind, evidenceOf(e, ref))
					picked[id] = true
				}
			}
			e.matched[i] = picked
		}
	}
	g.updateDangling(e)
}

// rematch checks one object that came, went or changed its labels against
// every Match that looks at its kind and namespace, adding or removing only
// the edges to it. Checking every Match in full again would cost the whole
// namespace on each pod that starts.
func (g *Graph) rematch(c *entry, present bool) {
	for wid := range g.watching[c.group] {
		w, ok := g.objects[wid]
		if !ok || wid == c.id {
			continue
		}
		changed := false
		for i, ref := range w.refs {
			if ref.Match == nil || groupKey(ref.Match.Resource, ref.Match.Namespace) != c.group {
				continue
			}
			was := w.matched[i][c.id]
			now := present && ref.Match.Selects(c.obj)
			switch {
			case now && !was:
				g.contribute(w, w.id, c.id, ref.Kind, evidenceOf(w, ref))
				w.matched[i][c.id] = true
			case was && !now:
				g.retract(w, w.id, c.id, ref)
				delete(w.matched[i], c.id)
			default:
				continue
			}
			changed = true
		}
		if changed {
			g.updateDangling(w)
		}
	}
}

// updateDangling records each Match that picks nothing, when it says so
// and its kind can be read.
func (g *Graph) updateDangling(e *entry) {
	var dangling []Dangling
	for i, ref := range e.refs {
		if m := ref.Match; m != nil && m.Dangling != "" && g.readable[m.Resource] && len(e.matched[i]) == 0 {
			dangling = append(dangling, Dangling{Kind: ref.Kind, Field: ref.Field, Reason: m.Dangling})
		}
	}
	slices.SortFunc(dangling, func(a, b Dangling) int {
		return cmp.Or(cmp.Compare(a.Field, b.Field), cmp.Compare(a.Kind, b.Kind))
	})
	g.nodes[e.id].Dangling = dangling
}

func evidenceOf(e *entry, ref Ref) Evidence {
	return Evidence{
		Builder: ref.Builder, Field: ref.Field, Note: ref.Note,
		ResourceVersion: e.rv, Seen: e.seen, Optional: ref.Optional,
	}
}

// target finds the node a reference points at, making a Missing or Unknown
// placeholder when there's no such object. A reference with a UID (an
// owner reference) only matches the object with that UID: an owner that
// was deleted and made again under the same name isn't the owner.
func (g *Graph) target(t Target) string {
	key := t.key()
	if t.Resource != "" && g.readable[t.Resource] {
		if id, ok := g.byKey[key]; ok && (t.UID == "" || t.UID == id) {
			return id
		}
		return g.placeholder(Missing, t, key)
	}
	return g.placeholder(Unknown, t, key)
}

func (g *Graph) placeholder(presence string, t Target, key string) string {
	id := presence + ":" + key
	if _, ok := g.nodes[id]; !ok {
		g.nodes[id] = &Node{
			ID: id, Resource: t.Resource, Kind: t.Kind,
			Namespace: t.Namespace, Name: t.Name, Presence: presence,
		}
	}
	return id
}

// contribute adds one piece of evidence to an edge, making the edge if
// it's the first.
func (g *Graph) contribute(e *entry, from, to, kind string, ev Evidence) {
	if from == to {
		g.dropIfUnused(from)
		return
	}
	id := EdgeID(from, to, kind)
	es, ok := g.edges[id]
	if !ok {
		es = &edgeState{from: from, to: to, kind: kind, evidence: map[string][]Evidence{}}
		g.edges[id] = es
		g.touch(from, 1)
		g.touch(to, 1)
	}
	if e.gave == nil {
		e.gave = map[string]bool{}
	}
	e.gave[id] = true
	es.evidence[e.id] = append(es.evidence[e.id], ev)
}

// retract takes back the evidence one reference gave to one edge.
func (g *Graph) retract(e *entry, from, to string, ref Ref) {
	id := EdgeID(from, to, ref.Kind)
	es, ok := g.edges[id]
	if !ok {
		return
	}
	left := slices.DeleteFunc(es.evidence[e.id], func(ev Evidence) bool {
		return ev.Builder == ref.Builder && ev.Field == ref.Field
	})
	if len(left) > 0 {
		es.evidence[e.id] = left
		return
	}
	delete(es.evidence, e.id)
	delete(e.gave, id)
	if len(es.evidence) == 0 {
		delete(g.edges, id)
		g.touch(es.from, -1)
		g.touch(es.to, -1)
	}
}

// withdraw takes back everything an object gave: its evidence, the edges
// left with none, the placeholders left with no edges, and its places in
// wanting and watching.
func (g *Graph) withdraw(e *entry) {
	for id := range e.gave {
		es := g.edges[id]
		delete(es.evidence, e.id)
		if len(es.evidence) == 0 {
			delete(g.edges, id)
			g.touch(es.from, -1)
			g.touch(es.to, -1)
		}
	}
	e.gave = nil
	for _, key := range e.wants {
		removeFrom(g.wanting, key, e.id)
	}
	for _, group := range e.watches {
		removeFrom(g.watching, group, e.id)
	}
	e.wants, e.watches = nil, nil
}

// touch counts the edges at a placeholder, removing it when none are left.
// Present nodes stay whatever their edges.
func (g *Graph) touch(id string, by int) {
	if n, ok := g.nodes[id]; !ok || n.Presence == Present {
		return
	}
	g.degree[id] += by
	g.dropIfUnused(id)
}

func (g *Graph) dropIfUnused(id string) {
	if n, ok := g.nodes[id]; ok && n.Presence != Present && g.degree[id] <= 0 {
		delete(g.nodes, id)
		delete(g.degree, id)
	}
}

// ---- Reading ----

// Snapshot is the whole graph, nodes and edges sorted by ID.
type Snapshot struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Snapshot copies the whole graph.
func (g *Graph) Snapshot() Snapshot {
	s := Snapshot{Nodes: make([]Node, 0, len(g.nodes)), Edges: make([]Edge, 0, len(g.edges))}
	for _, id := range slices.Sorted(maps.Keys(g.nodes)) {
		s.Nodes = append(s.Nodes, g.node(id))
	}
	for _, id := range slices.Sorted(maps.Keys(g.edges)) {
		s.Edges = append(s.Edges, g.edge(id))
	}
	return s
}

func (g *Graph) node(id string) Node {
	n := *g.nodes[id]
	n.Dangling = slices.Clone(n.Dangling)
	return n
}

func (g *Graph) edge(id string) Edge {
	es := g.edges[id]
	e := Edge{ID: id, From: es.from, To: es.to, Kind: es.kind, Optional: true}
	for _, reader := range slices.Sorted(maps.Keys(es.evidence)) {
		e.Evidence = append(e.Evidence, es.evidence[reader]...)
	}
	slices.SortStableFunc(e.Evidence, func(a, b Evidence) int {
		return cmp.Or(cmp.Compare(a.Builder, b.Builder), cmp.Compare(a.Field, b.Field))
	})
	for _, ev := range e.Evidence {
		e.Optional = e.Optional && ev.Optional
	}
	return e
}

// ---- Small helpers ----

func groupKey(resource, namespace string) string {
	return resource + "/" + namespace
}

func addTo(index map[string]map[string]bool, key, id string) {
	if index[key] == nil {
		index[key] = map[string]bool{}
	}
	index[key][id] = true
}

func removeFrom(index map[string]map[string]bool, key, id string) {
	delete(index[key], id)
	if len(index[key]) == 0 {
		delete(index, key)
	}
}
