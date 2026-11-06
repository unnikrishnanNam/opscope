package resources

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

// Source is where list functions read objects from: a collector's watch
// cache, or the cluster itself (Direct). The row code doesn't know which.
type Source interface {
	// List returns the objects of one kind, in q.Namespace for namespaced
	// kinds ("" for all): typed objects for built-in kinds (*corev1.Pod,
	// ...), *unstructured.Unstructured for Gateway API. The objects may be
	// shared with a cache, so they must not be changed.
	List(ctx context.Context, resource string, q Query) ([]runtime.Object, error)
}

// ErrNoHistory is the answer to a query for a moment in the past
// (Query.At). Only "now" can be read until Opscope records history.
var ErrNoHistory = errors.New("Opscope doesn't record history yet; only the current state can be read")

// ListOf lists one kind from src as typed objects, e.g.
// ListOf[*corev1.Pod](ctx, src, "pods", q).
func ListOf[T runtime.Object](ctx context.Context, src Source, resource string, q Query) ([]T, error) {
	if !q.At.IsZero() {
		return nil, ErrNoHistory
	}
	objects, err := src.List(ctx, resource, q)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(objects))
	for _, obj := range objects {
		typed, ok := obj.(T)
		if !ok {
			return nil, fmt.Errorf("listing %s: got %T", resource, obj)
		}
		out = append(out, typed)
	}
	return out, nil
}

// Direct reads straight from the cluster, one list request per call, as
// Opscope did before it had a cache. It's what a cluster's pages read when
// collectors are off, and what the cache falls back to.
func Direct(clients Clients) Source {
	return direct{clients}
}

type direct struct {
	clients Clients
}

// typedList lists one built-in kind with the typed client. Cluster-wide
// kinds ignore the namespace.
type typedList func(ctx context.Context, kube kubernetes.Interface, namespace string) (runtime.Object, error)

var typedLists = map[string]typedList{
	"namespaces": func(ctx context.Context, k kubernetes.Interface, _ string) (runtime.Object, error) {
		return k.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	},
	"nodes": func(ctx context.Context, k kubernetes.Interface, _ string) (runtime.Object, error) {
		return k.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	},
	"events": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
	},
	"pods": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
	},
	"deployments": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{})
	},
	"statefulsets": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{})
	},
	"daemonsets": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{})
	},
	"jobs": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{})
	},
	"cronjobs": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{})
	},
	"configmaps": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().ConfigMaps(ns).List(ctx, metav1.ListOptions{})
	},
	"secrets": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().Secrets(ns).List(ctx, metav1.ListOptions{})
	},
	"services": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().Services(ns).List(ctx, metav1.ListOptions{})
	},
	"ingresses": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
	},
	"persistentvolumeclaims": func(ctx context.Context, k kubernetes.Interface, ns string) (runtime.Object, error) {
		return k.CoreV1().PersistentVolumeClaims(ns).List(ctx, metav1.ListOptions{})
	},
	"persistentvolumes": func(ctx context.Context, k kubernetes.Interface, _ string) (runtime.Object, error) {
		return k.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	},
	"storageclasses": func(ctx context.Context, k kubernetes.Interface, _ string) (runtime.Object, error) {
		return k.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	},
}

func (d direct) List(ctx context.Context, resource string, q Query) ([]runtime.Object, error) {
	if list, ok := typedLists[resource]; ok {
		result, err := list(ctx, d.clients.Kube, q.Namespace)
		if err != nil {
			return nil, err
		}
		// ExtractList returns pointers into the list's Items.
		return apimeta.ExtractList(result)
	}

	info, ok := kinds[resource]
	if !ok || info.GVR.Group != gatewayAPI("").Group {
		return nil, fmt.Errorf("no way to list %q", resource)
	}
	namespace := q.Namespace
	if !info.Namespaced {
		namespace = ""
	}
	list, err := d.clients.Dynamic.Resource(info.GVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if apierrors.IsNotFound(err) {
		// The API server answers 404 for a resource type it doesn't know.
		return nil, ErrGatewayAPINotInstalled
	}
	if err != nil {
		return nil, err
	}
	objects := make([]runtime.Object, len(list.Items))
	for i := range list.Items {
		objects[i] = &list.Items[i]
	}
	return objects, nil
}
