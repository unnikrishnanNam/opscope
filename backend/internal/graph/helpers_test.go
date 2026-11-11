package graph

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// Test objects: each gets a UID named after its resource and name, so a
// test can say which node it means, and a resourceVersion.

var seen = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func uidOf(resource, name string) types.UID {
	return types.UID("uid-" + resource + "-" + name)
}

func om(resource, namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: uidOf(resource, name), ResourceVersion: "1"}
}

func owner(apiVersion, kind, resource, name string) metav1.OwnerReference {
	yes := true
	return metav1.OwnerReference{APIVersion: apiVersion, Kind: kind, Name: name, UID: uidOf(resource, name), Controller: &yes}
}

func obj(resource string, o runtime.Object) Object {
	return Object{Resource: resource, Object: o, Seen: seen}
}

func node(name string) Object {
	return obj("nodes", &corev1.Node{ObjectMeta: om("nodes", "", name)})
}

func configMap(ns, name string) Object {
	return obj("configmaps", &corev1.ConfigMap{ObjectMeta: om("configmaps", ns, name)})
}

func secret(ns, name string) Object {
	return obj("secrets", &corev1.Secret{ObjectMeta: om("secrets", ns, name)})
}

func serviceAccount(ns, name string) Object {
	return obj("serviceaccounts", &corev1.ServiceAccount{ObjectMeta: om("serviceaccounts", ns, name)})
}

func pod(ns, name string, labels map[string]string, spec corev1.PodSpec, owners ...metav1.OwnerReference) Object {
	meta := om("pods", ns, name)
	meta.Labels, meta.OwnerReferences = labels, owners
	return obj("pods", &corev1.Pod{ObjectMeta: meta, Spec: spec})
}

func deployment(ns, name string, spec corev1.PodSpec, owners ...metav1.OwnerReference) Object {
	meta := om("deployments", ns, name)
	meta.OwnerReferences = owners
	return obj("deployments", &appsv1.Deployment{ObjectMeta: meta, Spec: appsv1.DeploymentSpec{
		Template: corev1.PodTemplateSpec{Spec: spec},
	}})
}

func replicaSet(ns, name string, owners ...metav1.OwnerReference) Object {
	meta := om("replicasets", ns, name)
	meta.OwnerReferences = owners
	return obj("replicasets", &appsv1.ReplicaSet{ObjectMeta: meta})
}

func service(ns, name string, selector map[string]string) Object {
	return obj("services", &corev1.Service{ObjectMeta: om("services", ns, name), Spec: corev1.ServiceSpec{Selector: selector}})
}

func claim(ns, name, volume string, class *string) Object {
	return obj("persistentvolumeclaims", &corev1.PersistentVolumeClaim{
		ObjectMeta: om("persistentvolumeclaims", ns, name),
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: volume, StorageClassName: class},
	})
}

func gatewayObject(resource, ns, name string, spec map[string]any, owners ...metav1.OwnerReference) Object {
	kinds := map[string]string{"gateways": "Gateway", "httproutes": "HTTPRoute", "gatewayclasses": "GatewayClass"}
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": gatewayGroup + "/v1",
		"kind":       kinds[resource],
		"metadata":   map[string]any{"name": name},
		"spec":       spec,
	}}
	if ns != "" {
		u.SetNamespace(ns)
	}
	u.SetUID(uidOf(resource, name))
	u.SetResourceVersion("1")
	u.SetOwnerReferences(owners)
	return obj(resource, u)
}

func ptr[T any](v T) *T { return &v }

// shop is a namespace with something for every edge kind, every lesson and
// every kind of gap.
func shop() []Object {
	const ns = "shop"
	apiSpec := corev1.PodSpec{
		ServiceAccountName: "api",
		ImagePullSecrets:   []corev1.LocalObjectReference{{Name: "regcred"}}, // doesn't exist
		Volumes: []corev1.Volume{
			{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"},
			}}},
			{Name: "creds", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "db-creds"}}},
		},
		Containers: []corev1.Container{{
			Name: "api",
			Env: []corev1.EnvVar{
				{Name: "DB_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "db-creds"}, Key: "password",
				}}},
				{Name: "FEATURE", ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "flags"}, Key: "feature", Optional: ptr(true),
				}}},
			},
			EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: "stripe-key"}, // doesn't exist
			}}},
		}},
	}
	podSpec := *apiSpec.DeepCopy()
	podSpec.NodeName = "node-1"
	podSpec.Volumes = append(podSpec.Volumes,
		corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}},
		corev1.Volume{Name: "kube-api-access-x1", VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{
			Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: kubeRootCA}}},
			},
		}}},
	)

	dbSpec := corev1.PodSpec{ServiceAccountName: "default", NodeName: "node-1"}
	sts := &appsv1.StatefulSet{ObjectMeta: om("statefulsets", ns, "db"), Spec: appsv1.StatefulSetSpec{
		VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"}}},
	}}

	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: om("endpointslices", ns, "api-abcde"),
		Endpoints: []discoveryv1.Endpoint{
			{TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-7d9f-x1", UID: uidOf("pods", "api-7d9f-x1")}},
			{TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-7d9f-x2", UID: uidOf("pods", "api-7d9f-x2")},
				Conditions: discoveryv1.EndpointConditions{Ready: ptr(false)}},
		},
	}
	slice.OwnerReferences = []metav1.OwnerReference{owner("v1", "Service", "services", "api")}

	ingress := &networkingv1.Ingress{ObjectMeta: om("ingresses", ns, "web"), Spec: networkingv1.IngressSpec{
		TLS: []networkingv1.IngressTLS{{SecretName: "tls-cert"}},
		Rules: []networkingv1.IngressRule{{Host: "shop.example", IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{
				{Path: "/api", Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
				{Path: "/old", Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "old-api"}}},
			}},
		}}},
	}}

	cronJob := &batchv1.CronJob{ObjectMeta: om("cronjobs", ns, "nightly")}
	job := &batchv1.Job{ObjectMeta: om("jobs", ns, "nightly-123")}
	job.OwnerReferences = []metav1.OwnerReference{owner("batch/v1", "CronJob", "cronjobs", "nightly")}

	etcd := pod("kube-system", "etcd-node-1", nil, corev1.PodSpec{NodeName: "node-1"},
		metav1.OwnerReference{APIVersion: "v1", Kind: "Node", Name: "node-1", UID: uidOf("nodes", "node-1")})

	return []Object{
		node("node-1"),
		obj("storageclasses", &storagev1.StorageClass{ObjectMeta: om("storageclasses", "", "fast")}),
		obj("persistentvolumes", &corev1.PersistentVolume{ObjectMeta: om("persistentvolumes", "", "pv-1"),
			Spec: corev1.PersistentVolumeSpec{StorageClassName: "fast"}}),
		claim(ns, "data", "pv-1", ptr("fast")),
		claim(ns, "pending", "", ptr("slow")), // a class that doesn't exist
		claim(ns, "data-db-0", "", nil),
		claim(ns, "data-db-old", "", nil), // not made from the template: no ordinal
		configMap(ns, "app-config"),
		configMap(ns, "flags"),
		configMap(ns, kubeRootCA),
		secret(ns, "db-creds"),
		secret(ns, "tls-cert"),
		serviceAccount(ns, "api"),
		serviceAccount(ns, "default"),

		deployment(ns, "api", apiSpec),
		replicaSet(ns, "api-7d9f", owner("apps/v1", "Deployment", "deployments", "api")),
		pod(ns, "api-7d9f-x1", map[string]string{"app": "api"}, podSpec, owner("apps/v1", "ReplicaSet", "replicasets", "api-7d9f")),
		pod(ns, "api-7d9f-x2", map[string]string{"app": "api"}, podSpec, owner("apps/v1", "ReplicaSet", "replicasets", "api-7d9f")),
		service(ns, "api", map[string]string{"app": "api"}),
		service(ns, "ghost", map[string]string{"app": "nothing"}),
		service(ns, "external", nil),
		obj("endpointslices", slice),
		obj("ingresses", ingress),

		obj("statefulsets", sts),
		pod(ns, "db-0", nil, dbSpec, owner("apps/v1", "StatefulSet", "statefulsets", "db")),

		obj("cronjobs", cronJob),
		obj("jobs", job),
		etcd,

		gatewayObject("gatewayclasses", "", "nginx", map[string]any{"controllerName": "nginx.org/gateway"}),
		gatewayObject("gateways", ns, "edge", map[string]any{
			"gatewayClassName": "nginx",
			"listeners": []any{map[string]any{
				"name": "https", "port": int64(443), "protocol": "HTTPS",
				"tls": map[string]any{"certificateRefs": []any{map[string]any{"name": "tls-cert"}}},
			}},
		}),
		gatewayObject("httproutes", ns, "store", map[string]any{
			"parentRefs": []any{map[string]any{"name": "edge", "sectionName": "https"}},
			"rules": []any{map[string]any{"backendRefs": []any{
				map[string]any{"name": "api", "port": int64(80)},
				map[string]any{"name": "missing-svc", "port": int64(80)},
			}}},
		}),
		// The Gateway's controller runs it with a Deployment it owns.
		deployment(ns, "edge-nginx", corev1.PodSpec{}, metav1.OwnerReference{
			APIVersion: gatewayGroup + "/v1", Kind: "Gateway", Name: "edge", UID: uidOf("gateways", "edge"),
		}),
	}
}

// ---- Finding things in a snapshot ----

// named maps "resource/namespace/name" (or "presence:..." for placeholders)
// to node IDs, so tests can name nodes the way people do.
func named(s Snapshot) map[string]string {
	out := map[string]string{}
	for _, n := range s.Nodes {
		key := objectKey(n.Resource, n.Namespace, n.Name)
		if n.Presence != Present {
			key = n.ID // "presence:key", the key naming a kind Opscope doesn't know by kind and group
		}
		out[key] = n.ID
	}
	return out
}

func findEdge(t *testing.T, s Snapshot, from, to, kind string) Edge {
	t.Helper()
	ids := named(s)
	f, ok := ids[from]
	if !ok {
		t.Fatalf("no node %s", from)
	}
	g, ok := ids[to]
	if !ok {
		t.Fatalf("no node %s", to)
	}
	for _, e := range s.Edges {
		if e.From == f && e.To == g && e.Kind == kind {
			return e
		}
	}
	t.Fatalf("no %s edge %s → %s", kind, from, to)
	return Edge{}
}

func hasEdge(s Snapshot, from, to, kind string) bool {
	ids := named(s)
	for _, e := range s.Edges {
		if e.From == ids[from] && e.To == ids[to] && e.Kind == kind {
			return true
		}
	}
	return false
}

func fields(e Edge) []string {
	var out []string
	for _, ev := range e.Evidence {
		out = append(out, fmt.Sprintf("%s %s %s", ev.Builder, ev.Field, ev.Note))
	}
	return out
}
