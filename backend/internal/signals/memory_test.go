package signals

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// BenchmarkEngineMemory reports what the Signal engine adds on top of the
// watch cache per 1,000 pods, a tenth of them crash-looping with two
// events each, in Deployments of five: the numbers behind the "Memory"
// section in docs/deployment.md. The objects themselves are the cache's,
// shared, so they're made before the measurement starts.
//
//	go test ./internal/signals -run '^$' -bench EngineMemory -benchtime 3x
func BenchmarkEngineMemory(b *testing.B) {
	const pods = 10_000
	objects := memoryObjects(pods)
	var total float64
	for range b.N {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		w := newWorld("lab", Default, t0)
		for _, k := range Default.Kinds() {
			w.setReadable(k, true)
		}
		for _, o := range objects {
			w.upsert(o.resource, o.obj, t0)
		}
		w.tick(t0.Add(3 * time.Minute))
		w.drain()
		runtime.GC()
		runtime.ReadMemStats(&after)
		total += float64(after.HeapAlloc - before.HeapAlloc)
		runtime.KeepAlive(w)
	}
	b.ReportMetric(total/float64(b.N)/pods*1000/1024, "KiB/1000pods")
	b.ReportMetric(0, "ns/op")
}

type memoryObject struct {
	resource string
	obj      k8sruntime.Object
}

func memoryObjects(pods int) []memoryObject {
	var out []memoryObject
	five := int32(5)
	for d := range pods / 5 {
		dep := fmt.Sprintf("app-%d", d)
		ns := fmt.Sprintf("team-%d", d%20)
		m := metav1.ObjectMeta{Namespace: ns, Name: dep, UID: types.UID("uid-" + dep)}
		ready := int32(5)
		if d%2 == 0 {
			ready = 4 // half the Deployments are one short: the failing pods are theirs
		}
		out = append(out, memoryObject{"deployments", &appsv1.Deployment{ObjectMeta: m, Spec: appsv1.DeploymentSpec{Replicas: &five},
			Status: appsv1.DeploymentStatus{ReadyReplicas: ready}}})
		rs := dep + "-5d8c"
		out = append(out, memoryObject{"replicasets", &appsv1.ReplicaSet{ObjectMeta: controlled(
			metav1.ObjectMeta{Namespace: ns, Name: rs, UID: types.UID("uid-" + rs)}, "apps/v1", "Deployment", dep)}})
		for p := range 5 {
			name := fmt.Sprintf("%s-%d", rs, p)
			pm := controlled(metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("uid-" + name)}, "apps/v1", "ReplicaSet", rs)
			pod := running(name)
			if d%2 == 0 && p == 0 {
				pod = crashing(name, 12)
				for e := range 2 {
					ev := fmt.Sprintf("%s.%d", name, e)
					out = append(out, memoryObject{"events", &corev1.Event{
						ObjectMeta:     metav1.ObjectMeta{Namespace: ns, Name: ev, UID: types.UID("uid-" + ev)},
						InvolvedObject: corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: name, UID: pm.UID},
						Reason:         "BackOff", Type: corev1.EventTypeWarning, Count: 12,
					}})
				}
			}
			pod.ObjectMeta = pm
			out = append(out, memoryObject{"pods", pod})
		}
	}
	return out
}
