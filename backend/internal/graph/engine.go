package graph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/bus"
	"opscope/internal/collector"
	"opscope/internal/resources"
)

// Feed is where the engine reads clusters from: the collectors
// (collector.Set). Tests use a fake.
type Feed interface {
	// Subscribe follows every collector's events.
	Subscribe(buffer int) *bus.Subscription
	// Modes says how each kind of a cluster is read; ok is false for a
	// cluster with no collector.
	Modes(cluster string) (status collector.Status, ok bool)
	// Cached is one kind's cache, when it's read from the cache and has
	// its first list.
	Cached(cluster, resource string) (objects []runtime.Object, ok bool)
}

// How a graph was built, as answers say.
const (
	BuiltLive      = "live"       // kept up to date from the cache and the bus
	BuiltOnRequest = "on_request" // built from a Source for this request, and kept for a few seconds
)

// onRequestFor is how long a graph built on request is reused: as long as
// pages that aren't live wait between reads.
const onRequestFor = 10 * time.Second

// subscriptionBuffer holds a large cluster's first list, which arrives all
// at once. A subscriber that still falls behind is sent a gap, and the
// engine reads every kind again from the cache.
const subscriptionBuffer = 1 << 16

// Info says how the graph a query read was made, and what it couldn't see.
type Info struct {
	Built   string        `json:"built"`
	Partial []PartialKind `json:"partial"`
}

// PartialKind is a kind the graph couldn't read. References to its objects
// are Unknown nodes. Code says why, for a program, with the name index's
// codes: not_installed (the cluster doesn't have the kind: Gateway API) or
// forbidden (this user isn't allowed to list it). Reason says it in words.
type PartialKind struct {
	Resource string `json:"resource"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
}

// PartialKind codes.
const (
	PartialNotInstalled = "not_installed"
	PartialForbidden    = "forbidden"
)

// Engine keeps one graph per cluster, up to date from the bus, and builds
// graphs on request for clusters it can't keep that way.
//
// A cluster's graph is kept live when every kind the graph holds is in the
// cache (watch or poll) with its first list, or isn't: forbidden, not
// installed, or read per namespace (direct). Those kinds are left out and
// named, and references to their objects are Unknown. That way a default
// install, which can't list Secrets, still has a live graph that shows
// which pods use which Secret, read from the pods.
//
// Queries build a graph from the pages' own Source instead, so they see
// what the pages see, when live data is off, the cluster can't be reached,
// a kind hasn't been probed yet or is on its first list, or pods are read
// per namespace: a live graph without pods would show next to nothing.
type Engine struct {
	feed   Feed
	sub    *bus.Subscription
	logger *slog.Logger
	now    func() time.Time

	mu        sync.Mutex
	clusters  map[string]*liveGraph
	requested map[string]*requestedGraph // by cluster and namespace
}

type liveGraph struct {
	mu       sync.RWMutex
	g        *Graph
	stale    map[string]bool // kinds to read again from the cache: a gap said changes may be missing
	complete bool            // every kind is cached, or can't be read at all
	partial  []PartialKind
}

type requestedGraph struct {
	used time.Time // when it was last asked for; under Engine.mu

	mu    sync.Mutex // held while it's built
	g     *Graph
	info  Info
	built time.Time
}

// NewEngine subscribes to the feed at once, so call it before collectors
// start: their first lists then reach the engine too. (If they don't, a
// cluster's graph is read from its cache when it's first needed.)
func NewEngine(feed Feed, logger *slog.Logger) *Engine {
	return &Engine{
		feed:      feed,
		sub:       feed.Subscribe(subscriptionBuffer),
		logger:    logger,
		now:       time.Now,
		clusters:  map[string]*liveGraph{},
		requested: map[string]*requestedGraph{},
	}
}

// Run applies events until ctx ends. Every second it also checks each
// cluster's kinds, so a kind whose mode changed (a permission granted,
// Gateway API installed) is read in.
func (e *Engine) Run(ctx context.Context) {
	defer e.sub.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-e.sub.C:
			if !ok {
				return
			}
			e.handle(ev)
		case <-ticker.C:
			e.syncAll()
		}
	}
}

func (e *Engine) handle(ev bus.Event) {
	switch v := ev.(type) {
	case bus.ObjectChanged:
		if !slices.Contains(NodeKinds, v.Resource) {
			return
		}
		l := e.live(v.Cluster)
		if l == nil {
			return
		}
		l.mu.Lock()
		if v.Type == bus.Deleted {
			l.g.Delete(v.UID)
		} else if v.New != nil {
			l.g.Upsert(Object{Resource: v.Resource, Object: v.New, Seen: v.Seen})
		}
		l.mu.Unlock()

	case bus.CollectorGap:
		if v.Reason == bus.GapStopped {
			e.drop(v.Cluster)
			return
		}
		e.mu.Lock()
		affected := map[string]*liveGraph{}
		for id, l := range e.clusters {
			if v.Cluster == "" || v.Cluster == id {
				affected[id] = l
			}
		}
		e.mu.Unlock()
		for id, l := range affected {
			l.mu.Lock()
			for _, k := range NodeKinds {
				if v.Resource == "" || v.Resource == k {
					l.stale[k] = true
				}
			}
			e.sync(id, l)
			l.mu.Unlock()
		}
	}
}

// live returns a cluster's live graph, making it (read from the cache)
// the first time. nil means the cluster has no collector.
func (e *Engine) live(cluster string) *liveGraph {
	e.mu.Lock()
	l, ok := e.clusters[cluster]
	if !ok {
		if status, found := e.feed.Modes(cluster); !found || !status.Enabled {
			e.mu.Unlock()
			return nil
		}
		l = &liveGraph{g: New([]string{}), stale: map[string]bool{}}
		e.clusters[cluster] = l
		l.mu.Lock() // before e.mu is let go, so nobody reads it unsynced
		e.mu.Unlock()
		e.sync(cluster, l)
		l.mu.Unlock()
		return l
	}
	e.mu.Unlock()
	return l
}

// drop forgets a cluster whose collector stopped. If another collector
// starts for it, the graph is read from that one's cache when it's next
// needed.
func (e *Engine) drop(cluster string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.clusters, cluster)
	for key := range e.requested {
		if strings.HasPrefix(key, cluster+"\x00") {
			delete(e.requested, key)
		}
	}
}

func (e *Engine) syncAll() {
	e.mu.Lock()
	clusters := maps.Clone(e.clusters)
	e.mu.Unlock()
	for id, l := range clusters {
		l.mu.Lock()
		e.sync(id, l)
		l.mu.Unlock()
	}
}

// sync brings a cluster's graph in line with how its kinds are read now:
// a kind newly in the cache, or marked stale by a gap, is read from the
// cache in full; one that can't be read loses its objects. Call it with
// l.mu held.
func (e *Engine) sync(cluster string, l *liveGraph) {
	status, ok := e.feed.Modes(cluster)
	if !ok || !status.Enabled {
		l.complete = false
		return
	}
	modes := map[string]collector.KindStatus{}
	for _, k := range status.Kinds {
		modes[k.Resource] = k
	}
	complete := status.State != "unreachable"
	var partial []PartialKind
	for _, resource := range NodeKinds {
		k, known := modes[resource]
		switch {
		case known && (k.Mode == collector.ModeWatch || k.Mode == collector.ModePoll) && k.Synced:
			if !l.g.Readable(resource) || l.stale[resource] {
				objects, ok := e.feed.Cached(cluster, resource)
				if !ok {
					complete = false // it changed mode since Modes; the next sync gets it
					continue
				}
				l.g.SetReadable(resource, true)
				l.g.Replace(resource, objectsOf(resource, objects, e.now()))
				delete(l.stale, resource)
			}
		case known && (k.Mode == collector.ModeForbidden || k.Mode == collector.ModeNotInstalled ||
			k.Mode == collector.ModeDirect && resource != "pods"):
			l.g.SetReadable(resource, false)
			delete(l.stale, resource)
			partial = append(partial, PartialKind{Resource: resource, Code: codeOf(k), Reason: reasonOf(k)})
		default: // pending, pods read per namespace, or a cache still on its first list
			l.g.SetReadable(resource, false)
			complete = false
		}
	}
	l.complete, l.partial = complete, partial
}

func codeOf(k collector.KindStatus) string {
	if k.Mode == collector.ModeNotInstalled {
		return PartialNotInstalled
	}
	return PartialForbidden // forbidden, or only per namespace (direct)
}

func reasonOf(k collector.KindStatus) string {
	if k.Reason != "" {
		return k.Reason
	}
	if k.Mode == collector.ModeNotInstalled {
		return "not installed"
	}
	return "not allowed"
}

func objectsOf(resource string, objects []runtime.Object, seen time.Time) []Object {
	out := make([]Object, len(objects))
	for i, obj := range objects {
		out[i] = Object{Resource: resource, Object: obj, Seen: seen}
	}
	return out
}

// Read runs fn on a cluster's graph. It's the live graph when the cluster
// has one that's complete; otherwise a graph built from src (the pages'
// Source) for namespace ("" for all) and the cluster-wide kinds, reused for
// a few seconds. fn mustn't keep the graph: it can change once fn returns.
func (e *Engine) Read(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(*Graph, Info) error) error {
	if l := e.live(cluster); l != nil {
		l.mu.RLock()
		if l.complete {
			defer l.mu.RUnlock()
			return fn(l.g, Info{Built: BuiltLive, Partial: slices.Clone(l.partial)})
		}
		l.mu.RUnlock()
	}
	return e.onRequest(ctx, cluster, src, namespace, fn)
}

func (e *Engine) onRequest(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(*Graph, Info) error) error {
	key := cluster + "\x00" + namespace
	now := e.now()
	e.mu.Lock()
	for k, r := range e.requested {
		if now.Sub(r.used) > time.Minute {
			delete(e.requested, k) // nobody has asked for a minute
		}
	}
	r, ok := e.requested[key]
	if !ok {
		r = &requestedGraph{}
		e.requested[key] = r
	}
	r.used = now
	e.mu.Unlock()

	// One build at a time per cluster and namespace; whoever waits gets it.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.g == nil || now.Sub(r.built) >= onRequestFor {
		g, info, err := buildFrom(ctx, src, namespace)
		if err != nil {
			return err
		}
		r.g, r.info, r.built = g, info, e.now()
	}
	return fn(r.g, Info{Built: r.info.Built, Partial: slices.Clone(r.info.Partial)})
}

// buildFrom lists every kind the graph holds from src, in parallel, and
// builds a graph of them. A kind that can't be read (forbidden, not
// installed) is left out and named in the answer; any other error fails.
func buildFrom(ctx context.Context, src resources.Source, namespace string) (*Graph, Info, error) {
	type result struct {
		objects []runtime.Object
		err     error
	}
	results := make([]result, len(NodeKinds))
	var wg sync.WaitGroup
	for i, resource := range NodeKinds {
		wg.Go(func() {
			objects, err := src.List(ctx, resource, resources.Query{Namespace: namespace})
			results[i] = result{objects, err}
		})
	}
	wg.Wait()

	info := Info{Built: BuiltOnRequest}
	var readable []string
	var objects []Object
	seen := time.Now()
	for i, resource := range NodeKinds {
		switch err := results[i].err; {
		case err == nil:
			readable = append(readable, resource)
			objects = append(objects, objectsOf(resource, results[i].objects, seen)...)
		case apierrors.IsForbidden(err):
			info.Partial = append(info.Partial, PartialKind{Resource: resource, Code: PartialForbidden, Reason: "not allowed"})
		case apierrors.IsNotFound(err), errors.Is(err, resources.ErrGatewayAPINotInstalled):
			info.Partial = append(info.Partial, PartialKind{Resource: resource, Code: PartialNotInstalled, Reason: "not installed"})
		default:
			return nil, Info{}, fmt.Errorf("reading %s: %w", resource, err)
		}
	}
	if readable == nil {
		readable = []string{}
	}
	return Build(readable, objects), info, nil
}
