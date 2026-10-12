package snapshot

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"opscope/internal/kube"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
)

// meta is a shortcut for object metadata in tests. The fake clients don't
// assign UIDs, so tests give each object one.
func meta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("uid-" + namespace + "-" + name)}
}

// ownedBy marks an object as owned by another, like a controller does.
func ownedBy(m metav1.ObjectMeta, kind string, owner metav1.ObjectMeta) metav1.ObjectMeta {
	m.OwnerReferences = append(m.OwnerReferences, metav1.OwnerReference{Kind: kind, Name: owner.Name, UID: owner.UID})
	return m
}

var gatewayKinds = map[string]string{
	"gateways":        "Gateway",
	"httproutes":      "HTTPRoute",
	"gatewayclasses":  "GatewayClass",
	"referencegrants": "ReferenceGrant",
}

// fakeDynamic is a fake dynamic client that knows the Gateway API kinds.
// Objects are added with an explicit resource, because the fake would
// otherwise guess "gatewaies" from the kind.
func fakeDynamic(objects ...*unstructured.Unstructured) *dynamicfake.FakeDynamicClient {
	listKinds := map[schema.GroupVersionResource]string{}
	resourceOf := map[string]string{}
	for resource, kind := range gatewayKinds {
		listKinds[kube.GatewayAPI(resource)] = kind + "List"
		resourceOf[kind] = resource
	}
	// Clusters may serve ReferenceGrants only as v1beta1, which the loader
	// falls back to.
	listKinds[schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1beta1", Resource: "referencegrants"}] = "ReferenceGrantList"
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds)
	for _, obj := range objects {
		if err := client.Tracker().Create(kube.GatewayAPI(resourceOf[obj.GetKind()]), obj, obj.GetNamespace()); err != nil {
			panic(err)
		}
	}
	return client
}

func gatewayObject(kind, namespace, name string, fields map[string]any) *unstructured.Unstructured {
	obj := map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       kind,
		"metadata":   map[string]any{"name": name, "namespace": namespace, "uid": "uid-" + namespace + "-" + name},
	}
	for k, v := range fields {
		obj[k] = v
	}
	return &unstructured.Unstructured{Object: obj}
}

// fakeMetadata is a fake metadata client holding Secrets. Its scheme has to
// be told that a Secret's metadata is a PartialObjectMetadata.
func fakeMetadata(secrets ...*metav1.PartialObjectMetadata) *metadatafake.FakeMetadataClient {
	scheme := metadatafake.NewTestScheme()
	scheme.AddKnownTypeWithName(corev1.SchemeGroupVersion.WithKind("Secret"), &metav1.PartialObjectMetadata{})
	objects := make([]runtime.Object, len(secrets))
	for i, s := range secrets {
		objects[i] = s
	}
	return metadatafake.NewSimpleMetadataClient(scheme, objects...)
}

// secretMeta is a Secret as the metadata client returns it: no data at all.
func secretMeta(namespace, name string) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: meta(namespace, name),
	}
}

// notFound makes every list call of a fake fail like an API server does for
// a resource type it doesn't know.
func notFound(action clienttesting.Action) (bool, runtime.Object, error) {
	return true, nil, apierrors.NewNotFound(action.GetResource().GroupResource(), "")
}

func forbidden(action clienttesting.Action) (bool, runtime.Object, error) {
	return true, nil, apierrors.NewForbidden(action.GetResource().GroupResource(), "", errors.New("RBAC"))
}

func TestLoad(t *testing.T) {
	deploy := meta("shop", "web")
	rs := ownedBy(meta("shop", "web-7d9f"), "Deployment", deploy)
	pod := ownedBy(meta("shop", "web-7d9f-x2k"), "ReplicaSet", rs)
	pod.ManagedFields = []metav1.ManagedFieldsEntry{{Manager: "kubectl"}}
	pod.Annotations = map[string]string{lastApplied: `{"huge":"copy"}`, "team": "shop"}

	older := metav1.NewTime(time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	newer := metav1.NewTime(older.Add(time.Minute))
	warning := func(name string, at metav1.Time) *corev1.Event {
		return &corev1.Event{
			ObjectMeta:     meta("shop", name),
			Type:           corev1.EventTypeWarning,
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: pod.Name, UID: pod.UID},
			LastTimestamp:  at,
		}
	}

	kube := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: meta("", "shop")},
		&corev1.Node{ObjectMeta: meta("", "worker-1")},
		&appsv1.Deployment{ObjectMeta: deploy},
		&appsv1.ReplicaSet{ObjectMeta: rs},
		&corev1.Pod{ObjectMeta: pod},
		&corev1.ConfigMap{
			ObjectMeta: meta("shop", "settings"),
			Data:       map[string]string{"b.yaml": "secret-looking value", "a.env": "x"},
			BinaryData: map[string][]byte{"logo.png": {1, 2}},
		},
		warning("old", older),
		warning("new", newer),
		&corev1.Event{ObjectMeta: meta("shop", "pulled"), Type: corev1.EventTypeNormal, InvolvedObject: corev1.ObjectReference{UID: pod.UID}},
	)
	dyn := fakeDynamic(
		gatewayObject("Gateway", "infra", "edge", map[string]any{"spec": map[string]any{"gatewayClassName": "nginx"}}),
		gatewayObject("HTTPRoute", "shop", "web", map[string]any{"spec": map[string]any{
			"parentRefs": []any{map[string]any{"name": "edge", "namespace": "infra"}},
		}}),
	)
	secrets := fakeMetadata(secretMeta("shop", "stripe-key"))

	s, err := Load(context.Background(), kubeClients(kube, dyn, secrets))
	if err != nil {
		t.Fatal(err)
	}

	if len(s.Skipped) != 0 {
		t.Errorf("nothing should be skipped, got %+v", s.Skipped)
	}
	if s.Taken.IsZero() {
		t.Error("Taken should be set")
	}

	t.Run("every kind is indexed", func(t *testing.T) {
		for _, key := range []Key{
			{"namespaces", "", "shop"}, {"nodes", "", "worker-1"}, {"deployments", "shop", "web"},
			{"replicasets", "shop", "web-7d9f"}, {"pods", "shop", "web-7d9f-x2k"}, {"configmaps", "shop", "settings"},
			{"secrets", "shop", "stripe-key"}, {"gateways", "infra", "edge"}, {"httproutes", "shop", "web"},
		} {
			if _, ok := s.Get(key.Resource, key.Namespace, key.Name); !ok {
				t.Errorf("%s not found", key)
			}
		}
		if s.Size() != 9 {
			t.Errorf("Size() = %d, want 9", s.Size())
		}
	})

	t.Run("objects are slimmed", func(t *testing.T) {
		obj, _ := s.Get("pods", "shop", "web-7d9f-x2k")
		p := obj.(*corev1.Pod)
		if p.ManagedFields != nil {
			t.Error("managedFields should be dropped")
		}
		if _, ok := p.Annotations[lastApplied]; ok || p.Annotations["team"] != "shop" {
			t.Errorf("only last-applied should be dropped, got %v", p.Annotations)
		}
	})

	t.Run("configmaps keep only their key names", func(t *testing.T) {
		obj, _ := s.Get("configmaps", "shop", "settings")
		cm := obj.(*ConfigMap)
		if !slices.Equal(cm.Keys, []string{"a.env", "b.yaml", "logo.png"}) {
			t.Errorf("keys = %v", cm.Keys)
		}
	})

	t.Run("secrets come from the metadata client", func(t *testing.T) {
		if len(s.Secrets) != 1 || s.Secrets[0].Name != "stripe-key" {
			t.Errorf("secrets = %+v", s.Secrets)
		}
		for _, action := range kube.Actions() {
			if action.GetResource().Resource == "secrets" {
				t.Errorf("secrets must never be read with the typed client (it returns values): %v", action)
			}
		}
	})

	t.Run("owners, UIDs and events", func(t *testing.T) {
		if got := s.Owned(deploy.UID); !slices.Equal(got, []Key{{"replicasets", "shop", "web-7d9f"}}) {
			t.Errorf("Owned(deployment) = %v", got)
		}
		if got := s.Owned(rs.UID); !slices.Equal(got, []Key{{"pods", "shop", "web-7d9f-x2k"}}) {
			t.Errorf("Owned(replicaset) = %v", got)
		}
		if key, ok := s.Lookup(pod.UID); !ok || key.Name != "web-7d9f-x2k" {
			t.Errorf("Lookup(pod) = %v, %v", key, ok)
		}
		events := s.EventsFor(pod.UID)
		if len(events) != 2 || events[0].Name != "new" || events[1].Name != "old" {
			t.Errorf("want the two warnings, newest first; got %d events", len(events))
		}
	})
}

func TestLoadSkipsWhatItCantRead(t *testing.T) {
	dyn := fakeDynamic()
	dyn.PrependReactor("list", "*", notFound) // no Gateway API
	secrets := fakeMetadata()
	secrets.PrependReactor("list", "secrets", forbidden) // no Secrets role

	s, err := Load(context.Background(), kubeClients(fake.NewClientset(), dyn, secrets))
	if err != nil {
		t.Fatal(err)
	}

	codes := map[string]string{}
	for _, skipped := range s.Skipped {
		codes[skipped.Resource] = skipped.Code
	}
	want := map[string]string{
		"secrets": "forbidden", "gateways": "not_installed", "httproutes": "not_installed",
		"gatewayclasses": "not_installed", "referencegrants": "not_installed",
	}
	for resource, code := range want {
		if codes[resource] != code {
			t.Errorf("%s: got %q, want %q", resource, codes[resource], code)
		}
	}
	if len(codes) != len(want) {
		t.Errorf("unexpected skips: %v", codes)
	}

	if s.Readable("secrets") || !s.Readable("pods") {
		t.Error("Readable should be false only for skipped kinds")
	}
	if skipped, ok := s.Skip("secrets"); !ok || skipped.Code != "forbidden" {
		t.Errorf("Skip(secrets) = %+v, %v", skipped, ok)
	}
}

func TestLoadFailsWhenNothingCanBeRead(t *testing.T) {
	down := func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("connection refused")
	}
	kube := fake.NewClientset()
	kube.PrependReactor("list", "*", down)
	dyn := fakeDynamic()
	dyn.PrependReactor("list", "*", down)
	secrets := fakeMetadata()
	secrets.PrependReactor("list", "*", down)

	if _, err := Load(context.Background(), kubeClients(kube, dyn, secrets)); err == nil {
		t.Fatal("want an error when the cluster can't be read at all")
	}
}

// A large list arrives in pages; every page is kept.
func TestLoadReadsEveryPage(t *testing.T) {
	kube := fake.NewClientset()
	var limits []int64
	kube.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		options := action.(clienttesting.ListActionImpl).ListOptions
		limits = append(limits, options.Limit)
		if options.Continue == "" {
			return true, &corev1.PodList{ListMeta: metav1.ListMeta{Continue: "page-2"}, Items: []corev1.Pod{{ObjectMeta: meta("a", "one")}}}, nil
		}
		return true, &corev1.PodList{Items: []corev1.Pod{{ObjectMeta: meta("a", "two")}}}, nil
	})

	s, err := Load(context.Background(), kubeClients(kube, fakeDynamic(), fakeMetadata()))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pods) != 2 {
		t.Errorf("got %d pods, want 2 (one per page)", len(s.Pods))
	}
	if !slices.Equal(limits, []int64{pageSize, pageSize}) {
		t.Errorf("limits = %v, want two pages of %d", limits, pageSize)
	}
}

func TestKeyString(t *testing.T) {
	if got := (Key{"pods", "shop", "web"}).String(); got != "pods/shop/web" {
		t.Errorf("got %q", got)
	}
	if got := (Key{"nodes", "", "worker-1"}).String(); got != "nodes/worker-1" {
		t.Errorf("got %q", got)
	}
}

func kubeClients(k *fake.Clientset, d *dynamicfake.FakeDynamicClient, m *metadatafake.FakeMetadataClient) kube.Clients {
	return kube.Clients{Kube: k, Dynamic: d, Metadata: m}
}
