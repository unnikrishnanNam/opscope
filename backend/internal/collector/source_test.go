package collector

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clienttesting "k8s.io/client-go/testing"

	"opscope/internal/bus"
	"opscope/internal/resources"
)

func om(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("uid-" + namespace + "-" + name), ResourceVersion: "1",
		CreationTimestamp: metav1.NewTime(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC))}
}

// aCluster has something of every kind, in two namespaces, with the cases
// the pages treat specially: a claim used by a pod, claims made from a
// StatefulSet, a claim whose StorageClass doesn't exist, Secrets with values,
// events of both types at the same moment, and Gateway API objects.
func aCluster() *fakeCluster {
	at := metav1.NewTime(time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC))
	standard := "standard"
	missing := "does-not-exist"
	replicas := int32(2)
	event := func(namespace, name, typ, kind, object string, uid types.UID) *corev1.Event {
		return &corev1.Event{ObjectMeta: om(namespace, name), Type: typ, Reason: "Test", LastTimestamp: at,
			InvolvedObject: corev1.ObjectReference{Kind: kind, Namespace: namespace, Name: object, UID: uid}}
	}
	claimVolume := func(claim string) corev1.Volume {
		return corev1.Volume{Name: claim, VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim}}}
	}
	pending := corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}
	bound := corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound,
		Capacity: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}}

	objects := []runtime.Object{
		&corev1.Namespace{ObjectMeta: om("", "web")},
		&corev1.Namespace{ObjectMeta: om("", "data")},
		&corev1.Node{ObjectMeta: om("", "worker-1"), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}},
		&corev1.Pod{ObjectMeta: om("web", "api-1"), Spec: corev1.PodSpec{NodeName: "worker-1", Containers: []corev1.Container{{Name: "api"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: om("web", "api-2"), Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api"}}},
			Status: corev1.PodStatus{Phase: corev1.PodPending}},
		&corev1.Pod{ObjectMeta: om("data", "db-0"), Spec: corev1.PodSpec{NodeName: "worker-1", Volumes: []corev1.Volume{claimVolume("data-db-0")}}},
		&corev1.Pod{ObjectMeta: om("data", "backup"), Spec: corev1.PodSpec{Volumes: []corev1.Volume{claimVolume("data-db-0")}}},
		event("web", "api-1.a", "Warning", "Pod", "api-1", "uid-web-api-1"),
		event("web", "api-1.b", "Normal", "Pod", "api-1", "uid-web-api-1"),
		event("data", "db-0.a", "Warning", "Pod", "db-0", "uid-data-db-0"),
		&appsv1.Deployment{ObjectMeta: om("web", "api"), Spec: appsv1.DeploymentSpec{Replicas: &replicas}},
		&appsv1.StatefulSet{ObjectMeta: om("data", "db"), Spec: appsv1.StatefulSetSpec{Replicas: &replicas,
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"}}}}},
		&appsv1.DaemonSet{ObjectMeta: om("web", "agent")},
		&batchv1.Job{ObjectMeta: om("data", "migrate")},
		&batchv1.CronJob{ObjectMeta: om("data", "nightly"), Spec: batchv1.CronJobSpec{Schedule: "0 3 * * *"}},
		&corev1.ConfigMap{ObjectMeta: om("web", "app"), Data: map[string]string{"mode": "fast"}, BinaryData: map[string][]byte{"logo": {1}}},
		&corev1.Secret{ObjectMeta: om("data", "db-password"), Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"password": []byte("hunter2")}},
		&corev1.Service{ObjectMeta: om("web", "api"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.0.0.1"}},
		&networkingv1.Ingress{ObjectMeta: om("web", "api")},
		&corev1.PersistentVolumeClaim{ObjectMeta: om("data", "data-db-0"), Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &standard, VolumeName: "pv-1"}, Status: bound},
		&corev1.PersistentVolumeClaim{ObjectMeta: om("data", "data-db-1"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard}, Status: pending},
		&corev1.PersistentVolumeClaim{ObjectMeta: om("web", "orphan"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &missing}, Status: pending},
		&corev1.PersistentVolume{ObjectMeta: om("", "pv-1"), Spec: corev1.PersistentVolumeSpec{StorageClassName: standard}},
		&storagev1.StorageClass{ObjectMeta: om("", "standard"), Provisioner: "example.com/disk"},
	}
	gateway := func(kind, namespace, name string) *unstructured.Unstructured {
		meta := map[string]any{"name": name, "uid": "uid-" + namespace + "-" + name}
		if namespace != "" {
			meta["namespace"] = namespace
		}
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "gateway.networking.k8s.io/v1", "kind": kind, "metadata": meta}}
	}
	f := newFakeCluster(objects)
	for _, o := range []struct {
		resource string
		obj      *unstructured.Unstructured
	}{
		{"gateways", gateway("Gateway", "web", "main")},
		{"httproutes", gateway("HTTPRoute", "web", "api")},
		{"gatewayclasses", gateway("GatewayClass", "", "nginx")},
	} {
		gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: "v1", Resource: o.resource}
		if err := f.dyn.Tracker().Create(gvr, o.obj, o.obj.GetNamespace()); err != nil {
			panic(err)
		}
	}
	// Detail pages read their object with the dynamic client.
	gvrs := map[string]schema.GroupVersionResource{}
	for _, k := range resources.Kinds() {
		gvrs[k.Resource] = k.GVR
	}
	for resource, obj := range map[string]runtime.Object{
		"pods":                   objects[3],
		"persistentvolumeclaims": objects[19],
		"statefulsets":           objects[11],
		"storageclasses":         objects[23],
		"persistentvolumes":      objects[22],
	} {
		raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		if err != nil {
			panic(err)
		}
		u := &unstructured.Unstructured{Object: raw}
		if err := f.dyn.Tracker().Create(gvrs[resource], u, u.GetNamespace()); err != nil {
			panic(err)
		}
	}
	return f
}

// asJSON shows an answer, or its error, as text to compare.
func asJSON(v any, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "unencodable: " + err.Error()
	}
	return string(out)
}

// The cache and the cluster give the same answer for every list, the
// overview, the name index and the detail pages' related lists.
func TestCacheAndClusterGiveTheSameAnswers(t *testing.T) {
	cluster := aCluster()
	c := Start(t.Context(), "lab", cluster.clients(), bus.New(), quiet)
	eventually(t, "every kind synced", func() bool { return c.Status().State == "live" })

	clients := resources.Clients{Kube: cluster.kube, Dynamic: cluster.dyn}
	direct := resources.Direct(clients)
	cached := &cacheSource{collector: c, direct: failingSource{t}, wait: 0}
	ctx := context.Background()

	queries := []resources.Query{{}, {Namespace: "web"}, {Namespace: "data"}, {Namespace: "nowhere"}, {Type: "Warning"}, {Namespace: "web", Type: "Normal"}}
	for resource, lister := range resources.Listers {
		for _, q := range queries {
			want := asJSON(lister(ctx, direct, q))
			got := asJSON(lister(ctx, cached, q))
			mustAnswer(t, resource, want)
			if got != want {
				t.Errorf("%s %+v:\ncache   %s\ncluster %s", resource, q, got, want)
			}
		}
	}
	for _, q := range queries[:3] {
		want := asJSON(resources.GetOverview(ctx, direct, q))
		mustAnswer(t, "overview", want)
		if got := asJSON(resources.GetOverview(ctx, cached, q)); got != want {
			t.Errorf("overview %+v:\ncache   %s\ncluster %s", q, got, want)
		}
	}
	want := asJSON(resources.ListNames(ctx, direct))
	mustAnswer(t, "names", want)
	if got := asJSON(resources.ListNames(ctx, cached)); got != want {
		t.Errorf("names:\ncache   %s\ncluster %s", got, want)
	}
	for _, d := range []struct{ resource, namespace, name string }{
		{"pods", "web", "api-1"},
		{"persistentvolumeclaims", "data", "data-db-0"},
		{"statefulsets", "data", "db"},
		{"storageclasses", "", "standard"},
		{"persistentvolumes", "", "pv-1"},
	} {
		want := asJSON(resources.GetDetail(ctx, clients, direct, d.resource, d.namespace, d.name))
		mustAnswer(t, d.resource+" detail", want)
		if got := asJSON(resources.GetDetail(ctx, clients, cached, d.resource, d.namespace, d.name)); got != want {
			t.Errorf("%s %s/%s detail:\ncache   %s\ncluster %s", d.resource, d.namespace, d.name, got, want)
		}
	}
}

// mustAnswer fails when the cluster's own answer is an error: two equal
// errors would compare as equal and prove nothing.
func mustAnswer(t *testing.T, what, answer string) {
	t.Helper()
	if strings.HasPrefix(answer, "error:") {
		t.Errorf("%s: %s", what, answer)
	}
}

// failingSource stands in for the cluster where a test expects every kind
// to come from the cache.
type failingSource struct{ t *testing.T }

func (f failingSource) List(_ context.Context, resource string, _ resources.Query) ([]runtime.Object, error) {
	f.t.Errorf("%s was read from the cluster, not the cache", resource)
	return nil, errors.New("not from the cache")
}

// A kind that failed to list is read from the cluster at once, without
// waiting, so its error (here: Gateway API not installed) is the one the
// pages already know how to show.
func TestFailedKindIsReadFromTheCluster(t *testing.T) {
	cluster := newFakeCluster(nil)
	cluster.dyn.PrependReactor("list", "gateways", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: "gateways"}, "")
	})
	c := Start(t.Context(), "lab", cluster.clients(), bus.New(), quiet)
	eventually(t, "the gateways error", func() bool {
		k := c.kind("gateways")
		k.mu.Lock()
		defer k.mu.Unlock()
		return k.lastErr != nil
	})

	src := &cacheSource{collector: c, direct: resources.Direct(resources.Clients{Kube: cluster.kube, Dynamic: cluster.dyn}), wait: time.Minute}
	start := time.Now()
	_, err := src.List(context.Background(), "gateways", resources.Query{})
	if !errors.Is(err, resources.ErrGatewayAPINotInstalled) {
		t.Errorf("err = %v, want ErrGatewayAPINotInstalled", err)
	}
	if time.Since(start) > time.Second {
		t.Error("a kind that failed shouldn't wait for its sync")
	}
}

// Before a kind's first list finishes, a request waits for it, up to the
// limit, then reads from the cluster.
func TestUnsyncedKindWaitsThenReadsTheCluster(t *testing.T) {
	cluster := newFakeCluster([]runtime.Object{pod("api-1", "1", nil)})
	release := make(chan struct{})
	cluster.kube.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		<-release // the collector's first list of pods hangs until released
		return false, nil, nil
	})
	t.Cleanup(func() { close(release) })
	c := Start(t.Context(), "lab", cluster.clients(), bus.New(), quiet)

	var fromCluster bool
	direct := sourceFunc(func(ctx context.Context, resource string, q resources.Query) ([]runtime.Object, error) {
		fromCluster = true
		return nil, nil
	})
	src := &cacheSource{collector: c, direct: direct, wait: 100 * time.Millisecond}
	start := time.Now()
	if _, err := src.List(context.Background(), "pods", resources.Query{}); err != nil {
		t.Fatal(err)
	}
	if !fromCluster || time.Since(start) < 100*time.Millisecond {
		t.Errorf("want a wait of the limit, then a read from the cluster (read from cluster: %v)", fromCluster)
	}
}

type sourceFunc func(context.Context, string, resources.Query) ([]runtime.Object, error)

func (f sourceFunc) List(ctx context.Context, resource string, q resources.Query) ([]runtime.Object, error) {
	return f(ctx, resource, q)
}

// A query for a moment in the past is refused, whatever the source.
func TestQueryAtIsNotRecordedYet(t *testing.T) {
	_, err := resources.Listers["pods"](context.Background(), sourceFunc(nil), resources.Query{At: time.Now().Add(-time.Hour)})
	if !errors.Is(err, resources.ErrNoHistory) {
		t.Errorf("err = %v, want ErrNoHistory", err)
	}
}
