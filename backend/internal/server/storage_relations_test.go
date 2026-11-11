package server

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"opscope/internal/graph"
	"opscope/internal/resources"
)

// The storage pages' related parts, read from the graph: who uses a claim,
// a class that doesn't exist, a class's counts, a StatefulSet's claims.
// (Before v1.5 these were in resources/storage_detail_test.go.)

// detailFromGraph reads a detail page and completes it from a graph of the
// same objects, as the server does.
func detailFromGraph(t *testing.T, objects []runtime.Object, resource, namespace, name string) *resources.Detail {
	t.Helper()
	clients := clientsFor(t, objects)
	src := resources.Direct(clients)
	g, _, _ := buildGraph(t.Context(), src)
	d, err := resources.GetDetail(context.Background(), clients, src, resource, namespace, name)
	if err != nil {
		t.Fatal(err)
	}
	addRelations(t.Context(), &fakeGraphs{g: g, info: graph.Info{Built: graph.BuiltLive}}, "lab", src, d, resource)
	return d
}

func smeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID("uid-" + namespace + "-" + name)}
}

func fieldOf(d *resources.Detail, label string) *resources.Field {
	for i := range d.Fields {
		if d.Fields[i].Label == label {
			return &d.Fields[i]
		}
	}
	return nil
}

func tableOf(d *resources.Detail, title string) *resources.Table {
	for i := range d.Tables {
		if d.Tables[i].Title == title {
			return &d.Tables[i]
		}
	}
	return nil
}

func claimVolume(claim string) corev1.Volume {
	return corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
	}}
}

func TestClaimUsersFromTheGraph(t *testing.T) {
	standard := "standard"
	d := detailFromGraph(t, []runtime.Object{
		&corev1.PersistentVolumeClaim{ObjectMeta: smeta("data", "db"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard, VolumeName: "pvc-123"}},
		&storagev1.StorageClass{ObjectMeta: smeta("", "standard")},
		&corev1.Node{ObjectMeta: smeta("", "worker-1")},
		&corev1.Pod{ObjectMeta: smeta("data", "db-0"), Spec: corev1.PodSpec{NodeName: "worker-1", Volumes: []corev1.Volume{claimVolume("db")}}},
		&corev1.Pod{ObjectMeta: smeta("data", "other"), Spec: corev1.PodSpec{Volumes: []corev1.Volume{claimVolume("cache")}}},
		// Same claim name, other namespace: a different claim.
		&corev1.Pod{ObjectMeta: smeta("web", "db-0"), Spec: corev1.PodSpec{Volumes: []corev1.Volume{claimVolume("db")}}},
	}, "persistentvolumeclaims", "data", "db")

	users := tableOf(d, "Used by")
	if users == nil || len(users.Rows) != 1 || users.Rows[0][0] != "db-0" || users.Rows[0][2] != "worker-1" {
		t.Fatalf("used by = %+v", users)
	}
	if l := users.Links[0][0]; l == nil || *l != (resources.Link{Resource: "pods", Namespace: "data", Name: "db-0"}) {
		t.Errorf("pod link = %+v", l)
	}
	// The volume pvc-123 doesn't exist here: its link says so.
	if f := fieldOf(d, "Volume"); f == nil || f.Value != "pvc-123 (not found)" || f.Link != nil {
		t.Errorf("volume = %+v", f)
	}
	if f := fieldOf(d, "StorageClass"); f == nil || f.Value != "standard" || f.Link == nil {
		t.Errorf("class = %+v", f)
	}
}

func TestClaimWithMissingClass(t *testing.T) {
	missing := "does-not-exist"
	d := detailFromGraph(t, []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: smeta("data", "db"),
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &missing},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
	}, "persistentvolumeclaims", "data", "db")
	if f := fieldOf(d, "StorageClass"); f == nil || f.Value != "does-not-exist (not found)" || f.Link != nil {
		t.Errorf("fields = %+v", d.Fields)
	}
}

func TestStorageClassCountsFromTheGraph(t *testing.T) {
	standard, other := "standard", "other"
	d := detailFromGraph(t, []runtime.Object{
		&storagev1.StorageClass{ObjectMeta: smeta("", "standard")},
		&corev1.PersistentVolume{ObjectMeta: smeta("", "pv-1"), Spec: corev1.PersistentVolumeSpec{StorageClassName: "standard"}},
		&corev1.PersistentVolume{ObjectMeta: smeta("", "pv-2"), Spec: corev1.PersistentVolumeSpec{StorageClassName: "other"}},
		&corev1.PersistentVolumeClaim{ObjectMeta: smeta("a", "x"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard}},
		&corev1.PersistentVolumeClaim{ObjectMeta: smeta("b", "y"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard}},
		&corev1.PersistentVolumeClaim{ObjectMeta: smeta("b", "z"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &other}},
	}, "storageclasses", "", "standard")
	for label, value := range map[string]string{"Volumes": "1", "Claims": "2"} {
		if f := fieldOf(d, label); f == nil || f.Value != value {
			t.Errorf("%s = %+v", label, f)
		}
	}
}

func TestStatefulSetClaimsFromTheGraph(t *testing.T) {
	gi := resource.MustParse("1Gi")
	claim := func(name, volume string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: smeta("data", name),
			Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: volume},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: corev1.ResourceList{corev1.ResourceStorage: gi}},
		}
	}
	d := detailFromGraph(t, []runtime.Object{
		&appsv1.StatefulSet{ObjectMeta: smeta("data", "db"), Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{ObjectMeta: metav1.ObjectMeta{Name: "data"}}},
		}},
		claim("data-db-1", "pv-b"),
		claim("data-db-0", "pv-a"),
		claim("data-db-backup", ""), // not an ordinal
		claim("data-dbx-0", ""),     // another StatefulSet's
		&corev1.PersistentVolume{ObjectMeta: smeta("", "pv-a")},
		&corev1.PersistentVolume{ObjectMeta: smeta("", "pv-b")},
	}, "statefulsets", "data", "db")

	claims := tableOf(d, "Claims")
	want := [][]string{{"data-db-0", "Bound", "1 GiB", "pv-a"}, {"data-db-1", "Bound", "1 GiB", "pv-b"}}
	if claims == nil || !reflect.DeepEqual(claims.Rows, want) {
		t.Fatalf("claims = %+v", claims)
	}
	if l := claims.Links[0][3]; l == nil || *l != (resources.Link{Resource: "persistentvolumes", Name: "pv-a"}) {
		t.Errorf("volume link = %+v", l)
	}
}

// "Used by" puts workloads first, counts their pods under them however
// many owners away (a CronJob's Job's pod), then lists pods nothing listed
// owns, then anything else that uses it.
func TestUsedBy(t *testing.T) {
	yes := true
	ownedBy := func(m metav1.ObjectMeta, apiVersion, kind, name string) metav1.ObjectMeta {
		m.OwnerReferences = []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: name, UID: types.UID("uid-shop-" + name), Controller: &yes}}
		return m
	}
	pull := corev1.PodSpec{ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}}}
	job := &batchv1.Job{ObjectMeta: ownedBy(smeta("shop", "report-1"), "batch/v1", "CronJob", "report")}
	job.Spec.Template.Spec = pull
	cron := &batchv1.CronJob{ObjectMeta: smeta("shop", "report")}
	cron.Spec.JobTemplate.Spec.Template.Spec = pull
	d := detailFromGraph(t, []runtime.Object{
		&corev1.Secret{ObjectMeta: smeta("shop", "regcred")},
		cron, job,
		&corev1.Pod{ObjectMeta: ownedBy(smeta("shop", "report-1-x"), "batch/v1", "Job", "report-1"), Spec: pull},
		&corev1.Pod{ObjectMeta: smeta("shop", "lonely"), Spec: pull},
		&networkingv1.Ingress{ObjectMeta: smeta("shop", "web"), Spec: networkingv1.IngressSpec{TLS: []networkingv1.IngressTLS{{SecretName: "regcred"}}}},
	}, "secrets", "shop", "regcred")

	used := tableOf(d, "Used by")
	want := [][]string{
		{"CronJob", "report", "image pull secret", "1"},
		{"Job", "report-1", "image pull secret", "1"},
		{"Pod", "lonely", "image pull secret", ""},
		{"Ingress", "web", "TLS", ""},
	}
	if used == nil || !reflect.DeepEqual(used.Rows, want) {
		t.Fatalf("used by = %+v", used)
	}
}
