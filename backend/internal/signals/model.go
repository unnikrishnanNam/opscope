// Package signals is the Signal engine: it turns objects' statuses,
// conditions and events into signals, one problem with one object each in
// a fixed vocabulary, and works out each object's health from them. See
// docs/design/issues.md.
//
// A signal is raised on the object that shows the symptom, and nothing is
// inferred from relations: a crash-looping pod has crash_loop, its
// Deployment has replicas_unavailable, and linking the two is diagnosis
// (v1.10). Every signal carries the fields, events or log patterns it was
// read from (contract 4), and its ID comes from its content (contract 3).
package signals

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

// Severities, from worst.
const (
	Critical = "critical" // it isn't working
	Warning  = "warning"  // working, but at risk or partly broken
	Notice   = "info"     // worth knowing; not a problem yet (severity "info")
)

// Signal states. A signal whose condition went away is resolving for a
// while (resolveAfter) before it's resolved, so a condition that flickers
// stays one signal.
const (
	Open      = "open"
	Resolving = "resolving"
	Resolved  = "resolved"
)

// Health states, from signals.
const (
	Healthy  = "healthy"
	Degraded = "degraded" // a warning signal is open
	Failing  = "failing"  // a critical signal is open
	Unknown  = "unknown"  // no detector reads its kind
)

// Ref names an object: by UID (contract 1), with its kind, namespace and
// name for display.
type Ref struct {
	UID       string `json:"uid"`
	Resource  string `json:"resource"` // URL name, e.g. "pods"
	Kind      string `json:"kind"`     // e.g. "Pod"
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Signal is one problem with one object.
type Signal struct {
	ID        string     `json:"id"`       // SignalID(cluster, uid, type)
	Type      string     `json:"type"`     // from the vocabulary: "crash_loop"
	Title     string     `json:"title"`    // the type in a few words: "Crash loop"
	Severity  string     `json:"severity"` // critical, warning, info
	Cluster   string     `json:"cluster"`
	Resource  Ref        `json:"resource"`
	Owner     *Ref       `json:"owner,omitempty"` // the workload that controls it, for grouping only
	Message   string     `json:"message"`         // plain words: "Restarted 14 times; last exit Error (exit 1)"
	FirstSeen time.Time  `json:"firstSeen"`       // when this occurrence started
	LastSeen  time.Time  `json:"lastSeen"`        // when a detector last found it
	Count     int64      `json:"count"`           // what the type counts: restarts, events; at least 1
	State     string     `json:"state"`           // open, resolving, resolved
	Resolved  *time.Time `json:"resolved,omitempty"`
	// Stale is set while the object's kind is behind a collector gap: the
	// signal stays open, but hasn't been checked again since.
	Stale    bool       `json:"stale,omitempty"`
	Evidence []Evidence `json:"evidence"`
}

// Evidence is one thing a signal was read from (contract 4). It never holds
// a Secret's value or a log line.
type Evidence struct {
	Source          string    `json:"source"`   // field, event, log or metric
	Detector        string    `json:"detector"` // e.g. "pods.containers"
	Field           string    `json:"field,omitempty"`
	Value           string    `json:"value,omitempty"`
	Event           *EventRef `json:"event,omitempty"`
	Pattern         string    `json:"pattern,omitempty"` // a log pattern's name, with Matches
	Matches         int       `json:"matches,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"` // of the object the field is in
	Seen            time.Time `json:"seen"`
}

// Evidence sources.
const (
	FromField  = "field"
	FromEvent  = "event"
	FromLog    = "log"
	FromMetric = "metric"
)

// EventRef is a Kubernetes event a signal was read from.
type EventRef struct {
	UID      string    `json:"uid"`
	Reason   string    `json:"reason"`
	Count    int32     `json:"count"`
	LastSeen time.Time `json:"lastSeen"`
}

// Health is an object's health, from its open signals.
type Health struct {
	State   string    `json:"state"`
	Since   time.Time `json:"since,omitzero"`    // when it last changed; left out if not since Opscope started
	Signals []string  `json:"signals,omitempty"` // IDs of the open signals behind it
}

// Transition is one change of an object's health.
type Transition struct {
	UID     string    `json:"uid"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	At      time.Time `json:"at"`
	Signals []string  `json:"signals,omitempty"` // what was open after it
}

// SignalID is derived from the signal's content (contract 3): one object
// has at most one signal of each type, and its ID stays the same across
// occurrences and restarts. Each part is length-prefixed, as for edge IDs.
func SignalID(cluster, uid, typ string) string {
	h := sha256.New()
	for _, part := range []string{cluster, uid, typ} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// severityRank orders severities, worst highest.
func severityRank(s string) int {
	switch s {
	case Critical:
		return 3
	case Warning:
		return 2
	case Notice:
		return 1
	}
	return 0
}
