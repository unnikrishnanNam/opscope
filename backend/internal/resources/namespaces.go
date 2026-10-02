package resources

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Namespace is one row of the namespace list.
type Namespace struct {
	Meta
	Status string `json:"status"`
}

// listNamespaces ignores the namespace argument: namespaces aren't inside one.
func listNamespaces(ctx context.Context, client kubernetes.Interface, _ string) (any, error) {
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]Namespace, 0, len(list.Items))
	for _, ns := range list.Items {
		rows = append(rows, Namespace{Meta: metaOf(ns.ObjectMeta), Status: string(ns.Status.Phase)})
	}
	sortRows(rows)
	return rows, nil
}
