package resources

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// LogOptions are the choices in the log viewer.
type LogOptions struct {
	Container string // which container; may be empty when the pod has only one
	Tail      int64  // how many of the latest lines to start with
	Previous  bool   // logs of the container's previous run (useful after a crash)
	Follow    bool   // keep the stream open and send new lines as they're written
}

// PodLogs opens a pod's logs as a stream. The caller must Close it. With
// Follow, the stream stays open until ctx is cancelled.
func PodLogs(ctx context.Context, client kubernetes.Interface, namespace, name string, opts LogOptions) (io.ReadCloser, error) {
	tail := opts.Tail
	return client.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{
		Container: opts.Container,
		TailLines: &tail,
		Previous:  opts.Previous,
		Follow:    opts.Follow,
	}).Stream(ctx)
}
