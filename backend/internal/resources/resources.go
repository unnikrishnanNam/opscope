// Package resources reads Kubernetes objects and turns them into small,
// flat rows for the UI.
//
// Raw Kubernetes objects are big and deeply nested. Each lister here picks
// only the fields a table shows and works out derived values (like a pod's
// status) in Go, so the frontend stays simple.
package resources

import (
	"context"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Lister lists one kind of resource. An empty namespace means all namespaces.
// It takes kubernetes.Interface (not the concrete client) so tests can pass
// client-go's fake client.
type Lister func(ctx context.Context, client kubernetes.Interface, namespace string) (any, error)

// Listers maps the name used in the URL (/api/clusters/{id}/{resource}) to
// the function that lists it. Adding a resource type means adding a line here.
var Listers = map[string]Lister{
	"namespaces":   listNamespaces,
	"pods":         listPods,
	"deployments":  listDeployments,
	"statefulsets": listStatefulSets,
	"daemonsets":   listDaemonSets,
	"jobs":         listJobs,
	"cronjobs":     listCronJobs,
}

// Meta holds the fields every row has. Row types embed it, and Go's JSON
// encoder writes embedded fields as if they were declared in the row itself.
type Meta struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	Created   time.Time `json:"created"`
}

func metaOf(obj metav1.ObjectMeta) Meta {
	return Meta{Name: obj.Name, Namespace: obj.Namespace, Created: obj.CreationTimestamp.Time}
}

// sortKey is used by sortRows. Every row type gets it through the embedded Meta.
func (m Meta) sortKey() string {
	return m.Namespace + "/" + m.Name
}

// sortRows orders rows by namespace, then name, so the API always returns
// the same order. The [T ...] part makes it work for any row type that has
// a sortKey method (all of ours do, via Meta).
func sortRows[T interface{ sortKey() string }](rows []T) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].sortKey() < rows[j].sortKey() })
}

// timePtr turns an optional Kubernetes time into an optional Go time, which
// the JSON encoder writes as null when it's missing.
func timePtr(t *metav1.Time) *time.Time {
	if t == nil {
		return nil
	}
	return &t.Time
}

// replicas reads an optional replica count. Kubernetes treats "not set" as 1.
func replicas(n *int32) int32 {
	if n == nil {
		return 1
	}
	return *n
}
