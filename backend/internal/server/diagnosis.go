package server

import (
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/diagnose"
	"opscope/internal/snapshot"
)

type diagnosisResponse struct {
	*diagnose.Trace
	Snapshot snapshotInfo `json:"snapshot"`
}

// GET /api/clusters/{id}/diagnosis/{resource}/{namespace}/{name}
// GET /api/clusters/{id}/diagnosis/{resource}/{name}   (cluster-wide kinds, e.g. a node)
//
// Why an object is failing: its findings, each followed down to its root
// causes, and what the object's own problems affect.
func getDiagnosis(manager *clusters.Manager, store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		s, err := store.Get(r.Context(), cluster)
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}

		id := snapshot.Key{Resource: r.PathValue("resource"), Namespace: r.PathValue("namespace"), Name: r.PathValue("name")}.String()
		trace, found := diagnose.Of(s).Trace(id)
		if !found {
			writeError(w, http.StatusNotFound, "no object "+id+" in this cluster")
			return
		}
		writeJSON(w, http.StatusOK, diagnosisResponse{Trace: trace, Snapshot: infoOf(s)})
	}
}
