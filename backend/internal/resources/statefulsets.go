package resources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
)

// StatefulSet is one row of the statefulset list.
type StatefulSet struct {
	Meta
	Desired int32 `json:"desired"`
	Ready   int32 `json:"ready"`
}

func listStatefulSets(ctx context.Context, src Source, q Query) ([]StatefulSet, error) {
	items, err := ListOf[*appsv1.StatefulSet](ctx, src, "statefulsets", q)
	if err != nil {
		return nil, err
	}

	rows := make([]StatefulSet, 0, len(items))
	for _, s := range items {
		rows = append(rows, StatefulSet{
			Meta:    metaOf(s.ObjectMeta),
			Desired: replicas(s.Spec.Replicas),
			Ready:   s.Status.ReadyReplicas,
		})
	}
	sortRows(rows)
	return rows, nil
}
