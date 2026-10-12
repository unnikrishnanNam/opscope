package diagnose

import (
	"slices"
	"sort"

	"opscope/internal/graph"
)

// ForView returns the findings about the objects a view shows, the pods in
// its groups included, so a drawing can say what's wrong with each object
// and follow the chain to a root cause without another request.
//
// It also adds to the view what those findings blame and the view doesn't
// show, joined by a "blames" edge. Most culprits are already there, reached
// by the graph's own edges; a ResourceQuota isn't, since nothing points at
// it until a rule finds it in the way.
func (d *Diagnosis) ForView(v *graph.View) []*Finding {
	// The view node each object is drawn as: itself, or its pod group.
	drawnAs := map[string]string{}
	var queue []string
	for _, n := range v.Nodes {
		drawnAs[n.ID] = n.ID
		queue = append(queue, n.ID)
		if n.Pods != nil {
			for _, member := range n.Pods.Members {
				drawnAs[member] = n.ID
				queue = append(queue, member)
			}
		}
	}

	findings := []*Finding{}
	added := false
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		for _, f := range d.byObject[id] {
			findings = append(findings, f)
			for _, blamed := range f.Blames {
				if drawnAs[blamed] != "" {
					continue
				}
				n, ok := d.g.Node(blamed)
				if !ok {
					continue
				}
				drawnAs[blamed] = blamed
				v.Nodes = append(v.Nodes, n)
				v.Edges = append(v.Edges, &graph.Edge{From: drawnAs[id], To: blamed, Type: graph.Blames})
				queue = append(queue, blamed)
				added = true
			}
		}
	}
	if added {
		sort.Slice(v.Nodes, func(i, j int) bool { return v.Nodes[i].ID < v.Nodes[j].ID })
		graph.SortEdges(v.Edges)
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Object < findings[j].Object })
	return findings
}

// ForCluster returns the problems a cluster view shows, as the findings
// they're made of, and names on each card the objects whose findings it
// shows (Node.Members). A problem is shown on the card of the object it's
// about, or, when no card holds that object (a missing StorageClass), on the
// cards of the apps it affects. Only root causes are sent: a card says
// what's wrong at its root, and its own map has the rest.
func (d *Diagnosis) ForCluster(v *graph.View) []*Finding {
	cardOf := map[string]string{}
	for _, n := range v.Nodes {
		cardOf[n.ID] = n.ID
		for _, member := range v.Members(n.ID) {
			cardOf[member] = n.ID
		}
	}
	shown := map[string]map[string]bool{} // card → objects whose findings it shows
	show := func(card, object string) {
		if shown[card] == nil {
			shown[card] = map[string]bool{}
		}
		shown[card][object] = true
	}

	findings := []*Finding{}
	for _, p := range d.Problems("") {
		// The cards of what it affects, for objects that have none.
		var affected []string
		for _, n := range p.Affects {
			if card := cardOf[n.ID]; card != "" && v.Members(card) != nil && !slices.Contains(affected, card) {
				affected = append(affected, card)
			}
		}
		for _, object := range p.Objects {
			cards := []string{cardOf[object]}
			if cards[0] == "" {
				cards = affected
			}
			if len(cards) == 0 {
				continue // about nothing this view shows
			}
			for _, f := range d.byObject[object] {
				if f.Rule == p.Rule && f.Title == p.Title {
					findings = append(findings, f)
				}
			}
			for _, card := range cards {
				if card != object {
					show(card, object)
				}
			}
		}
	}
	// The cards that list members are the view's own, made for it; the
	// shared ones (a Gateway, a node) are their own objects already.
	for _, n := range v.Nodes {
		if objects := shown[n.ID]; objects != nil && v.Members(n.ID) != nil {
			n.Members = make([]string, 0, len(objects))
			for object := range objects {
				n.Members = append(n.Members, object)
			}
			sort.Strings(n.Members)
		}
	}
	sort.SliceStable(findings, func(i, j int) bool { return findings[i].Object < findings[j].Object })
	return findings
}

// Troubled lists the objects with a problem (a root cause that isn't a
// note), and those it affects, with how bad it is, for the cluster view to
// count toward their apps: the pods on a node that stopped reporting still
// say Running, but their apps don't work.
func (d *Diagnosis) Troubled() map[string]graph.State {
	troubled := map[string]graph.State{}
	for _, p := range d.Problems("") {
		state := graph.State(p.Severity)
		for _, object := range p.Objects {
			troubled[object] = graph.Worse(troubled[object], state)
		}
		for _, n := range p.Affects {
			troubled[n.ID] = graph.Worse(troubled[n.ID], state)
		}
	}
	return troubled
}
