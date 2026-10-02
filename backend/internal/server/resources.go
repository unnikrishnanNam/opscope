package server

import (
	"errors"
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

		rows, err := lister(r.Context(), clientsOf(cluster), queryFrom(r))
		if errors.Is(err, resources.ErrGatewayAPINotInstalled) {
			// Not a failure, just a missing feature: the "code" lets the UI
			// show a calm note instead of an error box.
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "not_installed"})
			return
		}
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

		overview, err := resources.GetOverview(r.Context(), clientsOf(cluster), queryFrom(r))
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, overview)
	}
}

// GET /api/clusters/{id}/secrets/{namespace}/{name}/{key}
//
// Returns {"value": "...", "base64": false} for one key of one secret. The
// list endpoint never includes values; this is the only way to read one.
func getSecretValue(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		value, err := resources.GetSecretValue(r.Context(), cluster.Client,
			r.PathValue("namespace"), r.PathValue("name"), r.PathValue("key"))
		switch {
		case errors.Is(err, resources.ErrKeyNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			writeClusterError(w, err, cluster)
		default:
			// Tell the browser (and anything in between) not to keep a copy.
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, http.StatusOK, value)
		}
	}
}

// clientsOf bundles a cluster's clients for the resources package.
func clientsOf(cluster *clusters.Cluster) resources.Clients {
	return resources.Clients{Kube: cluster.Client, Dynamic: cluster.Dynamic}
}

// queryFrom reads the list options from the URL's query string.
func queryFrom(r *http.Request) resources.Query {
	return resources.Query{
		Namespace: r.URL.Query().Get("namespace"),
		Type:      r.URL.Query().Get("type"),
	}
}
