package graph

// Builders for claims and volumes.

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// betaClassAnnotation set the class before storageClassName existed.
// Kubernetes still honours it, and it wins over the field.
const betaClassAnnotation = "volume.beta.kubernetes.io/storage-class"

func init() {
	Register(Builder{Name: "persistentvolumeclaims.binding", Resource: "persistentvolumeclaims", Refs: claimBinding})
	Register(Builder{Name: "persistentvolumes.class", Resource: "persistentvolumes", Refs: volumeClass})
}

// claimBinding: a claim binds to its volume, and asks for a class. The
// class is an edge too, so a pending claim still shows the class it waits
// for, or that it doesn't exist. A claim with no class (or "", which means
// no class on purpose) has no class edge.
func claimBinding(obj runtime.Object, r *Refs) {
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok {
		return
	}
	r.To(Binds, "persistentvolumes", "", pvc.Spec.VolumeName, "spec.volumeName", "", false)
	if class, ok := pvc.Annotations[betaClassAnnotation]; ok {
		r.To(Binds, "storageclasses", "", class, "metadata.annotations["+betaClassAnnotation+"]", "", false)
	} else if pvc.Spec.StorageClassName != nil {
		r.To(Binds, "storageclasses", "", *pvc.Spec.StorageClassName, "spec.storageClassName", "", false)
	}
}

func volumeClass(obj runtime.Object, r *Refs) {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok {
		return
	}
	if class, ok := pv.Annotations[betaClassAnnotation]; ok {
		r.To(Binds, "storageclasses", "", class, "metadata.annotations["+betaClassAnnotation+"]", "", false)
	} else {
		r.To(Binds, "storageclasses", "", pv.Spec.StorageClassName, "spec.storageClassName", "", false)
	}
}
