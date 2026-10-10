package snapshot

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// generated builds a snapshot shaped like a large cluster: 1,000
// Deployments with a ReplicaSet and 5 pods each (5,000 pods), a Service and
// ConfigMap per Deployment, 50 nodes and 10,000 warning events.
func generated() *Snapshot {
	s := &Snapshot{Taken: time.Now()}
	for i := range 50 {
		s.Nodes = append(s.Nodes, corev1.Node{ObjectMeta: meta("", fmt.Sprintf("node-%d", i))})
	}
	for d := range 1000 {
		ns := fmt.Sprintf("team-%d", d%40)
		deploy := meta(ns, fmt.Sprintf("app-%d", d))
		rs := ownedBy(meta(ns, deploy.Name+"-7d9f"), "Deployment", deploy)
		s.Deployments = append(s.Deployments, appsv1.Deployment{ObjectMeta: deploy})
		s.ReplicaSets = append(s.ReplicaSets, appsv1.ReplicaSet{ObjectMeta: rs})
		s.Services = append(s.Services, corev1.Service{ObjectMeta: meta(ns, deploy.Name)})
		s.ConfigMaps = append(s.ConfigMaps, ConfigMap{ObjectMeta: meta(ns, deploy.Name+"-config"), Keys: []string{"app.yaml"}})
		for p := range 5 {
			pod := ownedBy(meta(ns, fmt.Sprintf("%s-%d", rs.Name, p)), "ReplicaSet", rs)
			s.Pods = append(s.Pods, corev1.Pod{ObjectMeta: pod, Spec: corev1.PodSpec{NodeName: fmt.Sprintf("node-%d", p*d%50)}})
		}
	}
	for i := range 10_000 {
		pod := s.Pods[i%len(s.Pods)]
		s.Events = append(s.Events, corev1.Event{
			ObjectMeta:     meta(pod.Namespace, fmt.Sprintf("event-%d", i)),
			Type:           corev1.EventTypeWarning,
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: pod.Name, UID: pod.UID},
			LastTimestamp:  metav1.NewTime(s.Taken.Add(-time.Duration(i) * time.Second)),
		})
	}
	return s
}

// BenchmarkBuildIndex measures the work done on every snapshot after the
// lists are read. Loading itself is dominated by the API server and the
// network; measure it on a real cluster with TestLoadLive.
func BenchmarkBuildIndex(b *testing.B) {
	s := generated()
	b.ReportAllocs()
	for b.Loop() {
		s.buildIndex()
	}
	b.ReportMetric(float64(s.Size()), "objects")
}
