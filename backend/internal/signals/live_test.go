package signals

import (
	"context"
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

// issuesScenario is what test/scenarios/issues.yaml raises: "name:type",
// where a name ending in "-" is a prefix (a Deployment's pods and
// ReplicaSets), and a "?" at the end marks one that comes and goes with the
// moment (the OOM-killed pod is crash-looping only between kills). See
// test/scenarios/README.md.
var issuesScenario = []string{
	"crash:crash_loop",
	"oom:oom_killed", "oom:crash_loop?",
	"once:container_error",
	"bad-image:image_pull_failed",
	"no-secret:config_missing", "no-secret:reference_missing",
	"bad-init:init_failing",
	"nowhere:unschedulable",
	"no-volume:mount_failed", "no-volume:reference_missing",
	"unready:probe_failing", "unready:no_ready_endpoints",
	"typo:selector_matches_nothing",
	"stuck:replicas_unavailable", "stuck:rollout_stuck", "stuck-:image_pull_failed",
	"no-account:replicas_unavailable", "no-account-:create_failed",
	"no-config:reference_missing", "no-config:replicas_unavailable", "no-config-:config_missing",
	"report:job_failed",
	"old:backend_missing",
	"orphan:pvc_unbound",
	"expiring-cert:cert_expiring",
}

// TestLiveCluster runs the engine on a real cluster, as Opscope does: a
// collector, the bus and the live world. It only reads. Set
// OPSCOPE_TEST_KUBECONFIG to run it, and OPSCOPE_TEST_NAMESPACE to look at
// one namespace. It logs every open signal. With OPSCOPE_TEST_EXPECT=issues
// it checks issues.yaml's namespace: every signal the README promises,
// within OPSCOPE_TEST_WAIT (30s by default) of the engine starting, and no
// others.
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
	engine := NewEngine(set, Default, quiet)
	go engine.Run(ctx)
	set.Follow(manager)
	src := resources.Direct(resources.Clients{Kube: cluster.Client, Dynamic: cluster.Dynamic})

	namespace := os.Getenv("OPSCOPE_TEST_NAMESPACE")
	var want []string
	if os.Getenv("OPSCOPE_TEST_EXPECT") == "issues" {
		namespace, want = "issues-test", issuesScenario
	}
	wait := 30 * time.Second
	if d, err := time.ParseDuration(os.Getenv("OPSCOPE_TEST_WAIT")); err == nil {
		wait = d
	}

	start := time.Now()
	var got []Signal
	var info Info
	for {
		err := engine.Read(ctx, cluster.ID, src, namespace, func(v View, i Info) error {
			got, info = v.Signals(Filter{Namespace: namespace, State: Open}), i
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if info.Built == BuiltLive && (want == nil || len(missingFrom(got, want)) == 0) || time.Since(start) > wait {
			break
		}
		time.Sleep(time.Second)
	}
	t.Logf("%s after %s, partial %v", info.Built, time.Since(start).Round(time.Second), info.Partial)
	for _, s := range got {
		t.Logf("%-10s %-9s %-28s %s/%s: %s (since %s)", s.Severity, s.State, s.Type, s.Resource.Kind, s.Resource.Name, s.Message,
			s.FirstSeen.Format(time.TimeOnly))
	}
	if info.Built != BuiltLive {
		t.Errorf("the world wasn't live after %s", wait)
	}
	if want == nil {
		return
	}
	for _, m := range missingFrom(got, want) {
		t.Errorf("missing: %s", m)
	}
	for _, s := range got {
		if !slices.ContainsFunc(want, func(w string) bool { return matches(s, w) }) {
			t.Errorf("not expected: %s:%s", s.Resource.Name, s.Type)
		}
	}
}

func matches(s Signal, want string) bool {
	name, typ, _ := strings.Cut(strings.TrimSuffix(want, "?"), ":")
	if s.Type != typ {
		return false
	}
	if strings.HasSuffix(name, "-") {
		return strings.HasPrefix(s.Resource.Name, name)
	}
	return s.Resource.Name == name
}

func missingFrom(got []Signal, want []string) []string {
	var out []string
	for _, w := range want {
		if strings.HasSuffix(w, "?") {
			continue
		}
		if !slices.ContainsFunc(got, func(s Signal) bool { return matches(s, w) }) {
			out = append(out, w)
		}
	}
	return out
}
