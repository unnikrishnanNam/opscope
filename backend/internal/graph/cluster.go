package graph

import (
	"fmt"
	"slices"
	"sort"
)

// Group is a namespace in the cluster view, holding the cards of its apps.
type Group struct {
	ID    string `json:"id"` // "namespaces/shop"
	Name  string `json:"name"`
	State State  `json:"state"` // the worst state among its apps
	Apps  int    `json:"apps"`
}

// Cluster shows the whole cluster at once, as namespaces holding apps:
//
//   - each app is one card ("3 workloads, 1 failing") in its namespace's
//     group
//   - a namespace with nothing wrong in it is one card ("4 apps"), unless
//     it's expanded (Options.Expand["namespaces/<name>"], or Options.All)
//   - Gateways, GatewayClasses and IngressClasses are the entry points the
//     apps share, and the edges are what joins the cards: an app's route
//     attaches to a Gateway, its Ingress uses a class, a Gateway runs on an
//     app of its own, an app routes to a Service of another
//   - every node (machine) is shown, for a strip of their health
//
// With Options.Problems, only cards that aren't OK, and what they're joined
// to. Namespaces with nothing in them aren't shown.
func (g *Graph) Cluster(o Options) *View {
	// The apps of each namespace, with what they hold.
	type app struct {
		node    *Node
		members []string
	}
	apps := map[string]*app{} // by card ID: "apps/<namespace>/<app>"
	worst := map[string]State{}
	for _, n := range g.nodes {
		if n.Namespace == "" || n.App == "" || entryPoints[n.Resource] {
			continue
		}
		id := "apps/" + n.Namespace + "/" + n.App
		a := apps[id]
		if a == nil {
			a = &app{node: &Node{ID: id, Resource: "apps", Kind: "App", Namespace: n.Namespace, Name: n.App, App: n.App, State: OK,
				Group: "namespaces/" + n.Namespace}}
			apps[id] = a
		}
		state := Worse(countedState(n.State), o.Troubled[n.ID])
		a.node.State = Worse(a.node.State, state)
		a.members = append(a.members, n.ID)
		if _, ok := worst[n.Namespace]; !ok {
			worst[n.Namespace] = OK
		}
		worst[n.Namespace] = Worse(worst[n.Namespace], state)
	}

	// Each app's summary, and its namespace's.
	byNamespace := map[string][]*app{}
	for _, a := range apps {
		sort.Strings(a.members)
		a.node.Status = g.appSummary(a.members)
		byNamespace[a.node.Namespace] = append(byNamespace[a.node.Namespace], a)
	}

	view := &View{Nodes: []*Node{}, Edges: []*Edge{}, Groups: []Group{}, members: map[string][]string{}}
	cardOf := map[string]string{} // object → the card it's drawn in
	for namespace, list := range byNamespace {
		sort.Slice(list, func(i, j int) bool { return list[i].node.ID < list[j].node.ID })
		groupID := "namespaces/" + namespace
		expanded := o.All || o.Expand[groupID] || (worst[namespace] != OK && worst[namespace] != Progressing)
		if !expanded {
			tile := &Node{ID: groupID, Resource: "namespaces", Kind: "Namespace", Name: namespace, State: OK,
				Status: plural(len(list), "app")}
			if ns, ok := g.nodes[groupID]; ok {
				tile.State = ns.State // terminating is "progressing"
			}
			for _, a := range list {
				tile.State = Worse(tile.State, a.node.State)
				for _, m := range a.members {
					cardOf[m] = groupID
				}
				view.members[groupID] = append(view.members[groupID], a.members...)
			}
			view.Nodes = append(view.Nodes, tile)
			continue
		}
		group := Group{ID: groupID, Name: namespace, State: OK, Apps: len(list)}
		for _, a := range list {
			group.State = Worse(group.State, a.node.State)
			for _, m := range a.members {
				cardOf[m] = a.node.ID
			}
			view.members[a.node.ID] = a.members
			view.Nodes = append(view.Nodes, a.node)
		}
		view.Groups = append(view.Groups, group)
	}
	for _, n := range g.nodes {
		if entryPoints[n.Resource] || n.Resource == "nodes" {
			cardOf[n.ID] = n.ID
			view.Nodes = append(view.Nodes, n)
		}
	}

	// Edges between cards, merged: several routes of one app attach to the
	// same Gateway as one edge, with the routes listed.
	merged := map[edgeKey]*Edge{}
	for k, e := range g.edges {
		from, to := cardOf[k.from], cardOf[k.to]
		if from == "" || to == "" || from == to || g.nodes[k.from].Resource == "nodes" || g.nodes[k.to].Resource == "nodes" {
			continue
		}
		key := edgeKey{from, to, k.typ}
		m := merged[key]
		if m == nil {
			m = &Edge{From: from, To: to, Type: k.typ, Optional: true}
			merged[key] = m
		}
		m.Optional = m.Optional && e.Optional
		// Name the object inside a card the edge comes from (an app's route),
		// or else goes to (a Gateway's own Deployment, in its namespace).
		inside := ""
		if from != k.from {
			inside = k.from
		} else if to != k.to {
			inside = k.to
		}
		if inside != "" {
			if via := g.nodes[inside].Kind + " " + g.nodes[inside].Name; !slices.Contains(m.Via, via) {
				m.Via = append(m.Via, via)
			}
		}
	}
	for _, m := range merged {
		sort.Strings(m.Via)
		if len(m.Via) > maxVia {
			m.Via = append(m.Via[:maxVia], fmt.Sprintf("and %d more", len(m.Via)-maxVia))
		}
		view.Edges = append(view.Edges, m)
	}

	// Classes and Gateways nothing uses only crowd the picture.
	used := map[string]bool{}
	for _, e := range view.Edges {
		used[e.From], used[e.To] = true, true
	}
	view.Nodes = slices.DeleteFunc(view.Nodes, func(n *Node) bool {
		return (n.Resource == "gatewayclasses" || n.Resource == "ingressclasses") && !used[n.ID] && n.State == OK
	})

	if o.Problems {
		view.onlyProblems()
	}
	sort.Slice(view.Nodes, func(i, j int) bool { return view.Nodes[i].ID < view.Nodes[j].ID })
	SortEdges(view.Edges)
	sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].Name < view.Groups[j].Name })
	return view
}

// entryPoints are the kinds the cluster view shows on their own, shared by
// apps rather than inside one.
var entryPoints = set("gateways", "gatewayclasses", "ingressclasses")

// countedState is an object's state as it counts toward its app's and
// namespace's: an object Opscope can't read (Secrets, without the role)
// isn't a problem, and a missing one makes its app fail.
func countedState(s State) State {
	switch s {
	case Unknown:
		return OK
	case Missing:
		return Failing
	}
	return s
}

// appSummary says what an app holds: "3 workloads, 1 failing", or for an
// app without workloads (a Service on its own), how many objects.
func (g *Graph) appSummary(members []string) string {
	count, failing, degraded := 0, 0, 0
	for _, id := range members {
		n := g.nodes[id]
		if !workloads[n.Resource] || g.ownedByWorkload(id) {
			continue
		}
		count++
		switch n.State {
		case Failing:
			failing++
		case Degraded:
			degraded++
		}
	}
	if count == 0 {
		return plural(len(members), "object")
	}
	summary := plural(count, "workload")
	if failing > 0 {
		summary += fmt.Sprintf(", %d failing", failing)
	}
	if degraded > 0 {
		summary += fmt.Sprintf(", %d degraded", degraded)
	}
	return summary
}

func (g *Graph) ownedByWorkload(id string) bool {
	for _, e := range g.in[id] {
		if e.Type == Owns && workloads[g.nodes[e.From].Resource] {
			return true
		}
	}
	return false
}

// onlyProblems keeps the cards that aren't OK, and what they're joined to.
func (v *View) onlyProblems() {
	keep := map[string]bool{}
	for _, n := range v.Nodes {
		if n.State != OK && n.State != Progressing && n.State != Unknown { // what can't be read isn't a problem
			keep[n.ID] = true
		}
	}
	for _, e := range v.Edges {
		if keep[e.From] || keep[e.To] {
			keep[e.From], keep[e.To] = true, true
		}
	}
	v.Nodes = slices.DeleteFunc(v.Nodes, func(n *Node) bool { return !keep[n.ID] })
	v.Edges = slices.DeleteFunc(v.Edges, func(e *Edge) bool { return !keep[e.From] || !keep[e.To] })
	groups := map[string]bool{}
	for _, n := range v.Nodes {
		groups[n.Group] = true
	}
	v.Groups = slices.DeleteFunc(v.Groups, func(gr Group) bool { return !groups[gr.ID] })
}

// Members lists the objects a card of the cluster view stands for: an
// app's, or a folded namespace's.
func (v *View) Members(id string) []string {
	return v.members[id]
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
