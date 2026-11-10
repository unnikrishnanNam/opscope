package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"

	"opscope/internal/graph"
	"opscope/internal/resources"
)

// The objects every detail page in detailLinks is read from: workloads,
// storage in every state, and things referred to that don't exist.
func linkObjects() []runtime.Object {
	const ns = "shop"
	meta := func(namespace, name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("uid-" + name), ResourceVersion: "1"}
	}
	yes := true
	owned := func(m metav1.ObjectMeta, apiVersion, kind, name string) metav1.ObjectMeta {
		m.OwnerReferences = []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: name, UID: types.UID("uid-" + name), Controller: &yes}}
		return m
	}
	gi := resource.MustParse("1Gi")
	vol := func(name string, src corev1.VolumeSource) corev1.Volume {
		return corev1.Volume{Name: name, VolumeSource: src}
	}
	webSpec := corev1.PodSpec{
		NodeName: "n1",
		Volumes: []corev1.Volume{
			vol("config", corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}}}),
			vol("creds", corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "db-creds"}}),
			vol("data", corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}),
			vol("gone", corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "missing-config"}}}),
			vol("scratch", corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{}}),
		},
		Containers: []corev1.Container{{Name: "web", Env: []corev1.EnvVar{{Name: "PASSWORD", ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "db-creds"}, Key: "p"},
		}}}}},
	}
	three := int32(3)
	fast, slow := "fast", "slow"
	claim := func(name, volume string, class *string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
		c := &corev1.PersistentVolumeClaim{ObjectMeta: meta(ns, name), Spec: corev1.PersistentVolumeClaimSpec{VolumeName: volume, StorageClassName: class},
			Status: corev1.PersistentVolumeClaimStatus{Phase: phase}}
		if phase == corev1.ClaimBound {
			c.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: gi}
		}
		return c
	}
	pv := func(name, class, claimName string, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{ObjectMeta: meta("", name), Spec: corev1.PersistentVolumeSpec{
			StorageClassName: class, ClaimRef: &corev1.ObjectReference{Namespace: ns, Name: claimName},
		}, Status: corev1.PersistentVolumeStatus{Phase: phase}}
	}
	pod := func(m metav1.ObjectMeta, labels map[string]string, spec corev1.PodSpec) *corev1.Pod {
		m.Labels = labels
		return &corev1.Pod{ObjectMeta: m, Spec: spec, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	}
	return []runtime.Object{
		&corev1.Node{ObjectMeta: meta("", "n1")},
		&storagev1.StorageClass{ObjectMeta: meta("", "fast"), Provisioner: "example.com/disk"},
		&corev1.ConfigMap{ObjectMeta: meta(ns, "app-config")},
		&corev1.Secret{ObjectMeta: meta(ns, "db-creds")},
		&appsv1.Deployment{ObjectMeta: meta(ns, "web"), Spec: appsv1.DeploymentSpec{Replicas: &three, Template: corev1.PodTemplateSpec{Spec: webSpec}}},
		&appsv1.ReplicaSet{ObjectMeta: owned(meta(ns, "web-abc"), "apps/v1", "Deployment", "web")},
		pod(owned(meta(ns, "web-abc-1"), "apps/v1", "ReplicaSet", "web-abc"), map[string]string{"app": "web"}, webSpec),
		pod(meta(ns, "debug"), nil, corev1.PodSpec{NodeName: "n2", Containers: []corev1.Container{{Name: "sh", EnvFrom: []corev1.EnvFromSource{{
			ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "app-config"}},
		}}}}}),
		claim("data", "pv-data", &fast, corev1.ClaimBound),
		claim("web-abc-1-scratch", "", &fast, corev1.ClaimPending),
		claim("orphan", "", &slow, corev1.ClaimPending),
		pv("pv-data", "fast", "data", corev1.VolumeBound),
		pv("pv-released", "manual", "old", corev1.VolumeReleased),
		&appsv1.StatefulSet{ObjectMeta: meta(ns, "db"), Spec: appsv1.StatefulSetSpec{
			Replicas: &three,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"},
				Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &fast}}},
		}},
		claim("data-db-0", "pv-db-0", &fast, corev1.ClaimBound),
		claim("data-db-1", "", &fast, corev1.ClaimPending),
		pv("pv-db-0", "fast", "data-db-0", corev1.VolumeBound),
		pod(owned(meta(ns, "db-0"), "apps/v1", "StatefulSet", "db"), map[string]string{"app": "db"}, corev1.PodSpec{NodeName: "n1", Volumes: []corev1.Volume{
			vol("data", corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data-db-0"}}),
		}}),
	}
}

// detailPages are the pages whose links are compared.
var detailPages = []struct{ resource, namespace, name string }{
	{"pods", "shop", "web-abc-1"},
	{"pods", "shop", "debug"},
	{"pods", "shop", "db-0"},
	{"deployments", "shop", "web"},
	{"statefulsets", "shop", "db"},
	{"persistentvolumeclaims", "shop", "data"},
	{"persistentvolumeclaims", "shop", "data-db-0"},
	{"persistentvolumeclaims", "shop", "orphan"},
	{"persistentvolumes", "", "pv-data"},
	{"persistentvolumes", "", "pv-released"},
	{"storageclasses", "", "fast"},
	{"configmaps", "shop", "app-config"},
	{"secrets", "shop", "db-creds"},
	{"nodes", "", "n1"},
}

// linkClients are client-go fakes holding linkObjects: typed for lists,
// dynamic for each detail page's own object.
func linkClients(t *testing.T) resources.Clients {
	t.Helper()
	return clientsFor(t, linkObjects())
}

// clientsFor makes client-go fakes holding objects.
func clientsFor(t *testing.T, objects []runtime.Object) resources.Clients {
	t.Helper()
	listKinds := map[schema.GroupVersionResource]string{}
	for _, k := range resources.Kinds() {
		listKinds[k.GVR] = k.KindName + "List"
	}
	dyn := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	for _, obj := range objects {
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: raw}
		k, ok := resources.LookupKind(groupOf(obj), kindOf(obj))
		if !ok {
			t.Fatalf("no kind for %T", obj)
		}
		if err := dyn.Tracker().Create(k.GVR, u, u.GetNamespace()); err != nil {
			t.Fatal(err)
		}
	}
	return resources.Clients{Kube: fake.NewClientset(objects...), Dynamic: dyn}
}

func kindOf(obj runtime.Object) string {
	switch obj.(type) {
	case *corev1.Node:
		return "Node"
	case *storagev1.StorageClass:
		return "StorageClass"
	case *corev1.ConfigMap:
		return "ConfigMap"
	case *corev1.Secret:
		return "Secret"
	case *appsv1.Deployment:
		return "Deployment"
	case *appsv1.ReplicaSet:
		return "ReplicaSet"
	case *appsv1.StatefulSet:
		return "StatefulSet"
	case *corev1.Pod:
		return "Pod"
	case *corev1.PersistentVolumeClaim:
		return "PersistentVolumeClaim"
	case *corev1.PersistentVolume:
		return "PersistentVolume"
	case *batchv1.Job:
		return "Job"
	case *batchv1.CronJob:
		return "CronJob"
	case *networkingv1.Ingress:
		return "Ingress"
	}
	return ""
}

func groupOf(obj runtime.Object) string {
	switch obj.(type) {
	case *appsv1.Deployment, *appsv1.ReplicaSet, *appsv1.StatefulSet:
		return "apps"
	case *storagev1.StorageClass:
		return "storage.k8s.io"
	case *batchv1.Job, *batchv1.CronJob:
		return "batch"
	case *networkingv1.Ingress:
		return "networking.k8s.io"
	}
	return ""
}

// links is the part of a detail page that names other objects: its fields,
// owners and tables.
type links struct {
	Fields []resources.Field `json:"fields"`
	Owners []resources.Owner `json:"owners"`
	Tables []resources.Table `json:"tables"`
}

// detailLinks reads every page in detailPages.
func detailLinks(t *testing.T, read func(ctx context.Context, resource, namespace, name string) (*resources.Detail, error)) map[string]links {
	t.Helper()
	out := map[string]links{}
	for _, p := range detailPages {
		d, err := read(t.Context(), p.resource, p.namespace, p.name)
		if err != nil {
			t.Fatalf("%s %s/%s: %v", p.resource, p.namespace, p.name, err)
		}
		out[p.resource+"/"+p.namespace+"/"+p.name] = links{Fields: d.Fields, Owners: d.Owners, Tables: d.Tables}
	}
	return out
}

const goldenLinks = "testdata/detail-links-v1.4.json"

// TestRecordV14Links writes what v1.4's detail pages link to, from the
// code as it was before the graph, so later code can be compared with it.
// It only runs with OPSCOPE_RECORD_LINKS=1.
func TestRecordV14Links(t *testing.T) {
	if os.Getenv("OPSCOPE_RECORD_LINKS") != "1" {
		t.Skip("records the v1.4 links; set OPSCOPE_RECORD_LINKS=1")
	}
	clients := linkClients(t)
	got := detailLinks(t, func(ctx context.Context, resource, namespace, name string) (*resources.Detail, error) {
		return resources.GetDetail(ctx, clients, resources.Direct(clients), resource, namespace, name)
	})
	b, _ := json.MarshalIndent(got, "", "  ")
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(goldenLinks, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Every detail page links to the same objects as in v1.4, read now from
// the graph, apart from the changes listed here, which are the point:
// a pod's node is a link, a link to something missing says so, and
// ConfigMaps and Secrets list what uses them.
func TestDetailLinksMatchV14(t *testing.T) {
	raw, err := os.ReadFile(goldenLinks)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]links
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	field := func(page, label string, f func(*resources.Field)) {
		l := want[page]
		for i := range l.Fields {
			if l.Fields[i].Label == label {
				f(&l.Fields[i])
			}
		}
		want[page] = l
	}
	nodeLink := func(f *resources.Field) { f.Link = &resources.Link{Resource: "nodes", Name: f.Value} }
	field("pods/shop/web-abc-1", "Node", nodeLink)
	field("pods/shop/db-0", "Node", nodeLink)
	field("pods/shop/debug", "Node", func(f *resources.Field) { f.Value += " (not found)" })
	for i, row := range want["pods/shop/web-abc-1"].Tables[0].Rows {
		if row[2] == "missing-config" {
			row[2] += " (not found)"
			want["pods/shop/web-abc-1"].Tables[0].Links[i][2] = nil
		}
	}
	usedBy := func(page string, rows ...[]string) {
		l := want[page]
		t := resources.Table{Title: "Used by", Columns: []string{"Kind", "Name", "How", "Pods"}}
		for _, r := range rows {
			t.Rows = append(t.Rows, r)
			t.Links = append(t.Links, []*resources.Link{nil, {Resource: map[string]string{"Deployment": "deployments", "Pod": "pods"}[r[0]], Namespace: "shop", Name: r[1]}, nil, nil})
		}
		l.Tables = append(l.Tables, t)
		want[page] = l
	}
	usedBy("configmaps/shop/app-config", []string{"Deployment", "web", "volume config", "1"}, []string{"Pod", "debug", "envFrom", ""})
	usedBy("secrets/shop/db-creds", []string{"Deployment", "web", "env PASSWORD, volume creds", "1"})

	clients := linkClients(t)
	src := resources.Direct(clients)
	g, _, err := buildGraph(t.Context(), src)
	if err != nil {
		t.Fatal(err)
	}
	reader := &fakeGraphs{g: g, info: graph.Info{Built: graph.BuiltLive}}
	got := detailLinks(t, func(ctx context.Context, resource, namespace, name string) (*resources.Detail, error) {
		d, err := resources.GetDetail(ctx, clients, src, resource, namespace, name)
		if err == nil {
			addRelations(ctx, reader, "lab", src, d, resource)
		}
		return d, err
	})

	for page := range want {
		a, _ := json.MarshalIndent(got[page], "", " ")
		b, _ := json.MarshalIndent(want[page], "", " ")
		if string(a) != string(b) {
			t.Errorf("%s:\ngot  %s\nwant %s", page, a, b)
		}
	}
}

// buildGraph builds a graph of everything src lists, as the engine does
// for a graph built on request.
func buildGraph(ctx context.Context, src resources.Source) (*graph.Graph, graph.Info, error) {
	var objects []graph.Object
	for _, k := range graph.NodeKinds {
		list, err := src.List(ctx, k, resources.Query{})
		if err != nil {
			continue // Gateway API isn't in the fakes
		}
		for _, o := range list {
			objects = append(objects, graph.Object{Resource: k, Object: o})
		}
	}
	return graph.Build(nil, objects), graph.Info{Built: graph.BuiltOnRequest}, nil
}
