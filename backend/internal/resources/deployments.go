package resources

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Deployment is one row of the deployment list.
type Deployment struct {
	Meta
	Desired   int32 `json:"desired"`   // replicas asked for in the spec
	Ready     int32 `json:"ready"`     // replicas passing their readiness checks
	UpToDate  int32 `json:"upToDate"`  // replicas running the latest version
	Available int32 `json:"available"` // replicas ready for long enough to count
}

func listDeployments(ctx context.Context, client kubernetes.Interface, q Query) ([]Deployment, error) {
	list, err := client.AppsV1().Deployments(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Deployment, 0, len(list.Items))
	for _, d := range list.Items {
		rows = append(rows, Deployment{
			Meta:      metaOf(d.ObjectMeta),
			Desired:   replicas(d.Spec.Replicas),
			Ready:     d.Status.ReadyReplicas,
			UpToDate:  d.Status.UpdatedReplicas,
			Available: d.Status.AvailableReplicas,
		})
	}
	sortRows(rows)
	return rows, nil
}
