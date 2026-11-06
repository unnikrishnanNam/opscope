package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
)

// Namespace is one row of the namespace list.
type Namespace struct {
	Meta
	Status string `json:"status"`
}

// listNamespaces ignores q.Namespace: namespaces aren't inside one.
func listNamespaces(ctx context.Context, src Source, q Query) ([]Namespace, error) {
	items, err := ListOf[*corev1.Namespace](ctx, src, "namespaces", q)
	if err != nil {
		return nil, err
	}

	rows := make([]Namespace, 0, len(items))
	for _, ns := range items {
		rows = append(rows, Namespace{Meta: metaOf(ns.ObjectMeta), Status: string(ns.Status.Phase)})
	}
	sortRows(rows)
	return rows, nil
}
