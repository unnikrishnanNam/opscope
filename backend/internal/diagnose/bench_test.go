package diagnose

import (
	"fmt"
	"testing"

	"opscope/internal/graph"
	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// large is a cluster of 1,000 Deployments in 40 namespaces with 5 pods
// each (5,000 pods) on 50 nodes, a Service per Deployment, and one in ten
// Deployments broken: its pods can't start for want of a Secret.
func large() *snapshot.Snapshot {
	s := &snapshot.Snapshot{}
	for i := range 50 {
		s.Nodes = append(s.Nodes, ready(fmt.Sprintf("node-%d", i)))
	}
	for d := range 1000 {
		ns, name := fmt.Sprintf("team-%d", d%40), fmt.Sprintf("app-%d", d)
		broken := d%10 == 0
		spec := corev1.PodSpec{Containers: []corev1.Container{{Name: "app", EnvFrom: []corev1.EnvFromSource{
			{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}}}}}}}
		deploy := meta(ns, name, "app", name)
		rs := meta(ns, name+"-7d9f")
		rs.OwnerReferences = []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: deploy.UID}}
		ready := int32(5)
		if broken {
			ready = 0
		} else {
			s.Secrets = append(s.Secrets, metav1.PartialObjectMetadata{ObjectMeta: meta(ns, name)})
		}
		s.Deployments = append(s.Deployments, appsv1.Deployment{ObjectMeta: deploy, Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(5)),
			Template: corev1.PodTemplateSpec{Spec: spec}}, Status: appsv1.DeploymentStatus{ReadyReplicas: ready, UpdatedReplicas: 5}})
		s.ReplicaSets = append(s.ReplicaSets, appsv1.ReplicaSet{ObjectMeta: rs, Spec: appsv1.ReplicaSetSpec{Replicas: ptr(int32(5))},
			Status: appsv1.ReplicaSetStatus{Replicas: 5, ReadyReplicas: ready}})
		s.Services = append(s.Services, corev1.Service{ObjectMeta: meta(ns, name), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": name}}})
		for p := range 5 {
			pod := waiting(meta(ns, fmt.Sprintf("%s-%d", rs.Name, p), "app", name), "app", "CreateContainerConfigError", fmt.Sprintf(`secret %q not found`, name))
			if !broken {
				pod.Status = corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
					ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}
			}
			pod.Spec = spec
			pod.Spec.NodeName = fmt.Sprintf("node-%d", (d+p)%50)
			pod.OwnerReferences = []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID}}
			s.Pods = append(s.Pods, pod)
		}
	}
	// No EndpointSlices here: Services are judged by their pods' readiness.
	s.Skipped = []kube.Skipped{{Resource: "endpointslices", Code: "forbidden"}}
	s.Taken = now
	s.BuildIndex()
	return s
}

// BenchmarkBuildAndDiagnose is the work done once per snapshot: the graph,
// then every rule on every object. The plan's budget is 200 ms.
func BenchmarkBuildAndDiagnose(b *testing.B) {
	s := large()
	b.ReportAllocs()
	for b.Loop() {
		graph.Build(s)
		Run(s)
	}
}

// BenchmarkProblems is one request for the overview's problems.
func BenchmarkProblems(b *testing.B) {
	d := Run(large())
	if got := len(d.Problems("")); got != 100 {
		b.Fatalf("want a problem per broken Deployment's missing Secret, got %d", got)
	}
	b.ReportAllocs()
	for b.Loop() {
		d.Problems("team-1")
	}
}
