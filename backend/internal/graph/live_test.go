package graph

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"opscope/internal/bus"
	"opscope/internal/clusters"
	"opscope/internal/collector"
	"opscope/internal/resources"
)

// TestLiveCluster checks the engine on a real cluster: it only reads.
// Set OPSCOPE_TEST_KUBECONFIG (and OPSCOPE_TEST_CONTEXT if needed) to run it.
//
// It starts a collector and the engine, as Opscope does, waits for a live
// graph, optionally keeps it running for OPSCOPE_TEST_WATCH (a duration,
// while you change the cluster), and then checks that the live graph is the
// graph built from the cluster directly, which is what a graph kept up to
// date from the bus must be. It logs what it found, and writes the graph to
// OPSCOPE_TEST_OUT if that's set. With OPSCOPE_TEST_EXPECT=all, every edge
// kind must be there. OPSCOPE_TEST_BUILT=on_request expects a graph built on
// request instead of a live one (a user who can read only some namespaces),
// and OPSCOPE_TEST_NAMESPACE reads one namespace rather than all.
func TestLiveCluster(t *testing.T) {
	path := os.Getenv("OPSCOPE_TEST_KUBECONFIG")
	if path == "" {
		t.Skip("OPSCOPE_TEST_KUBECONFIG isn't set")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager := clusters.NewManager(t.TempDir())
	cluster, err := manager.LoadFromFile(path, os.Getenv("OPSCOPE_TEST_CONTEXT"), "test")
	if err != nil {
		t.Fatal(err)
	}
	set := collector.NewSet(ctx, true, bus.New(), quiet)
	engine := NewEngine(set, quiet)
	go engine.Run(ctx)
	set.Follow(manager)
	direct := resources.Direct(resources.Clients{Kube: cluster.Client, Dynamic: cluster.Dynamic})
	namespace := os.Getenv("OPSCOPE_TEST_NAMESPACE")
	built := os.Getenv("OPSCOPE_TEST_BUILT")
	if built == "" {
		built = BuiltLive
	}

	start := time.Now()
	var info Info
	for {
		var err error
		_, info, err = liveSnapshot(ctx, engine, cluster.ID, direct, namespace)
		if err == nil && info.Built == built {
			break
		}
		if time.Since(start) > 2*time.Minute {
			t.Fatalf("no %s graph after 2 minutes: %+v %v", built, info, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("%s after %s; partial: %+v", info.Built, time.Since(start).Round(time.Millisecond), info.Partial)

	if d, err := time.ParseDuration(os.Getenv("OPSCOPE_TEST_WATCH")); err == nil {
		t.Logf("watching for %s", d)
		time.Sleep(d)
	}

	// The cluster can change between reading the live graph and listing it
	// again, and polled kinds are up to 30 seconds behind, so a difference is
	// tried again for 45 seconds before it counts.
	var live, fresh Snapshot
	for try := range 45 {
		live, _, err = liveSnapshot(ctx, engine, cluster.ID, direct, namespace)
		if err != nil {
			t.Fatal(err)
		}
		if built == BuiltOnRequest {
			break // nothing to compare: it is the graph built from the cluster
		}
		g, _, err := buildFrom(ctx, direct, namespace)
		if err != nil {
			t.Fatal(err)
		}
		fresh = normalized(g.Snapshot())
		if a, b := asJSON(live), asJSON(fresh); a == b {
			break
		} else if try == 44 {
			t.Fatalf("the live graph differs from one built from the cluster:\n%s", firstDifference(a, b))
		}
		time.Sleep(time.Second)
	}
	report(t, live)
	if os.Getenv("OPSCOPE_TEST_EXPECT") == "all" {
		kinds := map[string]bool{}
		for _, e := range live.Edges {
			kinds[e.Kind] = true
		}
		for _, k := range []string{Owns, References, Binds, RunsOn, Selects, Endpoints, Routes, Attaches} {
			if !kinds[k] {
				t.Errorf("no %s edge on this cluster", k)
			}
		}
	}
	if out := os.Getenv("OPSCOPE_TEST_OUT"); out != "" {
		b, _ := json.MarshalIndent(live, "", "  ")
		if err := os.WriteFile(out, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func liveSnapshot(ctx context.Context, e *Engine, cluster string, src resources.Source, namespace string) (Snapshot, Info, error) {
	var s Snapshot
	var info Info
	err := e.Read(ctx, cluster, src, namespace, func(g *Graph, i Info) error {
		s, info = normalized(g.Snapshot()), i
		return nil
	})
	return s, info, err
}

// normalized leaves out what depends on when the graph was read rather than
// on the cluster: evidence times.
func normalized(s Snapshot) Snapshot {
	for i := range s.Edges {
		for j := range s.Edges[i].Evidence {
			s.Edges[i].Evidence[j].Seen = time.Time{}
		}
	}
	return s
}

func asJSON(s Snapshot) string {
	b, _ := json.MarshalIndent(s, "", " ")
	return string(b)
}

// report logs what the graph holds: nodes by presence, edges by kind, and
// every gap.
func report(t *testing.T, s Snapshot) {
	presence, kinds := map[string]int{}, map[string]int{}
	var gaps []string
	for _, n := range s.Nodes {
		presence[n.Presence]++
		if n.Presence != Present {
			gaps = append(gaps, n.ID)
		}
		for _, d := range n.Dangling {
			gaps = append(gaps, fmt.Sprintf("dangling %s %s/%s: %s %s", n.Kind, n.Namespace, n.Name, d.Field, d.Reason))
		}
	}
	for _, e := range s.Edges {
		kinds[e.Kind]++
	}
	slices.Sort(gaps)
	t.Logf("nodes %v, edges %v", presence, kinds)
	t.Logf("gaps:\n  %s", strings.Join(gaps, "\n  "))
}
