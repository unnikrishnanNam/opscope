package graph

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// A view is the part of the graph one page shows, shaped for drawing:
//
//   - pods are collapsed into one node per workload ("3/4 ready"), unless
//     expanded
//   - a ReplicaSet is hidden unless it matters: during a rollout (more than
//     one of a Deployment's ReplicaSets has pods), when it can't create
//     pods, or when nothing owns it. Its owner then points straight at its
//     pods.
//   - nodes (machines) are left out unless they're unhealthy, or next to the
//     focused object
//   - namespaces aren't shown; they're the frame, not a dependency

// PodGroup is set on a node that stands for several pods.
type PodGroup struct {
	Owner   string   `json:"owner"` // the workload they belong to, as shown: "deployments/shop/web"
	Ready   int      `json:"ready"`
	Total   int      `json:"total"`
	Members []string `json:"members"` // the pods' IDs
}

// View is what the topology endpoint returns: nodes and the edges between
// them, sorted, plus the apps of a namespace view.
type View struct {
	Nodes []*Node `json:"nodes"`
	Edges []*Edge `json:"edges"`
	Apps  []App   `json:"apps,omitempty"`
}

// App sums up one app of a namespace, for the app picker.
type App struct {
	Name      string `json:"name"`
	State     State  `json:"state"`     // the worst state among its objects
	Workloads int    `json:"workloads"` // Deployments, StatefulSets, DaemonSets, CronJobs, and Jobs and pods of their own
}

// Options change what a view shows.
type Options struct {
	Expand   map[string]bool // workloads whose pods are shown one by one, by ID
	All      bool            // expand every workload's pods
	Problems bool            // only what isn't OK, and what points at it
}

// Namespace shows everything in a namespace, what points at it from other
// namespaces (a route sending traffic here), and what all of that needs: a
// Gateway in another namespace, volumes, classes, missing objects.
func (g *Graph) Namespace(namespace string, o Options) *View {
	p := g.project(o, "")
	seeds := p.where(func(n *Node) bool { return n.Namespace == namespace && n.Resource != "namespaces" })
	parents := p.parents(seeds, nil)
	inside := func(n *Node) bool { return n.Namespace == namespace || parents[n.ID] }
	view := p.view(p.dependencies(append(seeds, sortedKeys(parents)...), inside), "", o)
	view.Apps = p.apps(namespace)
	return view
}

// App shows one app of a namespace: its objects, what sends traffic to them
// or scales them, and what all of that needs.
func (g *Graph) App(namespace, app string, o Options) *View {
	p := g.project(o, "")
	seeds := p.where(func(n *Node) bool { return n.Namespace == namespace && n.App == app })
	parents := p.parents(seeds, set(RoutesTo, Selects, Scales))
	inside := func(n *Node) bool { return (n.Namespace == namespace && n.App == app) || parents[n.ID] }
	return p.view(p.dependencies(append(seeds, sortedKeys(parents)...), inside), "", o)
}

// Neighbourhood shows one object and everything within depth edges of it,
// in either direction, plus everything behind any unhealthy node among them,
// so a problem's cause is never cut off. It returns false when there's no
// such object.
func (g *Graph) Neighbourhood(id string, depth int, o Options) (*View, bool) {
	focus, ok := g.nodes[id]
	if !ok {
		return nil, false
	}
	// The focused object is always shown as itself: a pod isn't hidden in
	// its group, a ReplicaSet isn't hidden behind its Deployment.
	if focus.Resource == "pods" {
		if owner := g.owner(id); owner != "" {
			o.Expand = withKey(o.Expand, owner)
		}
	}
	p := g.project(o, id)

	keep := map[string]bool{id: true}
	frontier := []string{id}
	for range depth {
		var next []string
		for _, from := range frontier {
			for _, e := range p.out[from] {
				if !keep[e.To] {
					keep[e.To] = true
					next = append(next, e.To)
				}
			}
			for _, e := range p.in[from] {
				if !keep[e.From] {
					keep[e.From] = true
					next = append(next, e.From)
				}
			}
		}
		frontier = next
	}

	var unhealthy []string
	for n := range keep {
		if p.nodes[n].State != OK {
			unhealthy = append(unhealthy, n)
		}
	}
	for n := range p.dependencies(unhealthy, func(*Node) bool { return true }) {
		keep[n] = true
	}
	return p.view(keep, id, o), true
}

// ---- Projection ----

// maxVia is how many of an edge's references a view lists. A container
// can read thirty keys of one ConfigMap; the first few say enough.
const maxVia = 5

// projection is the graph as views draw it: pods grouped, unimportant
// ReplicaSets folded into their owners.
type projection struct {
	nodes map[string]*Node
	out   map[string][]*Edge
	in    map[string][]*Edge
}

// project builds the projection. show names an object that must stay
// visible even if it would normally be folded away.
func (g *Graph) project(o Options, show string) *projection {
	// Where each node goes: itself, its pod group, or (a hidden ReplicaSet)
	// its owner.
	target := map[string]string{}
	groups := map[string]*Node{}

	for id, n := range g.nodes {
		target[id] = id
		if n.Resource == "replicasets" && id != show && !g.replicaSetMatters(id) {
			target[id] = g.owner(id)
		}
	}
	for id, n := range g.nodes {
		if n.Resource != "pods" {
			continue
		}
		owner := g.owner(id)
		if owner == "" || o.All || o.Expand[owner] || o.Expand[target[owner]] {
			continue
		}
		groupID := "pods:" + owner
		group, ok := groups[groupID]
		if !ok {
			shown := g.nodes[target[owner]]
			group = &Node{
				ID: groupID, Resource: "pods", Kind: "Pod", Namespace: n.Namespace, Name: shown.Name, App: n.App,
				State: OK, Pods: &PodGroup{Owner: shown.ID},
			}
			groups[groupID] = group
		}
		group.State = Worse(group.State, n.State)
		group.Pods.Total++
		group.Pods.Members = append(group.Pods.Members, id)
		if n.State == OK {
			group.Pods.Ready++
		}
		target[id] = groupID
	}

	p := &projection{nodes: map[string]*Node{}, out: map[string][]*Edge{}, in: map[string][]*Edge{}}
	for id, n := range g.nodes {
		if target[id] == id && n.Resource != "namespaces" {
			p.nodes[id] = n
		}
	}
	for id, group := range groups {
		sort.Strings(group.Pods.Members)
		group.Status = fmt.Sprintf("%d/%d ready", group.Pods.Ready, group.Pods.Total)
		group.Message = groupMessage(g, group)
		p.nodes[id] = group
	}

	// Edges follow their ends to where those went. Several edges can land
	// on the same pair (three pods using one Secret); they merge into one.
	merged := map[edgeKey]*Edge{}
	for k, e := range g.edges {
		from, to := target[k.from], target[k.to]
		if from == to || p.nodes[from] == nil || p.nodes[to] == nil {
			continue // folded into itself, or into something not shown
		}
		key := edgeKey{from, to, k.typ}
		m, ok := merged[key]
		if !ok {
			m = &Edge{From: from, To: to, Type: k.typ, Optional: true}
			merged[key] = m
			p.out[from] = append(p.out[from], m)
			p.in[to] = append(p.in[to], m)
		}
		m.Optional = m.Optional && e.Optional
		for _, via := range e.Via {
			if !slices.Contains(m.Via, via) {
				m.Via = append(m.Via, via)
			}
		}
	}
	for _, m := range merged {
		sort.Strings(m.Via)
		if len(m.Via) > maxVia {
			m.Via = append(m.Via[:maxVia], fmt.Sprintf("and %d more", len(m.Via)-maxVia))
		}
	}
	p.dropTemplateEdges()
	return p
}

// dropTemplateEdges removes a workload's uses edges that its own pods have
// too. The pod template's edges are there for a workload with no pods; with
// pods, the pods say the same thing, and they're the ones that fail.
func (p *projection) dropTemplateEdges() {
	for id, edges := range p.out {
		if !workloads[p.nodes[id].Resource] || p.nodes[id].Pods != nil {
			continue
		}
		usedByPods := map[string]bool{}
		for _, pod := range p.ownedPods(id) {
			for _, e := range p.out[pod] {
				if e.Type == Uses {
					usedByPods[e.To] = true
				}
			}
		}
		kept := edges[:0]
		for _, e := range edges {
			if e.Type == Uses && usedByPods[e.To] {
				p.in[e.To] = slices.DeleteFunc(p.in[e.To], func(other *Edge) bool { return other == e })
				continue
			}
			kept = append(kept, e)
		}
		p.out[id] = kept
	}
}

// ownedPods lists the pods (or pod groups) under a workload, through any
// ReplicaSets or Jobs shown between them.
func (p *projection) ownedPods(id string) []string {
	var pods []string
	for _, e := range p.out[id] {
		if e.Type != Owns {
			continue
		}
		if p.nodes[e.To].Resource == "pods" {
			pods = append(pods, e.To)
		} else {
			pods = append(pods, p.ownedPods(e.To)...)
		}
	}
	return pods
}

// groupMessage explains a group that isn't OK by its first unhealthy pod.
func groupMessage(g *Graph, group *Node) string {
	for _, id := range group.Pods.Members {
		if n := g.nodes[id]; n.State == group.State && n.State != OK {
			return firstOf(n.Message, n.Status)
		}
	}
	return ""
}

// owner is the object that owns another: a pod's ReplicaSet, StatefulSet,
// DaemonSet or Job, a ReplicaSet's Deployment. It's "" for an object of its own.
func (g *Graph) owner(id string) string {
	for _, e := range g.in[id] {
		if e.Type == Owns {
			return e.From
		}
	}
	return ""
}

// replicaSetMatters says whether a ReplicaSet is worth showing between its
// Deployment and its pods. One whose pods are failing isn't: the pods say
// so already. One that can't create pods is: nothing else would show why.
func (g *Graph) replicaSetMatters(id string) bool {
	owner := g.owner(id)
	switch {
	case owner == "" || g.cantCreatePods[id]:
		return true
	case !g.hasPods(id):
		return false // an old revision, kept for rolling back
	}
	withPods := 0
	for _, e := range g.out[owner] {
		if e.Type == Owns && g.hasPods(e.To) {
			withPods++
		}
	}
	return withPods > 1 // a rollout: old and new pods side by side
}

func (g *Graph) hasPods(id string) bool {
	for _, e := range g.out[id] {
		if e.Type == Owns && strings.HasPrefix(e.To, "pods/") {
			return true
		}
	}
	return false
}

// dependencies returns the seeds and everything they need, following edges
// forward. An edge is followed from a node inside the view (as inside says),
// and from anywhere to a cluster-wide object: a namespace view shows a
// Gateway in another namespace and that Gateway's class, but not the
// Services that Gateway's other routes lead to.
func (p *projection) dependencies(seeds []string, inside func(*Node) bool) map[string]bool {
	keep := map[string]bool{}
	for _, id := range seeds {
		keep[id] = true
	}
	queue := append([]string{}, seeds...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		from := p.nodes[id]
		for _, e := range p.out[id] {
			if keep[e.To] || !(inside(from) || p.nodes[e.To].Namespace == "") {
				continue
			}
			keep[e.To] = true
			queue = append(queue, e.To)
		}
	}
	return keep
}

// parents finds what points at the seeds from outside them: a route in
// another namespace, an Ingress of another app. With edge types given, it
// follows those edges back as far as they go (an Ingress → a Service →
// pods); without, it takes only the direct parents.
func (p *projection) parents(seeds []string, types map[string]bool) map[string]bool {
	inSeeds := map[string]bool{}
	for _, id := range seeds {
		inSeeds[id] = true
	}
	found := map[string]bool{}
	queue := append([]string{}, seeds...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, e := range p.in[id] {
			if inSeeds[e.From] || found[e.From] || (types != nil && !types[e.Type]) {
				continue
			}
			found[e.From] = true
			if types != nil {
				queue = append(queue, e.From)
			}
		}
	}
	return found
}

// where lists the IDs of the nodes that match, sorted.
func (p *projection) where(match func(*Node) bool) []string {
	var ids []string
	for id, n := range p.nodes {
		if match(n) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// view turns a set of node IDs into a View: machines left out unless they
// matter, the problems filter applied, everything sorted.
func (p *projection) view(keep map[string]bool, focus string, o Options) *View {
	for id := range keep {
		n := p.nodes[id]
		if n.Resource == "nodes" && n.State == OK && id != focus && !p.adjacent(id, focus) {
			delete(keep, id)
		}
	}

	if o.Problems {
		problems := map[string]bool{}
		for id := range keep {
			if p.nodes[id].State != OK || id == focus {
				problems[id] = true
				for _, e := range p.in[id] {
					if keep[e.From] {
						problems[e.From] = true // what points at it, for context
					}
				}
			}
		}
		keep = problems
	}

	view := &View{Nodes: []*Node{}, Edges: []*Edge{}}
	connected := map[string]bool{}
	for id := range keep {
		for _, e := range p.out[id] {
			if keep[e.To] {
				view.Edges = append(view.Edges, e)
				connected[e.From], connected[e.To] = true, true
			}
		}
	}
	for id := range keep {
		// Configuration nothing uses (every namespace's kube-root-ca.crt and
		// "default" service account, Helm's release Secrets) would only
		// crowd the picture.
		if unusedConfig[p.nodes[id].Resource] && !connected[id] && id != focus {
			continue
		}
		view.Nodes = append(view.Nodes, p.nodes[id])
	}
	sort.Slice(view.Nodes, func(i, j int) bool { return view.Nodes[i].ID < view.Nodes[j].ID })
	sortEdges(view.Edges)
	return view
}

func (p *projection) adjacent(a, b string) bool {
	if b == "" {
		return false
	}
	for _, e := range p.in[a] {
		if e.From == b {
			return true
		}
	}
	return false
}

var unusedConfig = set("configmaps", "secrets", "serviceaccounts")

// apps lists the apps of a namespace with their worst state.
func (p *projection) apps(namespace string) []App {
	byName := map[string]*App{}
	for _, n := range p.nodes {
		if n.Namespace != namespace || n.App == "" {
			continue
		}
		app, ok := byName[n.App]
		if !ok {
			app = &App{Name: n.App, State: OK}
			byName[n.App] = app
		}
		app.State = Worse(app.State, n.State)
		if workloads[n.Resource] && n.Pods == nil && !p.ownedByWorkload(n.ID) {
			app.Workloads++
		}
	}
	apps := []App{}
	for _, app := range byName {
		apps = append(apps, *app)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

func (p *projection) ownedByWorkload(id string) bool {
	for _, e := range p.in[id] {
		if e.Type == Owns && workloads[p.nodes[e.From].Resource] {
			return true
		}
	}
	return false
}

func withKey(m map[string]bool, key string) map[string]bool {
	copied := map[string]bool{key: true}
	for k, v := range m {
		copied[k] = v
	}
	return copied
}
