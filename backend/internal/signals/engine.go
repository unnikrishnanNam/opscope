package signals

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/bus"
	"opscope/internal/collector"
	"opscope/internal/resources"
	"opscope/internal/signals/logs"
)

// Feed is where the engine reads clusters from and publishes to: the
// collectors (collector.Set). Tests use a fake.
type Feed interface {
	Subscribe(buffer int) *bus.Subscription
	Publish(e bus.Event)
	Modes(cluster string) (status collector.Status, ok bool)
	Cached(cluster, resource string) (objects []runtime.Object, ok bool)
}

// How an answer was made.
const (
	BuiltLive      = "live"       // kept up to date from the cache and the bus, with history
	BuiltOnRequest = "on_request" // worked out from a Source for this answer: no history
)

const (
	onRequestFor       = 10 * time.Second
	subscriptionBuffer = 1 << 16
)

// Info says how an answer was made and what it couldn't see.
type Info struct {
	Built string `json:"built"`
	// Partial lists the kinds the detectors couldn't read, and why.
	Partial []PartialKind `json:"partial"`
	// HistorySince is how far back signals and health go: when the engine
	// first read the cluster. Zero for an answer built on request.
	HistorySince time.Time `json:"historySince,omitzero"`
}

// PartialKind is a kind the engine couldn't read, with the name index's
// codes: not_installed or forbidden.
type PartialKind struct {
	Resource string `json:"resource"`
	Code     string `json:"code"`
	Reason   string `json:"reason"`
}

// Engine keeps one world per cluster, up to date from the bus, as the
// graph engine does, and works answers out on request for clusters it
// can't keep that way (live data off, a kind not cached yet).
type Engine struct {
	feed   Feed
	reg    *Registry
	sub    *bus.Subscription
	logger *slog.Logger
	now    func() time.Time

	mu        sync.Mutex
	clusters  map[string]*liveWorld
	requested map[string]*requestedWorld

	// health is cluster → *clusterHealth, for readers that mustn't wait on
	// a world's lock (the graph, from inside its own).
	health sync.Map

	// fills reads how full each claim's volume is; nil while volume usage
	// is off (see SetVolumeUsage).
	fills func(ctx context.Context, cluster string) (map[string]Fill, error)

	// readLog opens a container's log; nil while log signals are off (see
	// SetLogReader). logRead is when each container's log was last read.
	readLog LogReader
	logRead map[string]time.Time
}

// LogReader opens the last logs.MaxLines lines of a container's log, its
// previous run's when previous is set. The engine closes it.
type LogReader func(ctx context.Context, cluster, namespace, pod, container string, previous bool) (io.ReadCloser, error)

// Limits on reading logs (docs/design/issues.md, "Logs").
const (
	logScanEvery = 10 * time.Second // how often the engine looks for logs to read
	logEvery     = 5 * time.Minute  // the same container's log at most this often
	logParallel  = 5                // reads at once, per cluster
	logTimeout   = 10 * time.Second
)

// SetLogReader turns on log signals: the logs of containers whose pods
// crash-loop or fail their probes are read, a few lines at a time, matched
// against known patterns, and dropped (contract 11). Call it before Run.
// Off (OPSCOPE_LOG_SIGNALS=false), no log is ever read.
func (e *Engine) SetLogReader(fn LogReader) {
	e.readLog = fn
}

// fillsEvery is how often volume usage is read: the kubelet measures
// volumes about once a minute.
const fillsEvery = time.Minute

// SetVolumeUsage gives the engine where volume usage comes from, keyed
// "namespace/claim". Call it before Run, and only when volume usage is on
// (OPSCOPE_VOLUME_USAGE): reading it calls every node's kubelet.
func (e *Engine) SetVolumeUsage(fn func(ctx context.Context, cluster string) (map[string]Fill, error)) {
	e.fills = fn
}

// clusterHealth is a live world's health as other engines read it: the
// objects whose health isn't simply "healthy since Opscope first saw them",
// and whether the world is complete, without which nobody can say an
// object without an entry is healthy.
type clusterHealth struct {
	changed  *sync.Map // UID → *Health
	complete atomic.Bool
}

type liveWorld struct {
	mu       sync.Mutex
	w        *world
	complete bool
	partial  []PartialKind
	shared   *clusterHealth
}

type requestedWorld struct {
	used  time.Time
	mu    sync.Mutex
	w     *world
	info  Info
	built time.Time
}

// NewEngine subscribes at once, so call it before the collectors start.
func NewEngine(feed Feed, reg *Registry, logger *slog.Logger) *Engine {
	return &Engine{
		feed: feed, reg: reg, sub: feed.Subscribe(subscriptionBuffer), logger: logger, now: time.Now,
		clusters: map[string]*liveWorld{}, requested: map[string]*requestedWorld{},
	}
}

// Run applies events until ctx ends. Every second it also does what time
// alone does (resolving, waiting, timed detectors) and checks each
// cluster's kinds, as the graph engine does.
func (e *Engine) Run(ctx context.Context) {
	defer e.sub.Close()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	if e.fills != nil {
		go e.readFills(ctx)
	}
	if e.readLog != nil {
		go e.readLogs(ctx)
	}
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
			e.tickAll()
		}
	}
}

// readLogs reads the logs live worlds ask for (world.logWants), within
// the limits, away from the engine's loop. What each read found goes back
// into its world as counts; the lines never leave logs.Match.
func (e *Engine) readLogs(ctx context.Context) {
	ticker := time.NewTicker(logScanEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		e.mu.Lock()
		clusters := maps.Clone(e.clusters)
		e.mu.Unlock()
		for id, l := range clusters {
			e.readClusterLogs(ctx, id, l)
		}
	}
}

func (e *Engine) readClusterLogs(ctx context.Context, cluster string, l *liveWorld) {
	l.mu.Lock()
	wants := l.w.logWants()
	l.mu.Unlock()

	now := e.now()
	var due []LogWant
	e.mu.Lock()
	if e.logRead == nil {
		e.logRead = map[string]time.Time{}
	}
	for key, at := range e.logRead {
		if now.Sub(at) > 2*logEvery {
			delete(e.logRead, key) // a container that stopped failing
		}
	}
	for _, w := range wants {
		key := fmt.Sprintf("%s/%s/%s", cluster, w.PodUID, w.Container) // either run counts against the limit
		if at, ok := e.logRead[key]; ok && now.Sub(at) < logEvery {
			continue
		}
		e.logRead[key] = now
		due = append(due, w)
	}
	e.mu.Unlock()

	sem := make(chan struct{}, logParallel)
	var wg sync.WaitGroup
	for _, w := range due {
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			f := e.readOne(ctx, cluster, w)
			l.mu.Lock()
			l.w.setLog(w.PodUID, w.Container, f, e.now())
			out := l.w.drain()
			l.mu.Unlock()
			e.publish(out)
		})
	}
	wg.Wait()
}

// readOne reads one container's log and keeps only what logs.Match counts.
// An error is put in words that carry nothing from the log or the API's
// message.
func (e *Engine) readOne(ctx context.Context, cluster string, w LogWant) LogFinding {
	f := LogFinding{Previous: w.Previous, Read: e.now(), Patterns: map[string]int{}}
	ctx, cancel := context.WithTimeout(ctx, logTimeout)
	defer cancel()
	rc, err := e.readLog(ctx, cluster, w.Namespace, w.Pod, w.Container, w.Previous)
	if err == nil {
		defer rc.Close()
		var res logs.Result
		if res, err = logs.Match(rc, logs.Patterns); err == nil {
			f.Lines, f.Patterns, f.Other = res.Lines, res.Patterns, res.Other
			return f
		}
	}
	switch {
	case apierrors.IsForbidden(err):
		f.Error = "not allowed (needs get on pods/log)"
	case apierrors.IsNotFound(err), apierrors.IsBadRequest(err):
		f.Error = "not available" // no previous run yet, or the container is gone
	default:
		f.Error = "the read failed"
	}
	e.logger.Debug("signals: can't read a log", "cluster", cluster, "pod", w.Namespace+"/"+w.Pod, "container", w.Container, "reason", f.Error)
	f.Patterns = nil
	return f
}

// readFills reads volume usage for every live cluster once a minute, away
// from the engine's loop: it calls every node. A cluster whose usage can't
// be read keeps its claims' signals as they are.
func (e *Engine) readFills(ctx context.Context) {
	ticker := time.NewTicker(fillsEvery)
	defer ticker.Stop()
	for {
		e.mu.Lock()
		clusters := maps.Clone(e.clusters)
		e.mu.Unlock()
		for id, l := range clusters {
			readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			fills, err := e.fills(readCtx, id)
			cancel()
			if err != nil {
				e.logger.Debug("signals: can't read volume usage", "cluster", id, "error", err)
				continue
			}
			l.mu.Lock()
			l.w.setFills(fills, e.now())
			out := l.w.drain()
			l.mu.Unlock()
			e.publish(out)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Engine) handle(ev bus.Event) {
	switch v := ev.(type) {
	case bus.ObjectChanged:
		if !slices.Contains(e.reg.Kinds(), v.Resource) {
			return
		}
		l := e.live(v.Cluster)
		if l == nil {
			return
		}
		l.mu.Lock()
		if !l.w.stale[v.Resource] && l.w.readable(v.Resource) {
			if v.Type == bus.Deleted {
				l.w.remove(v.Resource, v.UID, e.now())
			} else if v.New != nil {
				l.w.upsert(v.Resource, v.New, e.now())
			}
		}
		out := l.w.drain()
		l.mu.Unlock()
		e.publish(out)

	case bus.CollectorGap:
		if v.Reason == bus.GapStopped {
			e.drop(v.Cluster)
			return
		}
		e.mu.Lock()
		affected := map[string]*liveWorld{}
		for id, l := range e.clusters {
			if v.Cluster == "" || v.Cluster == id {
				affected[id] = l
			}
		}
		e.mu.Unlock()
		for id, l := range affected {
			l.mu.Lock()
			for _, k := range e.reg.Kinds() {
				if v.Resource == "" || v.Resource == k {
					l.w.markStale(k)
				}
			}
			e.sync(id, l)
			out := l.w.drain()
			l.mu.Unlock()
			e.publish(out)
		}
	}
}

func (e *Engine) publish(events []bus.Event) {
	for _, ev := range events {
		e.feed.Publish(ev)
	}
}

// live returns a cluster's world, reading it from the cache the first
// time; nil when the cluster has no collector.
func (e *Engine) live(cluster string) *liveWorld {
	e.mu.Lock()
	l, ok := e.clusters[cluster]
	if ok {
		e.mu.Unlock()
		return l
	}
	if status, found := e.feed.Modes(cluster); !found || !status.Enabled {
		e.mu.Unlock()
		return nil
	}
	l = &liveWorld{w: newWorld(cluster, e.reg, e.now()), shared: &clusterHealth{}}
	l.shared.changed = l.w.published
	e.health.Store(cluster, l.shared)
	e.clusters[cluster] = l
	l.mu.Lock()
	e.mu.Unlock()
	e.sync(cluster, l)
	out := l.w.drain()
	l.mu.Unlock()
	e.publish(out)
	return l
}

func (e *Engine) drop(cluster string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.clusters, cluster)
	e.health.Delete(cluster)
	for key := range e.requested {
		if strings.HasPrefix(key, cluster+"\x00") {
			delete(e.requested, key)
		}
	}
}

func (e *Engine) tickAll() {
	e.mu.Lock()
	clusters := maps.Clone(e.clusters)
	e.mu.Unlock()
	for id, l := range clusters {
		l.mu.Lock()
		e.sync(id, l)
		l.w.tick(e.now())
		out := l.w.drain()
		l.mu.Unlock()
		e.publish(out)
	}
}

// sync brings a cluster's world in line with how its kinds are read now:
// a kind newly cached, or behind a gap, is read from the cache in full; one
// that can't be read loses its objects. Call it with l.mu held.
func (e *Engine) sync(cluster string, l *liveWorld) {
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
	for _, resource := range e.reg.Kinds() {
		k, known := modes[resource]
		switch {
		case known && (k.Mode == collector.ModeWatch || k.Mode == collector.ModePoll) && k.Synced:
			if !l.w.readable(resource) || l.w.stale[resource] {
				objects, ok := e.feed.Cached(cluster, resource)
				if !ok {
					complete = false
					continue
				}
				l.w.setReadable(resource, true)
				l.w.replace(resource, objects, e.now())
			}
		case known && (k.Mode == collector.ModeForbidden || k.Mode == collector.ModeNotInstalled ||
			k.Mode == collector.ModeDirect && resource != "pods"):
			l.w.setReadable(resource, false)
			partial = append(partial, PartialKind{Resource: resource, Code: codeOf(k), Reason: reasonOf(k)})
		default: // pending, pods per namespace, or a cache on its first list
			complete = false
		}
	}
	l.complete, l.partial = complete, partial
	l.shared.complete.Store(complete)
}

func codeOf(k collector.KindStatus) string {
	if k.Mode == collector.ModeNotInstalled {
		return "not_installed"
	}
	return "forbidden"
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

// HealthOf is an object's health in a live world, without waiting on the
// world's lock. It's nil when it isn't known: no live world, or one still
// reading the cluster, or a kind no detector reads. The caller knows the
// object exists (the graph has it); one with no entry has been healthy
// since Opscope first saw it.
func (e *Engine) HealthOf(cluster, resource, uid string) *Health {
	v, ok := e.health.Load(cluster)
	if !ok || len(e.reg.For(resource)) == 0 {
		return nil
	}
	ch := v.(*clusterHealth)
	if h, ok := ch.changed.Load(uid); ok {
		return h.(*Health)
	}
	if !ch.complete.Load() {
		return nil
	}
	return &Health{State: Healthy}
}

// Read runs fn on a cluster's signals: the live world when it's complete,
// otherwise one worked out from src for namespace ("" for all) and reused
// for a few seconds. fn mustn't keep the View.
func (e *Engine) Read(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(View, Info) error) error {
	if l := e.live(cluster); l != nil {
		l.mu.Lock()
		if l.complete {
			defer l.mu.Unlock()
			return fn(View{l.w, e.now()}, Info{Built: BuiltLive, Partial: slices.Clone(l.partial), HistorySince: l.w.started})
		}
		l.mu.Unlock()
	}
	return e.onRequest(ctx, cluster, src, namespace, fn)
}

func (e *Engine) onRequest(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(View, Info) error) error {
	key := cluster + "\x00" + namespace
	now := e.now()
	e.mu.Lock()
	for k, r := range e.requested {
		if now.Sub(r.used) > time.Minute {
			delete(e.requested, k)
		}
	}
	r, ok := e.requested[key]
	if !ok {
		r = &requestedWorld{}
		e.requested[key] = r
	}
	r.used = now
	e.mu.Unlock()

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w == nil || now.Sub(r.built) >= onRequestFor {
		w, info, err := e.buildFrom(ctx, cluster, src, namespace, now)
		if err != nil {
			return err
		}
		r.w, r.info, r.built = w, info, now
	}
	return fn(View{r.w, r.built}, Info{Built: r.info.Built, Partial: slices.Clone(r.info.Partial)})
}

// buildFrom lists every kind the detectors read from src, in parallel,
// and works out their signals at once. A kind that can't be read is left
// out and named; any other error fails.
func (e *Engine) buildFrom(ctx context.Context, cluster string, src resources.Source, namespace string, now time.Time) (*world, Info, error) {
	kinds := e.reg.Kinds()
	type result struct {
		objects []runtime.Object
		err     error
	}
	results := make([]result, len(kinds))
	var wg sync.WaitGroup
	for i, resource := range kinds {
		wg.Go(func() {
			objects, err := src.List(ctx, resource, resources.Query{Namespace: namespace})
			results[i] = result{objects, err}
		})
	}
	wg.Wait()

	w := newWorld(cluster, e.reg, now)
	w.onRequest = true
	info := Info{Built: BuiltOnRequest}
	// Events first, so each object's detectors see them when it's added.
	order := slices.Clone(kinds)
	slices.SortStableFunc(order, func(a, b string) int {
		if a == "events" {
			return -1
		}
		if b == "events" {
			return 1
		}
		return 0
	})
	for _, resource := range order {
		i := slices.Index(kinds, resource)
		switch err := results[i].err; {
		case err == nil:
			w.setReadable(resource, true)
			w.replace(resource, results[i].objects, now)
		case apierrors.IsForbidden(err):
			info.Partial = append(info.Partial, PartialKind{Resource: resource, Code: "forbidden", Reason: "not allowed"})
		case apierrors.IsNotFound(err), errors.Is(err, resources.ErrGatewayAPINotInstalled):
			info.Partial = append(info.Partial, PartialKind{Resource: resource, Code: "not_installed", Reason: "not installed"})
		default:
			return nil, Info{}, fmt.Errorf("reading %s: %w", resource, err)
		}
	}
	// Triggers ran as objects arrived, in whatever order; one more pass
	// gives every object the same answer whatever the order was.
	for _, resource := range kinds {
		for _, uid := range sortedKeys(w.objects[resource]) {
			w.evaluate(uid, now)
		}
	}
	w.drain() // nothing listens to a world built for one answer
	return w, info, nil
}
