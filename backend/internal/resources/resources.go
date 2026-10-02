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
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Query holds the URL options a lister may use.
type Query struct {
	Namespace string // "" means all namespaces
	Type      string // events only: "Warning" or "Normal"; "" means both
}

// Clients holds the two ways of talking to a cluster. Listers take
// interfaces (not the concrete clients) so tests can pass client-go's fakes.
type Clients struct {
	Kube    kubernetes.Interface // typed: built-in kinds like Pods and Services
	Dynamic dynamic.Interface    // untyped: any kind, used for Gateway API custom resources
}

// Lister lists one kind of resource as JSON-ready rows.
type Lister func(ctx context.Context, clients Clients, q Query) (any, error)

// Listers maps the name used in the URL (/api/clusters/{id}/{resource}) to
// the function that lists it. Adding a resource type means adding a line here.
var Listers = map[string]Lister{
	"namespaces":   asLister(listNamespaces),
	"nodes":        asLister(listNodes),
	"events":       asLister(listEvents),
	"pods":         asLister(listPods),
	"deployments":  asLister(listDeployments),
	"statefulsets": asLister(listStatefulSets),
	"daemonsets":   asLister(listDaemonSets),
	"jobs":         asLister(listJobs),
	"cronjobs":     asLister(listCronJobs),
	"configmaps":   asLister(listConfigMaps),
	"secrets":      asLister(listSecrets),
	"services":     asLister(listServices),
	"ingresses":    asLister(listIngresses),

	"gateways":       asDynamicLister(listGateways),
	"httproutes":     asDynamicLister(listHTTPRoutes),
	"gatewayclasses": asDynamicLister(listGatewayClasses),
}

// asLister wraps a typed list function (returning e.g. []Pod) so it fits in
// the Listers map. Keeping the functions typed lets other Go code, like the
// overview, use their results without converting from `any`.
func asLister[T any](list func(context.Context, kubernetes.Interface, Query) ([]T, error)) Lister {
	return func(ctx context.Context, clients Clients, q Query) (any, error) {
		return list(ctx, clients.Kube, q)
	}
}

// asDynamicLister does the same for list functions that use the dynamic client.
func asDynamicLister[T any](list func(context.Context, dynamic.Interface, Query) ([]T, error)) Lister {
	return func(ctx context.Context, clients Clients, q Query) (any, error) {
		return list(ctx, clients.Dynamic, q)
	}
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
