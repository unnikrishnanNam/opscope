package server

import (
	"errors"
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/metrics"
)

// nodeMetricsResponse is the body of GET /api/clusters/{id}/metrics/nodes.
type nodeMetricsResponse struct {
	Nodes       []metrics.NodeUsage        `json:"nodes"`
	Total       metrics.NodeUsage          `json:"total"`       // the whole cluster
	History     []metrics.Point            `json:"history"`     // cluster totals, oldest first
	NodeHistory map[string][]metrics.Point `json:"nodeHistory"` // per node, oldest first
}

// GET /api/clusters/{id}/metrics/nodes
//
// Current usage per node (read live) plus the last 15 minutes of history
// (from the background collector).
func getNodeMetrics(manager *clusters.Manager, history *metrics.History) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		nodes, err := metrics.ReadNodes(r.Context(), cluster.Client, cluster.Dynamic)
		if err != nil {
			writeMetricsError(w, err, cluster)
			return
		}
		total, perNode := history.Cluster(cluster.ID)
		writeJSON(w, http.StatusOK, nodeMetricsResponse{
			Nodes:       nodes,
			Total:       metrics.Totals(nodes),
			History:     total,
			NodeHistory: perNode,
		})
	}
}

// GET /api/clusters/{id}/metrics/pods?namespace=...
func getPodMetrics(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		pods, err := metrics.ReadPods(r.Context(), cluster.Dynamic, r.URL.Query().Get("namespace"))
		if err != nil {
			writeMetricsError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, pods)
	}
}

// GET /api/clusters/{id}/metrics/volumes?namespace=...
//
// Used and free space for the claims mounted by running pods. Off unless
// OPSCOPE_VOLUME_USAGE=true; then it answers 404 with a code, before
// looking at the cluster, so nothing ever reaches a kubelet.
func getVolumeUsage(enabled bool, volumes *metrics.Volumes, manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !enabled {
			writeJSON(w, http.StatusNotFound, map[string]string{
				"error": "volume usage is off (OPSCOPE_VOLUME_USAGE)",
				"code":  "volume_usage_off",
			})
			return
		}
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		report, err := volumes.Read(r.Context(), cluster.ID, cluster.Client, r.URL.Query().Get("namespace"))
		if err != nil {
			writeVolumeUsageError(w, err, cluster)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// writeVolumeUsageError tells "no permission for the node proxy" apart from
// other errors, so the UI can say which permission is missing.
func writeVolumeUsageError(w http.ResponseWriter, err error, cluster *clusters.Cluster) {
	if errors.Is(err, metrics.ErrVolumeUsageForbidden) {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":  metrics.ErrVolumeUsageForbidden.Error(),
			"code":   "volume_usage_forbidden",
			"detail": err.Error(),
		})
		return
	}
	writeClusterError(w, err, cluster)
}

// writeMetricsError answers "no metrics-server" with a code the UI turns
// into install instructions; anything else is a normal cluster error.
func writeMetricsError(w http.ResponseWriter, err error, cluster *clusters.Cluster) {
	if errors.Is(err, metrics.ErrUnavailable) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "metrics_unavailable"})
		return
	}
	writeClusterError(w, err, cluster)
}
