package signals

import (
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
	Default.Register(Detector{
		Name:   "workloads.conditions",
		Kinds:  []string{"deployments", "jobs"},
		Types:  []string{"rollout_stuck", "job_failed"},
		Detect: workloadConditions,
	})
	Default.Register(Detector{
		Name:   "workloads.events",
		Kinds:  []string{"replicasets", "statefulsets", "daemonsets", "jobs"},
		Types:  []string{"create_failed"},
		Timed:  true, // events grow old
		Detect: workloadEvents,
	})
}

// workloadConditions reads a Deployment's progress and a Job's failure
// from their conditions.
func workloadConditions(obj runtime.Object, c Context) []Found {
	switch o := obj.(type) {
	case *appsv1.Deployment:
		if o.DeletionTimestamp != nil || o.Spec.Paused {
			return nil
		}
		for i, cond := range o.Status.Conditions {
			if cond.Type == appsv1.DeploymentProgressing && cond.Status == corev1.ConditionFalse && cond.Reason == "ProgressDeadlineExceeded" {
				return []Found{{
					Type: "rollout_stuck", Since: cond.LastTransitionTime.Time,
					Message: "The rollout stopped: " + orElse(cond.Message, "it passed its progress deadline"),
					Evidence: []Evidence{{Source: FromField, Field: fmt.Sprintf("status.conditions[%d]", i),
						Value: "Progressing=False (ProgressDeadlineExceeded)", ResourceVersion: o.ResourceVersion}},
				}}
			}
		}
	case *batchv1.Job:
		if o.DeletionTimestamp != nil {
			return nil
		}
		for i, cond := range o.Status.Conditions {
			if cond.Type == batchv1.JobFailed && cond.Status == corev1.ConditionTrue {
				return []Found{{
					Type: "job_failed", Since: cond.LastTransitionTime.Time,
					Message: "Failed: " + orElse(cond.Message, cond.Reason),
					Evidence: []Evidence{{Source: FromField, Field: fmt.Sprintf("status.conditions[%d]", i),
						Value: "Failed=True (" + cond.Reason + ")", ResourceVersion: o.ResourceVersion}},
				}}
			}
		}
	}
	return nil
}

// createWindow is how recent a FailedCreate event must be. Controllers
// retry a failing create with a back-off that reaches about 16 minutes
// (the work queue's 1000-second cap), so their events are that far apart.
const createWindow = 20 * time.Minute

// workloadEvents finds controllers that can't create their pods: a
// quota, an admission webhook, a ServiceAccount that doesn't exist. A
// ReplicaSet says so in its ReplicaFailure condition, for as long as it
// lasts; the others only in events, which count while they're recent.
func workloadEvents(obj runtime.Object, c Context) []Found {
	m, ok := obj.(metav1.Object)
	if !ok || m.GetDeletionTimestamp() != nil {
		return nil
	}
	var f Found
	if rs, isRS := obj.(*appsv1.ReplicaSet); isRS {
		for i, cond := range rs.Status.Conditions {
			if cond.Type == appsv1.ReplicaSetReplicaFailure && cond.Status == corev1.ConditionTrue {
				f.Since = cond.LastTransitionTime.Time
				f.Message = "Can't create its pods: " + firstLine(orElse(cond.Message, cond.Reason))
				f.Evidence = append(f.Evidence, Evidence{Source: FromField, Field: fmt.Sprintf("status.conditions[%d]", i),
					Value: "ReplicaFailure=True (" + cond.Reason + ")", ResourceVersion: rs.ResourceVersion})
			}
		}
	}
	for _, e := range c.Events(string(m.GetUID())) {
		if e.Type == corev1.EventTypeWarning && e.Reason == "FailedCreate" && c.Now().Sub(eventTime(e)) <= createWindow {
			addEvent(&f, e)
			if f.Message == "" {
				f.Message = "Can't create its pods: " + firstLine(e.Message)
			}
		}
	}
	if f.Evidence == nil {
		return nil
	}
	f.Type = "create_failed"
	return []Found{f}
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
