package diagnose

import (
	"slices"
	"strings"
	"testing"

	"opscope/internal/graph"
)

// drawn maps every object a view shows to the node it's drawn as.
func drawn(v *graph.View) map[string]string {
	m := map[string]string{}
	for _, n := range v.Nodes {
		m[n.ID] = n.ID
		if n.Pods != nil {
			for _, member := range n.Pods.Members {
				m[member] = n.ID
			}
		}
	}
	return m
}

// Every scenario's map reaches its root causes, and has the findings to
// say so: the Map tab never cuts a trace short.
func TestViewsReachRootCauses(t *testing.T) {
	d := Run(loadScenarios(t))
	for id := range scenarioRoots {
		t.Run(id, func(t *testing.T) {
			view, ok := d.g.Neighbourhood(id, 2, graph.Options{})
			if !ok {
				t.Fatal("not in the graph")
			}
			findings := d.ForView(view)
			shown := drawn(view)
			trace, _ := d.Trace(id)
			for _, root := range roots(trace.Steps) {
				for _, object := range root.Objects {
					if shown[object] == "" {
						t.Errorf("root cause %s (%s) isn't in the map", object, root.Title)
					}
				}
				if !slices.ContainsFunc(findings, func(f *Finding) bool { return f.ID == root.ID }) {
					t.Errorf("the map has no finding %s", root.ID)
				}
			}
			for _, f := range findings {
				if shown[f.Object] == "" {
					t.Errorf("finding about %s, which the map doesn't show", f.Object)
				}
			}
		})
	}
}

// A ResourceQuota has no edges; the map shows it because the ReplicaSet's
// finding blames it.
func TestViewAddsWhatFindingsBlame(t *testing.T) {
	d := Run(loadScenarios(t))
	view, _ := d.g.Neighbourhood("deployments/opscope-scenarios/over-quota", 2, graph.Options{})
	d.ForView(view)
	quota := "resourcequotas/opscope-scenarios/memory"
	if drawn(view)[quota] == "" {
		t.Fatalf("%s isn't in the map", quota)
	}
	i := slices.IndexFunc(view.Edges, func(e *graph.Edge) bool { return e.To == quota })
	if i < 0 || view.Edges[i].Type != graph.Blames || !strings.HasPrefix(view.Edges[i].From, "replicasets/opscope-scenarios/over-quota-") {
		t.Errorf("edges to the quota: %+v", view.Edges)
	}
}
