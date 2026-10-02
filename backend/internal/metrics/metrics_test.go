package metrics

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// fakeMetrics is a fake dynamic client serving the given metrics objects.
func fakeMetrics(t *testing.T, nodes, pods []map[string]any) *dynamicfake.FakeDynamicClient {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{
		nodeMetricsAPI: "NodeMetricsList",
		podMetricsAPI:  "PodMetricsList",
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	add := func(gvr schema.GroupVersionResource, kind string, obj map[string]any) {
		obj["apiVersion"] = "metrics.k8s.io/v1beta1"
		obj["kind"] = kind
		u := &unstructured.Unstructured{Object: obj}
		if err := client.Tracker().Create(gvr, u, u.GetNamespace()); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range nodes {
		add(nodeMetricsAPI, "NodeMetrics", n)
	}
	for _, p := range pods {
		add(podMetricsAPI, "PodMetrics", p)
	}
	return client
}

func TestReadNodesJoinsAllocatable(t *testing.T) {
	dyn := fakeMetrics(t, []map[string]any{{
		"metadata": map[string]any{"name": "worker-1"},
		// metrics-server reports CPU in nanocores: 57924697n is about 58 millicores.
		"usage": map[string]any{"cpu": "57924697n", "memory": "1Gi"},
	}}, nil)
	kube := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
		Status: corev1.NodeStatus{Allocatable: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		}},
	})

	nodes, err := ReadNodes(context.Background(), kube, dyn)
	if err != nil {
		t.Fatal(err)
	}
	n := nodes[0]
	if n.CPU != 58 || n.Memory != 1<<30 || n.CPUAllocatable != 2000 || n.MemoryAllocatable != 4<<30 {
		t.Errorf("unexpected node usage: %+v", n)
	}
}

func TestReadPodsSumsContainers(t *testing.T) {
	dyn := fakeMetrics(t, nil, []map[string]any{{
		"metadata": map[string]any{"name": "api-1", "namespace": "web"},
		"containers": []any{
			map[string]any{"name": "app", "usage": map[string]any{"cpu": "100m", "memory": "100Mi"}},
			map[string]any{"name": "proxy", "usage": map[string]any{"cpu": "20m", "memory": "28Mi"}},
		},
	}})

	pods, err := ReadPods(context.Background(), dyn, "web")
	if err != nil {
		t.Fatal(err)
	}
	if p := pods[0]; p.CPU != 120 || p.Memory != 128<<20 || p.Name != "api-1" {
		t.Errorf("unexpected pod usage: %+v", p)
	}
}

func TestMissingMetricsServer(t *testing.T) {
	// 404: the metrics API isn't registered. 503: registered, but not answering.
	for _, failure := range []error{
		apierrors.NewNotFound(schema.GroupResource{Group: "metrics.k8s.io", Resource: "nodes"}, ""),
		apierrors.NewServiceUnavailable("the server is currently unable to handle the request"),
	} {
		dyn := fakeMetrics(t, nil, nil)
		dyn.PrependReactor("list", "*", func(clienttesting.Action) (bool, runtime.Object, error) {
			return true, nil, failure
		})
		if _, err := ReadNodes(context.Background(), fake.NewClientset(), dyn); !errors.Is(err, ErrUnavailable) {
			t.Errorf("for %v: got %v, want ErrUnavailable", failure, err)
		}
	}
}

func TestHistoryKeepsFifteenMinutes(t *testing.T) {
	h := NewHistory()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range 100 {
		h.Record("lab", []NodeUsage{{Name: "a", CPU: int64(i)}, {Name: "b", CPU: 1}}, start.Add(time.Duration(i)*SampleEvery))
	}

	total, nodes := h.Cluster("lab")
	if len(total) != keepSamples {
		t.Fatalf("kept %d samples, want %d", len(total), keepSamples)
	}
	if total[0].CPU != 41 || total[len(total)-1].CPU != 100 { // oldest kept: i=40 (+1 from node b)
		t.Errorf("unexpected oldest/newest totals: %d, %d", total[0].CPU, total[len(total)-1].CPU)
	}
	if len(nodes["a"]) != keepSamples || len(nodes["b"]) != keepSamples {
		t.Errorf("per-node history has %d and %d samples", len(nodes["a"]), len(nodes["b"]))
	}

	// A node that disappears, and a cluster that's removed, are forgotten.
	h.Record("lab", []NodeUsage{{Name: "a"}}, start)
	if _, nodes := h.Cluster("lab"); nodes["b"] != nil {
		t.Error("history of a removed node should be dropped")
	}
	h.Forget(map[string]bool{})
	if total, _ := h.Cluster("lab"); len(total) != 0 {
		t.Error("history of a removed cluster should be dropped")
	}
}
