package resources

import (
	"context"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// ConfigMap is one row of the configmap list. Values aren't included;
// a configmap can hold up to 1 MiB, and the list only needs the key names.
type ConfigMap struct {
	Meta
	Keys []string `json:"keys"`
}

func listConfigMaps(ctx context.Context, client kubernetes.Interface, q Query) ([]ConfigMap, error) {
	list, err := client.CoreV1().ConfigMaps(q.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	rows := make([]ConfigMap, 0, len(list.Items))
	for _, cm := range list.Items {
		keys := []string{}
		for key := range cm.Data {
			keys = append(keys, key)
		}
		for key := range cm.BinaryData {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		rows = append(rows, ConfigMap{Meta: metaOf(cm.ObjectMeta), Keys: keys})
	}
	sortRows(rows)
	return rows, nil
}
