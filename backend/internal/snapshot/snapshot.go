// Package snapshot reads a whole cluster at once: every kind Opscope knows,
// across all namespaces, as it was at one moment.
//
// The topology graph and the diagnosis need all of it together, since an
// object's problem is often in another object (a pod's missing Secret, a
// route's Service in another namespace). Everything they compute starts from
// a Snapshot, so this is the only package that talks to the cluster for them.
//
// A snapshot never holds secret values: Secrets are listed through the
// metadata client, so their data never reaches Opscope, and ConfigMaps keep
// only their key names.
package snapshot

import (
	"sort"
	"sync"
	"time"

	"opscope/internal/kube"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Snapshot is one cluster at one moment. The lists are read-only once Load
// returns: several requests may use the same snapshot at the same time.
type Snapshot struct {
	Taken   time.Time      // when reading started; the data is at most this old
	Skipped []kube.Skipped // kinds that couldn't be read, and why

	Namespaces               []corev1.Namespace
	Nodes                    []corev1.Node
	Pods                     []corev1.Pod
	ReplicaSets              []appsv1.ReplicaSet
	Deployments              []appsv1.Deployment
	StatefulSets             []appsv1.StatefulSet
	DaemonSets               []appsv1.DaemonSet
	Jobs                     []batchv1.Job
	CronJobs                 []batchv1.CronJob
	Services                 []corev1.Service
	EndpointSlices           []discoveryv1.EndpointSlice
	Ingresses                []networkingv1.Ingress
	IngressClasses           []networkingv1.IngressClass
	ConfigMaps               []ConfigMap
	Secrets                  []metav1.PartialObjectMetadata // metadata only: no type, keys or values
	PersistentVolumeClaims   []corev1.PersistentVolumeClaim
	PersistentVolumes        []corev1.PersistentVolume
	StorageClasses           []storagev1.StorageClass
	ServiceAccounts          []corev1.ServiceAccount
	ResourceQuotas           []corev1.ResourceQuota
	HorizontalPodAutoscalers []autoscalingv2.HorizontalPodAutoscaler
	PodDisruptionBudgets     []policyv1.PodDisruptionBudget
	Gateways                 []kube.Gateway
	HTTPRoutes               []kube.HTTPRoute
	GatewayClasses           []kube.GatewayClass
	ReferenceGrants          []kube.ReferenceGrant
	Events                   []corev1.Event // Warning events only

	index

	derivedMu sync.Mutex
	derived   map[string]any
}

// ConfigMap is a ConfigMap without its values: its metadata and the names
// of its keys (from both data and binaryData), sorted.
type ConfigMap struct {
	metav1.ObjectMeta
	Keys []string
}

// Key names one object the way Opscope's URLs do: the resource type's API
// name ("pods"), then the namespace (empty for cluster-wide kinds) and name.
type Key struct {
	Resource  string
	Namespace string
	Name      string
}

// String writes the key as "pods/shop/web-1", or "nodes/worker-1" for a
// cluster-wide kind. The graph uses it as a node's ID.
func (k Key) String() string {
	if k.Namespace == "" {
		return k.Resource + "/" + k.Name
	}
	return k.Resource + "/" + k.Namespace + "/" + k.Name
}

// index holds the lookups built once a snapshot is loaded.
type index struct {
	objects map[Key]metav1.Object
	uids    map[types.UID]Key
	owned   map[types.UID][]Key           // owner's UID → the objects it owns
	events  map[types.UID][]*corev1.Event // object's UID → its events, newest first
	skipped map[string]kube.Skipped
}

// Get returns one object, as a pointer into the snapshot's lists. Type-assert
// it to what the resource holds: *corev1.Pod for "pods", *ConfigMap for
// "configmaps", *metav1.PartialObjectMetadata for "secrets", *kube.Gateway
// for "gateways", and so on.
func (s *Snapshot) Get(resource, namespace, name string) (metav1.Object, bool) {
	obj, ok := s.objects[Key{resource, namespace, name}]
	return obj, ok
}

// Lookup finds an object by its UID, as owner references and events name
// objects.
func (s *Snapshot) Lookup(uid types.UID) (Key, bool) {
	key, ok := s.uids[uid]
	return key, ok
}

// Owned lists the objects that name uid as an owner: a Deployment's
// ReplicaSets, a ReplicaSet's pods, a CronJob's Jobs. Sorted by key.
func (s *Snapshot) Owned(uid types.UID) []Key {
	return s.owned[uid]
}

// EventsFor returns the Warning events about one object, newest first.
func (s *Snapshot) EventsFor(uid types.UID) []*corev1.Event {
	return s.events[uid]
}

// Readable says whether a kind was read. When it wasn't, an object of that
// kind that can't be found may still exist: it's unknown, not missing.
func (s *Snapshot) Readable(resource string) bool {
	_, skipped := s.skipped[resource]
	return !skipped
}

// Skip says why a kind wasn't read.
func (s *Snapshot) Skip(resource string) (kube.Skipped, bool) {
	skipped, ok := s.skipped[resource]
	return skipped, ok
}

// Derived returns what build computes from this snapshot, building it only
// the first time it's asked for. The graph and the diagnosis are kept this
// way: computed once per snapshot, shared by every request that uses it,
// and gone when the snapshot is.
func (s *Snapshot) Derived(name string, build func() any) any {
	s.derivedMu.Lock()
	defer s.derivedMu.Unlock()
	if value, ok := s.derived[name]; ok {
		return value
	}
	if s.derived == nil {
		s.derived = map[string]any{}
	}
	value := build()
	s.derived[name] = value
	return value
}

// Size is how many objects the snapshot holds, not counting events.
func (s *Snapshot) Size() int {
	return len(s.objects)
}

// BuildIndex fills in the lookups. Load calls it once every list is read;
// call it yourself on a snapshot put together by hand, as tests do.
func (s *Snapshot) BuildIndex() {
	s.index = index{
		objects: map[Key]metav1.Object{},
		uids:    map[types.UID]Key{},
		owned:   map[types.UID][]Key{},
		events:  map[types.UID][]*corev1.Event{},
		skipped: map[string]kube.Skipped{},
	}
	for _, skipped := range s.Skipped {
		s.skipped[skipped.Resource] = skipped
	}

	addAll(s, "namespaces", s.Namespaces)
	addAll(s, "nodes", s.Nodes)
	addAll(s, "pods", s.Pods)
	addAll(s, "replicasets", s.ReplicaSets)
	addAll(s, "deployments", s.Deployments)
	addAll(s, "statefulsets", s.StatefulSets)
	addAll(s, "daemonsets", s.DaemonSets)
	addAll(s, "jobs", s.Jobs)
	addAll(s, "cronjobs", s.CronJobs)
	addAll(s, "services", s.Services)
	addAll(s, "endpointslices", s.EndpointSlices)
	addAll(s, "ingresses", s.Ingresses)
	addAll(s, "ingressclasses", s.IngressClasses)
	addAll(s, "configmaps", s.ConfigMaps)
	addAll(s, "secrets", s.Secrets)
	addAll(s, "persistentvolumeclaims", s.PersistentVolumeClaims)
	addAll(s, "persistentvolumes", s.PersistentVolumes)
	addAll(s, "storageclasses", s.StorageClasses)
	addAll(s, "serviceaccounts", s.ServiceAccounts)
	addAll(s, "resourcequotas", s.ResourceQuotas)
	addAll(s, "horizontalpodautoscalers", s.HorizontalPodAutoscalers)
	addAll(s, "poddisruptionbudgets", s.PodDisruptionBudgets)
	addAll(s, "gateways", s.Gateways)
	addAll(s, "httproutes", s.HTTPRoutes)
	addAll(s, "gatewayclasses", s.GatewayClasses)
	addAll(s, "referencegrants", s.ReferenceGrants)

	for _, keys := range s.owned {
		sortKeys(keys)
	}

	for i := range s.Events {
		e := &s.Events[i]
		if uid := e.InvolvedObject.UID; uid != "" {
			s.events[uid] = append(s.events[uid], e)
		}
	}
	for _, events := range s.events {
		sort.SliceStable(events, func(i, j int) bool { return kube.EventLastSeen(events[i]).After(kube.EventLastSeen(events[j])) })
	}
}

// addAll indexes one list. PT is "pointer to T", which every Kubernetes
// object type implements metav1.Object through; Go works it out from T.
func addAll[T any, PT interface {
	*T
	metav1.Object
}](s *Snapshot, resource string, items []T) {
	for i := range items {
		obj := PT(&items[i])
		key := Key{resource, obj.GetNamespace(), obj.GetName()}
		s.objects[key] = obj
		if uid := obj.GetUID(); uid != "" {
			s.uids[uid] = key
		}
		for _, owner := range obj.GetOwnerReferences() {
			s.owned[owner.UID] = append(s.owned[owner.UID], key)
		}
	}
}

func sortKeys(keys []Key) {
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
}
