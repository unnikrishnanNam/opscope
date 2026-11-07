package resources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
)

// Deployment is one row of the deployment list.
type Deployment struct {
	Meta
	Desired   int32 `json:"desired"`   // replicas asked for in the spec
	Ready     int32 `json:"ready"`     // replicas passing their readiness checks
	UpToDate  int32 `json:"upToDate"`  // replicas running the latest version
	Available int32 `json:"available"` // replicas ready for long enough to count
}

func listDeployments(ctx context.Context, src Source, q Query) ([]Deployment, error) {
	items, err := ListOf[*appsv1.Deployment](ctx, src, "deployments", q)
	if err != nil {
		return nil, err
	}

	rows := make([]Deployment, 0, len(items))
	for _, d := range items {
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
