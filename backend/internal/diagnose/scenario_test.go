package diagnose

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"opscope/internal/snapshot"
)

// loadScenarios reads the objects captured from the kind cluster running
// test/scenarios/, as they were at the time in testdata/scenarios/taken.
func loadScenarios(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	data, err := os.ReadFile("testdata/scenarios/objects.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := snapshot.FromYAML(data)
	if err != nil {
		t.Fatal(err)
	}
	taken, err := os.ReadFile("testdata/scenarios/taken")
	if err != nil {
		t.Fatal(err)
	}
	if s.Taken, err = time.Parse(time.RFC3339, strings.TrimSpace(string(taken))); err != nil {
		t.Fatal(err)
	}
	return s
}

// render writes a trace as indented text, as the detail page shows it.
func render(steps []*TraceStep, indent string) string {
	var b strings.Builder
	for _, step := range steps {
		objects := step.Object
		if len(step.Objects) > 1 {
			objects = fmt.Sprintf("%d objects", len(step.Objects))
		}
		root := ""
		if step.Root {
			root = "  [root cause]"
		}
		fmt.Fprintf(&b, "%s%s %s: %s (%s, %s)%s\n", indent, step.Severity, objects, step.Title, step.Rule, step.Confidence, root)
		if step.Detail != "" {
			fmt.Fprintf(&b, "%s    %s\n", indent, step.Detail)
		}
		b.WriteString(render(step.Causes, indent+"  "))
	}
	return b.String()
}

// root is a root cause a trace must reach: the rule, the object it's about
// (a prefix, since pod names end in a random suffix) and words of its title.
type root struct{ rule, object, title string }

// scenarioRoots are the root causes of each broken object in
// test/scenarios/, as captured in testdata/scenarios/.
var scenarioRoots = map[string][]root{
	"deployments/opscope-scenarios/missing-secret":       {{"missing", "secrets/opscope-scenarios/stripe-key", "doesn't exist"}},
	"deployments/opscope-scenarios/missing-configmap":    {{"missing", "configmaps/opscope-scenarios/app-config", "doesn't exist"}},
	"deployments/opscope-scenarios/missing-key":          {{"pod.config", "pods/opscope-scenarios/missing-key-", "has no key DATABASE_URL"}},
	"deployments/opscope-scenarios/crash-loop":           {{"pod.crash", "pods/opscope-scenarios/crash-loop-", "exits with code 1"}},
	"deployments/opscope-scenarios/oom":                  {{"pod.crash", "pods/opscope-scenarios/oom-", "runs out of memory"}},
	"deployments/opscope-scenarios/liveness":             {{"pod.crash", "pods/opscope-scenarios/liveness-", "fails its liveness probe"}},
	"deployments/opscope-scenarios/bad-image":            {{"pod.image", "pods/opscope-scenarios/bad-image-", "doesn't exist"}},
	"deployments/opscope-scenarios/unreachable-registry": {{"pod.image", "pods/opscope-scenarios/unreachable-registry-", "registry.opscope.invalid can't be reached"}},
	"deployments/opscope-scenarios/unschedulable":        {{"pod.unschedulable", "pods/opscope-scenarios/unschedulable-", "No node can run it"}},
	"deployments/opscope-scenarios/missing-account":      {{"missing", "serviceaccounts/opscope-scenarios/ghost", "doesn't exist"}},
	"deployments/opscope-scenarios/over-quota":           {{"create-failed", "replicasets/opscope-scenarios/over-quota-", "ResourceQuota memory"}},
	"services/opscope-scenarios/wrong-selector":          {{"service.selector", "services/opscope-scenarios/wrong-selector", "matches no pods"}},
	"services/opscope-scenarios/crash-loop":              {{"pod.crash", "pods/opscope-scenarios/crash-loop-", "exits with code 1"}},
	"ingresses/opscope-scenarios/shop": {
		{"missing", "services/opscope-scenarios/api", "doesn't exist"},
		{"pod.crash", "pods/opscope-scenarios/crash-loop-", "exits with code 1"},
	},
}

func TestScenarios(t *testing.T) {
	d := Run(loadScenarios(t))
	for id, want := range scenarioRoots {
		t.Run(id, func(t *testing.T) {
			trace, ok := d.Trace(id)
			if !ok {
				t.Fatal("not in the graph")
			}
			got := roots(trace.Steps)
			if len(got) != len(want) {
				t.Errorf("got %d root causes, want %d:\n%s", len(got), len(want), render(trace.Steps, "  "))
				return
			}
			for i, w := range want {
				g := got[i]
				if g.Rule != w.rule || !strings.HasPrefix(g.Object, w.object) || !strings.Contains(g.Title, w.title) {
					t.Errorf("root %d: got %s on %s: %q, want %s on %s…: %q\n%s", i, g.Rule, g.Object, g.Title, w.rule, w.object, w.title, render(trace.Steps, "  "))
				}
			}
		})
	}
}

// Every scenario's Deployment is explained, so no failing workload is left
// without a root cause.
func TestEveryScenarioIsExplained(t *testing.T) {
	d := Run(loadScenarios(t))
	for _, n := range d.g.Nodes() {
		if n.Namespace != "opscope-scenarios" || n.Resource != "deployments" {
			continue
		}
		if _, listed := scenarioRoots[n.ID]; !listed {
			t.Errorf("%s isn't in scenarioRoots", n.ID)
		}
	}
}

// The missing Service is a root cause, so its page says what it breaks.
func TestScenarioImpact(t *testing.T) {
	d := Run(loadScenarios(t))
	trace, _ := d.Trace("secrets/opscope-scenarios/stripe-key")
	var impact []string
	for _, n := range trace.Impact {
		impact = append(impact, n.ID)
	}
	// The pod is left out: its Deployment says it.
	if strings.Join(impact, ",") != "deployments/opscope-scenarios/missing-secret" {
		t.Errorf("impact = %v", impact)
	}
}

// roots collects a trace's root causes, sorted by rule then object.
func roots(steps []*TraceStep) []*TraceStep {
	var found []*TraceStep
	for _, step := range steps {
		if step.Root {
			found = append(found, step)
		}
		found = append(found, roots(step.Causes)...)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Rule != found[j].Rule {
			return found[i].Rule < found[j].Rule
		}
		return found[i].Object < found[j].Object
	})
	return found
}

// TestPrintScenarios prints every scenario's trace as the page would show
// it, for writing scenarioRoots or checking the wording. From backend/:
//
//	PRINT=1 go test -count=1 -v -run TestPrintScenarios ./internal/diagnose/
func TestPrintScenarios(t *testing.T) {
	if os.Getenv("PRINT") == "" {
		t.Skip()
	}
	d := Run(loadScenarios(t))
	for _, n := range d.g.Nodes() {
		if n.Namespace != "opscope-scenarios" || (n.Resource != "deployments" && n.Resource != "services" && n.Resource != "ingresses") {
			continue
		}
		trace, _ := d.Trace(n.ID)
		fmt.Printf("== %s (%s %s)\n%s", n.ID, n.State, n.Status, render(trace.Steps, "  "))
		for _, i := range trace.Impact {
			fmt.Printf("  affects %s\n", i.ID)
		}
	}
}
