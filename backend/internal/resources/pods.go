package resources

import (
	"context"

	"opscope/internal/kube"

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
			Status:     kube.PodStatus(&pod),
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

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
