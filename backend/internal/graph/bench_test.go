package graph

import (
	"fmt"
	"runtime"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// apps makes a namespace of n apps, each a Deployment with its ReplicaSet,
// pods, a Service and its EndpointSlice, a ConfigMap and a Secret, with
// the pods spread over the given nodes.
func apps(ns string, n, podsEach int, nodes []string) []Object {
	var out []Object
	for i := range n {
		name := fmt.Sprintf("app-%03d", i)
		labels := map[string]string{"app": name}
		spec := corev1.PodSpec{
			ServiceAccountName: "default",
			Volumes: []corev1.Volume{
				{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-config"},
				}}},
				{Name: "kube-api-access", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{{ConfigMap: &corev1.ConfigMapProjection{
						LocalObjectReference: corev1.LocalObjectReference{Name: kubeRootCA},
					}}},
				}}},
			},
			Containers: []corev1.Container{{Name: "app", Env: []corev1.EnvVar{{Name: "PASSWORD", ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name + "-secret"}, Key: "p"},
			}}}}},
		}
		meta := func(resource, n string) metav1.ObjectMeta {
			m := om(resource, ns, n)
			m.UID = uidOf(resource, ns+"-"+n)
			return m
		}
		ownedBy := func(apiVersion, kind, resource, n string) []metav1.OwnerReference {
			yes := true
			return []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: n, UID: uidOf(resource, ns+"-"+n), Controller: &yes}}
		}

		out = append(out,
			Object{"configmaps", &corev1.ConfigMap{ObjectMeta: meta("configmaps", name+"-config")}, seen},
			Object{"secrets", &corev1.Secret{ObjectMeta: meta("secrets", name+"-secret")}, seen},
			Object{"deployments", &appsv1.Deployment{ObjectMeta: meta("deployments", name), Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{Spec: spec},
			}}, seen},
		)
		rs := meta("replicasets", name+"-5d8c")
		rs.OwnerReferences = ownedBy("apps/v1", "Deployment", "deployments", name)
		out = append(out, Object{"replicasets", &appsv1.ReplicaSet{ObjectMeta: rs}, seen})

		svc := meta("services", name)
		out = append(out, Object{"services", &corev1.Service{ObjectMeta: svc, Spec: corev1.ServiceSpec{Selector: labels}}, seen})
		slice := &discoveryv1.EndpointSlice{ObjectMeta: meta("endpointslices", name+"-x")}
		slice.OwnerReferences = ownedBy("v1", "Service", "services", name)

		for p := range podsEach {
			podName := fmt.Sprintf("%s-5d8c-%d", name, p)
			m := meta("pods", podName)
			m.Labels = labels
			m.OwnerReferences = ownedBy("apps/v1", "ReplicaSet", "replicasets", name+"-5d8c")
			s := *spec.DeepCopy()
			s.NodeName = nodes[(i*podsEach+p)%len(nodes)]
			out = append(out, Object{"pods", &corev1.Pod{ObjectMeta: m, Spec: s}, seen})
			slice.Endpoints = append(slice.Endpoints, discoveryv1.Endpoint{
				TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: podName, UID: m.UID},
			})
		}
		out = append(out, Object{"endpointslices", slice, seen})
	}
	return out
}

func nodeNames(n int) ([]string, []Object) {
	var names []string
	var objects []Object
	for i := range n {
		name := fmt.Sprintf("node-%02d", i)
		names = append(names, name)
		objects = append(objects, node(name))
	}
	return names, objects
}

// A namespace of about 1,000 objects: 110 apps of 3 pods.
func BenchmarkBuildNamespace(b *testing.B) {
	names, nodes := nodeNames(10)
	objects := append(nodes, apps("shop", 110, 3, names)...)
	b.ReportMetric(float64(len(objects)), "objects")
	for b.Loop() {
		Build(nil, objects)
	}
}

// A cluster of 5,000 pods: 50 namespaces of 20 apps of 5 pods, on 50 nodes.
func BenchmarkBuildCluster(b *testing.B) {
	names, objects := nodeNames(50)
	for i := range 50 {
		objects = append(objects, apps(fmt.Sprintf("ns-%02d", i), 20, 5, names)...)
	}
	b.ReportMetric(float64(len(objects)), "objects")
	for b.Loop() {
		Build(nil, objects)
	}
}

// One pod's status changing in the 5,000-pod cluster: the most common change.
func BenchmarkPodUpdate(b *testing.B) {
	names, objects := nodeNames(50)
	for i := range 50 {
		objects = append(objects, apps(fmt.Sprintf("ns-%02d", i), 20, 5, names)...)
	}
	g := Build(nil, objects)
	var p Object
	for _, o := range objects {
		if o.Resource == "pods" {
			p = o
			break
		}
	}
	pod := p.Object.(*corev1.Pod)
	i := 0
	for b.Loop() {
		i++
		pod.ResourceVersion = fmt.Sprint(i)
		g.Upsert(p)
	}
}

// One pod's labels changing in a namespace of 110 Services and 330 pods,
// which checks every selector there again.
func BenchmarkPodRelabel(b *testing.B) {
	names, nodes := nodeNames(10)
	objects := append(nodes, apps("shop", 110, 3, names)...)
	g := Build(nil, objects)
	var p Object
	for _, o := range objects {
		if o.Resource == "pods" {
			p = o
			break
		}
	}
	pod := p.Object.(*corev1.Pod)
	i := 0
	for b.Loop() {
		i++
		pod.Labels = map[string]string{"app": "app-000", "n": fmt.Sprint(i % 2)}
		g.Upsert(p)
	}
}

// BenchmarkGraphMemory reports how much the graph itself holds per 1,000
// objects, on top of the cache: in a live graph the objects are the
// cache's, and the graph keeps its nodes, edges, evidence and indexes.
//
//	go test ./internal/graph -run '^$' -bench GraphMemory -benchtime 3x
func BenchmarkGraphMemory(b *testing.B) {
	names, objects := nodeNames(50)
	for i := range 50 {
		objects = append(objects, apps(fmt.Sprintf("ns-%02d", i), 20, 5, names)...)
	}
	var total float64
	for range b.N {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		g := Build(nil, objects)
		runtime.GC()
		runtime.ReadMemStats(&after)
		runtime.KeepAlive(g)
		total += float64(after.HeapAlloc) - float64(before.HeapAlloc)
	}
	b.ReportMetric(float64(len(objects)), "objects")
	b.ReportMetric(total/float64(b.N)/float64(len(objects))*1000/1024, "KiB/1000")
	b.ReportMetric(0, "ns/op")
}
