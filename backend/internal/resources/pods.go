package resources

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Pod is one row of the pod list.
type Pod struct {
	Meta
	Status     string `json:"status"`
	Ready      int    `json:"ready"`      // containers that are ready
	Containers int    `json:"containers"` // containers in total (not counting init containers)
	Restarts   int32  `json:"restarts"`
	Node       string `json:"node"`
	IP         string `json:"ip"`
}

func listPods(ctx context.Context, client kubernetes.Interface, q Query) ([]Pod, error) {
	list, err := client.CoreV1().Pods(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Pod, 0, len(list.Items))
	for _, pod := range list.Items {
		row := Pod{
			Meta:       metaOf(pod.ObjectMeta),
			Status:     podStatus(&pod),
			Containers: len(pod.Spec.Containers),
			Node:       pod.Spec.NodeName,
			IP:         pod.Status.PodIP,
		}
		for _, c := range pod.Status.ContainerStatuses {
			if c.Ready {
				row.Ready++
			}
			row.Restarts += c.RestartCount
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// podStatus works out the one-word status that `kubectl get pods` shows.
//
// The pod's phase alone (Pending, Running, ...) hides the interesting cases:
// a "Running" pod may have a container in CrashLoopBackOff. So, like
// kubectl, we look at the containers and use the most telling reason.
// This is a simplified version of kubectl's printPod logic.
func podStatus(pod *corev1.Pod) string {
	// A pod being deleted says so, whatever its containers are doing.
	if pod.DeletionTimestamp != nil {
		if pod.Status.Reason == "NodeLost" {
			return "Unknown"
		}
		return "Terminating"
	}

	status := string(pod.Status.Phase)
	if pod.Status.Reason != "" { // e.g. "Evicted"
		status = pod.Status.Reason
	}

	// Init containers run one after another before the main containers.
	for i, c := range pod.Status.InitContainerStatuses {
		switch {
		case c.State.Terminated != nil && c.State.Terminated.ExitCode == 0:
			continue // this one finished fine, check the next
		case isSidecar(pod, c.Name) && c.Started != nil && *c.Started:
			continue // a sidecar keeps running next to the main containers; that's fine
		case c.State.Terminated != nil:
			return "Init:" + orDefault(c.State.Terminated.Reason, "Error")
		case c.State.Waiting != nil && c.State.Waiting.Reason != "" && c.State.Waiting.Reason != "PodInitializing":
			return "Init:" + c.State.Waiting.Reason
		default:
			return fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
		}
	}

	// Main containers: a waiting or terminated reason beats the phase.
	running := false
	for _, c := range pod.Status.ContainerStatuses {
		switch {
		case c.State.Waiting != nil && c.State.Waiting.Reason != "":
			status = c.State.Waiting.Reason // e.g. CrashLoopBackOff, ImagePullBackOff
		case c.State.Terminated != nil && c.State.Terminated.Reason != "":
			status = c.State.Terminated.Reason // e.g. Completed, OOMKilled, Error
		case c.State.Terminated != nil:
			status = fmt.Sprintf("ExitCode:%d", c.State.Terminated.ExitCode)
		case c.State.Running != nil && c.Ready:
			running = true
		}
	}
	// One container finished but another still runs: the pod is running.
	if status == "Completed" && running {
		status = "Running"
	}
	return status
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// isSidecar reports whether an init container is a "native sidecar"
// (restartPolicy: Always, Kubernetes 1.29+). These start before the main
// containers and keep running alongside them.
func isSidecar(pod *corev1.Pod, name string) bool {
	for _, c := range pod.Spec.InitContainers {
		if c.Name == name {
			return c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways
		}
	}
	return false
}
