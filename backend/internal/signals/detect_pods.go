package signals

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Detectors on pods. Each reads the pod's own status (and, from I3, its
// events); none looks at other objects.

func init() {
	Default.Register(Detector{
		Name:   "pods.containers",
		Kinds:  []string{"pods"},
		Types:  []string{"crash_loop"},
		Detect: podContainers,
	})
}

// podContainers finds containers waiting in CrashLoopBackOff: one signal
// for the pod, counting every such container's restarts.
func podContainers(obj runtime.Object, c Context) []Found {
	pod, ok := obj.(*corev1.Pod)
	if !ok || pod.DeletionTimestamp != nil {
		return nil
	}
	var f Found
	var names []string
	for i, s := range pod.Status.ContainerStatuses {
		if s.State.Waiting == nil || s.State.Waiting.Reason != "CrashLoopBackOff" {
			continue
		}
		names = append(names, s.Name)
		f.Count += int64(s.RestartCount)
		f.Evidence = append(f.Evidence, Evidence{
			Source: FromField, Field: fmt.Sprintf("status.containerStatuses[%d].state.waiting.reason", i),
			Value: "CrashLoopBackOff", ResourceVersion: pod.ResourceVersion,
		})
		if t := s.LastTerminationState.Terminated; t != nil {
			f.Evidence = append(f.Evidence, Evidence{
				Source: FromField, Field: fmt.Sprintf("status.containerStatuses[%d].lastState.terminated", i),
				Value: exitText(t), ResourceVersion: pod.ResourceVersion,
			})
		}
	}
	if names == nil {
		return nil
	}
	f.Type = "crash_loop"
	f.Message = crashMessage(pod, names, f.Count)
	return []Found{f}
}

// crashMessage: "Container app restarted 14 times; last exit Error (exit 1)".
func crashMessage(pod *corev1.Pod, names []string, restarts int64) string {
	who := "Container " + names[0]
	if len(names) > 1 {
		who = "Containers " + strings.Join(names, ", ")
	}
	msg := fmt.Sprintf("%s restarted %d %s", who, restarts, plural(restarts, "time", "times"))
	for _, s := range pod.Status.ContainerStatuses {
		if s.Name == names[0] && s.LastTerminationState.Terminated != nil {
			msg += "; last exit " + exitText(s.LastTerminationState.Terminated)
		}
	}
	return msg
}

// exitText: "Error (exit 1)", "OOMKilled (exit 137)", "exit 2".
func exitText(t *corev1.ContainerStateTerminated) string {
	if t.Reason == "" {
		return fmt.Sprintf("exit %d", t.ExitCode)
	}
	return fmt.Sprintf("%s (exit %d)", t.Reason, t.ExitCode)
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
