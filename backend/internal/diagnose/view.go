package diagnose

import (
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
