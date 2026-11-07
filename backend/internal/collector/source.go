package collector

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	"opscope/internal/clusters"
	"opscope/internal/resources"
)

// syncWait is how long a request waits for a kind's first list to finish
// before it reads that kind from the cluster directly instead. Only the
// first requests after a collector starts, or after a kind changes mode,
// can wait at all.
const syncWait = 5 * time.Second

// Source is where a cluster's pages read from: its collector's cache when
// one is running, or the cluster itself when collectors are off.
func (s *Set) Source(cluster *clusters.Cluster) resources.Source {
	direct := resources.Direct(resources.Clients{Kube: cluster.Client, Dynamic: cluster.Dynamic})
	c, ok := s.Get(cluster.ID)
	if !ok {
		return direct
	}
	return &cacheSource{collector: c, direct: direct, wait: syncWait}
}

// cacheSource reads from a collector's cache, and from the cluster for any
// kind the cache can't serve: one that isn't watched or polled (see the
// modes), one still waiting for its first list, or every kind while the
// cluster can't be reached, so the page says so instead of showing old
// data. Those give the same answer, or the same error, as before there was
// a cache.
type cacheSource struct {
	collector *Collector
	direct    resources.Source
	wait      time.Duration
}

func (s *cacheSource) List(ctx context.Context, resource string, q resources.Query) ([]runtime.Object, error) {
	k := s.collector.kind(resource)
	if k == nil || s.collector.unreachable() {
		return s.direct.List(ctx, resource, q)
	}
	store, ok := k.readable(ctx, s.wait)
	if !ok {
		return s.direct.List(ctx, resource, q)
	}

	var items []any
	if k.Namespaced && q.Namespace != "" {
		items, _ = store.ByIndex(cache.NamespaceIndex, q.Namespace)
	} else {
		items = store.List()
	}
	objects := make([]runtime.Object, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(runtime.Object); ok {
			objects = append(objects, obj)
		}
	}
	return objects, nil
}

// kind returns the watched kind with this URL name, or nil.
func (c *Collector) kind(resource string) *kind {
	for _, k := range c.kinds {
		if k.Resource == resource {
			return k
		}
	}
	return nil
}
