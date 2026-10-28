package resources

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PersistentVolumeClaim is one row of the PVC list.
type PersistentVolumeClaim struct {
	Meta
	Status       string   `json:"status"`       // Bound, Pending, Lost or Terminating
	Volume       string   `json:"volume"`       // the bound PV, "" while pending
	Capacity     int64    `json:"capacity"`     // provisioned bytes; the request while pending
	Requested    int64    `json:"requested"`    // bytes asked for in spec.resources.requests
	AccessModes  []string `json:"accessModes"`  // short form, like kubectl: RWO, ROX, RWX, RWOP
	StorageClass string   `json:"storageClass"` // "" means none set
	VolumeMode   string   `json:"volumeMode"`   // Filesystem or Block
}

func listPersistentVolumeClaims(ctx context.Context, client kubernetes.Interface, q Query) ([]PersistentVolumeClaim, error) {
	list, err := client.CoreV1().PersistentVolumeClaims(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]PersistentVolumeClaim, 0, len(list.Items))
	for _, pvc := range list.Items {
		requested := pvc.Spec.Resources.Requests.Storage().Value()
		capacity := requested
		if size, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
			capacity = size.Value()
		}
		rows = append(rows, PersistentVolumeClaim{
			Meta:         metaOf(pvc.ObjectMeta),
			Status:       storageStatus(pvc.ObjectMeta, string(pvc.Status.Phase)),
			Volume:       pvc.Spec.VolumeName,
			Capacity:     capacity,
			Requested:    requested,
			AccessModes:  accessModes(pvc.Spec.AccessModes),
			StorageClass: claimClass(&pvc),
			VolumeMode:   volumeMode(pvc.Spec.VolumeMode),
		})
	}
	sortRows(rows)
	return rows, nil
}

// PersistentVolume is one row of the PV list.
type PersistentVolume struct {
	Meta
	Status        string   `json:"status"`   // Available, Bound, Released, Failed, Pending or Terminating
	Capacity      int64    `json:"capacity"` // bytes
	AccessModes   []string `json:"accessModes"`
	ReclaimPolicy string   `json:"reclaimPolicy"` // Delete, Retain or Recycle
	Claim         string   `json:"claim"`         // "namespace/name" of the claim it's (or was) bound to
	StorageClass  string   `json:"storageClass"`
	Source        string   `json:"source"`  // e.g. "CSI: ebs.csi.aws.com", "HostPath", "NFS"
	Message       string   `json:"message"` // why it's Failed, shown on hover
}

// listPersistentVolumes ignores q.Namespace: PVs aren't inside a namespace.
func listPersistentVolumes(ctx context.Context, client kubernetes.Interface, q Query) ([]PersistentVolume, error) {
	list, err := client.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]PersistentVolume, 0, len(list.Items))
	for _, pv := range list.Items {
		row := PersistentVolume{
			Meta:          metaOf(pv.ObjectMeta),
			Status:        storageStatus(pv.ObjectMeta, string(pv.Status.Phase)),
			Capacity:      pv.Spec.Capacity.Storage().Value(),
			AccessModes:   accessModes(pv.Spec.AccessModes),
			ReclaimPolicy: string(pv.Spec.PersistentVolumeReclaimPolicy),
			StorageClass:  volumeClass(&pv),
			Source:        volumeSource(pv.Spec.PersistentVolumeSource),
			Message:       pv.Status.Message,
		}
		// A Released volume keeps its claimRef, so this also says which
		// (now deleted) claim the data belonged to.
		if ref := pv.Spec.ClaimRef; ref != nil {
			row.Claim = ref.Namespace + "/" + ref.Name
		}
		if row.Message == "" {
			row.Message = pv.Status.Reason
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// StorageClass is one row of the StorageClass list.
type StorageClass struct {
	Meta
	Provisioner    string `json:"provisioner"`
	ReclaimPolicy  string `json:"reclaimPolicy"`
	BindingMode    string `json:"bindingMode"` // Immediate or WaitForFirstConsumer
	AllowExpansion bool   `json:"allowExpansion"`
	Default        bool   `json:"default"` // claims without a class get this one
}

// listStorageClasses ignores q.Namespace: StorageClasses are cluster-wide.
func listStorageClasses(ctx context.Context, client kubernetes.Interface, q Query) ([]StorageClass, error) {
	list, err := client.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]StorageClass, 0, len(list.Items))
	for _, sc := range list.Items {
		row := StorageClass{
			Meta:          metaOf(sc.ObjectMeta),
			Provisioner:   sc.Provisioner,
			ReclaimPolicy: string(corev1.PersistentVolumeReclaimDelete), // the API's default
			BindingMode:   string(storagev1.VolumeBindingImmediate),
			Default:       isDefaultClass(sc.Annotations),
		}
		if sc.ReclaimPolicy != nil {
			row.ReclaimPolicy = string(*sc.ReclaimPolicy)
		}
		if sc.VolumeBindingMode != nil {
			row.BindingMode = string(*sc.VolumeBindingMode)
		}
		if sc.AllowVolumeExpansion != nil {
			row.AllowExpansion = *sc.AllowVolumeExpansion
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// storageStatus is the phase, or Terminating while the object is being
// deleted. A claim still used by a pod stays Terminating until the pod goes
// (the kubernetes.io/pvc-protection finalizer), which is worth seeing.
func storageStatus(obj metav1.ObjectMeta, phase string) string {
	if obj.DeletionTimestamp != nil {
		return "Terminating"
	}
	return phase
}

// accessModes shortens access modes the way kubectl does.
func accessModes(modes []corev1.PersistentVolumeAccessMode) []string {
	short := map[corev1.PersistentVolumeAccessMode]string{
		corev1.ReadWriteOnce:    "RWO",
		corev1.ReadOnlyMany:     "ROX",
		corev1.ReadWriteMany:    "RWX",
		corev1.ReadWriteOncePod: "RWOP",
	}
	result := []string{}
	for _, m := range modes {
		if s, ok := short[m]; ok {
			result = append(result, s)
		} else {
			result = append(result, string(m))
		}
	}
	return result
}

func volumeMode(mode *corev1.PersistentVolumeMode) string {
	if mode == nil {
		return string(corev1.PersistentVolumeFilesystem) // the API's default
	}
	return string(*mode)
}

// The class used to be set with an annotation, before storageClassName
// existed. Kubernetes still honours it, and it wins over the field.
const betaClassAnnotation = "volume.beta.kubernetes.io/storage-class"

func claimClass(pvc *corev1.PersistentVolumeClaim) string {
	if class, ok := pvc.Annotations[betaClassAnnotation]; ok {
		return class
	}
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	return ""
}

func volumeClass(pv *corev1.PersistentVolume) string {
	if class, ok := pv.Annotations[betaClassAnnotation]; ok {
		return class
	}
	return pv.Spec.StorageClassName
}

// isDefaultClass reads the annotation that marks the default StorageClass,
// under its current name or the older beta one.
func isDefaultClass(annotations map[string]string) bool {
	return annotations["storageclass.kubernetes.io/is-default-class"] == "true" ||
		annotations["storageclass.beta.kubernetes.io/is-default-class"] == "true"
}

// volumeSource names where a PV's data lives. Exactly one field of the
// source is set, and its Go name is a fine label ("HostPath", "NFS",
// "Local", ...), so it's found by looking at the fields rather than listing
// all of them here. CSI volumes, by far the most common today, also say
// which driver they use.
func volumeSource(src corev1.PersistentVolumeSource) string {
	if src.CSI != nil {
		return "CSI: " + src.CSI.Driver
	}
	v := reflect.ValueOf(src)
	for i := range v.NumField() {
		if f := v.Field(i); f.Kind() == reflect.Pointer && !f.IsNil() {
			return v.Type().Field(i).Name
		}
	}
	return ""
}
