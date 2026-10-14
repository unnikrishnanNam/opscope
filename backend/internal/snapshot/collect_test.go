package snapshot

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"opscope/internal/clusters"
)

// clockStore is a Store on a clock the test moves, counting reads per
// cluster; failing lists the clusters that don't answer.
type clockStore struct {
	*Store
	mu      sync.Mutex
	clock   time.Time
	reads   map[string]int
	failing map[string]bool
}

func newClockStore() *clockStore {
	cs := &clockStore{clock: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC), reads: map[string]int{}, failing: map[string]bool{}}
	cs.Store = NewStoreReading(func(ctx context.Context, c *clusters.Cluster) (*Snapshot, error) {
		cs.mu.Lock()
		defer cs.mu.Unlock()
		cs.reads[c.ID]++
		if cs.failing[c.ID] {
			return nil, errors.New("dial tcp: connection refused")
		}
		return &Snapshot{Taken: cs.clock}, nil
	})
	cs.Store.now = cs.now
	return cs
}

func (cs *clockStore) now() time.Time {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.clock
}

func (cs *clockStore) advance(d time.Duration) time.Time {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.clock = cs.clock.Add(d)
	return cs.clock
}

func (cs *clockStore) count(id string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.reads[id]
}

func (cs *clockStore) fail(id string, failing bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.failing[id] = failing
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// round runs one round of the collector at the store's clock and waits for
// its reads.
func round(c *collector, cs *clockStore, list []*clusters.Cluster) {
	c.round(context.Background(), list, cs.now())
	c.wg.Wait()
}

func TestCollectReadsEachClusterEveryInterval(t *testing.T) {
	cs := newClockStore()
	c := newCollector(cs.Store, 30*time.Second, quiet)
	a, b := &clusters.Cluster{ID: "a"}, &clusters.Cluster{ID: "b"}
	list := []*clusters.Cluster{a, b}

	round(c, cs, list)
	if cs.count("a") != 1 || cs.count("b") != 1 {
		t.Fatalf("the first round reads every cluster: %v", cs.reads)
	}
	cs.advance(20 * time.Second)
	round(c, cs, list)
	if cs.count("a") != 1 {
		t.Errorf("read again before the interval: %v", cs.reads)
	}
	cs.advance(10 * time.Second)
	round(c, cs, list)
	if cs.count("a") != 2 || cs.count("b") != 2 {
		t.Errorf("not read again after the interval: %v", cs.reads)
	}
}

// A page that just read a cluster saves the collector a read.
func TestCollectUsesWhatAPageRead(t *testing.T) {
	cs := newClockStore()
	c := newCollector(cs.Store, 30*time.Second, quiet)
	a := &clusters.Cluster{ID: "a"}

	cs.Get(context.Background(), a)
	cs.advance(5 * time.Second)
	round(c, cs, []*clusters.Cluster{a})
	if cs.count("a") != 1 {
		t.Errorf("got %d reads, want the page's one", cs.count("a"))
	}
	// Due 30 seconds after the page's read, not after the round.
	cs.advance(25 * time.Second)
	round(c, cs, []*clusters.Cluster{a})
	if cs.count("a") != 2 {
		t.Errorf("got %d reads, want 2", cs.count("a"))
	}
}

// A cluster that doesn't answer is tried after 1, 2, 4 and then every 5
// minutes, and every interval again once it answers. The others carry on.
func TestCollectBacksOff(t *testing.T) {
	cs := newClockStore()
	c := newCollector(cs.Store, 30*time.Second, quiet)
	down, up := &clusters.Cluster{ID: "down"}, &clusters.Cluster{ID: "up"}
	list := []*clusters.Cluster{down, up}
	cs.fail("down", true)

	round(c, cs, list) // fails at 0
	var waits []time.Duration
	last := cs.now()
	for range 6 {
		// Step 10 seconds at a time until it tries again.
		for reads := cs.count("down"); cs.count("down") == reads; {
			cs.advance(10 * time.Second)
			round(c, cs, list)
		}
		waits = append(waits, cs.now().Sub(last))
		last = cs.now()
	}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute, 5 * time.Minute}
	for i := range want {
		if waits[i] != want[i] {
			t.Fatalf("waits = %v, want %v", waits, want)
		}
	}
	if cs.count("up") < 40 {
		t.Errorf("the cluster that answers was read %d times in 22 minutes", cs.count("up"))
	}

	cs.fail("down", false)
	for reads := cs.count("down"); cs.count("down") == reads; {
		cs.advance(10 * time.Second)
		round(c, cs, list)
	}
	before := cs.count("down")
	cs.advance(30 * time.Second)
	round(c, cs, list)
	if cs.count("down") != before+1 {
		t.Errorf("once it answers, it's read every interval again")
	}
}

// When a page reads a cluster that's backing off, the collector stops
// waiting for it.
func TestCollectRecoversThroughAPage(t *testing.T) {
	cs := newClockStore()
	c := newCollector(cs.Store, 30*time.Second, quiet)
	a := &clusters.Cluster{ID: "a"}
	cs.fail("a", true)
	round(c, cs, []*clusters.Cluster{a}) // next try in a minute

	cs.fail("a", false)
	cs.advance(10 * time.Second)
	cs.Get(context.Background(), a)
	cs.advance(31 * time.Second)
	round(c, cs, []*clusters.Cluster{a})
	if cs.count("a") != 3 {
		t.Errorf("got %d reads, want 3: the failure, the page's, and one 30 seconds after it", cs.count("a"))
	}
}

func TestCollectForgetsRemovedClusters(t *testing.T) {
	cs := newClockStore()
	c := newCollector(cs.Store, 30*time.Second, quiet)
	round(c, cs, []*clusters.Cluster{{ID: "a"}})
	round(c, cs, nil)
	if len(c.state) != 0 {
		t.Errorf("state kept for a removed cluster: %v", c.state)
	}
}

func TestParseInterval(t *testing.T) {
	for value, want := range map[string]time.Duration{"": DefaultInterval, "45s": 45 * time.Second, "2m": 2 * time.Minute, "0": 0, "off": 0, "0s": 0} {
		if got, err := ParseInterval(value); err != nil || got != want {
			t.Errorf("%q: got %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"5s", "soon", "-1m"} {
		if _, err := ParseInterval(value); err == nil {
			t.Errorf("%q: want an error", value)
		}
	}
}
