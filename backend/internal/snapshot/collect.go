package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"opscope/internal/clusters"
)

const (
	// DefaultInterval is how often Collect reads each cluster.
	DefaultInterval = 30 * time.Second

	// MinInterval is the shortest interval allowed. Pages already read a
	// cluster up to every MaxAge while someone looks at them; reading more
	// often than that gains nothing.
	MinInterval = MaxAge

	// maxBackoff is the longest Collect waits before trying a cluster that
	// doesn't answer again.
	maxBackoff = 5 * time.Minute
)

// ParseInterval reads OPSCOPE_SNAPSHOT_INTERVAL: a duration such as "30s"
// or "2m", "" for the default, or "0" or "off" for no background snapshots
// (0 is returned). Anything shorter than MinInterval is refused.
func ParseInterval(value string) (time.Duration, error) {
	switch strings.TrimSpace(value) {
	case "":
		return DefaultInterval, nil
	case "0", "off":
		return 0, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%q isn't a duration like 30s or 2m", value)
	}
	if d == 0 {
		return 0, nil
	}
	if d < MinInterval {
		return 0, fmt.Errorf("%s is too short: the least is %s (or 0 to turn background snapshots off)", d, MinInterval)
	}
	return d, nil
}

// Collect keeps a recent snapshot of every cluster, whether anyone is
// looking or not, until ctx is cancelled. Every interval it reads each
// cluster whose latest snapshot is older than that; one a page asked for
// counts, so a cluster someone is looking at isn't read twice. A cluster
// that doesn't answer is tried again after twice as long each time, up to
// five minutes, and every interval again once it answers.
//
// The cluster view reads these snapshots, and the timeline will compare
// them. Run it in its own goroutine: `go store.Collect(ctx, ...)`.
func (s *Store) Collect(ctx context.Context, manager *clusters.Manager, interval time.Duration, logger *slog.Logger) {
	c := newCollector(s, interval, logger)
	logger.Info("taking snapshots in the background", "interval", interval)
	// Checking which clusters are due every second keeps each one on its
	// own schedule; a check is a look at a few times, nothing more.
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		c.round(ctx, manager.List(), s.now())
		select {
		case <-ctx.Done():
			c.wg.Wait()
			return
		case <-ticker.C:
		}
	}
}

// collector is the state of Collect: when each cluster is next due.
type collector struct {
	store    *Store
	interval time.Duration
	logger   *slog.Logger

	mu    sync.Mutex
	state map[string]*schedule // by cluster ID
	wg    sync.WaitGroup       // reads running now
}

type schedule struct {
	cluster  *clusters.Cluster
	next     time.Time // not read before this
	failures int       // how many reads in a row have failed
	failedAt time.Time
	reading  bool
}

func newCollector(s *Store, interval time.Duration, logger *slog.Logger) *collector {
	return &collector{store: s, interval: interval, logger: logger, state: map[string]*schedule{}}
}

// round starts a read of every cluster that's due, each on its own, so a
// cluster that doesn't answer holds up no other.
func (c *collector) round(ctx context.Context, list []*clusters.Cluster, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()

	known := map[string]bool{}
	for _, cluster := range list {
		known[cluster.ID] = true
		st := c.state[cluster.ID]
		if st == nil || st.cluster != cluster {
			st = &schedule{cluster: cluster}
			c.state[cluster.ID] = st
		}
		// A page may have read a cluster that's backing off; then it answers.
		if latest := c.store.latest(cluster); st.failures > 0 && latest != nil && latest.Taken.After(st.failedAt) {
			c.recovered(st, latest)
		}
		if st.reading || now.Before(st.next) {
			continue
		}
		st.reading = true
		c.wg.Go(func() {
			before := c.store.latest(cluster)
			snapshot, err := c.store.get(ctx, cluster, c.interval)
			if err == nil && snapshot != before {
				c.logger.Debug("snapshot taken", "cluster", cluster.ID, "objects", snapshot.Size(), "took", snapshot.Took.Round(time.Millisecond))
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			st.reading = false
			if err != nil {
				c.failed(st, err, c.store.now())
			} else {
				c.recovered(st, snapshot)
			}
		})
	}
	for id := range c.state {
		if !known[id] {
			delete(c.state, id)
		}
	}
}

func (c *collector) failed(st *schedule, err error, now time.Time) {
	if errors.Is(err, context.Canceled) {
		return // shutting down
	}
	st.failures++
	st.failedAt = now
	wait := min(c.interval<<st.failures, maxBackoff)
	st.next = now.Add(wait)
	if st.failures == 1 {
		c.logger.Warn("can't read cluster for snapshots; trying less often", "cluster", st.cluster.ID, "error", err)
	}
	c.logger.Debug("snapshot failed", "cluster", st.cluster.ID, "failures", st.failures, "next try in", wait)
}

func (c *collector) recovered(st *schedule, snapshot *Snapshot) {
	if st.failures > 0 {
		c.logger.Info("cluster answers again", "cluster", st.cluster.ID, "after failures", st.failures)
	}
	st.failures = 0
	st.next = snapshot.Taken.Add(c.interval)
}
