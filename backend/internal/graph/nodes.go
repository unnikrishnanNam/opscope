package graph

import (
	"time"

	"opscope/internal/snapshot"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// kinds maps each resource the graph holds to its kind. EndpointSlices and
// events aren't nodes: they describe other objects (a Service's ready pods,
// what happened to a pod) rather than being something an object needs.
var kinds = map[string]string{
	"namespaces":               "Namespace",
	"nodes":                    "Node",
	"pods":                     "Pod",
	"replicasets":              "ReplicaSet",
	"deployments":              "Deployment",
	"statefulsets":             "StatefulSet",
	"daemonsets":               "DaemonSet",
	"jobs":                     "Job",
	"cronjobs":                 "CronJob",
	"services":                 "Service",
	"ingresses":                "Ingress",
	"ingressclasses":           "IngressClass",
	"configmaps":               "ConfigMap",
	"secrets":                  "Secret",
	"persistentvolumeclaims":   "PersistentVolumeClaim",
	"persistentvolumes":        "PersistentVolume",
	"storageclasses":           "StorageClass",
	"serviceaccounts":          "ServiceAccount",
	"resourcequotas":           "ResourceQuota",
	"horizontalpodautoscalers": "HorizontalPodAutoscaler",
	"poddisruptionbudgets":     "PodDisruptionBudget",
	"gateways":                 "Gateway",
	"httproutes":               "HTTPRoute",
	"gatewayclasses":           "GatewayClass",
	"referencegrants":          "ReferenceGrant",
}

// resourceOf is the reverse of kinds: "Deployment" → "deployments".
var resourceOf = func() map[string]string {
	m := map[string]string{}
	for resource, kind := range kinds {
		m[kind] = resource
	}
	return m
}()

// stuckAfter is how long something may take to start before it counts as
// failing rather than progressing: a pod still Pending, a claim still
// unbound, a GatewayClass nobody has accepted.
const stuckAfter = 5 * time.Minute

// builder holds what building one graph needs.
type builder struct {
	s   *snapshot.Snapshot
	g   *Graph
	now time.Time // the snapshot's time, so states don't depend on when they're computed

	podsByNamespace map[string][]*corev1.Pod
}

func (b *builder) addNodes() {
	s := b.s
	addAll(b, "namespaces", s.Namespaces)
	addAll(b, "nodes", s.Nodes)
	addAll(b, "pods", s.Pods)
	addAll(b, "replicasets", s.ReplicaSets)
	addAll(b, "deployments", s.Deployments)
	addAll(b, "statefulsets", s.StatefulSets)
	addAll(b, "daemonsets", s.DaemonSets)
	addAll(b, "jobs", s.Jobs)
	addAll(b, "cronjobs", s.CronJobs)
	addAll(b, "services", s.Services)
	addAll(b, "ingresses", s.Ingresses)
	addAll(b, "ingressclasses", s.IngressClasses)
	addAll(b, "configmaps", s.ConfigMaps)
	addAll(b, "secrets", s.Secrets)
	addAll(b, "persistentvolumeclaims", s.PersistentVolumeClaims)
	addAll(b, "persistentvolumes", s.PersistentVolumes)
	addAll(b, "storageclasses", s.StorageClasses)
	addAll(b, "serviceaccounts", s.ServiceAccounts)
	addAll(b, "resourcequotas", s.ResourceQuotas)
	addAll(b, "horizontalpodautoscalers", s.HorizontalPodAutoscalers)
	addAll(b, "poddisruptionbudgets", s.PodDisruptionBudgets)
	addAll(b, "gateways", s.Gateways)
	addAll(b, "httproutes", s.HTTPRoutes)
	addAll(b, "gatewayclasses", s.GatewayClasses)
	addAll(b, "referencegrants", s.ReferenceGrants)

	b.podsByNamespace = map[string][]*corev1.Pod{}
	for i := range s.Pods {
		pod := &s.Pods[i]
		b.podsByNamespace[pod.Namespace] = append(b.podsByNamespace[pod.Namespace], pod)
	}
}

// addAll adds a node per object. States are filled in later (addStates);
// until then every node is OK.
func addAll[T any, PT interface {
	*T
	metav1.Object
}](b *builder, resource string, items []T) {
	for i := range items {
		obj := PT(&items[i])
		key := snapshot.Key{Resource: resource, Namespace: obj.GetNamespace(), Name: obj.GetName()}
		b.g.nodes[key.String()] = &Node{
			ID:        key.String(),
			Resource:  resource,
			Kind:      kinds[resource],
			Namespace: key.Namespace,
			Name:      key.Name,
			State:     OK,
			labels:    obj.GetLabels(),
		}
	}
}

// target returns the ID of a referenced object, adding a missing or unknown
// node when the snapshot doesn't have it.
func (b *builder) target(key snapshot.Key) string {
	id := key.String()
	if _, ok := b.g.nodes[id]; ok {
		return id
	}
	n := &Node{
		ID:        id,
		Resource:  key.Resource,
		Kind:      kinds[key.Resource],
		Namespace: key.Namespace,
		Name:      key.Name,
		State:     Missing,
		Status:    "Missing",
		Message:   "It's referred to, but doesn't exist.",
	}
	if skipped, ok := b.s.Skip(key.Resource); ok {
		n.State, n.Status = Unknown, "Can't read"
		n.Message = "Opscope can't read " + n.Kind + "s in this cluster, so it can't tell whether this exists."
		if skipped.Code == "not_installed" {
			n.Message = "This cluster doesn't serve " + n.Kind + "s."
		}
	}
	b.g.nodes[id] = n
	return id
}

// ref adds an edge from an object to something it refers to. A reference
// that is optional and points at nothing is left out: it can't break
// anything, and a "missing" node would only alarm.
func (b *builder) ref(from string, to snapshot.Key, typ, via string, optional bool) {
	if _, exists := b.g.nodes[to.String()]; !exists && optional {
		return
	}
	b.g.addEdge(from, b.target(to), typ, via, optional)
}

// set fills in a node's state.
func (b *builder) set(id string, state State, status, message string) {
	if n, ok := b.g.nodes[id]; ok {
		n.State, n.Status, n.Message = state, status, message
	}
}

// object returns the snapshot's object behind a node, or nil for a missing
// or unknown one.
func (b *builder) object(id string) metav1.Object {
	n, ok := b.g.nodes[id]
	if !ok {
		return nil
	}
	obj, ok := b.s.Get(n.Resource, n.Namespace, n.Name)
	if !ok {
		return nil
	}
	return obj
}

func key(resource string, obj metav1.Object) string {
	return snapshot.Key{Resource: resource, Namespace: obj.GetNamespace(), Name: obj.GetName()}.String()
}
