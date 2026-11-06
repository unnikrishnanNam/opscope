package collector

import (
	"context"
	"sync"
	"time"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

// How a kind is read. The collector picks the mode by probing what the
// cluster allows (see probe.go), and probes again later, so a permission
// granted or Gateway API installed is picked up without a restart.
const (
	// ModePending: not probed yet, or the probe got no answer (the cluster
	// didn't respond). Pages read the cluster directly meanwhile.
	ModePending = "pending"
	// ModeWatch: listed once, then watched. The cache is live.
	ModeWatch = "watch"
	// ModePoll: watching isn't allowed, so the kind is listed again every
	// pollEvery into the same cache.
	ModePoll = "poll"
	// ModeDirect: listing across all namespaces isn't allowed. Pages read
	// the cluster per request, in the namespace they ask for, as in v1.3.
	ModeDirect = "direct"
	// ModeNotInstalled: the cluster doesn't have this kind (Gateway API).
	ModeNotInstalled = "not_installed"
	// ModeForbidden: listing a cluster-wide kind isn't allowed.
	ModeForbidden = "forbidden"
)

// cached reports whether pages read a kind in this mode from the cache.
func cached(mode string) bool {
	return mode == ModeWatch || mode == ModePoll
}

var namespaceIndex = cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}

// kind is one resource type in one cluster: its mode and, in watch and
// poll modes, its cache and the goroutine that keeps it.
type kind struct {
	resources.Kind
	c *Collector

	mu         sync.Mutex
	mode       string
	reason     string        // why it isn't watched, in words
	store      cache.Indexer // the cache, in watch and poll modes
	synced     func() bool   // whether the cache has its first list
	stop       context.CancelFunc
	lastErr    error     // the last failed list, watch or probe
	lastChange time.Time // the last change seen after the first list
	nextProbe  time.Time // zero: not probed again unless something fails
	failures   int       // probes in a row that got no answer, for the backoff

	// Watch mode: noticing relists (see noteCall).
	listed     bool      // the informer has started its first list
	lastGood   time.Time // the last moment its watch was known to work
	relistFrom time.Time // set while a relist is under way
}

// setMode stops whatever keeps the kind now and starts what the new mode
// needs. Only the collector's own goroutine calls it, so two calls never
// overlap.
func (k *kind) setMode(ctx context.Context, mode, reason string) {
	k.mu.Lock()
	old, oldStop := k.mode, k.stop
	k.mu.Unlock()
	if oldStop != nil {
		oldStop()
	}
	if cached(old) {
		// The new cache starts from a fresh list, published as initial adds;
		// whatever changed during the switch is only in that list.
		now := time.Now()
		k.c.bus.Publish(bus.CollectorGap{Cluster: k.c.cluster, Resource: k.Resource, From: now, To: now, Reason: bus.GapRelist})
	}

	var store cache.Indexer
	var synced func() bool
	var stop context.CancelFunc
	switch mode {
	case ModeWatch:
		runCtx, cancel := context.WithCancel(ctx)
		informer := k.newInformer()
		store, synced, stop = informer.GetIndexer(), informer.HasSynced, cancel
		k.c.wg.Go(func() { informer.RunWithContext(runCtx) })
	case ModePoll:
		runCtx, cancel := context.WithCancel(ctx)
		p := &poller{k: k, store: cache.NewIndexer(cache.MetaNamespaceKeyFunc, namespaceIndex)}
		store, synced, stop = p.store, p.isSynced, cancel
		k.c.wg.Go(func() { p.run(runCtx) })
	}

	k.mu.Lock()
	k.mode, k.reason = mode, reason
	k.store, k.synced, k.stop = store, synced, stop
	k.lastErr = nil
	k.listed, k.lastGood, k.relistFrom = false, time.Time{}, time.Time{}
	k.mu.Unlock()
}

// stopRunner stops the kind's informer or poller, if it has one.
func (k *kind) stopRunner() {
	k.mu.Lock()
	stop := k.stop
	k.stop = nil
	k.mu.Unlock()
	if stop != nil {
		stop()
	}
}

// newInformer makes the informer for watch mode, with no periodic resync:
// a resync only replays objects that didn't change. Each kind gets its own,
// rather than one factory for all, so a kind can change mode on its own.
func (k *kind) newInformer() cache.SharedIndexInformer {
	var informer cache.SharedIndexInformer
	if k.Custom {
		informer = dynamicinformer.NewFilteredDynamicInformer(k.c.clients.Dynamic, k.GVR, metav1.NamespaceAll, 0, namespaceIndex, k.noteCall).Informer()
	} else {
		factory := informers.NewSharedInformerFactoryWithOptions(k.c.clients.Kube, 0, informers.WithTweakListOptions(k.noteCall))
		generic, err := factory.ForResource(k.GVR)
		if err != nil {
			// Only for a kind client-go has no informer for: a mistake in the
			// kind table, which the tests catch.
			panic(err)
		}
		informer = generic.Informer()
	}
	// These can only fail once the informer runs, and it doesn't yet.
	_ = informer.SetTransform(trim)
	_ = informer.SetWatchErrorHandlerWithContext(k.watchError)
	_, _ = informer.AddEventHandler(k.c.handler(k))
	return informer
}

// noteCall sees every list and watch the informer starts, which is how
// relists are noticed: client-go relists when a watch can't carry on from
// where it was (it expired, or broke and the cluster moved on), and changes
// made in between are collapsed into that list. It says so on the bus.
//
// The gap runs from the last moment the watch was known to work (its start,
// or its last event) to when the relist finished (the next watch starting),
// so it can be wider than the real outage, never narrower.
func (k *kind) noteCall(o *metav1.ListOptions) {
	// A watch has a timeout; a list doesn't, except a "watch list", which
	// streams the list over a watch and marks itself.
	isList := o.TimeoutSeconds == nil || (o.SendInitialEvents != nil && *o.SendInitialEvents)
	now := time.Now()

	k.mu.Lock()
	var gap *bus.CollectorGap
	switch {
	case isList && !k.listed:
		k.listed, k.lastGood = true, now
	case isList:
		if k.relistFrom.IsZero() { // a long list comes in pages; the first one counts
			k.relistFrom = k.lastGood
		}
	default:
		if !k.relistFrom.IsZero() {
			gap = &bus.CollectorGap{Cluster: k.c.cluster, Resource: k.Resource, From: k.relistFrom, To: now, Reason: bus.GapRelist}
			k.relistFrom = time.Time{}
		}
		k.lastGood = now
	}
	k.mu.Unlock()

	if gap != nil {
		k.c.bus.Publish(*gap)
	}
}

// watchError replaces client-go's default handler, which logs every failed
// list or watch at error level: an unreachable cluster would fill the log.
// The error is kept for the status instead, and a permission that's gone
// gets the kind probed again at once.
func (k *kind) watchError(_ context.Context, _ *cache.Reflector, err error) {
	k.mu.Lock()
	k.lastErr = err
	if isPermissionOrMissing(err) {
		k.nextProbe = time.Now()
	}
	k.mu.Unlock()
	k.c.logger.Debug("list or watch failed", "resource", k.Resource, "error", err)
}

// readable returns the kind's cache if pages can read it now, waiting up to
// wait for its first list. It doesn't wait for a kind whose list or probe
// has failed, which is unlikely to change in a few seconds: that kind is
// read from the cluster at once, as before there was a cache.
func (k *kind) readable(ctx context.Context, wait time.Duration) (cache.Indexer, bool) {
	deadline := time.Now().Add(wait)
	for {
		k.mu.Lock()
		mode, store, synced, failed := k.mode, k.store, k.synced, k.lastErr != nil
		k.mu.Unlock()

		switch {
		case cached(mode) && synced():
			return store, true
		case mode != ModePending && !cached(mode):
			return nil, false
		case failed || time.Now().After(deadline):
			return nil, false
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// poller keeps a kind's cache in poll mode: it lists every pollEvery and
// publishes what changed since the last list.
type poller struct {
	k     *kind
	store cache.Indexer

	mu       sync.Mutex
	synced   bool
	lastPoll time.Time
}

func (p *poller) isSynced() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.synced
}

func (p *poller) run(ctx context.Context) {
	for {
		if err := p.poll(ctx); err != nil && ctx.Err() == nil {
			p.k.mu.Lock()
			p.k.lastErr = err
			if isPermissionOrMissing(err) {
				p.k.nextProbe = time.Now()
			}
			p.k.mu.Unlock()
			p.k.c.logger.Debug("poll failed", "resource", p.k.Resource, "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(p.k.c.timing.pollEvery):
		}
	}
}

// change is one difference a poll found.
type change struct {
	kind     bus.ChangeType
	old, obj runtime.Object
}

func (p *poller) poll(ctx context.Context) error {
	listCtx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	direct := resources.Direct(resources.Clients{Kube: p.k.c.clients.Kube, Dynamic: p.k.c.clients.Dynamic})
	objects, err := direct.List(listCtx, p.k.Resource, resources.Query{})
	if err != nil {
		return err
	}

	p.mu.Lock()
	initial := !p.synced
	from := p.lastPoll
	p.mu.Unlock()

	var changes []change
	seen := map[string]bool{}
	for _, obj := range objects {
		trim(obj)
		key, err := cache.MetaNamespaceKeyFunc(obj)
		if err != nil {
			continue
		}
		seen[key] = true
		existing, found, _ := p.store.GetByKey(key)
		switch {
		case !found:
			changes = append(changes, change{bus.Added, nil, obj})
		case resourceVersion(existing) != resourceVersion(obj):
			changes = append(changes, change{bus.Updated, existing.(runtime.Object), obj})
		}
	}
	for _, key := range p.store.ListKeys() {
		if !seen[key] {
			existing, _, _ := p.store.GetByKey(key)
			changes = append(changes, change{bus.Deleted, existing.(runtime.Object), nil})
		}
	}

	// Several changes to one object between two polls arrive as one, so any
	// change found means some may have been collapsed.
	now := time.Now()
	if !initial && len(changes) > 0 {
		p.k.c.bus.Publish(bus.CollectorGap{Cluster: p.k.c.cluster, Resource: p.k.Resource, From: from, To: now, Reason: bus.GapRelist})
	}
	for _, ch := range changes {
		switch ch.kind {
		case bus.Added:
			p.store.Add(ch.obj)
		case bus.Updated:
			p.store.Update(ch.obj)
		case bus.Deleted:
			p.store.Delete(ch.old)
		}
		p.k.c.publish(p.k, ch.kind, ch.old, ch.obj, initial)
	}

	p.mu.Lock()
	p.synced, p.lastPoll = true, now
	p.mu.Unlock()
	p.k.mu.Lock()
	p.k.lastErr = nil
	p.k.mu.Unlock()
	return nil
}

func resourceVersion(obj any) string {
	m, err := apimeta.Accessor(obj)
	if err != nil {
		return ""
	}
	return m.GetResourceVersion()
}
