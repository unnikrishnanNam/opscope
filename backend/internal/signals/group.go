package signals

import (
	"cmp"
	"slices"
	"time"
)

// Group is one row of the Issues feed: one signal, or the signals of one
// type on the pods of one workload ("crash_loop · 3 pods of Deployment
// web"). Grouping is for display only: the engine keeps every signal apart.
type Group struct {
	ID        string    `json:"id"`    // the signal's ID; "group:<owner UID>:<type>" for several
	Type      string    `json:"type"`  // from the vocabulary
	Title     string    `json:"title"` // the type in a few words: "Crash loop"
	Severity  string    `json:"severity"`
	State     string    `json:"state"`           // open if any of its signals is open or resolving
	Owner     *Ref      `json:"owner,omitempty"` // the workload, for a group of its pods
	FirstSeen time.Time `json:"firstSeen"`       // the earliest of its signals
	LastSeen  time.Time `json:"lastSeen"`
	Signals   []Signal  `json:"signals"`
}

// GroupSignals groups pods' signals of one type under the workload that
// owns them, when there's more than one; every other signal is a group of
// its own. Groups come worst first, then newest, then by ID.
func GroupSignals(signals []Signal) []Group {
	var groups []Group
	byOwner := map[string]int{} // "owner UID/type" → index in groups
	for _, s := range signals {
		t, _ := typeOf(s.Type)
		if s.Resource.Resource == "pods" && s.Owner != nil {
			key := s.Owner.UID + "/" + s.Type + "/" + stateOf(s)
			if i, ok := byOwner[key]; ok {
				groups[i].Signals = append(groups[i].Signals, s)
				continue
			}
			byOwner[key] = len(groups)
		}
		groups = append(groups, Group{ID: s.ID, Type: s.Type, Title: t.Title, Owner: s.Owner, Signals: []Signal{s}})
	}
	for i := range groups {
		g := &groups[i]
		if len(g.Signals) > 1 {
			g.ID = "group:" + g.Owner.UID + ":" + g.Type
			if stateOf(g.Signals[0]) == Resolved {
				g.ID += ":resolved"
			}
		} else {
			g.Owner = nil // one signal names its own object
		}
		g.State = Resolved
		for _, s := range g.Signals {
			if severityRank(s.Severity) > severityRank(g.Severity) {
				g.Severity = s.Severity
			}
			if g.FirstSeen.IsZero() || s.FirstSeen.Before(g.FirstSeen) {
				g.FirstSeen = s.FirstSeen
			}
			if s.LastSeen.After(g.LastSeen) {
				g.LastSeen = s.LastSeen
			}
			if s.State != Resolved {
				g.State = Open
			}
		}
		slices.SortFunc(g.Signals, func(a, b Signal) int { return cmp.Compare(a.Resource.Name, b.Resource.Name) })
	}
	slices.SortFunc(groups, func(a, b Group) int {
		return cmp.Or(
			cmp.Compare(severityRank(b.Severity), severityRank(a.Severity)),
			b.FirstSeen.Compare(a.FirstSeen),
			cmp.Compare(a.ID, b.ID))
	})
	return groups
}

// stateOf puts open and resolving together: a group of a workload's pods
// is open while any is.
func stateOf(s Signal) string {
	if s.State == Resolved {
		return Resolved
	}
	return Open
}
