package signals

import (
	"cmp"
	"slices"
	"time"
)

// View reads one cluster's signals and health at one moment. It's only
// valid inside Engine.Read's function.
type View struct {
	w   *world
	now time.Time
}

// Filter narrows Signals. Empty fields match everything.
type Filter struct {
	Namespace string
	Severity  string
	Type      string
	Resource  string // the kind, by URL name
	UID       string // one object's
	State     string // open (open and resolving), resolved, or "" for both
}

func (f Filter) match(s *Signal) bool {
	switch {
	case f.Namespace != "" && s.Resource.Namespace != f.Namespace,
		f.Severity != "" && s.Severity != f.Severity,
		f.Type != "" && s.Type != f.Type,
		f.Resource != "" && s.Resource.Resource != f.Resource,
		f.UID != "" && s.Resource.UID != f.UID,
		f.State == Open && s.State == Resolved,
		f.State == Resolved && s.State != Resolved:
		return false
	}
	return true
}

// Signals returns copies of the signals that match: open before
// resolved, worst first, then newest first, then by ID, so the order never
// depends on map order.
func (v View) Signals(f Filter) []Signal {
	out := []Signal{}
	for id, s := range v.w.signals {
		if f.match(s) {
			c, _ := v.w.signal(id)
			out = append(out, c)
		}
	}
	slices.SortFunc(out, func(a, b Signal) int {
		resolved := func(s Signal) int {
			if s.State == Resolved {
				return 1
			}
			return 0
		}
		return cmp.Or(
			cmp.Compare(resolved(a), resolved(b)),
			cmp.Compare(severityRank(b.Severity), severityRank(a.Severity)),
			b.FirstSeen.Compare(a.FirstSeen),
			cmp.Compare(a.ID, b.ID))
	})
	return out
}

// Signal returns one signal by ID.
func (v View) Signal(id string) (Signal, bool) {
	return v.w.signal(id)
}

// Health is an object's health; ok is false when its kind has no
// detector or the object isn't known.
func (v View) Health(uid string) (Health, bool) {
	resource, exists := v.w.resourceOf[uid]
	if !exists || len(v.w.reg.For(resource)) == 0 {
		return Health{State: Unknown}, false
	}
	if h, ok := v.w.health[uid]; ok {
		h.Signals = slices.Clone(h.Signals)
		return h, true
	}
	return Health{State: Healthy}, true
}

// Transitions returns an object's health changes, oldest first; every
// object's when uid is "".
func (v View) Transitions(uid string) []Transition {
	out := []Transition{}
	for _, t := range v.w.history {
		if uid == "" || t.UID == uid {
			t.Signals = slices.Clone(t.Signals)
			out = append(out, t)
		}
	}
	return out
}

// Counts returns, per kind, how many objects have an open signal, in a
// namespace or all of them: the overview's "needs attention".
func (v View) Counts(namespace string) map[string]int {
	counts := map[string]int{}
	for uid := range v.w.byUID {
		resource := v.w.resourceOf[uid]
		obj := v.w.objects[resource][uid]
		if ref := refOf(resource, obj); namespace == "" || ref.Namespace == namespace {
			counts[resource]++
		}
	}
	return counts
}
