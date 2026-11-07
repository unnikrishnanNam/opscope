package resources

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
)

// DaemonSet is one row of the daemonset list.
type DaemonSet struct {
	Meta
	Desired      int32             `json:"desired"` // nodes that should run the pod
	Current      int32             `json:"current"` // nodes running it
	Ready        int32             `json:"ready"`   // nodes where it's ready
	NodeSelector map[string]string `json:"nodeSelector"`
}

func listDaemonSets(ctx context.Context, src Source, q Query) ([]DaemonSet, error) {
	items, err := ListOf[*appsv1.DaemonSet](ctx, src, "daemonsets", q)
	if err != nil {
		return nil, err
	}

	rows := make([]DaemonSet, 0, len(items))
	for _, d := range items {
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
