package resources

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

// StatusOf is the short status an object's list page shows, for places
// that show one object without its row, like the graph: "Running",
// "CrashLoopBackOff", "2/3" ready, "Bound", "Accepted". It's "" for kinds
// whose list has no status (ConfigMaps, Services, ...). It's what the
// tables say, worked out by the same code, not a judgement of health.
func StatusOf(resource string, obj runtime.Object) string {
	switch o := obj.(type) {
	case *corev1.Pod:
		return podStatus(o)
	case *corev1.Node:
		return nodeStatus(o)
	case *appsv1.Deployment:
		return readyOf(o.Status.ReadyReplicas, replicas(o.Spec.Replicas))
	case *appsv1.StatefulSet:
		return readyOf(o.Status.ReadyReplicas, replicas(o.Spec.Replicas))
	case *appsv1.ReplicaSet:
		return readyOf(o.Status.ReadyReplicas, replicas(o.Spec.Replicas))
	case *appsv1.DaemonSet:
		return readyOf(o.Status.NumberReady, o.Status.DesiredNumberScheduled)
	case *batchv1.Job:
		status, _ := jobStatus(o)
		return status
	case *batchv1.CronJob:
		if o.Spec.Suspend != nil && *o.Spec.Suspend {
			return "Suspended"
		}
		return ""
	case *corev1.PersistentVolumeClaim:
		return storageStatus(o.ObjectMeta, string(o.Status.Phase))
	case *corev1.PersistentVolume:
		return storageStatus(o.ObjectMeta, string(o.Status.Phase))
	case *unstructured.Unstructured:
		return customStatus(resource, o)
	}
	return ""
}

func readyOf(ready, desired int32) string {
	return fmt.Sprintf("%d/%d", ready, desired)
}

// customStatus reads a Gateway API object's status the way its list does.
func customStatus(resource string, u *unstructured.Unstructured) string {
	var status struct {
		Status struct {
			Conditions []metav1.Condition `json:"conditions"`
			Parents    []struct {
				Conditions []metav1.Condition `json:"conditions"`
			} `json:"parents"`
		} `json:"status"`
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &status); err != nil {
		return ""
	}
	switch resource {
	case "gateways":
		s, _ := gatewayStatus(status.Status.Conditions)
		return s
	case "httproutes":
		var parents [][]metav1.Condition
		for _, p := range status.Status.Parents {
			parents = append(parents, p.Conditions)
		}
		s, _ := routeStatus(parents)
		return s
	case "gatewayclasses":
		if c := apimeta.FindStatusCondition(status.Status.Conditions, "Accepted"); c != nil {
			switch c.Status {
			case metav1.ConditionTrue:
				return "Accepted"
			case metav1.ConditionFalse:
				return "NotAccepted"
			}
		}
		return "Pending"
	}
	return ""
}
