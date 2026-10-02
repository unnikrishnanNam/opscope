package server

import (
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/resources"
)

// GET /api/clusters/{id}/{resource}?namespace=...&type=...
//
// Lists one kind of resource, e.g. /api/clusters/lab/pods?namespace=default.
// Without ?namespace it lists across all namespaces. ?type is only used by
// events (Warning or Normal).
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

		rows, err := lister(r.Context(), cluster.Client, queryFrom(r))
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, rows)
	}
}

// GET /api/clusters/{id}/overview?namespace=...
//
// Counts and health for the cluster's front page.
func getOverview(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		overview, err := resources.GetOverview(r.Context(), cluster.Client, queryFrom(r))
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, overview)
	}
}

// queryFrom reads the list options from the URL's query string.
func queryFrom(r *http.Request) resources.Query {
	return resources.Query{
		Namespace: r.URL.Query().Get("namespace"),
		Type:      r.URL.Query().Get("type"),
	}
}
