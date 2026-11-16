package signals

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"opscope/internal/bus"
)

var t0 = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

func objMeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("uid-" + name), ResourceVersion: "1"}
}

func controlled(m metav1.ObjectMeta, apiVersion, kind, name string) metav1.ObjectMeta {
	yes := true
	m.OwnerReferences = []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: name, UID: types.UID("uid-" + name), Controller: &yes}}
	return m
}

// crashing is a pod whose container is in CrashLoopBackOff after restarts.
func crashing(name string, restarts int32) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: objMeta("shop", name), Status: corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", RestartCount: restarts,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}},
		}},
	}}
}

func running(name string) *corev1.Pod {
	return &corev1.Pod{ObjectMeta: objMeta("shop", name), Status: corev1.PodStatus{
		Phase: corev1.PodRunning,
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}},
	}}
}

func deployment(name string, desired, ready int32, conditions ...appsv1.DeploymentCondition) *appsv1.Deployment {
	return &appsv1.Deployment{ObjectMeta: objMeta("shop", name), Spec: appsv1.DeploymentSpec{Replicas: &desired},
		Status: appsv1.DeploymentStatus{ReadyReplicas: ready, Conditions: conditions}}
}

func testWorld() *world {
	w := newWorld("lab", Default, t0)
	for _, k := range Default.Kinds() {
		w.setReadable(k, true)
	}
	return w
}

func open(w *world) []Signal {
	return View{w, t0}.Signals(Filter{State: Open})
}

func types_(signals []Signal) []string {
	var out []string
	for _, s := range signals {
		out = append(out, s.Resource.Name+":"+s.Type+":"+s.State)
	}
	return out
}

func TestCrashLoopOpensAtOnceAndResolvesAfterADelay(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("api-1", 3), t0)

	got := open(w)
	if len(got) != 1 || got[0].Type != "crash_loop" || got[0].Severity != Critical || got[0].Count != 3 {
		t.Fatalf("signals = %+v", got)
	}
	s := got[0]
	if s.ID != SignalID("lab", "uid-api-1", "crash_loop") || !s.FirstSeen.Equal(t0) {
		t.Errorf("id %s, first seen %v", s.ID, s.FirstSeen)
	}
	if s.Message != "Container app restarted 3 times; last exit Error (exit 1)" {
		t.Errorf("message = %q", s.Message)
	}
	if len(s.Evidence) != 2 || s.Evidence[0].Field != "status.containerStatuses[0].state.waiting.reason" ||
		s.Evidence[0].Detector != "pods.containers" || !s.Evidence[0].Seen.Equal(t0) {
		t.Errorf("evidence = %+v", s.Evidence)
	}
	if h, _ := (View{w, t0}).Health("uid-api-1"); h.State != Failing || !h.Since.Equal(t0) {
		t.Errorf("health = %+v", h)
	}

	// The restart count rises: still one signal.
	w.upsert("pods", crashing("api-1", 4), t0.Add(time.Minute))
	if got := open(w); len(got) != 1 || got[0].Count != 4 || !got[0].FirstSeen.Equal(t0) {
		t.Errorf("after another restart: %+v", got)
	}

	// It recovers: resolving for 60 seconds, then resolved.
	fixed := t0.Add(2 * time.Minute)
	w.upsert("pods", running("api-1"), fixed)
	if got := open(w); len(got) != 1 || got[0].State != Resolving {
		t.Fatalf("after recovering: %+v", got)
	}
	w.tick(fixed.Add(59 * time.Second))
	if got := open(w); len(got) != 1 {
		t.Fatalf("resolved too soon: %+v", got)
	}
	w.tick(fixed.Add(60 * time.Second))
	if got := open(w); len(got) != 0 {
		t.Fatalf("still open: %+v", got)
	}
	resolved := View{w, t0}.Signals(Filter{State: Resolved})
	if len(resolved) != 1 || !resolved[0].Resolved.Equal(fixed.Add(60*time.Second)) {
		t.Errorf("resolved = %+v", resolved)
	}
	if h, _ := (View{w, t0}).Health("uid-api-1"); h.State != Healthy {
		t.Errorf("health after = %+v", h)
	}
	want := []Transition{
		{UID: "uid-api-1", From: Healthy, To: Failing, At: t0, Signals: []string{s.ID}},
		{UID: "uid-api-1", From: Failing, To: Healthy, At: fixed.Add(60 * time.Second)},
	}
	if got := (View{w, t0}).Transitions("uid-api-1"); !reflect.DeepEqual(got, want) {
		t.Errorf("transitions = %+v", got)
	}

	// The bus heard it open, the health change both ways, and the close.
	var heard []string
	for _, e := range w.drain() {
		heard = append(heard, fmt.Sprintf("%T", e))
	}
	if want := []string{"bus.SignalRaised", "bus.HealthChanged", "bus.SignalResolved", "bus.HealthChanged"}; !slices.Equal(heard, want) {
		t.Errorf("bus = %v", heard)
	}

	// Resolved signals are forgotten after an hour.
	w.tick(fixed.Add(60*time.Second + keepResolved + time.Second))
	if got := (View{w, t0}).Signals(Filter{}); len(got) != 0 {
		t.Errorf("kept = %+v", got)
	}
}

func TestAFlappingConditionIsOneSignal(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("api-1", 1), t0)
	w.upsert("pods", running("api-1"), t0.Add(10*time.Second))
	w.tick(t0.Add(30 * time.Second))
	w.upsert("pods", crashing("api-1", 2), t0.Add(40*time.Second))
	w.tick(t0.Add(5 * time.Minute))

	got := open(w)
	if len(got) != 1 || got[0].State != Open || !got[0].FirstSeen.Equal(t0) {
		t.Fatalf("signals = %+v", got)
	}
	raised := 0
	for _, e := range w.drain() {
		if _, ok := e.(bus.SignalRaised); ok {
			raised++
		}
	}
	if raised != 1 {
		t.Errorf("raised %d times", raised)
	}

	// Once it has resolved, a new occurrence is the same signal again,
	// with a new first-seen time.
	w.upsert("pods", running("api-1"), t0.Add(6*time.Minute))
	w.tick(t0.Add(8 * time.Minute))
	w.upsert("pods", crashing("api-1", 3), t0.Add(9*time.Minute))
	if got := open(w); len(got) != 1 || got[0].ID != SignalID("lab", "uid-api-1", "crash_loop") || !got[0].FirstSeen.Equal(t0.Add(9*time.Minute)) {
		t.Errorf("new occurrence = %+v", got)
	}
}

func TestATypeWithAWaitOpensOnceItHasLasted(t *testing.T) {
	w := testWorld()
	w.upsert("deployments", deployment("web", 2, 1), t0)
	w.tick(t0.Add(2*time.Minute - time.Second))
	if got := open(w); len(got) != 0 {
		t.Fatalf("opened before the wait: %+v", got)
	}
	w.tick(t0.Add(2 * time.Minute))
	got := open(w)
	if len(got) != 1 || got[0].Type != "replicas_unavailable" || got[0].Severity != Warning || !got[0].FirstSeen.Equal(t0) {
		t.Fatalf("signals = %+v", got)
	}
	if got[0].Message != "1 of 2 replicas ready" {
		t.Errorf("message = %q", got[0].Message)
	}

	// None ready is critical.
	w.upsert("deployments", deployment("web", 2, 0), t0.Add(3*time.Minute))
	if got := open(w); got[0].Severity != Critical {
		t.Errorf("severity = %s", got[0].Severity)
	}

	// A condition that goes before its wait is over never opens.
	w.upsert("deployments", deployment("api", 3, 2), t0)
	w.upsert("deployments", deployment("api", 3, 3), t0.Add(time.Minute))
	w.tick(t0.Add(5 * time.Minute))
	for _, s := range open(w) {
		if s.Resource.Name == "api" {
			t.Errorf("api opened: %+v", s)
		}
	}

	// When the object says since when, the wait counts from then: a
	// Deployment unavailable for an hour when Opscope starts is a signal
	// at once.
	long := deployment("old", 1, 0, appsv1.DeploymentCondition{Type: appsv1.DeploymentAvailable,
		Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(t0.Add(-time.Hour))})
	w.upsert("deployments", long, t0)
	var old *Signal
	for _, s := range open(w) {
		if s.Resource.Name == "old" {
			old = &s
		}
	}
	if old == nil || !old.FirstSeen.Equal(t0.Add(-time.Hour)) {
		t.Errorf("old = %+v", old)
	}
}

func TestReplicaSetsOfADeploymentAreLeftToIt(t *testing.T) {
	w := testWorld()
	two := int32(2)
	owned := &appsv1.ReplicaSet{ObjectMeta: controlled(objMeta("shop", "web-1"), "apps/v1", "Deployment", "web"), Spec: appsv1.ReplicaSetSpec{Replicas: &two}}
	alone := &appsv1.ReplicaSet{ObjectMeta: objMeta("shop", "alone"), Spec: appsv1.ReplicaSetSpec{Replicas: &two}}
	w.upsert("replicasets", owned, t0)
	w.upsert("replicasets", alone, t0)
	w.tick(t0.Add(3 * time.Minute))
	if got := types_(open(w)); !slices.Equal(got, []string{"alone:replicas_unavailable:open"}) {
		t.Errorf("signals = %v", got)
	}
}

func TestOrderDoesNotChangeTheAnswer(t *testing.T) {
	objects := []struct {
		resource string
		obj      runtime.Object
	}{
		{"pods", crashing("a", 2)},
		{"pods", running("b")},
		{"pods", &corev1.Pod{ObjectMeta: controlled(objMeta("shop", "web-1-x"), "apps/v1", "ReplicaSet", "web-1"), Status: crashing("x", 5).Status}},
		{"replicasets", &appsv1.ReplicaSet{ObjectMeta: controlled(objMeta("shop", "web-1"), "apps/v1", "Deployment", "web")}},
		{"deployments", deployment("web", 1, 0)},
		{"deployments", deployment("api", 2, 2)},
	}
	var want []Signal
	for i := range 20 {
		w := testWorld()
		order := rand.Perm(len(objects))
		for _, j := range order {
			w.upsert(objects[j].resource, objects[j].obj, t0)
		}
		w.tick(t0.Add(3 * time.Minute))
		got := View{w, t0}.Signals(Filter{})
		if i == 0 {
			want = got
			if len(want) != 3 {
				t.Fatalf("signals = %v", types_(want))
			}
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("order %v gave %v, want %v", order, types_(got), types_(want))
		}
	}
	// The pod's owner, for grouping, is the Deployment above its ReplicaSet.
	for _, s := range want {
		if s.Resource.Name == "web-1-x" && (s.Owner == nil || s.Owner.Kind != "Deployment" || s.Owner.Name != "web" || s.Owner.Resource != "deployments") {
			t.Errorf("owner = %+v", s.Owner)
		}
	}
}

func TestAGapNeverResolvesASignal(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("api-1", 1), t0)
	w.markStale("pods")
	if got := open(w); len(got) != 1 || !got[0].Stale {
		t.Fatalf("after the gap: %+v", got)
	}
	// Nothing is checked while the kind is behind, however long it takes.
	w.upsert("pods", running("api-1"), t0.Add(time.Minute))
	w.tick(t0.Add(10 * time.Minute))
	if got := open(w); len(got) != 1 || got[0].State != Open || !got[0].Stale {
		t.Fatalf("during the gap: %+v", got)
	}
	// Read again, still failing: no longer stale.
	w.replace("pods", []runtime.Object{crashing("api-1", 2)}, t0.Add(11*time.Minute))
	if got := open(w); len(got) != 1 || got[0].Stale || got[0].Count != 2 {
		t.Fatalf("after the relist: %+v", got)
	}
	// A kind that can't be read any more keeps its signals, stale.
	w.setReadable("pods", false)
	w.tick(t0.Add(time.Hour))
	if got := open(w); len(got) != 1 || !got[0].Stale {
		t.Errorf("after losing access: %+v", got)
	}
}

func TestADeletedObjectsSignalsResolveAtOnce(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("api-1", 1), t0)
	w.remove("pods", "uid-api-1", t0.Add(time.Second))
	if got := (View{w, t0}).Signals(Filter{}); len(got) != 1 || got[0].State != Resolved {
		t.Errorf("signals = %+v", got)
	}
	if _, ok := (View{w, t0}).Health("uid-api-1"); ok {
		t.Error("a deleted object has health")
	}
	// Deleting it again, or one never seen, is harmless.
	w.remove("pods", "uid-api-1", t0.Add(2*time.Second))
	w.remove("pods", "uid-nothing", t0)
}

func TestReplaceDropsObjectsThatAreGone(t *testing.T) {
	w := testWorld()
	w.upsert("pods", crashing("a", 1), t0)
	w.upsert("pods", crashing("b", 1), t0)
	w.replace("pods", []runtime.Object{crashing("b", 2)}, t0.Add(time.Second))
	if got := types_(View{w, t0}.Signals(Filter{})); !slices.Equal(got, []string{"b:crash_loop:open", "a:crash_loop:resolved"}) {
		t.Errorf("signals = %v", got)
	}
}

func TestHealthAndCounts(t *testing.T) {
	w := testWorld()
	w.upsert("pods", running("ok"), t0)
	w.upsert("pods", crashing("bad", 1), t0)
	w.upsert("configmaps", &corev1.ConfigMap{ObjectMeta: objMeta("shop", "cfg")}, t0) // not held: no detector reads it
	v := View{w, t0}
	if h, ok := v.Health("uid-ok"); !ok || h.State != Healthy || !h.Since.IsZero() {
		t.Errorf("ok = %+v, %v", h, ok)
	}
	if h, ok := v.Health("uid-cfg"); ok || h.State != Unknown {
		t.Errorf("configmap = %+v, %v", h, ok)
	}
	if got := v.Counts(""); !reflect.DeepEqual(got, map[string]int{"pods": 1}) {
		t.Errorf("counts = %v", got)
	}
	if got := v.Counts("other"); len(got) != 0 {
		t.Errorf("counts elsewhere = %v", got)
	}
	// Only objects whose health changed are shared with the graph.
	n := 0
	w.published.Range(func(_, _ any) bool { n++; return true })
	if n != 1 {
		t.Errorf("published %d", n)
	}
}

func TestTheHistoryIsCapped(t *testing.T) {
	w := testWorld()
	at := t0
	for i := range maxTransitions + maxTransitions/10 + 5 {
		at = at.Add(time.Second)
		w.upsert("pods", crashing(fmt.Sprintf("api-%d", i), 1), at) // one transition each
	}
	if n := len(w.history); n > maxTransitions+maxTransitions/10 || n < maxTransitions {
		t.Errorf("history has %d", n)
	}
	if last := w.history[len(w.history)-1]; !last.At.Equal(at) {
		t.Errorf("the newest was dropped: %+v", last)
	}
}

func TestOnRequestTakesAnUnknownStartAsLongEnough(t *testing.T) {
	w := newWorld("lab", Default, t0)
	w.onRequest = true
	w.setReadable("deployments", true)
	w.upsert("deployments", deployment("web", 2, 1), t0)
	if got := types_(open(w)); !slices.Equal(got, []string{"web:replicas_unavailable:open"}) {
		t.Errorf("signals = %v", got)
	}
}

func TestSignalIDs(t *testing.T) {
	a := SignalID("lab", "uid-1", "crash_loop")
	if a != SignalID("lab", "uid-1", "crash_loop") || len(a) != 32 {
		t.Errorf("id = %q", a)
	}
	for _, other := range []string{SignalID("prod", "uid-1", "crash_loop"), SignalID("lab", "uid-2", "crash_loop"),
		SignalID("lab", "uid-1", "oom_killed"), SignalID("lab", "uid-1c", "rash_loop")} {
		if other == a {
			t.Errorf("collision: %s", other)
		}
	}
}
