package resources

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"sync"

	"opscope/internal/kube"
)

// The command palette searches every object in a cluster by name. Rather
// than make the browser load every table, ListNames lists them all here and
// returns only kind, namespace and name, which is all a search needs.

// ObjectName is one object in the index.
type ObjectName struct {
	Resource  string `json:"resource"` // the API name, e.g. "pods"
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// Skipped is a kind that's left out of the index, and why (see kube.Skipped).
type Skipped = kube.Skipped

// NameIndex is what GET /api/clusters/{id}/names returns.
type NameIndex struct {
	Objects []ObjectName `json:"objects"`
	Skipped []Skipped    `json:"skipped"`
}

// notIndexed are listers left out of the index: events come and go, and
// aren't objects anyone opens by name.
var notIndexed = map[string]bool{"events": true}

// ListNames lists every kind in Listers (so a new kind is searchable as soon
// as it's listed there), all at once, across all namespaces.
//
// One kind failing doesn't fail the whole index: it's named in Skipped and
// the others are still returned. Only when nothing at all could be read is
// it an error, since then the cluster itself is the problem (unreachable,
// credentials rejected).
func ListNames(ctx context.Context, clients Clients) (*NameIndex, error) {
	var kinds []string
	for _, kind := range slices.Sorted(maps.Keys(Listers)) {
		if !notIndexed[kind] {
			kinds = append(kinds, kind)
		}
	}

	// Each kind is its own request to the API server, so they run in
	// parallel; each goroutine writes only its own slot.
	type result struct {
		rows any
		err  error
	}
	results := make([]result, len(kinds))
	var wg sync.WaitGroup
	for i, kind := range kinds {
		wg.Go(func() {
			rows, err := Listers[kind](ctx, clients, Query{})
			results[i] = result{rows, err}
		})
	}
	wg.Wait()

	index := &NameIndex{Objects: []ObjectName{}, Skipped: []Skipped{}}
	var firstErr error
	read := 0
	for i, kind := range kinds {
		r := results[i]
		if r.err != nil {
			skipped := kube.Skip(kind, r.err)
			index.Skipped = append(index.Skipped, skipped)
			if skipped.Code == "failed" && firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		read++
		for _, m := range metas(r.rows) {
			index.Objects = append(index.Objects, ObjectName{Resource: kind, Namespace: m.Namespace, Name: m.Name})
		}
	}

	if read == 0 && firstErr != nil {
		return nil, firstErr
	}
	return index, nil
}

// metas reads the Meta of each row a lister returned. Listers return typed
// slices ([]Pod, []Service, ...) as `any`, so reflection walks the slice;
// each row has the meta method from its embedded Meta.
func metas(rows any) []Meta {
	v := reflect.ValueOf(rows)
	if v.Kind() != reflect.Slice {
		return nil
	}
	out := make([]Meta, 0, v.Len())
	for i := range v.Len() {
		if row, ok := v.Index(i).Interface().(interface{ meta() Meta }); ok {
			out = append(out, row.meta())
		}
	}
	return out
}
