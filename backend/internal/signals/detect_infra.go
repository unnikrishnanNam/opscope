package signals

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Detectors on storage and nodes.

func init() {
	Default.Register(Detector{
		Name:   "claims.binding",
		Kinds:  []string{"persistentvolumeclaims"},
		Types:  []string{"pvc_unbound"},
		Reads:  []string{"storageclasses"},
		Detect: claimBinding,
		Triggers: []Trigger{
			// A claim whose class binds on first use waits for a pod; one
			// arriving makes the wait a problem.
			{Resource: "pods", Targets: func(obj runtime.Object, c Context) []Key {
				pod, ok := obj.(*corev1.Pod)
				if !ok {
					return nil
				}
				var out []Key
				for _, v := range pod.Spec.Volumes {
					if v.PersistentVolumeClaim != nil {
						out = append(out, Key{"persistentvolumeclaims", pod.Namespace, v.PersistentVolumeClaim.ClaimName})
					}
				}
				return out
			}},
		},
	})
	Default.Register(Detector{
		Name:   "claims.usage",
		Kinds:  []string{"persistentvolumeclaims"},
		Types:  []string{"volume_nearly_full"},
		Detect: claimUsage,
	})
	Default.Register(Detector{
		Name:   "volumes.phase",
		Kinds:  []string{"persistentvolumes"},
		Types:  []string{"volume_released"},
		Detect: volumePhase,
	})
	Default.Register(Detector{
		Name:   "nodes.conditions",
		Kinds:  []string{"nodes"},
		Types:  []string{"node_not_ready", "node_pressure", "node_unschedulable"},
		Detect: nodeConditions,
	})
}

// claimBinding: a claim that isn't bound. It waits two minutes from when
// the claim was made, for a provisioner to make its volume. A claim whose
// class binds when a pod first uses it is only waiting while no pod does.
func claimBinding(obj runtime.Object, c Context) []Found {
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok || pvc.DeletionTimestamp != nil {
		return nil
	}
	ev := []Evidence{{Source: FromField, Field: "status.phase", Value: string(pvc.Status.Phase), ResourceVersion: pvc.ResourceVersion}}
	switch pvc.Status.Phase {
	case corev1.ClaimLost:
		return []Found{{Type: "pvc_unbound", Since: pvc.CreationTimestamp.Time, Evidence: ev,
			Message: "Its volume " + orElse(pvc.Spec.VolumeName, "") + " is gone (Lost)"}}
	case corev1.ClaimPending:
		if waitsForConsumer(pvc, c) && !inUse(pvc, c) {
			return nil
		}
		msg := "Not bound to a volume"
		if class := classOf(pvc, c); class != "" {
			msg += " (StorageClass " + class + ")"
		}
		return []Found{{Type: "pvc_unbound", Since: pvc.CreationTimestamp.Time, Message: msg, Evidence: ev}}
	}
	return nil
}

// classOf is the claim's StorageClass, or the cluster's default when it
// names none.
func classOf(pvc *corev1.PersistentVolumeClaim, c Context) string {
	if pvc.Spec.StorageClassName != nil {
		return *pvc.Spec.StorageClassName
	}
	for _, o := range c.List("storageclasses", "") {
		if sc := o.(*storagev1.StorageClass); sc.Annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			return sc.Name
		}
	}
	return ""
}

// waitsForConsumer: the claim's class makes its volume when a pod first
// uses the claim. When the class can't be read, it's taken as not waiting:
// a Pending claim is then reported, as before this check.
func waitsForConsumer(pvc *corev1.PersistentVolumeClaim, c Context) bool {
	sc, ok := c.Get("storageclasses", "", classOf(pvc, c)).(*storagev1.StorageClass)
	return ok && sc.VolumeBindingMode != nil && *sc.VolumeBindingMode == storagev1.VolumeBindingWaitForFirstConsumer
}

func inUse(pvc *corev1.PersistentVolumeClaim, c Context) bool {
	if !c.Readable("pods") {
		return true // can't tell; report it
	}
	for _, o := range c.List("pods", pvc.Namespace) {
		for _, v := range o.(*corev1.Pod).Spec.Volumes {
			if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvc.Name {
				return true
			}
		}
	}
	return false
}

// claimUsage: a claim's volume is 90% full or more, in space or in inodes,
// as the kubelet measured it. Only with volume usage on.
func claimUsage(obj runtime.Object, c Context) []Found {
	pvc, ok := obj.(*corev1.PersistentVolumeClaim)
	if !ok || pvc.DeletionTimestamp != nil {
		return nil
	}
	fill, ok := c.VolumeFill(pvc.Namespace, pvc.Name)
	if !ok {
		return nil
	}
	space, inodes := percent(fill.Used, fill.Capacity), percent(fill.InodesUsed, fill.Inodes)
	worst, what := space, "space"
	if inodes > space {
		worst, what = inodes, "inodes"
	}
	if worst < 90 {
		return nil
	}
	f := Found{
		Type:    "volume_nearly_full",
		Message: fmt.Sprintf("%d%% of its %s used", worst, what),
		Evidence: []Evidence{
			{Source: FromMetric, Field: "kubelet stats: volume used/capacity", Value: fmt.Sprintf("%d/%d bytes", fill.Used, fill.Capacity), Seen: fill.Seen},
			{Source: FromMetric, Field: "kubelet stats: volume inodesUsed/inodes", Value: fmt.Sprintf("%d/%d", fill.InodesUsed, fill.Inodes), Seen: fill.Seen},
		},
	}
	if worst >= 98 {
		f.Severity = Critical
	}
	return []Found{f}
}

func percent(used, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(used * 100 / total)
}

// volumePhase: a volume left behind by its claim (Released), or one that
// failed to be reclaimed (Failed, a warning).
func volumePhase(obj runtime.Object, c Context) []Found {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok || pv.DeletionTimestamp != nil {
		return nil
	}
	ev := []Evidence{{Source: FromField, Field: "status.phase", Value: string(pv.Status.Phase), ResourceVersion: pv.ResourceVersion}}
	claim := ""
	if ref := pv.Spec.ClaimRef; ref != nil {
		claim = ref.Namespace + "/" + ref.Name
	}
	switch pv.Status.Phase {
	case corev1.VolumeReleased:
		return []Found{{Type: "volume_released", Evidence: ev,
			Message: "Its claim " + orElse(claim, "") + " is gone; the volume is kept (" + string(pv.Spec.PersistentVolumeReclaimPolicy) + ")"}}
	case corev1.VolumeFailed:
		return []Found{{Type: "volume_released", Severity: Warning, Evidence: ev,
			Message: "Reclaiming it failed: " + orElse(pv.Status.Message, "no reason given")}}
	}
	return nil
}

// nodeConditions: a node that isn't ready, one short of memory, disk or
// process IDs, and one cordoned.
func nodeConditions(obj runtime.Object, c Context) []Found {
	node, ok := obj.(*corev1.Node)
	if !ok || node.DeletionTimestamp != nil {
		return nil
	}
	rv := node.ResourceVersion
	var out []Found
	sawReady := false
	var pressure Found
	var short []string
	for i, cond := range node.Status.Conditions {
		field := fmt.Sprintf("status.conditions[%d]", i)
		switch cond.Type {
		case corev1.NodeReady:
			sawReady = true
			if cond.Status == corev1.ConditionTrue {
				continue
			}
			out = append(out, Found{Type: "node_not_ready", Since: cond.LastTransitionTime.Time,
				Message:  "Not ready: " + orElse(cond.Message, cond.Reason),
				Evidence: []Evidence{{Source: FromField, Field: field, Value: fmt.Sprintf("Ready=%s (%s)", cond.Status, cond.Reason), ResourceVersion: rv}}})
		case corev1.NodeMemoryPressure, corev1.NodeDiskPressure, corev1.NodePIDPressure:
			if cond.Status != corev1.ConditionTrue {
				continue
			}
			short = append(short, strings.TrimSuffix(string(cond.Type), "Pressure"))
			pressure.Evidence = append(pressure.Evidence, Evidence{Source: FromField, Field: field, Value: string(cond.Type) + "=True", ResourceVersion: rv})
			if pressure.Since.IsZero() || cond.LastTransitionTime.Time.Before(pressure.Since) {
				pressure.Since = cond.LastTransitionTime.Time
			}
		}
	}
	if !sawReady {
		out = append(out, Found{Type: "node_not_ready", Message: "It reports no Ready condition",
			Evidence: []Evidence{{Source: FromField, Field: "status.conditions", Value: "no Ready", ResourceVersion: rv}}})
	}
	if short != nil {
		pressure.Type = "node_pressure"
		pressure.Message = "Short of " + strings.ToLower(strings.Join(short, ", "))
		out = append(out, pressure)
	}
	if node.Spec.Unschedulable {
		out = append(out, Found{Type: "node_unschedulable", Message: "Cordoned: no new pods are scheduled on it",
			Evidence: []Evidence{{Source: FromField, Field: "spec.unschedulable", Value: "true", ResourceVersion: rv}}})
	}
	return out
}
