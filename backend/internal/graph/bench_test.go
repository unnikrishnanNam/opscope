package graph

import (
	"fmt"
	"testing"

	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
)

// large is a cluster of 1,000 Deployments in 40 namespaces, each with a
// ReplicaSet, 5 pods, a Service and a ConfigMap its pods use: 5,000 pods on
// 50 nodes.
func large() *snapshot.Snapshot {
	s := &snapshot.Snapshot{}
	for i := range 50 {
		s.Nodes = append(s.Nodes, readyNode(fmt.Sprintf("node-%d", i)))
	}
	for d := range 1000 {
		ns := fmt.Sprintf("team-%d", d%40)
		name := fmt.Sprintf("app-%d", d)
		spec := corev1.PodSpec{Volumes: []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}}}}
		deploy := deployment(meta(ns, name, "app", name), 5, 5)
		deploy.Spec.Template.Spec = spec
		rs := ownedBy(meta(ns, name+"-7d9f"), "Deployment", deploy.ObjectMeta)
		s.Deployments = append(s.Deployments, deploy)
		s.ReplicaSets = append(s.ReplicaSets, replicaSet(rs, 5, 5))
		s.Services = append(s.Services, corev1.Service{ObjectMeta: meta(ns, name), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": name}}})
		s.ConfigMaps = append(s.ConfigMaps, snapshot.ConfigMap{ObjectMeta: meta(ns, name)})
		for p := range 5 {
			pod := ownedBy(meta(ns, fmt.Sprintf("%s-%d", rs.Name, p), "app", name), "ReplicaSet", rs)
			s.Pods = append(s.Pods, readyPod(pod, fmt.Sprintf("node-%d", (d+p)%50), spec))
		}
	}
	unreadable(s, "forbidden", "endpointslices")
	s.Taken = now
	s.BuildIndex()
	return s
}

func BenchmarkBuild(b *testing.B) {
	s := large()
	b.ReportAllocs()
	for b.Loop() {
		Build(s)
	}
}

func BenchmarkNamespaceView(b *testing.B) {
	g := Build(large())
	b.ReportAllocs()
	for b.Loop() {
		g.Namespace("team-1", Options{})
	}
}
