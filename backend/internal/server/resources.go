package server

import (
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/resources"
)

// GET /api/clusters/{id}/{resource}?namespace=...
//
// Lists one kind of resource, e.g. /api/clusters/lab/pods?namespace=default.
// Without ?namespace it lists across all namespaces.
func listResource(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("resource")
		lister, ok := resources.Listers[name]
		if !ok {
			writeError(w, http.StatusNotFound, "unknown resource type: "+name)
			return
		}

		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		rows, err := lister(r.Context(), cluster.Client, r.URL.Query().Get("namespace"))
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}
