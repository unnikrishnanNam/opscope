package collector

import (
	"context"
	"log/slog"
	"sync"

	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/bus"
	"opscope/internal/clusters"
)

// Set holds one collector per connected cluster. It follows the clusters
// manager: a collector starts when a cluster is loaded or added, and stops
// when the cluster is removed.
type Set struct {
	enabled bool
	ctx     context.Context
	bus     *bus.Bus
	logger  *slog.Logger

	mu         sync.Mutex
	collectors map[string]*Collector
}

// NewSet makes an empty set. With enabled false (OPSCOPE_LIVE=false) it
// never starts a collector, and every cluster is read as in v1.3. Collectors
// stop when ctx is cancelled.
func NewSet(ctx context.Context, enabled bool, b *bus.Bus, logger *slog.Logger) *Set {
	return &Set{enabled: enabled, ctx: ctx, bus: b, logger: logger, collectors: map[string]*Collector{}}
}

// Follow starts a collector for every cluster the manager knows and keeps
// the set in step with it from then on.
func (s *Set) Follow(manager *clusters.Manager) {
	manager.Watch(s.add, s.remove)
}

// add starts a collector for a cluster, replacing one already running
// under the same id. It's called with the manager locked, so it only
// starts goroutines and returns.
func (s *Set) add(cluster *clusters.Cluster) {
	if !s.enabled {
		return
	}
	clients := Clients{Kube: cluster.Stream, Dynamic: cluster.StreamDynamic, Check: cluster.Client}
	c := Start(s.ctx, cluster.ID, clients, s.bus, s.logger)

	s.mu.Lock()
	old := s.collectors[cluster.ID]
	s.collectors[cluster.ID] = c
	s.mu.Unlock()
	if old != nil {
		old.Stop()
	}
	s.logger.Info("collector started", "cluster", cluster.ID)
}

// remove stops a cluster's collector. Its caches are freed once its
// informers have stopped, which happens in the background.
func (s *Set) remove(id string) {
	s.mu.Lock()
	c := s.collectors[id]
	delete(s.collectors, id)
	s.mu.Unlock()
	if c != nil {
		c.Stop()
		s.logger.Info("collector stopped", "cluster", id)
	}
}

// Get returns a cluster's collector, if one is running.
func (s *Set) Get(id string) (*Collector, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.collectors[id]
	return c, ok
}

// Status is a cluster's collector status, or "off" when collectors are
// turned off. ok is false for a cluster the set doesn't know.
func (s *Set) Status(id string) (status Status, ok bool) {
	return s.status(id, true)
}

// Modes is Status without the object counts, which are the costly part:
// for checking often whether a cluster's state or a kind's mode changed.
func (s *Set) Modes(id string) (status Status, ok bool) {
	return s.status(id, false)
}

func (s *Set) status(id string, counts bool) (Status, bool) {
	if !s.enabled {
		return Status{Enabled: false, State: "off", Kinds: []KindStatus{}}, true
	}
	c, ok := s.Get(id)
	if !ok {
		return Status{}, false
	}
	return c.status(counts), true
}

// Cached returns every object of one kind in a cluster's cache, when the
// kind is read from the cache (watch or poll) and has its first list. ok is
// false otherwise: the caller can't tell what the cluster holds from it.
// The objects are shared with the cache and must not be changed.
func (s *Set) Cached(id, resource string) (objects []runtime.Object, ok bool) {
	c, found := s.Get(id)
	if !found {
		return nil, false
	}
	k := c.kind(resource)
	if k == nil {
		return nil, false
	}
	k.mu.Lock()
	mode, store, synced := k.mode, k.store, k.synced
	k.mu.Unlock()
	if !cached(mode) || !synced() {
		return nil, false
	}
	for _, item := range store.List() {
		if obj, isObj := item.(runtime.Object); isObj {
			objects = append(objects, obj)
		}
	}
	return objects, true
}

// Enabled reports whether collectors run at all (OPSCOPE_LIVE).
func (s *Set) Enabled() bool {
	return s.enabled
}

// Subscribe follows every collector's events on the set's bus. Close the
// subscription when done.
func (s *Set) Subscribe(buffer int) *bus.Subscription {
	return s.bus.Subscribe(buffer)
}

// Done is closed when the set's context ends: Opscope is shutting down.
func (s *Set) Done() <-chan struct{} {
	return s.ctx.Done()
}
