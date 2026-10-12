package snapshot

import (
	"context"
	"sync"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/kube"
)

const (
	// MaxAge is how long a snapshot is used before a new one is taken. It
	// matches the UI's refresh interval, so a page that refreshes reads the
	// cluster again, and several requests in between share one snapshot.
	MaxAge = 10 * time.Second

	// loadTimeout bounds one whole snapshot. Each list call already has the
	// client's own timeout; this covers a large cluster's many pages.
	loadTimeout = time.Minute
)

// Store keeps the latest snapshot of each cluster. It is safe to use from
// many requests at once.
type Store struct {
	maxAge time.Duration
	load   func(context.Context, *clusters.Cluster) (*Snapshot, error)
	now    func() time.Time // the clock; tests set their own

	mu      sync.Mutex
	entries map[string]*entry // by cluster ID
}

type entry struct {
	// The snapshot belongs to this exact cluster. A cluster that is removed
	// and added again under the same ID is a new *Cluster, and starts afresh.
	cluster *clusters.Cluster
	latest  *Snapshot
	loading *pending // set while a load is running
}

// pending is a load in progress. Everyone who asks while it runs waits for
// the same one, instead of reading the whole cluster again.
type pending struct {
	done     chan struct{} // closed when the load has finished
	snapshot *Snapshot
	err      error
}

func NewStore() *Store {
	return NewStoreReading(loadCluster)
}

// NewStoreReading is a Store that takes snapshots with load instead of
// reading the cluster, for tests of code that uses a Store.
func NewStoreReading(load func(context.Context, *clusters.Cluster) (*Snapshot, error)) *Store {
	return &Store{
		maxAge:  MaxAge,
		load:    load,
		now:     time.Now,
		entries: map[string]*entry{},
	}
}

func loadCluster(ctx context.Context, c *clusters.Cluster) (*Snapshot, error) {
	return Load(ctx, kube.Clients{Kube: c.Client, Dynamic: c.Dynamic, Metadata: c.Metadata})
}

// Get returns a snapshot of the cluster no older than MaxAge, taking a new
// one if needed.
//
// The load runs on its own, not as part of the caller's request: if the
// caller gives up (the browser navigates away), others waiting for the same
// load still get it, and the next caller can use it.
func (s *Store) Get(ctx context.Context, cluster *clusters.Cluster) (*Snapshot, error) {
	return s.get(ctx, cluster, s.maxAge)
}

// get returns a snapshot no older than maxAge, taking a new one if needed.
func (s *Store) get(ctx context.Context, cluster *clusters.Cluster, maxAge time.Duration) (*Snapshot, error) {
	s.mu.Lock()
	e := s.entries[cluster.ID]
	if e == nil || e.cluster != cluster {
		e = &entry{cluster: cluster}
		s.entries[cluster.ID] = e
	}
	if e.latest != nil && s.now().Sub(e.latest.Taken) < maxAge {
		latest := e.latest
		s.mu.Unlock()
		return latest, nil
	}
	p := e.loading
	if p == nil {
		p = &pending{done: make(chan struct{})}
		e.loading = p
		go s.run(e, p)
	}
	s.mu.Unlock()

	select {
	case <-p.done:
		return p.snapshot, p.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// run takes one snapshot and hands it to everyone waiting. A failed load
// isn't kept: the next Get tries again.
func (s *Store) run(e *entry, p *pending) {
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	p.snapshot, p.err = s.load(ctx, e.cluster)

	s.mu.Lock()
	e.loading = nil
	if p.err == nil {
		e.latest = p.snapshot
	}
	s.mu.Unlock()
	close(p.done)
}

// latest is the cluster's latest snapshot, if there is one.
func (s *Store) latest(cluster *clusters.Cluster) *Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[cluster.ID]; e != nil && e.cluster == cluster {
		return e.latest
	}
	return nil
}

// Forget drops the snapshots of clusters that are no longer known.
func (s *Store) Forget(keep map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.entries {
		if !keep[id] {
			delete(s.entries, id)
		}
	}
}
