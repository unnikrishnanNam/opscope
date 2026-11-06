// Package collector keeps a live copy of each connected cluster in memory.
//
// One Collector runs per cluster. For every kind Opscope lists, it finds
// what the cluster allows (see probe.go), then lists the objects once and
// watches for changes, or lists them again on a timer where watching isn't
// allowed. It keeps them in a cache and publishes each add, update and
// delete on the bus, and a gap wherever it may have missed some. Objects
// are trimmed before they're stored: Secrets lose their values, ConfigMaps
// keep a hash instead of theirs (see transform.go).
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

// Clients are what a collector reads a cluster with.
type Clients struct {
	// Kube and Dynamic must not have a request timeout: a watch is one long
	// response, and a timeout would cut it.
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	// Check, with a timeout, asks the cluster for its version now and then,
	// to notice when it can't be reached. nil skips the check.
	Check kubernetes.Interface
}

// timing holds the collector's intervals; tests shorten them.
type timing struct {
	tick         time.Duration // how often the collector looks at its kinds
	checkEvery   time.Duration // how often it checks the cluster answers
	reprobeEvery time.Duration // how often kinds that aren't watched are probed again
	retryFirst   time.Duration // the first wait after a probe got no answer; it doubles
	pollEvery    time.Duration // how often poll mode lists
}

var defaultTiming = timing{
	tick:         time.Second,
	checkEvery:   10 * time.Second,
	reprobeEvery: 5 * time.Minute,
	retryFirst:   10 * time.Second,
	pollEvery:    30 * time.Second,
}

const (
	probeTimeout = 15 * time.Second
	pollTimeout  = 30 * time.Second
	checkTimeout = 10 * time.Second
)

// Collector watches one cluster.
type Collector struct {
	cluster string
	clients Clients
	bus     *bus.Bus
	logger  *slog.Logger
	timing  timing
	kinds   []*kind // in resources.Kinds order: by name
	cancel  context.CancelFunc
	done    chan struct{}  // closed once everything has stopped
	wg      sync.WaitGroup // informers and pollers

	mu               sync.Mutex
	unreachableSince time.Time // zero while the cluster answers
}

// Start begins watching a cluster and returns at once; the caches fill in
// the background. Stop it with Stop, or by cancelling ctx.
func Start(ctx context.Context, cluster string, clients Clients, b *bus.Bus, logger *slog.Logger) *Collector {
	return start(ctx, cluster, clients, b, logger, defaultTiming)
}

func start(ctx context.Context, cluster string, clients Clients, b *bus.Bus, logger *slog.Logger, t timing) *Collector {
	ctx, cancel := context.WithCancel(ctx)
	c := &Collector{
		cluster: cluster,
		clients: clients,
		bus:     b,
		logger:  logger.With("cluster", cluster),
		timing:  t,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	for _, rk := range resources.Kinds() {
		c.kinds = append(c.kinds, &kind{Kind: rk, c: c, mode: ModePending})
	}
	go c.run(ctx)
	return c
}

// run probes every kind, then looks after them until ctx is cancelled.
func (c *Collector) run(ctx context.Context) {
	defer func() {
		for _, k := range c.kinds {
			k.stopRunner()
		}
		c.wg.Wait()
		// Nothing is recorded from now on, until a collector starts again:
		// an open gap (no To).
		c.bus.Publish(bus.CollectorGap{Cluster: c.cluster, From: time.Now(), Reason: bus.GapStopped})
		close(c.done)
	}()

	c.probeAll(ctx, true)
	lastCheck := time.Now()
	ticker := time.NewTicker(c.timing.tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if now.Sub(lastCheck) >= c.timing.checkEvery {
				c.checkReachable(ctx)
				lastCheck = now
			}
			if !c.unreachable() {
				c.probeAll(ctx, false)
			}
		}
	}
}

// checkReachable asks the cluster for its version. Losing the cluster and
// getting it back are logged once each, not on every failed watch, and the
// time in between is published as a gap for every kind.
func (c *Collector) checkReachable(ctx context.Context) {
	if c.clients.Check == nil {
		return
	}
	// ServerVersion takes no context; the Check client's own timeout ends it.
	_, err := c.clients.Check.Discovery().ServerVersion()
	if ctx.Err() != nil {
		return
	}

	c.mu.Lock()
	since := c.unreachableSince
	switch {
	case err != nil && since.IsZero():
		c.unreachableSince = time.Now()
	case err == nil && !since.IsZero():
		c.unreachableSince = time.Time{}
	}
	c.mu.Unlock()

	switch {
	case err != nil && since.IsZero():
		c.logger.Warn("cluster unreachable; pages read it directly until it answers again", "error", err)
	case err == nil && !since.IsZero():
		now := time.Now()
		c.logger.Info("cluster reachable again", "after", now.Sub(since).Round(time.Second))
		c.bus.Publish(bus.CollectorGap{Cluster: c.cluster, From: since, To: now, Reason: bus.GapUnreachable})
		// Kinds that got no answer while it was gone are probed now.
		for _, k := range c.kinds {
			k.mu.Lock()
			if k.mode == ModePending {
				k.nextProbe = now
			}
			k.mu.Unlock()
		}
	}
}

func (c *Collector) unreachable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.unreachableSince.IsZero()
}

// Stop ends every watch and drops the caches. It returns at once; Done is
// closed when everything has stopped.
func (c *Collector) Stop() {
	c.cancel()
}

// Done is closed once the collector has stopped.
func (c *Collector) Done() <-chan struct{} {
	return c.done
}

func (c *Collector) handler(k *kind) cache.ResourceEventHandler {
	return cache.ResourceEventHandlerDetailedFuncs{
		AddFunc: func(obj any, initial bool) {
			c.publish(k, bus.Added, nil, obj, initial)
		},
		UpdateFunc: func(old, obj any) {
			c.publish(k, bus.Updated, old, obj, false)
		},
		DeleteFunc: func(obj any) {
			c.publish(k, bus.Deleted, obj, nil, false)
		},
	}
}

func (c *Collector) publish(k *kind, change bus.ChangeType, old, obj any, initial bool) {
	// A delete the watch missed (it happened while disconnected) arrives as
	// a tombstone holding the last state the cache had.
	if tomb, ok := old.(cache.DeletedFinalStateUnknown); ok {
		old = tomb.Obj
	}
	subject := obj
	if subject == nil {
		subject = old
	}
	m, err := meta.Accessor(subject)
	if err != nil {
		return
	}
	// After a relist, client-go reports every object as updated, changed or
	// not. Only a new resourceVersion means something changed.
	if change == bus.Updated {
		if before, err := meta.Accessor(old); err == nil && before.GetResourceVersion() == m.GetResourceVersion() {
			return
		}
	}

	now := time.Now()
	k.mu.Lock()
	k.lastGood = now // an event means the watch works
	if !initial {
		k.lastChange = now
	}
	k.mu.Unlock()
	oldObj, _ := old.(runtime.Object)
	newObj, _ := obj.(runtime.Object)
	c.bus.Publish(bus.ObjectChanged{
		Cluster:         c.cluster,
		Resource:        k.Resource,
		Type:            change,
		UID:             string(m.GetUID()),
		Namespace:       m.GetNamespace(),
		Name:            m.GetName(),
		ResourceVersion: m.GetResourceVersion(),
		Old:             oldObj,
		New:             newObj,
		Initial:         initial,
		Seen:            now,
	})
}

// Status is what GET /api/clusters/{id}/live returns.
type Status struct {
	Enabled bool `json:"enabled"`
	// State is the cluster's as a whole: live (every kind is watched, or read
	// from the cluster on each request, so nothing is behind), partial (some
	// are polled, so up to pollEvery behind), connecting (some aren't ready
	// yet), unreachable, or off.
	State string       `json:"state"`
	Kinds []KindStatus `json:"kinds"`
}

// KindStatus is one kind's part of Status.
type KindStatus struct {
	Resource   string     `json:"resource"`
	Mode       string     `json:"mode"`             // see the Mode constants
	Reason     string     `json:"reason,omitempty"` // why it isn't watched
	Synced     bool       `json:"synced"`           // its cache has its first list (watch and poll)
	Objects    int        `json:"objects"`
	LastChange *time.Time `json:"lastChange"`
	// Error is the last failed list, watch or probe, while the kind isn't
	// being read from its cache.
	Error string `json:"error,omitempty"`
}

// Status says how each kind is read, and how far its cache is.
func (c *Collector) Status() Status {
	s := Status{Enabled: true, Kinds: make([]KindStatus, 0, len(c.kinds))}
	waiting, partial := false, false
	for _, k := range c.kinds {
		k.mu.Lock()
		ks := KindStatus{Resource: k.Resource, Mode: k.mode, Reason: k.reason}
		if cached(k.mode) && k.synced() {
			ks.Synced = true
			ks.Objects = len(k.store.ListKeys())
		}
		if !k.lastChange.IsZero() {
			t := k.lastChange
			ks.LastChange = &t
		}
		if !ks.Synced && k.lastErr != nil {
			ks.Error = k.lastErr.Error()
		}
		switch {
		case k.mode == ModePending, cached(k.mode) && !ks.Synced:
			waiting = true
		case k.mode == ModePoll:
			partial = true
		}
		k.mu.Unlock()
		s.Kinds = append(s.Kinds, ks)
	}
	switch {
	case c.unreachable():
		s.State = "unreachable"
	case waiting:
		s.State = "connecting"
	case partial:
		s.State = "partial"
	default:
		s.State = "live"
	}
	return s
}
