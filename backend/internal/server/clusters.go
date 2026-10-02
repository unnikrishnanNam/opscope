package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"opscope/internal/clusters"
)

// maxKubeconfigBytes limits how much a client can upload in one request.
const maxKubeconfigBytes = 1 << 20 // 1 MiB

// GET /api/clusters
func listClusters(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, manager.List())
	}
}

// POST /api/clusters/inspect  {"kubeconfig": "..."}
//
// Reads a kubeconfig and lists its contexts, so the UI can offer a choice
// before anything is saved.
func inspectKubeconfig() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kubeconfig string `json:"kubeconfig"`
		}
		if !readJSON(w, r, &body) {
			return
		}

		contexts, current, err := clusters.ListContexts([]byte(body.Kubeconfig))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contexts": contexts, "current": current})
	}
}

// POST /api/clusters  {"name": "...", "kubeconfig": "...", "context": "..."}
func addCluster(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name       string `json:"name"`
			Kubeconfig string `json:"kubeconfig"`
			Context    string `json:"context"`
		}
		if !readJSON(w, r, &body) {
			return
		}

		cluster, err := manager.Add(body.Name, []byte(body.Kubeconfig), body.Context)

		var connErr *clusters.ConnectionError
		switch {
		case errors.As(err, &connErr):
			// The kubeconfig was fine, but the cluster didn't answer properly.
			writeErrorDetail(w, http.StatusBadGateway, connErr.Error(), connErr.Err.Error())
		case err != nil:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeJSON(w, http.StatusCreated, cluster)
		}
	}
}

// DELETE /api/clusters/{id}
func removeCluster(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := manager.Remove(r.PathValue("id"))
		switch {
		case errors.Is(err, clusters.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case err != nil:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// clusterStatus is the response for GET /api/clusters/{id}.
type clusterStatus struct {
	*clusters.Cluster
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// GET /api/clusters/{id}
//
// Always answers 200 when the cluster exists; whether the cluster itself
// answered is in the "reachable" field.
func getCluster(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		status := clusterStatus{Cluster: cluster}
		version, err := cluster.Version()
		if err != nil {
			status.Error = clusters.Explain(err, cluster.Server)
			status.Detail = err.Error()
		} else {
			status.Reachable = true
			status.Version = version
		}
		writeJSON(w, http.StatusOK, status)
	}
}

// findCluster looks up the {id} from the URL. If it doesn't exist it writes
// a 404 and returns false, so handlers can simply `return`.
func findCluster(w http.ResponseWriter, r *http.Request, manager *clusters.Manager) (*clusters.Cluster, bool) {
	id := r.PathValue("id")
	cluster, ok := manager.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no cluster with id "+id)
	}
	return cluster, ok
}

// writeClusterError reports a failed call to a cluster with a readable
// explanation and the original error as detail.
func writeClusterError(w http.ResponseWriter, err error, cluster *clusters.Cluster) {
	writeErrorDetail(w, http.StatusBadGateway, clusters.Explain(err, cluster.Server), err.Error())
}

// readJSON decodes a JSON request body into dst. On failure it writes a 400
// and returns false.
func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxKubeconfigBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}
