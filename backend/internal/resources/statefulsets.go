package resources

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// StatefulSet is one row of the statefulset list.
type StatefulSet struct {
	Meta
	Desired int32 `json:"desired"`
	Ready   int32 `json:"ready"`
}

func listStatefulSets(ctx context.Context, client kubernetes.Interface, namespace string) (any, error) {
	list, err := client.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]StatefulSet, 0, len(list.Items))
	for _, s := range list.Items {
		rows = append(rows, StatefulSet{
			Meta:    metaOf(s.ObjectMeta),
			Desired: replicas(s.Spec.Replicas),
			Ready:   s.Status.ReadyReplicas,
		})
	}
	sortRows(rows)
	return rows, nil
}
