package resources

import (
	"context"
	"sort"

	corev1 "k8s.io/api/core/v1"
)

// ConfigMap is one row of the configmap list. Values aren't included;
// a configmap can hold up to 1 MiB, and the list only needs the key names.
type ConfigMap struct {
	Meta
	Keys []string `json:"keys"`
}

func listConfigMaps(ctx context.Context, src Source, q Query) ([]ConfigMap, error) {
	items, err := ListOf[*corev1.ConfigMap](ctx, src, "configmaps", q)
	if err != nil {
		return nil, err
	}

	rows := make([]ConfigMap, 0, len(items))
	for _, cm := range items {
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
