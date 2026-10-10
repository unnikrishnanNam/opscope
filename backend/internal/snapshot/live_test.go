package snapshot

import (
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"opscope/internal/clusters"
)

// TestLoadLive takes a snapshot of a real cluster and reports how long it
// took and how much memory it holds. It only reads (get and list), and it is
// skipped unless OPSCOPE_TEST_KUBECONFIG names a kubeconfig. Give an
// absolute path, since tests run in the package's folder. From backend/:
//
//	OPSCOPE_TEST_KUBECONFIG=$PWD/../data/kind-opscope-test.kubeconfig go test -count=1 -run Live -v ./internal/snapshot/
func TestLoadLive(t *testing.T) {
	path := os.Getenv("OPSCOPE_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("set OPSCOPE_TEST_KUBECONFIG to read a real cluster")
	}
	cluster, err := clusters.NewManager(t.TempDir()).LoadFromFile(path, os.Getenv("OPSCOPE_TEST_CONTEXT"), "live")
	if err != nil {
		t.Fatal(err)
	}

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now()
	s, err := loadCluster(context.Background(), cluster)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)

	runtime.GC()
	runtime.ReadMemStats(&after)

	t.Logf("loaded in %v: %d objects, %d pods, %d warning events", took.Round(time.Millisecond), s.Size(), len(s.Pods), len(s.Events))
	t.Logf("held in memory: about %.1f MiB", float64(after.HeapAlloc-before.HeapAlloc)/(1<<20))
	for _, skipped := range s.Skipped {
		t.Logf("skipped %s: %s %s", skipped.Resource, skipped.Code, skipped.Error)
	}
	runtime.KeepAlive(s)
}
