package server

import (
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/collector"
)

// GET /api/clusters/{id}/live
//
// How far the cluster's collector is: per kind, whether its cache has
// synced, how many objects it holds and when it last saw a change. With
// collectors off (OPSCOPE_LIVE=false) it says "off".
func getLiveStatus(manager *clusters.Manager, live *collector.Set) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		status, ok := live.Status(cluster.ID)
		if !ok {
			// The manager knows the cluster but its collector isn't running,
			// which only happens for a moment while a cluster is added.
			writeError(w, http.StatusServiceUnavailable, "the collector for "+cluster.ID+" isn't running yet")
			return
		}
		writeJSON(w, http.StatusOK, status)
	}
}
