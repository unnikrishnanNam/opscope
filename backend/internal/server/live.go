package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"time"

	"opscope/internal/bus"
	"opscope/internal/clusters"
	"opscope/internal/collector"
)

// GET /api/clusters/{id}/live
//
// How far the cluster's collector is: per kind, whether its cache has
// synced, how many objects it holds and when it last saw a change. With
// collectors off (OPSCOPE_LIVE=false) it says "off".
func getLiveStatus(manager *clusters.Manager, live *collector.Set) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		status, ok := live.Status(cluster.ID)
		if !ok {
			// The manager knows the cluster but its collector isn't running,
			// which only happens for a moment while a cluster is added.
			writeError(w, http.StatusServiceUnavailable, "the collector for "+cluster.ID+" isn't running yet")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}

// How the live stream paces itself.
const (
	// changeEvery is the least time between two "changed" messages for one
	// resource: changes in between are gathered into the next one.
	changeEvery = time.Second
	// streamTick is how often the stream sends gathered changes and checks
	// whether the cluster's state or a kind's mode changed.
	streamTick = 250 * time.Millisecond
	// heartbeatEvery keeps proxies from closing a quiet stream, and lets the
	// browser notice one that died without closing.
	heartbeatEvery = 15 * time.Second
	// streamBuffer is how many bus events can wait for the stream. One that
	// falls further behind (thousands of objects listed at once) sends
	// "resync" instead, and the browser fetches everything on screen again.
	streamBuffer = 1024
	// streamRetry is how long the browser waits before reconnecting.
	streamRetry = 5 * time.Second
)

// liveFeed is what the stream needs from the collectors; *collector.Set,
// or a fake in tests.
type liveFeed interface {
	Enabled() bool
	Modes(id string) (collector.Status, bool)
	Subscribe(buffer int) *bus.Subscription
	Done() <-chan struct{}
}

// GET /api/clusters/{id}/live/stream
//
// Server-sent events that tell the browser when to fetch again. It never
// carries objects: the page asks for its list again, from the cache.
//
//	event: changed     {"resource":"pods","namespaces":["default"]}, at most
//	                   once a second per resource; no namespaces for
//	                   cluster-wide kinds
//	event: status      {"state":"live","kinds":[{"resource":"pods","mode":"watch"}]},
//	                   on connecting and whenever the state or a mode changes
//	event: resync      {}, when changes may have been missed: fetch everything
//	event: heartbeat   {}, every 15 seconds
//
// With collectors off it answers 204 No Content, which tells EventSource
// not to reconnect.
func streamLive(manager *clusters.Manager, live liveFeed) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		if !live.Enabled() {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		status, ok := live.Modes(cluster.ID)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "the collector for "+cluster.ID+" isn't running yet")
			return
		}

		// Subscribe before the first status, so nothing between the two is missed.
		sub := live.Subscribe(streamBuffer)
		defer sub.Close()

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Accel-Buffering", "no") // nginx (an Ingress) would hold messages back otherwise
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)

		send := func(event string, data any) bool {
			body, _ := json.Marshal(data)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); err != nil {
				return false
			}
			return rc.Flush() == nil
		}

		fmt.Fprintf(w, "retry: %d\n\n", streamRetry.Milliseconds())
		lastStatus := modesOf(status)
		if !send("status", lastStatus) {
			return
		}

		changes := newChangeGatherer(cluster.ID, changeEvery)
		tick := time.NewTicker(streamTick)
		defer tick.Stop()
		heartbeat := time.NewTicker(heartbeatEvery)
		defer heartbeat.Stop()

		for {
			select {
			case <-r.Context().Done(): // the browser went away
				return
			case <-live.Done(): // Opscope is stopping
				return
			case e, open := <-sub.C:
				if !open {
					return
				}
				if changes.add(e) && !send("resync", struct{}{}) {
					return
				}
			case now := <-tick.C:
				for _, c := range changes.due(now) {
					if !send("changed", c) {
						return
					}
				}
				status, ok := live.Modes(cluster.ID)
				if !ok {
					return // the cluster was removed
				}
				if next := modesOf(status); !reflect.DeepEqual(next, lastStatus) {
					lastStatus = next
					if !send("status", next) {
						return
					}
				}
			case <-heartbeat.C:
				if !send("heartbeat", struct{}{}) {
					return
				}
			}
		}
	}
}

// streamStatus is the stream's "status" message: the collector's status
// without counts, sync progress or errors, which change too often to send.
type streamStatus struct {
	State string       `json:"state"`
	Kinds []streamKind `json:"kinds"`
}

type streamKind struct {
	Resource string `json:"resource"`
	Mode     string `json:"mode"`
	Reason   string `json:"reason,omitempty"`
}

func modesOf(s collector.Status) streamStatus {
	out := streamStatus{State: s.State, Kinds: make([]streamKind, 0, len(s.Kinds))}
	for _, k := range s.Kinds {
		out.Kinds = append(out.Kinds, streamKind{Resource: k.Resource, Mode: k.Mode, Reason: k.Reason})
	}
	return out
}

// changed is the stream's "changed" message.
type changed struct {
	Resource   string   `json:"resource"`
	Namespaces []string `json:"namespaces,omitempty"` // sorted; none for cluster-wide kinds
}

// changeGatherer collects one cluster's changes per resource and hands
// each resource out at most once per `every`.
type changeGatherer struct {
	cluster  string
	every    time.Duration
	pending  map[string]map[string]bool // resource -> namespaces ("" for cluster-wide)
	lastSent map[string]time.Time
}

func newChangeGatherer(cluster string, every time.Duration) *changeGatherer {
	return &changeGatherer{cluster: cluster, every: every, pending: map[string]map[string]bool{}, lastSent: map[string]time.Time{}}
}

// add takes one bus event. It reports true when the subscriber lost events
// and the browser has to fetch everything again.
func (g *changeGatherer) add(e bus.Event) (resync bool) {
	switch e := e.(type) {
	case bus.ObjectChanged:
		if e.Cluster != g.cluster {
			return false
		}
		if g.pending[e.Resource] == nil {
			g.pending[e.Resource] = map[string]bool{}
		}
		g.pending[e.Resource][e.Namespace] = true
	case bus.CollectorGap:
		// Only a gap in what this stream received needs a refetch. Relists
		// publish what changed as ordinary changes, and an unreachable or
		// stopped cluster shows in the status.
		return e.Reason == bus.GapLagged
	}
	return false
}

// due returns the resources whose changes can be sent at now, in name
// order, and forgets them.
func (g *changeGatherer) due(now time.Time) []changed {
	var out []changed
	for resource, namespaces := range g.pending {
		if now.Sub(g.lastSent[resource]) < g.every {
			continue
		}
		c := changed{Resource: resource}
		for ns := range namespaces {
			if ns != "" {
				c.Namespaces = append(c.Namespaces, ns)
			}
		}
		slices.Sort(c.Namespaces)
		out = append(out, c)
		delete(g.pending, resource)
		g.lastSent[resource] = now
	}
	slices.SortFunc(out, func(a, b changed) int { return strings.Compare(a.Resource, b.Resource) })
	return out
}
