// Package bus carries typed events between parts of Opscope inside the one
// process: the collectors publish what they see in each cluster, and
// anything that wants to follow the clusters (the browser's live updates,
// later the change log) subscribes.
//
// It is plain Go: one buffered channel per subscriber, no broker. Publish
// never blocks, so a slow subscriber can't hold up a collector. A subscriber
// that falls behind loses events instead, and is sent a CollectorGap saying
// so once it has room again; nothing downstream may assume it saw every
// change.
package bus

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
)

// Event is anything the bus carries: ObjectChanged or CollectorGap.
type Event interface {
	isEvent()
}

// ChangeType says what happened to an object.
type ChangeType string

const (
	Added   ChangeType = "Added"
	Updated ChangeType = "Updated"
	Deleted ChangeType = "Deleted"
)

// ObjectChanged is published for every add, update and delete a collector
// sees. The objects are the collector's cached copies, trimmed as the cache
// keeps them (Secrets without values, ...), and shared: don't change them.
type ObjectChanged struct {
	Cluster         string
	Resource        string // the URL name, e.g. "pods"
	Type            ChangeType
	UID             string
	Namespace       string
	Name            string
	ResourceVersion string
	Old             runtime.Object // nil for Added
	New             runtime.Object // nil for Deleted
	// Initial marks the objects of a collector's first list: the state it
	// started from, not a change that happened then.
	Initial bool
	Seen    time.Time // when Opscope saw it, not when it happened
}

// Reasons for a CollectorGap.
const (
	GapRelist      = "relist"            // a watch broke and the collector listed again
	GapUnreachable = "unreachable"       // the cluster couldn't be reached
	GapStopped     = "stopped"           // the collector was stopped
	GapLagged      = "subscriber_lagged" // this subscriber fell behind and lost events
)

// CollectorGap says that changes between From and To may have been
// collapsed or missed. Resource is "" for every kind, and Cluster is "" for
// every cluster (a lagging subscriber can have missed anything).
type CollectorGap struct {
	Cluster  string
	Resource string
	From     time.Time
	To       time.Time
	Reason   string
}

func (ObjectChanged) isEvent() {}
func (CollectorGap) isEvent()  {}

// Bus fans events out to subscribers. The zero value isn't usable; use New.
type Bus struct {
	mu   sync.RWMutex
	subs map[*Subscription]struct{}
}

func New() *Bus {
	return &Bus{subs: map[*Subscription]struct{}{}}
}

// Subscription is one subscriber's queue. Read events from C until it's
// closed, which happens after Close.
type Subscription struct {
	C <-chan Event

	bus *Bus
	c   chan Event

	mu       sync.Mutex
	lostFrom time.Time // when it started losing events; zero while it keeps up
}

// Subscribe adds a subscriber whose queue holds up to buffer events, and at
// least two: a gap is only ever delivered together with the next event.
func (b *Bus) Subscribe(buffer int) *Subscription {
	c := make(chan Event, max(buffer, 2))
	s := &Subscription{C: c, bus: b, c: c}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

// Close removes the subscriber and closes its channel. It's safe to call
// more than once.
func (s *Subscription) Close() {
	s.bus.mu.Lock()
	defer s.bus.mu.Unlock()
	if _, ok := s.bus.subs[s]; ok {
		delete(s.bus.subs, s)
		close(s.c)
	}
}

// Publish hands e to every subscriber that has room, without waiting.
func (b *Bus) Publish(e Event) {
	// The read lock keeps Close from closing a channel while it's being
	// sent on; sends never block, so holding it is short.
	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		s.offer(e)
	}
}

func (s *Subscription) offer(e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.lostFrom.IsZero() {
		// It lost events earlier: the gap goes first, and only together with
		// e. Sending the gap alone would fill the last slot and lose e, so a
		// reader that keeps pace one event at a time would never catch up.
		if cap(s.c)-len(s.c) < 2 {
			return // still no room for both; e is lost too, and the gap covers it
		}
		// Only offer sends on s.c, under s.mu, so the room can't shrink
		// between the check and these sends: they don't block.
		s.c <- CollectorGap{From: s.lostFrom, To: time.Now(), Reason: GapLagged}
		s.c <- e
		s.lostFrom = time.Time{}
		return
	}
	select {
	case s.c <- e:
	default:
		s.lostFrom = time.Now()
	}
}
