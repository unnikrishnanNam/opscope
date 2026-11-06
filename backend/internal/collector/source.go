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
// first requests after a collector starts can wait at all.
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
// kind the cache can't serve: one that hasn't finished its first list, or
// one that failed to list (not installed, not allowed, unreachable). Those
// give the same answer, or the same error, as before there was a cache.
type cacheSource struct {
	collector *Collector
	direct    resources.Source
	wait      time.Duration
}

func (s *cacheSource) List(ctx context.Context, resource string, q resources.Query) ([]runtime.Object, error) {
	k := s.collector.kind(resource)
	if k == nil || !k.waitForSync(ctx, s.wait) {
		return s.direct.List(ctx, resource, q)
	}

	var items []any
	if k.Namespaced && q.Namespace != "" {
		items, _ = k.informer.GetIndexer().ByIndex(cache.NamespaceIndex, q.Namespace)
	} else {
		items = k.informer.GetStore().List()
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

// waitForSync reports whether the kind's cache can be read: it has
// finished its first list, or does within wait. It doesn't wait for a kind
// whose list or watch has failed, which is unlikely to change in a few
// seconds (Gateway API not installed, Secrets not allowed): that kind is
// read from the cluster at once, as before.
func (k *kind) waitForSync(ctx context.Context, wait time.Duration) bool {
	if k.informer == nil {
		return false
	}
	deadline := time.Now().Add(wait)
	for {
		if k.informer.HasSynced() {
			return true
		}
		k.mu.Lock()
		failed := k.lastErr != nil
		k.mu.Unlock()
		if failed || time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(20 * time.Millisecond):
		}
	}
}
