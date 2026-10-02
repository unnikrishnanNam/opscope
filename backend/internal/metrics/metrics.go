// Package metrics reads live CPU and memory usage from metrics-server and
// keeps a short history of it.
//
// metrics-server is an add-on that asks every node's kubelet for usage
// numbers and serves them through the Kubernetes API, under the
// metrics.k8s.io group. It only knows the current values; the history
// (for the sparklines) is kept by OpScope itself, in memory.
package metrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// metrics-server's API. Like Gateway API, it isn't part of client-go's typed
// client, so we read it with the dynamic client.
var (
	nodeMetricsAPI = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "nodes"}
	podMetricsAPI  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
)

// ErrUnavailable means the cluster has no working metrics-server.
var ErrUnavailable = errors.New("metrics-server isn't available on this cluster")

// NodeUsage is one node's current usage and how much it can give to pods.
type NodeUsage struct {
	Name              string `json:"name"`
	CPU               int64  `json:"cpu"`               // millicores in use
	Memory            int64  `json:"memory"`            // bytes in use
	CPUAllocatable    int64  `json:"cpuAllocatable"`    // millicores available to pods
	MemoryAllocatable int64  `json:"memoryAllocatable"` // bytes available to pods
}

// PodUsage is one pod's current usage, summed over its containers.
type PodUsage struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	CPU       int64  `json:"cpu"`    // millicores
	Memory    int64  `json:"memory"` // bytes
}

// usage is how metrics-server writes numbers: Kubernetes quantities like
// "57924697n" (nanocores) or "1132Ki".
type usage struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// values turns the quantity strings into millicores and bytes.
func (u usage) values() (cpu, memory int64, err error) {
	c, err := resource.ParseQuantity(u.CPU)
	if err != nil {
		return 0, 0, fmt.Errorf("bad cpu value %q: %w", u.CPU, err)
	}
	m, err := resource.ParseQuantity(u.Memory)
	if err != nil {
		return 0, 0, fmt.Errorf("bad memory value %q: %w", u.Memory, err)
	}
	return c.MilliValue(), m.Value(), nil
}

type nodeMetricsObject struct {
	Metadata metav1.ObjectMeta `json:"metadata"`
	Usage    usage             `json:"usage"`
}

type podMetricsObject struct {
	Metadata   metav1.ObjectMeta `json:"metadata"`
	Containers []struct {
		Usage usage `json:"usage"`
	} `json:"containers"`
}

// ReadNodes returns every node's usage, joined with its allocatable
// resources from the node list (metrics-server only knows usage).
func ReadNodes(ctx context.Context, kube kubernetes.Interface, dyn dynamic.Interface) ([]NodeUsage, error) {
	items, err := list[nodeMetricsObject](ctx, dyn, nodeMetricsAPI, "")
	if err != nil {
		return nil, err
	}

	nodes, err := kube.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	type allocatable struct{ cpu, memory int64 }
	alloc := map[string]allocatable{}
	for _, n := range nodes.Items {
		alloc[n.Name] = allocatable{n.Status.Allocatable.Cpu().MilliValue(), n.Status.Allocatable.Memory().Value()}
	}

	result := make([]NodeUsage, 0, len(items))
	for _, item := range items {
		cpu, memory, err := item.Usage.values()
		if err != nil {
			return nil, err
		}
		a := alloc[item.Metadata.Name]
		result = append(result, NodeUsage{
			Name: item.Metadata.Name, CPU: cpu, Memory: memory,
			CPUAllocatable: a.cpu, MemoryAllocatable: a.memory,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// ReadPods returns every pod's usage in a namespace ("" for all).
func ReadPods(ctx context.Context, dyn dynamic.Interface, namespace string) ([]PodUsage, error) {
	items, err := list[podMetricsObject](ctx, dyn, podMetricsAPI, namespace)
	if err != nil {
		return nil, err
	}

	result := make([]PodUsage, 0, len(items))
	for _, item := range items {
		pod := PodUsage{Namespace: item.Metadata.Namespace, Name: item.Metadata.Name}
		for _, c := range item.Containers {
			cpu, memory, err := c.Usage.values()
			if err != nil {
				return nil, err
			}
			pod.CPU += cpu
			pod.Memory += memory
		}
		result = append(result, pod)
	}
	return result, nil
}

// list fetches a metrics resource and converts each item into T.
func list[T any](ctx context.Context, dyn dynamic.Interface, gvr schema.GroupVersionResource, namespace string) ([]T, error) {
	raw, err := dyn.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	// 404: the metrics API isn't registered at all (metrics-server not installed).
	// 503: it's registered, but metrics-server isn't answering (still starting, or broken).
	if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
		return nil, ErrUnavailable
	}
	if err != nil {
		return nil, err
	}

	items := make([]T, 0, len(raw.Items))
	for _, item := range raw.Items {
		var obj T
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &obj); err != nil {
			return nil, err
		}
		items = append(items, obj)
	}
	return items, nil
}

// Totals adds up node usage for the whole cluster.
func Totals(nodes []NodeUsage) NodeUsage {
	total := NodeUsage{Name: "cluster"}
	for _, n := range nodes {
		total.CPU += n.CPU
		total.Memory += n.Memory
		total.CPUAllocatable += n.CPUAllocatable
		total.MemoryAllocatable += n.MemoryAllocatable
	}
	return total
}

// Now is the clock used for samples; tests replace it.
var Now = time.Now
