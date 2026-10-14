package diagnose

import (
	"sort"
	"strings"

	"opscope/internal/graph"
)

// Problem is a root cause, with everything whose trace leads to it.
// Alike root causes are one problem: thirty pods crashing the same way.
type Problem struct {
	*Finding
	Objects []string      `json:"objects"` // what the root cause is about
	Affects []*graph.Node `json:"affects"` // what it breaks; pods are left out when their workload is listed
}

// Problems lists the root causes in a namespace ("" for the whole cluster),
// the worst first, then those that affect the most. A problem belongs to a
// namespace when it, or anything it affects, is in it: a node that's down
// is a problem in every namespace with pods on it.
//
// Notes (info findings) aren't problems. A finding that blames only notes
// (a Secret Opscope can't check) is a root cause here.
func (d *Diagnosis) Problems(namespace string) []*Problem {
	byKey := map[string]*Problem{}
	var problems []*Problem
	for _, findings := range d.byObject {
		for _, f := range findings {
			if f.Severity == Info || !d.isRoot(f) {
				continue
			}
			key := f.Rule + "|" + f.Title + "|" + strings.Join(f.Blames, ",")
			p, ok := byKey[key]
			if !ok {
				p = &Problem{Finding: f}
				byKey[key] = p
				problems = append(problems, p)
			}
			p.Objects = append(p.Objects, f.Object)
		}
	}

	var shown []*Problem
	for _, p := range problems {
		sort.Strings(p.Objects)
		p.Finding = d.first(p) // the same finding whichever object came first in the map
		p.Affects = d.affects(p.Objects)
		if namespace == "" || p.touches(namespace, d.g) {
			shown = append(shown, p)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool {
		a, b := shown[i], shown[j]
		switch {
		case a.Severity != b.Severity:
			return severityOrder[a.Severity] < severityOrder[b.Severity]
		case len(a.Affects) != len(b.Affects):
			return len(a.Affects) > len(b.Affects)
		case a.Title != b.Title:
			return a.Title < b.Title
		}
		return a.Objects[0] < b.Objects[0]
	})
	return shown
}

// isRoot says whether nothing a finding blames has a problem of its own.
func (d *Diagnosis) isRoot(f *Finding) bool {
	for _, blamed := range f.Blames {
		for _, other := range d.byObject[blamed] {
			if other.Severity != Info {
				return false
			}
		}
	}
	return true
}

// first returns the problem's finding about its first object.
func (d *Diagnosis) first(p *Problem) *Finding {
	for _, f := range d.byObject[p.Objects[0]] {
		if f.Rule == p.Rule && f.Title == p.Title && strings.Join(f.Blames, ",") == strings.Join(p.Blames, ",") {
			return f
		}
	}
	return p.Finding
}

// affects joins what each object's problem affects, leaving out the
// objects themselves and pods whose workload is listed.
func (d *Diagnosis) affects(objects []string) []*graph.Node {
	ids := map[string]bool{}
	for _, id := range objects {
		for _, n := range d.impact(id) {
			ids[n.ID] = true
		}
	}
	for _, id := range objects {
		delete(ids, id)
	}
	affects := []*graph.Node{}
	for id := range ids {
		if owner := topOwner(d.g, id); owner != id && ids[owner] {
			continue
		}
		n, _ := d.g.Node(id)
		affects = append(affects, n)
	}
	sort.Slice(affects, func(i, j int) bool { return affects[i].ID < affects[j].ID })
	return affects
}

func (p *Problem) touches(namespace string, g *graph.Graph) bool {
	for _, id := range p.Objects {
		if n, ok := g.Node(id); ok && n.Namespace == namespace {
			return true
		}
	}
	for _, n := range p.Affects {
		if n.Namespace == namespace {
			return true
		}
	}
	return false
}

// Nodes returns every object a set of problems names, by ID, so a page can
// show and link them.
func (d *Diagnosis) Nodes(problems []*Problem) map[string]*graph.Node {
	nodes := map[string]*graph.Node{}
	for _, p := range problems {
		for _, id := range append(append(append([]string{}, p.Objects...), p.Blames...), p.Related...) {
			if n, ok := d.g.Node(id); ok {
				nodes[id] = n
			}
		}
		for _, n := range p.Affects {
			nodes[n.ID] = n
		}
	}
	return nodes
}
