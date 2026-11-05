// Package collector keeps a live copy of each connected cluster in memory.
//
// One Collector runs per cluster. For every kind Opscope lists, it lists the
// objects once and then watches for changes (a client-go informer), keeps
// them in a cache, and publishes each add, update and delete on the bus.
// Objects are trimmed before they're stored: Secrets lose their values,
// ConfigMaps keep a hash instead of theirs (see transform.go).
package collector

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

// Clients are what a collector reads a cluster with. They must not have a
// request timeout: a watch is one long response, and a timeout would cut it.
type Clients struct {
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
}

// Collector watches one cluster.
type Collector struct {
	cluster string
	bus     *bus.Bus
	logger  *slog.Logger
	kinds   []*kind // in resources.Kinds order: by name
	cancel  context.CancelFunc
	done    chan struct{} // closed once every informer has stopped
}

// kind is one resource type being watched.
type kind struct {
	resources.Kind
	informer cache.SharedIndexInformer

	mu         sync.Mutex
	lastErr    error     // the last list or watch error
	lastChange time.Time // the last change seen after the first list
}

// Start begins watching a cluster and returns at once; the caches fill in
// the background. Stop it with Stop, or by cancelling ctx.
func Start(ctx context.Context, cluster string, clients Clients, b *bus.Bus, logger *slog.Logger) *Collector {
	ctx, cancel := context.WithCancel(ctx)
	c := &Collector{
		cluster: cluster,
		bus:     b,
		logger:  logger.With("cluster", cluster),
		cancel:  cancel,
		done:    make(chan struct{}),
	}

	var wg sync.WaitGroup
	for _, rk := range resources.Kinds() {
		k := &kind{Kind: rk}
		informer, err := newInformer(clients, rk)
		if err != nil {
			// Only a kind client-go has no informer for; a mistake in the kind
			// table, which the tests catch.
			k.lastErr = err
			c.kinds = append(c.kinds, k)
			continue
		}
		k.informer = informer
		// These can only fail once the informer runs, and it doesn't yet.
		_ = informer.SetTransform(trim)
		_ = informer.SetWatchErrorHandlerWithContext(c.watchError(k))
		_, _ = informer.AddEventHandler(c.handler(k))
		c.kinds = append(c.kinds, k)
		wg.Go(func() { informer.RunWithContext(ctx) })
	}
	go func() {
		wg.Wait()
		close(c.done)
	}()
	return c
}

// newInformer makes the informer for one kind, with no periodic resync: a
// resync only replays objects that didn't change. Each kind gets its own,
// rather than one factory for all, so a kind can be stopped and started on
// its own later (when a permission changes, say).
func newInformer(clients Clients, k resources.Kind) (cache.SharedIndexInformer, error) {
	if k.Custom {
		indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
		return dynamicinformer.NewFilteredDynamicInformer(clients.Dynamic, k.GVR, metav1.NamespaceAll, 0, indexers, nil).Informer(), nil
	}
	generic, err := informers.NewSharedInformerFactory(clients.Kube, 0).ForResource(k.GVR)
	if err != nil {
		return nil, err
	}
	return generic.Informer(), nil
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

// watchError replaces client-go's default handler, which logs every failed
// list or watch at error level: an unreachable cluster or a kind the cluster
// doesn't have would fill the log. The error is kept for Status instead.
func (c *Collector) watchError(k *kind) cache.WatchErrorHandlerWithContext {
	return func(ctx context.Context, _ *cache.Reflector, err error) {
		k.mu.Lock()
		k.lastErr = err
		k.mu.Unlock()
		c.logger.Debug("list or watch failed", "resource", k.Resource, "error", err)
	}
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
	if !initial {
		k.mu.Lock()
		k.lastChange = now
		k.mu.Unlock()
	}
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
	Enabled bool         `json:"enabled"`
	State   string       `json:"state"` // live, partial, connecting, or off
	Kinds   []KindStatus `json:"kinds"`
}

// KindStatus is one kind's part of Status.
type KindStatus struct {
	Resource   string     `json:"resource"`
	Mode       string     `json:"mode"` // watch; the other modes come with the fallbacks
	Synced     bool       `json:"synced"`
	Objects    int        `json:"objects"`
	LastChange *time.Time `json:"lastChange"`
	// Error is the last list or watch error, while the kind hasn't synced.
	Error string `json:"error,omitempty"`
}

// Status says how far each kind's cache is.
func (c *Collector) Status() Status {
	s := Status{Enabled: true, Kinds: make([]KindStatus, 0, len(c.kinds))}
	synced := 0
	for _, k := range c.kinds {
		ks := KindStatus{Resource: k.Resource, Mode: "watch"}
		if k.informer != nil && k.informer.HasSynced() {
			ks.Synced = true
			ks.Objects = len(k.informer.GetStore().ListKeys())
			synced++
		}
		k.mu.Lock()
		if !k.lastChange.IsZero() {
			t := k.lastChange
			ks.LastChange = &t
		}
		if !ks.Synced && k.lastErr != nil {
			ks.Error = k.lastErr.Error()
		}
		k.mu.Unlock()
		s.Kinds = append(s.Kinds, ks)
	}
	switch synced {
	case len(c.kinds):
		s.State = "live"
	case 0:
		s.State = "connecting"
	default:
		s.State = "partial"
	}
	return s
}
