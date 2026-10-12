package server

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"opscope/internal/clusters"
	"opscope/internal/diagnose"
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
	Findings []*diagnose.Finding `json:"findings"` // about the objects shown, the pods in groups included
	Snapshot snapshotInfo        `json:"snapshot"`
}

// GET /api/clusters/{id}/topology?namespace=&app=&focus=&depth=&expand=&problems=
//
// One view of the cluster's graph:
//   - nothing: the whole cluster, as namespaces holding apps
//   - focus=pods/shop/web-1 (depth=1–5, default 2): one object and its neighbourhood
//   - namespace=shop: a namespace, and what it needs elsewhere
//   - namespace=shop&app=storefront: one app of a namespace
//
// expand lists workloads whose pods are shown one by one, or in the cluster
// view namespaces shown although nothing is wrong in them (comma-separated
// IDs, or "all"), and problems=true keeps only what isn't OK. The findings
// about what's shown come with it (in the cluster view, its root causes),
// and what they blame is added to the view.
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

		s, err := store.Get(r.Context(), cluster)
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		g := graph.Of(s)

		var view *graph.View
		wholeCluster := focus == "" && namespace == ""
		switch {
		case wholeCluster:
			options.Troubled = diagnose.Of(s).Troubled()
			view = g.Cluster(options)
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
		var findings []*diagnose.Finding
		if wholeCluster {
			findings = diagnose.Of(s).ForCluster(view)
		} else {
			findings = diagnose.Of(s).ForView(view)
		}
		writeJSON(w, http.StatusOK, topologyResponse{View: view, Findings: findings, Snapshot: infoOf(s)})
	}
}

func infoOf(s *snapshot.Snapshot) snapshotInfo {
	info := snapshotInfo{Taken: s.Taken, Skipped: s.Skipped}
	if info.Skipped == nil {
		info.Skipped = []kube.Skipped{}
	}
	return info
}
