package kube

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodStatus(t *testing.T) {
	waiting := func(reason string) corev1.ContainerState {
		return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
	}
	terminated := func(reason string, code int32) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code}}
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	always := corev1.ContainerRestartPolicyAlways
	yes := true
	now := metav1.Now()

	// Each case is a pod and the status we expect for it.
	tests := []struct {
		name string
		pod  corev1.Pod
		want string
	}{
		{
			name: "running and ready",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: running, Ready: true}},
			}},
			want: "Running",
		},
		{
			name: "crash loop wins over the Running phase",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: waiting("CrashLoopBackOff")}},
			}},
			want: "CrashLoopBackOff",
		},
		{
			name: "finished job pod",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodSucceeded,
				ContainerStatuses: []corev1.ContainerStatus{{State: terminated("Completed", 0)}},
			}},
			want: "Completed",
		},
		{
			name: "evicted",
			pod:  corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}},
			want: "Evicted",
		},
		{
			name: "waiting on the second of two init containers",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "a"}, {Name: "b"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "a", State: terminated("Completed", 0)},
						{Name: "b", State: running},
					},
				},
			},
			want: "Init:1/2",
		},
		{
			name: "init container failed",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "a"}}},
				Status: corev1.PodStatus{
					Phase:                 corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{{Name: "a", State: terminated("", 1)}},
				},
			},
			want: "Init:Error",
		},
		{
			name: "running native sidecar doesn't count as initialising",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "proxy", RestartPolicy: &always}}},
				Status: corev1.PodStatus{
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: []corev1.ContainerStatus{{Name: "proxy", State: running, Started: &yes}},
					ContainerStatuses:     []corev1.ContainerStatus{{State: running, Ready: true}},
				},
			},
			want: "Running",
		},
		{
			name: "being deleted",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			},
			want: "Terminating",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodStatus(&tt.pod); got != tt.want {
				t.Errorf("PodStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
