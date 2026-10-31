package resources

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

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

func storage(size string) corev1.ResourceList {
	return corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)}
}

func TestListPersistentVolumeClaims(t *testing.T) {
	standard := "standard"
	block := corev1.PersistentVolumeBlock
	now := metav1.Now()
	client := fake.NewClientset(
		&corev1.PersistentVolumeClaim{
			ObjectMeta: meta("data", "bound"),
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce, corev1.ReadWriteOncePod},
				Resources:        corev1.VolumeResourceRequirements{Requests: storage("1Gi")},
				StorageClassName: &standard,
				VolumeName:       "pvc-123",
			},
			// Provisioners often round up, so the volume can be bigger than asked.
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound, Capacity: storage("2Gi")},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: meta("data", "pending"),
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources:  corev1.VolumeResourceRequirements{Requests: storage("5Gi")},
				VolumeMode: &block,
			},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:         "data",
				Name:              "old-style",
				DeletionTimestamp: &now,
				Finalizers:        []string{"kubernetes.io/pvc-protection"},
				Annotations:       map[string]string{betaClassAnnotation: "slow"},
			},
			Spec:   corev1.PersistentVolumeClaimSpec{StorageClassName: &standard},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
		},
	)

	claims, err := listPersistentVolumeClaims(context.Background(), client, Query{Namespace: "data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 3 {
		t.Fatalf("got %d claims", len(claims))
	}
	bound, oldStyle, pending := claims[0], claims[1], claims[2]

	gi := int64(1 << 30)
	if bound.Status != "Bound" || bound.Volume != "pvc-123" || bound.Capacity != 2*gi || bound.Requested != gi ||
		strings.Join(bound.AccessModes, ",") != "RWO,RWOP" || bound.StorageClass != "standard" ||
		bound.VolumeMode != "Filesystem" {
		t.Errorf("unexpected bound claim: %+v", bound)
	}
	if pending.Status != "Pending" || pending.Volume != "" || pending.Capacity != 5*gi ||
		pending.StorageClass != "" || pending.VolumeMode != "Block" {
		t.Errorf("unexpected pending claim: %+v", pending)
	}
	if oldStyle.Status != "Terminating" || oldStyle.StorageClass != "slow" {
		t.Errorf("unexpected old-style claim: %+v", oldStyle)
	}
}

func TestListPersistentVolumes(t *testing.T) {
	client := fake.NewClientset(
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-123"},
			Spec: corev1.PersistentVolumeSpec{
				Capacity:                      storage("10Gi"),
				AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany},
				PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
				StorageClassName:              "fast",
				ClaimRef:                      &corev1.ObjectReference{Namespace: "data", Name: "db"},
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					CSI: &corev1.CSIPersistentVolumeSource{Driver: "ebs.csi.aws.com", VolumeHandle: "vol-1"},
				},
			},
			Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased},
		},
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "local-1"},
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					HostPath: &corev1.HostPathVolumeSource{Path: "/mnt/data"},
				},
			},
			Status: corev1.PersistentVolumeStatus{
				Phase:   corev1.VolumeFailed,
				Reason:  "VolumeFailedRecycle",
				Message: "recycler pod failed",
			},
		},
		&corev1.PersistentVolume{
			ObjectMeta: metav1.ObjectMeta{Name: "nfs-1"},
			Spec: corev1.PersistentVolumeSpec{
				PersistentVolumeSource: corev1.PersistentVolumeSource{
					NFS: &corev1.NFSVolumeSource{Server: "10.0.0.5", Path: "/exports"},
				},
			},
			Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeAvailable, Reason: "only a reason"},
		},
	)

	volumes, err := listPersistentVolumes(context.Background(), client, Query{Namespace: "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if len(volumes) != 3 {
		t.Fatalf("got %d volumes", len(volumes))
	}
	hostPath, nfs, csi := volumes[0], volumes[1], volumes[2]

	if csi.Status != "Released" || csi.Capacity != 10<<30 || strings.Join(csi.AccessModes, ",") != "RWX" ||
		csi.ReclaimPolicy != "Retain" || csi.Claim != "data/db" || csi.StorageClass != "fast" ||
		csi.Source != "CSI: ebs.csi.aws.com" {
		t.Errorf("unexpected CSI volume: %+v", csi)
	}
	if hostPath.Status != "Failed" || hostPath.Source != "HostPath" || hostPath.Claim != "" ||
		hostPath.Message != "recycler pod failed" {
		t.Errorf("unexpected hostPath volume: %+v", hostPath)
	}
	if nfs.Source != "NFS" || nfs.Message != "only a reason" {
		t.Errorf("unexpected NFS volume: %+v", nfs)
	}
}

func TestVolumeSourceWithNothingSet(t *testing.T) {
	if got := volumeSource(corev1.PersistentVolumeSource{}); got != "" {
		t.Errorf("volumeSource = %q, want empty", got)
	}
}

func TestListStorageClasses(t *testing.T) {
	retain := corev1.PersistentVolumeReclaimRetain
	waitForPod := storagev1.VolumeBindingWaitForFirstConsumer
	yes := true
	client := fake.NewClientset(
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "standard",
				Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "true"},
			},
			Provisioner:          "rancher.io/local-path",
			ReclaimPolicy:        &retain,
			VolumeBindingMode:    &waitForPod,
			AllowVolumeExpansion: &yes,
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "legacy",
				Annotations: map[string]string{"storageclass.beta.kubernetes.io/is-default-class": "true"},
			},
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "plain",
				Annotations: map[string]string{"storageclass.kubernetes.io/is-default-class": "false"},
			},
			Provisioner: "nfs.csi.k8s.io",
		},
	)

	classes, err := listStorageClasses(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) != 3 {
		t.Fatalf("got %d classes", len(classes))
	}
	legacy, plain, standard := classes[0], classes[1], classes[2]

	if !standard.Default || standard.Provisioner != "rancher.io/local-path" || standard.ReclaimPolicy != "Retain" ||
		standard.BindingMode != "WaitForFirstConsumer" || !standard.AllowExpansion {
		t.Errorf("unexpected standard class: %+v", standard)
	}
	if !legacy.Default {
		t.Errorf("the beta annotation should still mark the default: %+v", legacy)
	}
	// Nothing set: the API's defaults.
	if plain.Default || plain.ReclaimPolicy != "Delete" || plain.BindingMode != "Immediate" || plain.AllowExpansion {
		t.Errorf("unexpected plain class: %+v", plain)
	}
}

func TestStorageDetails(t *testing.T) {
	clients := detailClients(t, map[string]runtime.Object{
		"persistentvolumeclaims": &corev1.PersistentVolumeClaim{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
			ObjectMeta: meta("data", "db"),
		},
		"persistentvolumes": &corev1.PersistentVolume{
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolume"},
			ObjectMeta: metav1.ObjectMeta{Name: "pvc-123"},
		},
		"storageclasses": &storagev1.StorageClass{
			TypeMeta:   metav1.TypeMeta{APIVersion: "storage.k8s.io/v1", Kind: "StorageClass"},
			ObjectMeta: metav1.ObjectMeta{Name: "standard"},
		},
	})

	tests := []struct{ resource, namespace, name, kind string }{
		{"persistentvolumeclaims", "data", "db", "PersistentVolumeClaim"},
		// Cluster-wide kinds ignore the namespace in the URL.
		{"persistentvolumes", "anything", "pvc-123", "PersistentVolume"},
		{"storageclasses", "", "standard", "StorageClass"},
	}
	for _, tt := range tests {
		d, err := GetDetail(context.Background(), clients, tt.resource, tt.namespace, tt.name)
		if err != nil {
			t.Errorf("%s: %v", tt.resource, err)
			continue
		}
		if d.Kind != tt.kind || d.Name != tt.name || !strings.Contains(d.YAML, "kind: "+tt.kind) {
			t.Errorf("%s: unexpected detail: kind %q, name %q", tt.resource, d.Kind, d.Name)
		}
	}
}

func TestOverviewStorage(t *testing.T) {
	claim := func(namespace, name string, phase corev1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
		return &corev1.PersistentVolumeClaim{ObjectMeta: meta(namespace, name), Status: corev1.PersistentVolumeClaimStatus{Phase: phase}}
	}
	// Being deleted while a pod still uses it: Terminating, but still bound.
	terminating := claim("data", "leaving", corev1.ClaimBound)
	now := metav1.Now()
	terminating.DeletionTimestamp = &now
	terminating.Finalizers = []string{"kubernetes.io/pvc-protection"}
	volume := func(name string, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.PersistentVolumeStatus{Phase: phase}}
	}
	client := fake.NewClientset(
		claim("data", "db", corev1.ClaimBound),
		claim("data", "waiting", corev1.ClaimPending),
		claim("data", "lost", corev1.ClaimLost),
		claim("web", "cache", corev1.ClaimPending),
		terminating,
		volume("bound", corev1.VolumeBound),
		volume("spare", corev1.VolumeAvailable),
		volume("released", corev1.VolumeReleased),
		volume("failed", corev1.VolumeFailed),
	)
	clients := Clients{Kube: client, Dynamic: fakeDynamic()}

	o, err := GetOverview(context.Background(), clients, Query{})
	if err != nil {
		t.Fatal(err)
	}
	want := []WorkloadCount{
		{Resource: "persistentvolumeclaims", Total: 5, Unhealthy: 3},
		{Resource: "persistentvolumes", Total: 4, Unhealthy: 2},
	}
	if !slices.Equal(o.Storage, want) {
		t.Errorf("all namespaces: got %+v, want %+v", o.Storage, want)
	}

	// Claims follow the namespace; volumes are cluster-wide, like nodes.
	o, err = GetOverview(context.Background(), clients, Query{Namespace: "web"})
	if err != nil {
		t.Fatal(err)
	}
	want[0] = WorkloadCount{Resource: "persistentvolumeclaims", Total: 1, Unhealthy: 1}
	if !slices.Equal(o.Storage, want) {
		t.Errorf("namespace web: got %+v, want %+v", o.Storage, want)
	}
}

func TestOverviewWithoutStorage(t *testing.T) {
	o, err := GetOverview(context.Background(), Clients{Kube: fake.NewClientset(), Dynamic: fakeDynamic()}, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Storage != nil {
		t.Errorf("a cluster with no claims or volumes should get no counts, got %+v", o.Storage)
	}

	// Credentials that can't list volumes still get the rest of the overview.
	client := fake.NewClientset(&corev1.PersistentVolumeClaim{ObjectMeta: meta("data", "db")})
	client.PrependReactor("list", "persistentvolumes", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "persistentvolumes"}, "", errors.New("RBAC"))
	})
	o, err = GetOverview(context.Background(), Clients{Kube: client, Dynamic: fakeDynamic()}, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Storage != nil {
		t.Errorf("forbidden storage should be left out, got %+v", o.Storage)
	}
}
