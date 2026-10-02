package resources

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DaemonSet is one row of the daemonset list.
type DaemonSet struct {
	Meta
	Desired      int32             `json:"desired"` // nodes that should run the pod
	Current      int32             `json:"current"` // nodes running it
	Ready        int32             `json:"ready"`   // nodes where it's ready
	NodeSelector map[string]string `json:"nodeSelector"`
}

func listDaemonSets(ctx context.Context, client kubernetes.Interface, q Query) ([]DaemonSet, error) {
	list, err := client.AppsV1().DaemonSets(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]DaemonSet, 0, len(list.Items))
	for _, d := range list.Items {
		rows = append(rows, DaemonSet{
			Meta:         metaOf(d.ObjectMeta),
			Desired:      d.Status.DesiredNumberScheduled,
			Current:      d.Status.CurrentNumberScheduled,
			Ready:        d.Status.NumberReady,
			NodeSelector: d.Spec.Template.Spec.NodeSelector,
		})
	}
	sortRows(rows)
	return rows, nil
}
