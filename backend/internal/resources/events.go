package resources

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// maxEvents caps how many events we return. Busy clusters can hold thousands;
// the UI only shows the most recent ones.
const maxEvents = 100

// Event is one row of the event list: something that happened to an object,
// like a failed image pull or a pod being scheduled.
type Event struct {
	Meta
	Type    string `json:"type"`   // Normal or Warning
	Reason  string `json:"reason"` // short CamelCase word, e.g. BackOff
	Message string `json:"message"`
	Object  string `json:"object"` // what it happened to, e.g. "Pod/web-7d9f"
	// The same object as parts, so the UI can link to it. ObjectResource is
	// empty for kinds Opscope has no page for (like ReplicaSet).
	ObjectName     string    `json:"objectName"`
	ObjectResource string    `json:"objectResource,omitempty"`
	Count          int32     `json:"count"` // how many times it happened
	LastSeen       time.Time `json:"lastSeen"`
}

// listEvents returns the newest events first. q.Type limits them to one type
// ("Warning" or "Normal").
func listEvents(ctx context.Context, src Source, q Query) ([]Event, error) {
	items, err := ListOf[*corev1.Event](ctx, src, "events", q)
	if err != nil {
		return nil, err
	}

	rows := make([]Event, 0, len(items))
	for _, e := range items {
		// Filtered here rather than with a field selector, so the cache and
		// the cluster give the same answer.
		if q.Type == "" || e.Type == q.Type {
			rows = append(rows, eventRow(e))
		}
	}

	sortEvents(rows)
	if len(rows) > maxEvents {
		rows = rows[:maxEvents]
	}
	return rows, nil
}

// sortEvents puts the newest first. Events seen at the same moment (a burst
// of them, often) go by namespace and name, so the order never depends on
// the order they were read in.
func sortEvents(rows []Event) {
	sort.Slice(rows, func(i, j int) bool {
		if !rows[i].LastSeen.Equal(rows[j].LastSeen) {
			return rows[i].LastSeen.After(rows[j].LastSeen)
		}
		return rows[i].sortKey() < rows[j].sortKey()
	})
}

// eventRow turns a Kubernetes event into a row.
func eventRow(e *corev1.Event) Event {
	return Event{
		Meta:           metaOf(e.ObjectMeta),
		Type:           e.Type,
		Reason:         e.Reason,
		Message:        e.Message,
		Object:         e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
		ObjectName:     e.InvolvedObject.Name,
		ObjectResource: resourceForKind(e.InvolvedObject.Kind),
		Count:          eventCount(e),
		LastSeen:       eventLastSeen(e),
	}
}

// Events have been reported in a few different ways over the years, so the
// "when" and "how many" can sit in different fields. We take the first one set.

func eventLastSeen(e *corev1.Event) time.Time {
	switch {
	case e.Series != nil && !e.Series.LastObservedTime.IsZero():
		return e.Series.LastObservedTime.Time
	case !e.LastTimestamp.IsZero():
		return e.LastTimestamp.Time
	case !e.EventTime.IsZero():
		return e.EventTime.Time
	default:
		return e.CreationTimestamp.Time
	}
}

func eventCount(e *corev1.Event) int32 {
	switch {
	case e.Series != nil && e.Series.Count > 0:
		return e.Series.Count
	case e.Count > 0:
		return e.Count
	default:
		return 1
	}
}
