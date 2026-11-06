package collector

import (
	"context"
	"log/slog"
	"sync"

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
	if !s.enabled {
		return Status{Enabled: false, State: "off", Kinds: []KindStatus{}}, true
	}
	c, ok := s.Get(id)
	if !ok {
		return Status{}, false
	}
	return c.Status(), true
}
