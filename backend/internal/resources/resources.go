// Package resources reads Kubernetes objects and turns them into small,
// flat rows for the UI.
//
// Raw Kubernetes objects are big and deeply nested. Each lister here picks
// only the fields a table shows and works out derived values (like a pod's
// status) in Go, so the frontend stays simple.
package resources

import (
	"context"
	"maps"
	"slices"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// Query holds the URL options a lister may use.
type Query struct {
	Namespace string    // "" means all namespaces
	Type      string    // events only: "Warning" or "Normal"; "" means both
	At        time.Time // the moment to read; zero means now, the only one there is yet (see ErrNoHistory)
}

// Clients holds the two ways of talking to a cluster, for what's read from
// it directly: the Direct source, and a detail page's own object. They're
// interfaces (not the concrete clients) so tests can pass client-go's fakes.
type Clients struct {
	Kube    kubernetes.Interface // typed: built-in kinds like Pods and Services
	Dynamic dynamic.Interface    // untyped: any kind, used for Gateway API custom resources
}

// Lister lists one kind of resource as JSON-ready rows.
type Lister func(ctx context.Context, src Source, q Query) (any, error)

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

	"persistentvolumeclaims": asLister(listPersistentVolumeClaims),
	"persistentvolumes":      asLister(listPersistentVolumes),
	"storageclasses":         asLister(listStorageClasses),

	"gateways":       asLister(listGateways),
	"httproutes":     asLister(listHTTPRoutes),
	"gatewayclasses": asLister(listGatewayClasses),
}

// Kind is one resource type in Listers and where it lives in the
// Kubernetes API: what a collector watches so the lists can be served.
type Kind struct {
	Resource   string // the URL name, e.g. "pods"
	GVR        schema.GroupVersionResource
	Namespaced bool
	Custom     bool // a custom resource (Gateway API), read with the dynamic client
}

// listOnly are listed kinds without a detail view, so they aren't in kinds.
var listOnly = map[string]kindInfo{
	"events": {core("events"), "Event", true},
}

// Kinds returns every resource type in Listers, sorted by name. A lister
// without an entry in kinds or listOnly is a mistake caught by the tests.
func Kinds() []Kind {
	var out []Kind
	for _, resource := range slices.Sorted(maps.Keys(Listers)) {
		info, ok := kinds[resource]
		if !ok {
			info, ok = listOnly[resource]
		}
		if !ok {
			continue
		}
		out = append(out, Kind{
			Resource:   resource,
			GVR:        info.GVR,
			Namespaced: info.Namespaced,
			Custom:     info.GVR.Group == gatewayAPI("").Group,
		})
	}
	return out
}

// asLister wraps a typed list function (returning e.g. []Pod) so it fits in
// the Listers map. Keeping the functions typed lets other Go code, like the
// overview, use their results without converting from `any`.
func asLister[T any](list func(context.Context, Source, Query) ([]T, error)) Lister {
	return func(ctx context.Context, src Source, q Query) (any, error) {
		return list(ctx, src, q)
	}
}

// Meta holds the fields every row has. Row types embed it, and Go's JSON
// encoder writes embedded fields as if they were declared in the row itself.
//
// UID is what identifies an object over time: a name comes back when an
// object is deleted and made again, its UID doesn't.
type Meta struct {
	UID       string    `json:"uid"`
	Name      string    `json:"name"`
	Namespace string    `json:"namespace,omitempty"`
	Created   time.Time `json:"created"`
}

func metaOf(obj metav1.ObjectMeta) Meta {
	return Meta{UID: string(obj.UID), Name: obj.Name, Namespace: obj.Namespace, Created: obj.CreationTimestamp.Time}
}

// sortKey is used by sortRows. Every row type gets it through the embedded Meta.
func (m Meta) sortKey() string {
	return m.Namespace + "/" + m.Name
}

// meta returns the Meta itself. Every row type gets it through the embedded
// Meta, so code that only has rows as `any` can still read their names
// (see ListNames).
func (m Meta) meta() Meta {
	return m
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
