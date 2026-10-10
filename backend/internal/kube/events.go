package kube

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Events have been reported in a few different ways over the years, so the
// "when" and "how many" can sit in different fields. We take the first one set.

// EventLastSeen is when an event last happened.
func EventLastSeen(e *corev1.Event) time.Time {
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

// EventCount is how many times an event happened.
func EventCount(e *corev1.Event) int32 {
	switch {
	case e.Series != nil && e.Series.Count > 0:
		return e.Series.Count
	case e.Count > 0:
		return e.Count
	default:
		return 1
	}
}
