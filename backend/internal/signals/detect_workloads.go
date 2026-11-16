package signals

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Detectors on workloads: Deployments, StatefulSets, DaemonSets, and
// ReplicaSets no Deployment owns.

func init() {
	Default.Register(Detector{
		Name:   "workloads.replicas",
		Kinds:  []string{"deployments", "statefulsets", "daemonsets", "replicasets"},
		Types:  []string{"replicas_unavailable"},
		Detect: workloadReplicas,
	})
}

// workloadReplicas finds workloads with fewer replicas ready than wanted.
// It waits 2 minutes (the type's Wait), so a rollout or a scale-up doesn't
// raise it; when the object says since when (a Deployment's Available
// condition), the wait counts from then. None ready is critical.
func workloadReplicas(obj runtime.Object, c Context) []Found {
	var ready, desired int32
	var since time.Time
	var rv, readyField, desiredField string
	switch o := obj.(type) {
	case *appsv1.Deployment:
		if o.DeletionTimestamp != nil {
			return nil
		}
		ready, desired, rv = o.Status.ReadyReplicas, replicasOf(o.Spec.Replicas), o.ResourceVersion
		readyField, desiredField = "status.readyReplicas", "spec.replicas"
		for _, cond := range o.Status.Conditions {
			if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionFalse {
				since = cond.LastTransitionTime.Time
			}
		}
	case *appsv1.StatefulSet:
		if o.DeletionTimestamp != nil {
			return nil
		}
		ready, desired, rv = o.Status.ReadyReplicas, replicasOf(o.Spec.Replicas), o.ResourceVersion
		readyField, desiredField = "status.readyReplicas", "spec.replicas"
	case *appsv1.DaemonSet:
		if o.DeletionTimestamp != nil {
			return nil
		}
		ready, desired, rv = o.Status.NumberReady, o.Status.DesiredNumberScheduled, o.ResourceVersion
		readyField, desiredField = "status.numberReady", "status.desiredNumberScheduled"
	case *appsv1.ReplicaSet:
		// A Deployment's ReplicaSets come and go in every rollout; the
		// Deployment says whether it's short. Only a ReplicaSet on its own
		// is judged here.
		if o.DeletionTimestamp != nil || metav1.GetControllerOfNoCopy(o) != nil {
			return nil
		}
		ready, desired, rv = o.Status.ReadyReplicas, replicasOf(o.Spec.Replicas), o.ResourceVersion
		readyField, desiredField = "status.readyReplicas", "spec.replicas"
	default:
		return nil
	}
	if ready >= desired {
		return nil
	}
	f := Found{
		Type:    "replicas_unavailable",
		Message: fmt.Sprintf("%d of %d %s ready", ready, desired, plural(int64(desired), "replica", "replicas")),
		Since:   since,
		Evidence: []Evidence{
			{Source: FromField, Field: readyField, Value: fmt.Sprint(ready), ResourceVersion: rv},
			{Source: FromField, Field: desiredField, Value: fmt.Sprint(desired), ResourceVersion: rv},
		},
	}
	if ready == 0 {
		f.Severity = Critical
	}
	return []Found{f}
}

// replicasOf reads an optional replica count: Kubernetes treats unset as 1.
func replicasOf(n *int32) int32 {
	if n == nil {
		return 1
	}
	return *n
}
