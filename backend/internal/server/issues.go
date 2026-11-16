package server

import (
	"context"
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/resources"
	"opscope/internal/signals"
)

// signalReader reads a cluster's signals: *signals.Engine, or a fake in
// tests.
type signalReader interface {
	Read(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(signals.View, signals.Info) error) error
}

// attention is a View as the overview counts it (resources.Attention).
type attention struct {
	view      signals.View
	namespace string
}

func (a attention) Objects(resource string, types ...string) int {
	return a.view.Objects(a.namespace, resource, types...)
}

func (a attention) Types() map[string]int {
	return a.view.TypeCounts(a.namespace)
}

// healthCounts is what GET /health/counts returns: how many objects need
// a look, by kind and by signal type, and how the answer was made.
type healthCounts struct {
	signals.Info
	Kinds map[string]int `json:"kinds"`
	Types map[string]int `json:"types"`
}

// GET /api/clusters/{id}/health/counts?namespace=
//
// How many objects have an open signal, per kind and per signal type: what
// the overview's "needs attention" counts are made of.
func getHealthCounts(manager *clusters.Manager, issues signalReader, sources func(*clusters.Cluster) resources.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		namespace := r.URL.Query().Get("namespace")
		var out healthCounts
		err := issues.Read(r.Context(), cluster.ID, sources(cluster), namespace, func(v signals.View, info signals.Info) error {
			out = healthCounts{Info: info, Kinds: v.Counts(namespace), Types: v.TypeCounts(namespace)}
			return nil
		})
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		if out.Partial == nil {
			out.Partial = []signals.PartialKind{}
		}
		writeJSON(w, http.StatusOK, out)
	}
}
