package resources

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestStatusOf(t *testing.T) {
	three := int32(3)
	gateway := func(kind string, status map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{"kind": kind, "status": status}}
	}
	condition := func(typ, status string) map[string]any {
		return map[string]any{"type": typ, "status": status, "reason": "x", "message": "", "lastTransitionTime": "2026-11-10T00:00:00Z"}
	}
	cases := []struct {
		resource string
		obj      runtime.Object
		want     string
	}{
		{"pods", &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
		}}}}, "CrashLoopBackOff"},
		{"deployments", &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Replicas: &three}, Status: appsv1.DeploymentStatus{ReadyReplicas: 2}}, "2/3"},
		{"daemonsets", &appsv1.DaemonSet{Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 4, NumberReady: 4}}, "4/4"},
		{"persistentvolumeclaims", &corev1.PersistentVolumeClaim{Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}, "Bound"},
		{"persistentvolumeclaims", &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &metav1.Time{}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}, "Terminating"},
		{"configmaps", &corev1.ConfigMap{}, ""},
		{"gateways", gateway("Gateway", map[string]any{"conditions": []any{condition("Programmed", "True")}}), "Programmed"},
		{"httproutes", gateway("HTTPRoute", map[string]any{"parents": []any{map[string]any{"conditions": []any{condition("Accepted", "False")}}}}), "NotAccepted"},
		{"gatewayclasses", gateway("GatewayClass", map[string]any{}), "Pending"},
	}
	for _, c := range cases {
		if got := StatusOf(c.resource, c.obj); got != c.want {
			t.Errorf("%s: %q, want %q", c.resource, got, c.want)
		}
	}
}
