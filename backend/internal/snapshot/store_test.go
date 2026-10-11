package snapshot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opscope/internal/clusters"
)

// testStore is a Store whose loads are counted and can be held back, so
// tests can see how many times the cluster would be read.
type testStore struct {
	*Store
	loads   atomic.Int32
	release chan struct{} // when set, each load waits for it
	taken   time.Time     // the Taken time of the snapshots it returns
	err     error
}

func newTestStore() *testStore {
	ts := &testStore{taken: time.Now()}
	ts.Store = NewStoreReading(func(ctx context.Context, c *clusters.Cluster) (*Snapshot, error) {
		ts.loads.Add(1)
		if ts.release != nil {
			<-ts.release
		}
		if ts.err != nil {
			return nil, ts.err
		}
		return &Snapshot{Taken: ts.taken}, nil
	})
	return ts
}

func TestStoreReusesAFreshSnapshot(t *testing.T) {
	ts := newTestStore()
	cluster := &clusters.Cluster{ID: "lab"}

	first, err := ts.Get(context.Background(), cluster)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := ts.Get(context.Background(), cluster)
	if first != second || ts.loads.Load() != 1 {
		t.Errorf("want one load shared by both calls, got %d loads", ts.loads.Load())
	}
}

func TestStoreReloadsAnOldSnapshot(t *testing.T) {
	ts := newTestStore()
	ts.taken = time.Now().Add(-MaxAge - time.Second) // every snapshot is already too old
	cluster := &clusters.Cluster{ID: "lab"}

	ts.Get(context.Background(), cluster)
	ts.Get(context.Background(), cluster)
	if ts.loads.Load() != 2 {
		t.Errorf("got %d loads, want 2", ts.loads.Load())
	}
}

// Many requests at once wait for one load instead of each reading the
// whole cluster.
func TestStoreLoadsOnceForConcurrentCallers(t *testing.T) {
	ts := newTestStore()
	ts.release = make(chan struct{})
	cluster := &clusters.Cluster{ID: "lab"}

	var wg sync.WaitGroup
	results := make([]*Snapshot, 10)
	for i := range results {
		wg.Go(func() { results[i], _ = ts.Get(context.Background(), cluster) })
	}
	// Let every caller arrive before the load finishes.
	waitFor(t, func() bool { return ts.loads.Load() == 1 })
	time.Sleep(10 * time.Millisecond)
	close(ts.release)
	wg.Wait()

	if ts.loads.Load() != 1 {
		t.Errorf("got %d loads, want 1", ts.loads.Load())
	}
	for _, r := range results {
		if r == nil || r != results[0] {
			t.Fatal("every caller should get the same snapshot")
		}
	}
}

// A caller who gives up doesn't stop the load for the others.
func TestStoreCallerCancelling(t *testing.T) {
	ts := newTestStore()
	ts.release = make(chan struct{})
	cluster := &clusters.Cluster{ID: "lab"}

	ctx, cancel := context.WithCancel(context.Background())
	gaveUp := make(chan error)
	go func() {
		_, err := ts.Get(ctx, cluster)
		gaveUp <- err
	}()
	waitFor(t, func() bool { return ts.loads.Load() == 1 })
	cancel()
	if err := <-gaveUp; !errors.Is(err, context.Canceled) {
		t.Errorf("the caller who cancelled should get context.Canceled, got %v", err)
	}

	close(ts.release)
	if s, err := ts.Get(context.Background(), cluster); err != nil || s == nil {
		t.Errorf("the load should have finished for the next caller: %v", err)
	}
	if ts.loads.Load() != 1 {
		t.Errorf("got %d loads, want 1", ts.loads.Load())
	}
}

func TestStoreDoesntKeepErrors(t *testing.T) {
	ts := newTestStore()
	ts.err = errors.New("unreachable")
	cluster := &clusters.Cluster{ID: "lab"}

	if _, err := ts.Get(context.Background(), cluster); err == nil {
		t.Fatal("want the load's error")
	}
	ts.err = nil
	if _, err := ts.Get(context.Background(), cluster); err != nil {
		t.Fatalf("the next call should load again: %v", err)
	}
	if ts.loads.Load() != 2 {
		t.Errorf("got %d loads, want 2", ts.loads.Load())
	}
}

// A cluster removed and added again under the same ID never gets the old
// cluster's snapshot.
func TestStoreNewClusterSameID(t *testing.T) {
	ts := newTestStore()
	ts.Get(context.Background(), &clusters.Cluster{ID: "lab"})
	ts.Get(context.Background(), &clusters.Cluster{ID: "lab"})
	if ts.loads.Load() != 2 {
		t.Errorf("got %d loads, want 2", ts.loads.Load())
	}
}

func TestStoreForget(t *testing.T) {
	ts := newTestStore()
	lab, prod := &clusters.Cluster{ID: "lab"}, &clusters.Cluster{ID: "prod"}
	ts.Get(context.Background(), lab)
	ts.Get(context.Background(), prod)

	ts.Forget(map[string]bool{"prod": true})
	if _, ok := ts.entries["lab"]; ok {
		t.Error("lab should be forgotten")
	}
	if _, ok := ts.entries["prod"]; !ok {
		t.Error("prod should be kept")
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
