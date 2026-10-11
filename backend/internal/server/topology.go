package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/graph"
	"opscope/internal/kube"
	"opscope/internal/snapshot"
)

const (
	defaultDepth = 2
	maxDepth     = 5
)

// snapshotInfo says what a response was built from: when the cluster was
// read, and which kinds couldn't be.
type snapshotInfo struct {
	Taken   time.Time      `json:"taken"`
	Skipped []kube.Skipped `json:"skipped"`
}

type topologyResponse struct {
	*graph.View
	Snapshot snapshotInfo `json:"snapshot"`
}

// GET /api/clusters/{id}/topology?namespace=&app=&focus=&depth=&expand=&problems=
//
// One view of the cluster's graph:
//   - focus=pods/shop/web-1 (depth=1–5, default 2): one object and its neighbourhood
//   - namespace=shop: a namespace, and what it needs elsewhere
//   - namespace=shop&app=storefront: one app of a namespace
//
// expand lists workloads whose pods are shown one by one (comma-separated
// IDs, or "all"), and problems=true keeps only what isn't OK.
func getTopology(manager *clusters.Manager, store *snapshot.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		q := r.URL.Query()
		namespace, app, focus := q.Get("namespace"), q.Get("app"), q.Get("focus")
		options := graph.Options{Problems: q.Get("problems") == "true", Expand: map[string]bool{}}
		for _, id := range strings.Split(q.Get("expand"), ",") {
			switch id = strings.TrimSpace(id); id {
			case "":
			case "all":
				options.All = true
			default:
				options.Expand[id] = true
			}
		}
		depth := defaultDepth
		if d := q.Get("depth"); d != "" {
			n, err := strconv.Atoi(d)
			if err != nil || n < 1 || n > maxDepth {
				writeError(w, http.StatusBadRequest, "depth must be a number from 1 to "+strconv.Itoa(maxDepth))
				return
			}
			depth = n
		}
		if app != "" && namespace == "" {
			writeError(w, http.StatusBadRequest, "an app is named within a namespace: add ?namespace=")
			return
		}
		if focus == "" && namespace == "" {
			writeError(w, http.StatusBadRequest, "give a namespace, or an object to focus on")
			return
		}

		s, err := store.Get(r.Context(), cluster)
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		g := graph.Of(s)

		var view *graph.View
		switch {
		case focus != "":
			var found bool
			if view, found = g.Neighbourhood(focus, depth, options); !found {
				writeError(w, http.StatusNotFound, "no object "+focus+" in this cluster")
				return
			}
		case app != "":
			view = g.App(namespace, app, options)
		default:
			view = g.Namespace(namespace, options)
		}
		writeJSON(w, http.StatusOK, topologyResponse{View: view, Snapshot: infoOf(s)})
	}
}

func infoOf(s *snapshot.Snapshot) snapshotInfo {
	info := snapshotInfo{Taken: s.Taken, Skipped: s.Skipped}
	if info.Skipped == nil {
		info.Skipped = []kube.Skipped{}
	}
	return info
}
