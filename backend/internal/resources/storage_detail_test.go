package resources

import (
	"context"
	"errors"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

// relatedClients is detailClients with objects for the typed client too,
// which the related sections (pods using a claim, ...) list.
func relatedClients(t *testing.T, objects map[string]runtime.Object, kube ...runtime.Object) Clients {
	t.Helper()
	clients := detailClients(t, objects)
	clients.Kube = fake.NewClientset(kube...)
	return clients
}

func fieldLink(d *Detail, label string) *Link {
	for _, f := range d.Fields {
		if f.Label == label {
			return f.Link
		}
	}
	return nil
}

func table(d *Detail, title string) *Table {
	for i := range d.Tables {
		if d.Tables[i].Title == title {
			return &d.Tables[i]
		}
	}
	return nil
}

func TestClaimDetail(t *testing.T) {
	standard := "standard"
	claimVolume := func(claim string) corev1.Volume {
		return corev1.Volume{Name: "data", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim},
		}}
	}
	clients := relatedClients(t,
		map[string]runtime.Object{"persistentvolumeclaims": &corev1.PersistentVolumeClaim{
			ObjectMeta: meta("data", "db"),
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:        corev1.VolumeResourceRequirements{Requests: storage("1Gi")},
				StorageClassName: &standard,
				VolumeName:       "pvc-123",
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: storage("1536Mi")},
		}},
		&storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: "standard"}},
		&corev1.Pod{ObjectMeta: meta("data", "db-0"), Spec: corev1.PodSpec{NodeName: "worker-1", Volumes: []corev1.Volume{claimVolume("db")}}},
		&corev1.Pod{ObjectMeta: meta("data", "other"), Spec: corev1.PodSpec{Volumes: []corev1.Volume{claimVolume("cache")}}},
		// Same claim name, other namespace: a different claim.
		&corev1.Pod{ObjectMeta: meta("web", "db-0"), Spec: corev1.PodSpec{Volumes: []corev1.Volume{claimVolume("db")}}},
	)

	d, err := GetDetail(context.Background(), clients, "persistentvolumeclaims", "data", "db")
	if err != nil {
		t.Fatal(err)
	}
	for label, value := range map[string]string{
		"Status": "Bound", "Volume": "pvc-123", "StorageClass": "standard", "Requested": "1 GiB",
		"Capacity": "1.5 GiB", "Access modes": "ReadWriteOnce", "Volume mode": "Filesystem",
	} {
		if !hasField(d, label, value) {
			t.Errorf("want field %s = %q; fields = %+v", label, value, d.Fields)
		}
	}
	if l := fieldLink(d, "Volume"); l == nil || *l != (Link{Resource: "persistentvolumes", Name: "pvc-123"}) {
		t.Errorf("volume link = %+v", l)
	}
	if l := fieldLink(d, "StorageClass"); l == nil || *l != (Link{Resource: "storageclasses", Name: "standard"}) {
		t.Errorf("class link = %+v", l)
	}

	users := table(d, "Used by")
	if users == nil || len(users.Rows) != 1 || users.Rows[0][0] != "db-0" || users.Rows[0][2] != "worker-1" {
		t.Fatalf("used by = %+v", users)
	}
	if l := users.Links[0][0]; l == nil || *l != (Link{Resource: "pods", Namespace: "data", Name: "db-0"}) {
		t.Errorf("pod link = %+v", l)
	}
}

func TestClaimWithMissingClass(t *testing.T) {
	missing := "does-not-exist"
	clients := relatedClients(t, map[string]runtime.Object{
		"persistentvolumeclaims": &corev1.PersistentVolumeClaim{
			ObjectMeta: meta("data", "db"),
			Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &missing},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
	})

	d, err := GetDetail(context.Background(), clients, "persistentvolumeclaims", "data", "db")
	if err != nil {
		t.Fatal(err)
	}
	if !hasField(d, "StorageClass", "does-not-exist (not found)") || fieldLink(d, "StorageClass") != nil {
		t.Errorf("fields = %+v", d.Fields)
	}
}

func TestClaimDetailWithoutPodAccess(t *testing.T) {
	clients := relatedClients(t, map[string]runtime.Object{
		"persistentvolumeclaims": &corev1.PersistentVolumeClaim{ObjectMeta: meta("data", "db")},
	})
	clients.Kube.(*fake.Clientset).PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("RBAC"))
	})

	d, err := GetDetail(context.Background(), clients, "persistentvolumeclaims", "data", "db")
	if err != nil {
		t.Fatalf("a forbidden pod list should leave out \"Used by\", not fail: %v", err)
	}
	if table(d, "Used by") != nil {
		t.Error("want no \"Used by\" table")
	}
}

func TestVolumeDetail(t *testing.T) {
	volume := func(name string, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: name},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:                      storage("10Gi"),
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "local-path",
				ClaimRef:                      &corev1.ObjectReference{Namespace: "data", Name: "db"},
				PersistentVolumeSource: corev1.PersistentVolumeSource{CSI: &corev1.CSIPersistentVolumeSource{
					Driver:           "ebs.csi.aws.com",
					VolumeHandle:     "vol-1",
					VolumeAttributes: map[string]string{"type": "gp3"},
				}},
				NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{Key: "kubernetes.io/hostname", Operator: corev1.NodeSelectorOpIn, Values: []string{"worker-1"}},
					},
				}}}},
			},
			Status: corev1.PersistentVolumeStatus{Phase: phase},
		}
	}
	clients := relatedClients(t, map[string]runtime.Object{"persistentvolumes": volume("bound", corev1.VolumeBound)})

	d, err := GetDetail(context.Background(), clients, "persistentvolumes", "", "bound")
	if err != nil {
		t.Fatal(err)
	}
	if l := fieldLink(d, "Claim"); l == nil || *l != (Link{Resource: "persistentvolumeclaims", Namespace: "data", Name: "db"}) {
		t.Errorf("claim link = %+v", l)
	}
	if !hasField(d, "Source", "CSI: ebs.csi.aws.com") || !hasField(d, "Reclaim policy", "Retain") ||
		!hasField(d, "Capacity", "10 GiB") || !hasField(d, "Node affinity", "kubernetes.io/hostname in (worker-1)") {
		t.Errorf("fields = %+v", d.Fields)
	}
	settings := table(d, "Source settings")
	want := [][]string{{"driver", "ebs.csi.aws.com"}, {"volumeAttributes.type", "gp3"}, {"volumeHandle", "vol-1"}}
	if settings == nil || !reflect.DeepEqual(settings.Rows, want) {
		t.Errorf("source settings = %+v", settings)
	}

	// A Released volume names its old claim, but there's no claim left to link to.
	clients = relatedClients(t, map[string]runtime.Object{"persistentvolumes": volume("released", corev1.VolumeReleased)})
	d, err = GetDetail(context.Background(), clients, "persistentvolumes", "", "released")
	if err != nil {
		t.Fatal(err)
	}
	if !hasField(d, "Claim", "data/db (no longer bound)") || fieldLink(d, "Claim") != nil {
		t.Errorf("fields = %+v", d.Fields)
	}
}

func TestStorageClassDetail(t *testing.T) {
	standard, other := "standard", "other"
	clients := relatedClients(t,
		map[string]runtime.Object{"storageclasses": &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "standard",
				Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
			},
			Provisioner:  "rancher.io/local-path",
			Parameters:   map[string]string{"type": "gp3", "fsType": "ext4"},
			MountOptions: []string{"noatime"},
		}},
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-1"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "standard"}},
		&corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: "pv-2"}, Spec: corev1.PersistentVolumeSpec{StorageClassName: "other"}},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("a", "x"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard}},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("b", "y"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &standard}},
		&corev1.PersistentVolumeClaim{ObjectMeta: meta("b", "z"), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: &other}},
	)

	d, err := GetDetail(context.Background(), clients, "storageclasses", "", "standard")
	if err != nil {
		t.Fatal(err)
	}
	for label, value := range map[string]string{
		"Provisioner": "rancher.io/local-path", "Default": "Yes", "Reclaim policy": "Delete",
		"Binding mode": "Immediate", "Volume expansion": "No", "Mount options": "noatime",
		"Volumes": "1", "Claims": "2",
	} {
		if !hasField(d, label, value) {
			t.Errorf("want field %s = %q; fields = %+v", label, value, d.Fields)
		}
	}
	params := table(d, "Parameters")
	if params == nil || !reflect.DeepEqual(params.Rows, [][]string{{"fsType", "ext4"}, {"type", "gp3"}}) {
		t.Errorf("parameters = %+v", params)
	}
}

func TestPodVolumes(t *testing.T) {
	limit := resource.MustParse("64Mi")
	pod := &corev1.Pod{
		ObjectMeta: meta("web", "api-1"),
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "migrate", VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}}}},
			Containers: []corev1.Container{{Name: "app", VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: "/var/lib/app", ReadOnly: true},
				{Name: "config", MountPath: "/etc/app", SubPath: "app.yaml"},
			}}},
			Volumes: []corev1.Volume{
				{Name: "data", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}},
				{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"},
				}}},
				{Name: "scratch", VolumeSource: corev1.VolumeSource{Ephemeral: &corev1.EphemeralVolumeSource{}}},
				{Name: "cache", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory, SizeLimit: &limit,
				}}},
			},
		},
	}

	got := podVolumes(pod)
	wantRows := [][]string{
		{"data", "PersistentVolumeClaim", "api-data", "migrate: /data; app: /var/lib/app (read-only)"},
		{"config", "ConfigMap", "api-config", "app: /etc/app (subPath app.yaml)"},
		{"scratch", "Ephemeral", "api-1-scratch (ephemeral)", ""},
		{"cache", "EmptyDir", "memory, up to 64Mi", ""},
	}
	if !reflect.DeepEqual(got.Rows, wantRows) {
		t.Errorf("rows =\n  %v\nwant\n  %v", got.Rows, wantRows)
	}
	wantLinks := []*Link{
		claimLink("web", "api-data"),
		{Resource: "configmaps", Namespace: "web", Name: "api-config"},
		claimLink("web", "api-1-scratch"),
		nil,
	}
	for i, want := range wantLinks {
		if l := got.Links[i][2]; !reflect.DeepEqual(l, want) {
			t.Errorf("row %d source link = %+v, want %+v", i, l, want)
		}
	}
}

func TestStatefulSetClaims(t *testing.T) {
	fast := "fast"
	claim := func(name, volume string) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{
			ObjectMeta: meta("data", name),
			Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: volume},
			Status:     corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: storage("1Gi")},
		}
	}
	clients := relatedClients(t,
		map[string]runtime.Object{"statefulsets": &appsv1.StatefulSet{
			ObjectMeta: meta("data", "db"),
			Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data"},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					Resources:        corev1.VolumeResourceRequirements{Requests: storage("1Gi")},
					StorageClassName: &fast,
				},
			}}},
		}},
		claim("data-db-1", "pv-b"),
		claim("data-db-0", "pv-a"),
		claim("data-db-backup", ""), // not an ordinal
		claim("data-dbx-0", ""),     // another StatefulSet's
	)

	d, err := GetDetail(context.Background(), clients, "statefulsets", "data", "db")
	if err != nil {
		t.Fatal(err)
	}
	templates := table(d, "Volume claim templates")
	if templates == nil || !reflect.DeepEqual(templates.Rows, [][]string{{"data", "1 GiB", "ReadWriteOnce", "fast"}}) {
		t.Errorf("templates = %+v", templates)
	}
	claims := table(d, "Claims")
	want := [][]string{{"data-db-0", "Bound", "1 GiB", "pv-a"}, {"data-db-1", "Bound", "1 GiB", "pv-b"}}
	if claims == nil || !reflect.DeepEqual(claims.Rows, want) {
		t.Fatalf("claims = %+v", claims)
	}
	if l := claims.Links[0][3]; l == nil || *l != *volumeLink("pv-a") {
		t.Errorf("volume link = %+v", l)
	}
}

func TestAddRowKeepsLinksAligned(t *testing.T) {
	var tbl Table
	tbl.addRow(text("plain"))
	tbl.addRow(Cell{Text: "linked", Link: volumeLink("pv")})
	tbl.addRow(text("plain again"))
	if len(tbl.Links) != 3 || tbl.Links[0] != nil || tbl.Links[1][0] == nil || tbl.Links[2][0] != nil {
		t.Errorf("links = %+v", tbl.Links)
	}

	var plain Table
	plain.addRow(text("a"), text("b"))
	if plain.Links != nil {
		t.Error("a table without links should keep the plain shape")
	}
}

func TestSizeText(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1 << 30: "1 GiB", 3 << 29: "1.5 GiB", 256 << 20: "256 MiB"} {
		if got := sizeText(n); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", n, got, want)
		}
	}
}
