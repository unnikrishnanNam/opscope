package diagnose

import (
	"slices"
	"strings"
	"testing"
)

// The edge cases in test/scenarios/gateway-api and node-down, captured from a
// two-node kind cluster with Gateway API but no controller, seven minutes
// after its worker was stopped: the stranded pods still said Running, and
// their replacements couldn't be scheduled.
var edgeRoots = map[string][]root{
	"deployments/opscope-node-down/pinned": {
		{"node.not-ready", "nodes/opscope-g7-worker", "stopped reporting"},
	},
	"httproutes/opscope-edge/shop": {
		{"missing", "services/opscope-edge/api", "doesn't exist"},
		{"node.not-ready", "nodes/opscope-g7-worker", "stopped reporting"}, // web's pods ran there
	},
	"httproutes/opscope-edge/catalog": {
		{"missing", "referencegrants/opscope-backends/(none)", "lets HTTPRoute opscope-edge/catalog reach Service catalog"},
		{"node.not-ready", "nodes/opscope-g7-worker", "stopped reporting"},
	},
	"gateways/opscope-edge/edge": {
		{"gatewayclass.accepted", "gatewayclasses/opscope-nobody", "No controller has accepted it"},
	},
}

func TestEdgeCases(t *testing.T) {
	d := Run(loadCapture(t, "testdata/edge-cases"))
	for id, want := range edgeRoots {
		t.Run(id, func(t *testing.T) {
			trace, ok := d.Trace(id)
			if !ok {
				t.Fatal("not in the graph")
			}
			// A root can be reached along two paths (the pods stranded on
			// the node, and their replacements that can't be placed); count
			// it once.
			var got []*TraceStep
			for _, r := range faults(roots(trace.Steps)) {
				if !slices.ContainsFunc(got, func(g *TraceStep) bool { return g.ID == r.ID }) {
					got = append(got, r)
				}
			}
			var titles []string
			for _, g := range got {
				titles = append(titles, g.Rule+" on "+g.Object+": "+g.Title)
			}
			for _, w := range want {
				if !slices.ContainsFunc(got, func(g *TraceStep) bool {
					return g.Rule == w.rule && g.Object == w.object && strings.Contains(g.Title, w.title)
				}) {
					t.Errorf("no root %s on %s %q; got %q\n%s", w.rule, w.object, w.title, titles, render(trace.Steps, "  "))
				}
			}
			if len(got) != len(want) {
				t.Errorf("got %d root causes, want %d: %q", len(got), len(want), titles)
			}
		})
	}

	// The route with its ReferenceGrant is fine, apart from where its
	// backend's pods ran.
	trace, _ := d.Trace("httproutes/opscope-edge/reviews")
	for _, r := range faults(roots(trace.Steps)) {
		if r.Rule != "node.not-ready" {
			t.Errorf("reviews: %s %q", r.Rule, r.Title)
		}
	}

	// The pods Kubernetes couldn't place lead to the node too: its taint
	// says it's down, and the selector allows no other.
	pending := d.Findings("pods/opscope-node-down/pinned-5b84465d4-dc5vs")
	if len(pending) != 1 || !slices.Equal(pending[0].Blames, []string{"nodes/opscope-g7-worker"}) ||
		!strings.Contains(pending[0].Detail, "opscope-g7-worker (node.kubernetes.io/unreachable: it's down)") {
		t.Errorf("pending: %+v", pending)
	}
}
