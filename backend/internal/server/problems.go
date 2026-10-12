package server

import (
	"net/http"

	"opscope/internal/clusters"
	"opscope/internal/diagnose"
	"opscope/internal/graph"
	"opscope/internal/snapshot"
)

type problemsResponse struct {
	Problems []*diagnose.Problem    `json:"problems"`
	Nodes    map[string]*graph.Node `json:"nodes"`
	Snapshot snapshotInfo           `json:"snapshot"`
}

// GET /api/clusters/{id}/problems?namespace=
//
// The root causes of what's wrong, in a namespace or the whole cluster,
// each with what it affects. The worst come first.
func getProblems(manager *clusters.Manager, store *snapshot.Store) http.HandlerFunc {
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
		d := diagnose.Of(s)
		problems := d.Problems(r.URL.Query().Get("namespace"))
		if problems == nil {
			problems = []*diagnose.Problem{}
		}
		writeJSON(w, http.StatusOK, problemsResponse{Problems: problems, Nodes: d.Nodes(problems), Snapshot: infoOf(s)})
	}
}
