package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"opscope/internal/clusters"
)

const (
	// SampleEvery matches metrics-server's default resolution: asking more
	// often would just return the same numbers.
	SampleEvery = 15 * time.Second
	// keepSamples is 15 minutes of history at one sample every 15 seconds.
	keepSamples = 60
)

// Point is one sample: usage at one moment.
type Point struct {
	Time   time.Time `json:"t"`
	CPU    int64     `json:"cpu"`    // millicores
	Memory int64     `json:"memory"` // bytes
}

// History remembers recent usage per cluster: the cluster as a whole and
// each node. It lives only in memory, so it starts empty after a restart.
type History struct {
	mu       sync.Mutex
	clusters map[string]*clusterHistory
}

type clusterHistory struct {
	total []Point
	nodes map[string][]Point
}

func NewHistory() *History {
	return &History{clusters: map[string]*clusterHistory{}}
}

// Record adds one sample for a cluster.
func (h *History) Record(clusterID string, nodes []NodeUsage, at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	c, ok := h.clusters[clusterID]
	if !ok {
		c = &clusterHistory{nodes: map[string][]Point{}}
		h.clusters[clusterID] = c
	}

	total := Totals(nodes)
	c.total = appendPoint(c.total, Point{at, total.CPU, total.Memory})

	seen := map[string]bool{}
	for _, n := range nodes {
		seen[n.Name] = true
		c.nodes[n.Name] = appendPoint(c.nodes[n.Name], Point{at, n.CPU, n.Memory})
	}
	for name := range c.nodes {
		if !seen[name] {
			delete(c.nodes, name) // the node left the cluster
		}
	}
}

// appendPoint adds p and drops the oldest points beyond keepSamples.
func appendPoint(points []Point, p Point) []Point {
	points = append(points, p)
	if len(points) > keepSamples {
		points = points[len(points)-keepSamples:]
	}
	return points
}

// Cluster returns copies of a cluster's history: the totals, and per node.
// Copies, so the caller can't race with Record changing the slices.
func (h *History) Cluster(clusterID string) (total []Point, nodes map[string][]Point) {
	h.mu.Lock()
	defer h.mu.Unlock()

	total, nodes = []Point{}, map[string][]Point{}
	c, ok := h.clusters[clusterID]
	if !ok {
		return total, nodes
	}
	total = append(total, c.total...)
	for name, points := range c.nodes {
		nodes[name] = append([]Point{}, points...)
	}
	return total, nodes
}

// Forget drops the history of clusters that are no longer known.
func (h *History) Forget(keep map[string]bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id := range h.clusters {
		if !keep[id] {
			delete(h.clusters, id)
		}
	}
}

// Collect samples every cluster now and then every SampleEvery, until ctx
// is cancelled. Run it in its own goroutine: `go history.Collect(...)`.
func (h *History) Collect(ctx context.Context, manager *clusters.Manager, logger *slog.Logger) {
	ticker := time.NewTicker(SampleEvery)
	defer ticker.Stop()

	for {
		h.sampleAll(ctx, manager, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// sampleAll takes one sample from each cluster, one after another.
func (h *History) sampleAll(ctx context.Context, manager *clusters.Manager, logger *slog.Logger) {
	known := map[string]bool{}
	for _, c := range manager.List() {
		known[c.ID] = true

		sampleCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		nodes, err := ReadNodes(sampleCtx, c.Client, c.Dynamic)
		cancel()
		if err != nil {
			// Unreachable clusters and clusters without metrics-server are
			// normal here; Debug keeps them out of the log by default.
			logger.Debug("skipping metrics sample", "cluster", c.ID, "error", err)
			continue
		}
		h.Record(c.ID, nodes, Now())
	}
	h.Forget(known)
}
