// Package diagnose explains why objects fail.
//
// Rules look at one object at a time and return findings: what's wrong,
// the evidence for it, and which objects cause it ("blames"). A finding
// that blames nothing, or only objects that have no findings of their own,
// is a root cause. Following blames from an object's findings gives its
// trace: from the symptom down to the root causes.
//
// Rules name their culprit themselves. "Something this needs is unhealthy,
// so that's the cause" isn't used: it would blame a busy node for every crash
// loop on it.
//
// Everything is computed once per snapshot (see Of), so each request is a
// lookup.
package diagnose

import (
	"slices"
	"sort"
	"strings"
	"time"

	"opscope/internal/graph"
	"opscope/internal/snapshot"
)

// Severity is how bad a finding is.
type Severity string

const (
	Failing  Severity = "failing"
	Degraded Severity = "degraded"
	Info     Severity = "info" // worth knowing, not a fault: a rollout still under way, a kind Opscope can't read
)

var severityOrder = map[Severity]int{Failing: 0, Degraded: 1, Info: 2}

// Confidence says how a finding was reached.
type Confidence string

const (
	Certain  Confidence = "certain"  // from structured status: a container state, a condition, a missing object
	Likely   Confidence = "likely"   // from the text of an event or message, which can change between versions
	Possible Confidence = "possible" // a guess
)

// Evidence is one fact a finding rests on.
type Evidence struct {
	Source string     `json:"source"` // condition, container, event, spec, reference
	Text   string     `json:"text"`
	Time   *time.Time `json:"time,omitempty"`
	Count  int32      `json:"count,omitempty"` // for events: how many times it happened
}

// Step is a suggested next step. It is either a link to a page of Opscope
// (an object, and optionally one of its tabs) or a read-only kubectl
// command to copy. Fixes are described in a finding's Detail, never as a
// command.
type Step struct {
	Label   string `json:"label"`
	Object  string `json:"object,omitempty"`
	Tab     string `json:"tab,omitempty"`
	Query   string `json:"query,omitempty"` // more for the page's URL, e.g. "container=app&previous=true" for the logs before a restart
	Command string `json:"command,omitempty"`
}

// Finding is one thing wrong with one object.
type Finding struct {
	ID         string     `json:"id"` // stable: rule, object and blamed objects
	Rule       string     `json:"rule"`
	Object     string     `json:"object"` // the graph node it's about
	Severity   Severity   `json:"severity"`
	Confidence Confidence `json:"confidence"`
	Title      string     `json:"title"`
	Detail     string     `json:"detail,omitempty"`
	Evidence   []Evidence `json:"evidence"`
	Blames     []string   `json:"blames"`  // graph nodes that cause it
	Related    []string   `json:"related"` // unhealthy neighbours that may be involved, when no rule knows the cause
	Next       []Step     `json:"next"`
}

// Diagnosis holds every finding about one snapshot.
type Diagnosis struct {
	g        *graph.Graph
	byObject map[string][]*Finding // findings about each object
	blamedBy map[string][]*Finding // findings that blame each object
}

// Of returns the diagnosis of a snapshot, running the rules the first time.
func Of(s *snapshot.Snapshot) *Diagnosis {
	return s.Derived("diagnosis", func() any { return Run(s) }).(*Diagnosis)
}

// Run applies every rule to every object.
func Run(s *snapshot.Snapshot) *Diagnosis {
	g := graph.Of(s)
	c := &Context{S: s, G: g, Now: s.Taken}
	d := &Diagnosis{g: g, byObject: map[string][]*Finding{}, blamedBy: map[string][]*Finding{}}

	for _, n := range g.Nodes() {
		var found []*Finding
		switch n.State {
		case graph.Missing:
			found = []*Finding{missing(c, n)}
		case graph.Unknown:
			found = []*Finding{unknown(c, n)}
		default:
			for _, rule := range rulesFor[n.Resource] {
				for _, f := range rule.Check(c, n) {
					f.Rule = rule.ID
					found = append(found, f)
				}
			}
		}
		d.add(n, found)
	}
	// What no rule explains still gets a finding, marked as a guess.
	for _, n := range g.Nodes() {
		if (n.State == graph.Failing || n.State == graph.Degraded) && len(d.byObject[n.ID]) == 0 {
			d.add(n, []*Finding{unexplained(c, n)})
		}
	}
	return d
}

// add files findings under their object, completing what every finding has.
func (d *Diagnosis) add(n *graph.Node, found []*Finding) {
	for _, f := range found {
		f.Object = n.ID
		sort.Strings(f.Blames)
		f.ID = f.Rule + "|" + f.Object + "|" + strings.Join(f.Blames, ",")
		// Lists are always lists in the JSON, never null.
		for _, list := range []*[]string{&f.Blames, &f.Related} {
			if *list == nil {
				*list = []string{}
			}
		}
		if f.Evidence == nil {
			f.Evidence = []Evidence{}
		}
		if f.Next == nil {
			f.Next = []Step{}
		}
		d.byObject[n.ID] = append(d.byObject[n.ID], f)
		for _, blamed := range f.Blames {
			d.blamedBy[blamed] = append(d.blamedBy[blamed], f)
		}
	}
	sortFindings(d.byObject[n.ID])
}

// Findings returns the findings about one object.
func (d *Diagnosis) Findings(id string) []*Finding {
	return d.byObject[id]
}

// TraceStep is a finding in a trace, with the findings that cause it.
// Findings with the same rule, title and culprits are merged into one step:
// thirty pods failing the same way are one step that says "30 pods".
type TraceStep struct {
	*Finding
	Objects []string     `json:"objects"` // every object this step is about
	Root    bool         `json:"root"`    // nothing it blames has a finding: this is a root cause
	Causes  []*TraceStep `json:"causes"`
}

// Trace is an object's findings, each followed down to its root causes,
// and what the object's own problems affect.
type Trace struct {
	Object *graph.Node            `json:"object"`
	Steps  []*TraceStep           `json:"steps"`
	Impact []*graph.Node          `json:"impact"` // objects whose traces lead here
	Nodes  map[string]*graph.Node `json:"nodes"`  // every object the steps name, by ID, to show and link them
}

// maxDepth stops a trace that goes on too long; real ones are a few steps.
const maxDepth = 10

// Trace returns the trace of one object, or false when there's no such
// object in the graph.
func (d *Diagnosis) Trace(id string) (*Trace, bool) {
	n, ok := d.g.Node(id)
	if !ok {
		return nil, false
	}
	t := &Trace{
		Object: n,
		Steps:  d.steps(d.byObject[id], map[string]bool{id: true}, 0),
		Impact: d.impact(id),
		Nodes:  map[string]*graph.Node{},
	}
	d.collectNodes(t.Steps, t.Nodes)
	return t, true
}

// collectNodes adds every object the steps are about or blame.
func (d *Diagnosis) collectNodes(steps []*TraceStep, nodes map[string]*graph.Node) {
	for _, step := range steps {
		for _, id := range append(append(append([]string{}, step.Objects...), step.Blames...), step.Related...) {
			if n, ok := d.g.Node(id); ok {
				nodes[id] = n
			}
		}
		d.collectNodes(step.Causes, nodes)
	}
}

// steps turns findings into trace steps, merging alike ones, and follows
// each to the findings of what it blames. path holds the objects already on
// the way here, so a cycle can't loop.
func (d *Diagnosis) steps(findings []*Finding, path map[string]bool, depth int) []*TraceStep {
	steps := []*TraceStep{}
	byKey := map[string]*TraceStep{}
	for _, f := range findings {
		key := f.Rule + "|" + f.Title + "|" + strings.Join(f.Blames, ",")
		if step, ok := byKey[key]; ok {
			if !slices.Contains(step.Objects, f.Object) {
				step.Objects = append(step.Objects, f.Object)
			}
			continue
		}
		step := &TraceStep{Finding: f, Objects: []string{f.Object}, Causes: []*TraceStep{}}
		byKey[key] = step
		steps = append(steps, step)
	}

	for _, step := range steps {
		if depth < maxDepth {
			var causes []*Finding
			next := map[string]bool{}
			for id := range path {
				next[id] = true
			}
			for _, id := range step.Objects {
				next[id] = true
			}
			for _, blamed := range step.Blames {
				if !path[blamed] {
					causes = append(causes, d.byObject[blamed]...)
				}
			}
			step.Causes = d.steps(causes, next, depth+1)
		}
		step.Root = len(step.Causes) == 0
		sort.Strings(step.Objects)
	}
	sort.SliceStable(steps, func(i, j int) bool {
		return severityOrder[steps[i].Severity] < severityOrder[steps[j].Severity]
	})
	return steps
}

// impact lists the objects whose findings lead to this one, through any
// number of steps. Pods that belong to a workload already in the list are
// left out: "Deployment web" says it better than its 30 pods.
func (d *Diagnosis) impact(id string) []*graph.Node {
	seen := map[string]bool{id: true}
	queue := []string{id}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, f := range d.blamedBy[current] {
			if !seen[f.Object] {
				seen[f.Object] = true
				queue = append(queue, f.Object)
			}
		}
	}
	delete(seen, id)

	impact := []*graph.Node{}
	for other := range seen {
		if owner := topOwner(d.g, other); owner != other && seen[owner] {
			continue
		}
		n, _ := d.g.Node(other)
		impact = append(impact, n)
	}
	sort.Slice(impact, func(i, j int) bool { return impact[i].ID < impact[j].ID })
	return impact
}

func sortFindings(findings []*Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Severity != b.Severity {
			return severityOrder[a.Severity] < severityOrder[b.Severity]
		}
		return a.Title < b.Title
	})
}
