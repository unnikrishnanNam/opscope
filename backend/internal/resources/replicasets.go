package resources

import (
	"context"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// revisionAnnotation is where a Deployment numbers its rollouts, on itself
// (the latest) and on each of its ReplicaSets (the one it made them for).
const revisionAnnotation = "deployment.kubernetes.io/revision"

// ReplicaSet is one row of the ReplicaSet list. There's no ReplicaSet page
// in the sidebar (a Deployment shows its own as revisions), but the list
// keeps them searchable by name, and serves anyone who opens it by URL.
type ReplicaSet struct {
	Meta
	Desired  int32  `json:"desired"`
	Current  int32  `json:"current"`
	Ready    int32  `json:"ready"`
	Owner    string `json:"owner,omitempty"`    // its controller, e.g. "Deployment/web"
	Revision int    `json:"revision,omitempty"` // 0 when no Deployment numbered it
}

func listReplicaSets(ctx context.Context, src Source, q Query) ([]ReplicaSet, error) {
	items, err := ListOf[*appsv1.ReplicaSet](ctx, src, "replicasets", q)
	if err != nil {
		return nil, err
	}

	rows := make([]ReplicaSet, 0, len(items))
	for _, rs := range items {
		row := ReplicaSet{
			Meta:     metaOf(rs.ObjectMeta),
			Desired:  replicas(rs.Spec.Replicas),
			Current:  rs.Status.Replicas,
			Ready:    rs.Status.ReadyReplicas,
			Revision: revisionOf(rs.ObjectMeta),
		}
		if c := metav1.GetControllerOf(rs); c != nil {
			row.Owner = c.Kind + "/" + c.Name
		}
		rows = append(rows, row)
	}
	sortRows(rows)
	return rows, nil
}

// revisionOf reads the rollout number a Deployment gave an object; 0 when
// there's none.
func revisionOf(m metav1.ObjectMeta) int {
	n, _ := strconv.Atoi(m.Annotations[revisionAnnotation])
	return n
}
