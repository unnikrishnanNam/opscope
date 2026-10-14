package graph

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// now is when every test snapshot is taken. Objects are an hour old unless
// a test says otherwise.
var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

// uids makes every object's UID unique, even for objects of different kinds
// with the same name (a Deployment and a Service called "web").
var uids atomic.Int64

// meta builds object metadata with a UID (the snapshot finds owners by UID)
// and labels given as key, value pairs.
func meta(namespace, name string, labelPairs ...string) metav1.ObjectMeta {
	m := metav1.ObjectMeta{
		Namespace:         namespace,
		Name:              name,
		UID:               types.UID(fmt.Sprintf("uid-%d", uids.Add(1))),
		CreationTimestamp: metav1.NewTime(now.Add(-time.Hour)),
	}
	if len(labelPairs) > 0 {
		m.Labels = map[string]string{}
		for i := 0; i+1 < len(labelPairs); i += 2 {
			m.Labels[labelPairs[i]] = labelPairs[i+1]
		}
	}
	return m
}

func ownedBy(m metav1.ObjectMeta, kind string, owner metav1.ObjectMeta) metav1.ObjectMeta {
	m.OwnerReferences = append(m.OwnerReferences, metav1.OwnerReference{Kind: kind, Name: owner.Name, UID: owner.UID, Controller: ptr(true)})
	return m
}

func age(m metav1.ObjectMeta, d time.Duration) metav1.ObjectMeta {
	m.CreationTimestamp = metav1.NewTime(now.Add(-d))
	return m
}

// readyPod is a running pod with every container ready, on a node.
func readyPod(m metav1.ObjectMeta, node string, spec ...corev1.PodSpec) corev1.Pod {
	pod := corev1.Pod{ObjectMeta: m}
	if len(spec) > 0 {
		pod.Spec = spec[0]
	}
	pod.Spec.NodeName = node
	pod.Status = corev1.PodStatus{
		Phase:             corev1.PodRunning,
		Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}},
	}
	return pod
}

// waitingPod is a pod whose container waits for the given reason.
func waitingPod(m metav1.ObjectMeta, reason, message string) corev1.Pod {
	return corev1.Pod{ObjectMeta: m, Status: corev1.PodStatus{
		Phase:      corev1.PodPending,
		Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}},
		ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}},
		}},
	}}
}

func readyNode(name string) corev1.Node {
	return corev1.Node{ObjectMeta: meta("", name), Status: corev1.NodeStatus{
		Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
	}}
}

func deployment(m metav1.ObjectMeta, desired, ready int32) appsv1.Deployment {
	return appsv1.Deployment{ObjectMeta: m, Spec: appsv1.DeploymentSpec{Replicas: ptr(desired)},
		Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: desired}}
}

func replicaSet(m metav1.ObjectMeta, desired, ready int32) appsv1.ReplicaSet {
	return appsv1.ReplicaSet{ObjectMeta: m, Spec: appsv1.ReplicaSetSpec{Replicas: ptr(desired)},
		Status: appsv1.ReplicaSetStatus{Replicas: desired, ReadyReplicas: ready}}
}

// build indexes a snapshot made by hand and builds its graph.
func build(s *snapshot.Snapshot) *Graph {
	s.Taken = now
	s.BuildIndex()
	return Build(s)
}

// unreadable marks kinds as skipped, as Load does when it can't list them.
func unreadable(s *snapshot.Snapshot, code string, resources ...string) {
	for _, r := range resources {
		s.Skipped = append(s.Skipped, kube.Skipped{Resource: r, Code: code})
	}
}

// findEdge returns the edge from → to of a type, failing the test if
// there's none.
func findEdge(t *testing.T, g *Graph, from, to, typ string) *Edge {
	t.Helper()
	for _, e := range g.Out(from) {
		if e.To == to && e.Type == typ {
			return e
		}
	}
	t.Fatalf("no %s edge %s → %s; edges from it: %s", typ, from, to, describe(g.Out(from)))
	return nil
}

func noEdge(t *testing.T, g *Graph, from, to string) {
	t.Helper()
	for _, e := range g.Out(from) {
		if e.To == to {
			t.Errorf("unexpected %s edge %s → %s", e.Type, from, to)
		}
	}
}

func node(t *testing.T, g *Graph, id string) *Node {
	t.Helper()
	n, ok := g.Node(id)
	if !ok {
		t.Fatalf("no node %s", id)
	}
	return n
}

func wantState(t *testing.T, g *Graph, id string, state State) {
	t.Helper()
	if n := node(t, g, id); n.State != state {
		t.Errorf("%s: state %s (%q, %q), want %s", id, n.State, n.Status, n.Message, state)
	}
}

func describe(edges []*Edge) string {
	out := ""
	for _, e := range edges {
		out += " " + e.Type + ">" + e.To
	}
	return out
}

func ids(nodes []*Node) []string {
	var out []string
	for _, n := range nodes {
		out = append(out, n.ID)
	}
	return out
}

func hasID(nodes []*Node, id string) bool {
	return slices.Contains(ids(nodes), id)
}

func ptr[T any](v T) *T { return &v }
