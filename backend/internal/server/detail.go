package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"

	"opscope/internal/clusters"
	"opscope/internal/collector"
	"opscope/internal/graph"
	"opscope/internal/resources"
)

// GET /api/clusters/{id}/{resource}/{namespace}/{name}   (e.g. a pod)
// GET /api/clusters/{id}/{resource}/{name}               (cluster-wide kinds, e.g. a node)
//
// Everything the detail page shows about one object.
//
// The object itself is read from the cluster; what it's connected to (the
// pods using a claim, what uses a Secret, whether a link's object exists)
// comes from the graph, so the page and the Topology page agree. If the
// graph can't be read, the page is shown without those parts.
func getDetail(manager *clusters.Manager, live *collector.Set, graphs graphReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		// For the cluster-wide route there's no {namespace}; PathValue gives "".
		resource := r.PathValue("resource")
		src := live.Source(cluster)
		detail, err := resources.GetDetail(r.Context(), clientsOf(cluster), src, resource, r.PathValue("namespace"), r.PathValue("name"))
		switch {
		case errors.Is(err, resources.ErrUnknownKind):
			writeError(w, http.StatusNotFound, err.Error()+": "+resource)
		case err != nil:
			writeClusterError(w, err, cluster)
		default:
			addRelations(r.Context(), graphs, cluster.ID, src, detail, resource)
			writeJSON(w, http.StatusOK, detail)
		}
	}
}

// addRelations completes a detail page from the cluster's graph, read for
// the page's namespace (which a graph built on request needs).
func addRelations(ctx context.Context, graphs graphReader, cluster string, src resources.Source, d *resources.Detail, resource string) {
	_ = graphs.Read(ctx, cluster, src, d.Namespace, func(g *graph.Graph, _ graph.Info) error {
		resources.AddRelations(ctx, src, d, resource, g.Relations())
		return nil
	})
}

const (
	defaultLogLines = 500
	maxLogLines     = 10000
)

// GET /api/clusters/{id}/pods/{namespace}/{name}/logs?container=&tail=500&previous=true&follow=true
//
// Sends logs as plain text. With follow=true the response stays open and new
// lines are sent as the container writes them, until the browser stops reading.
func streamLogs(manager *clusters.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}

		q := r.URL.Query()
		opts := resources.LogOptions{
			Container: q.Get("container"),
			Tail:      defaultLogLines,
			Previous:  q.Get("previous") == "true",
			Follow:    q.Get("follow") == "true",
		}
		if n, err := strconv.ParseInt(q.Get("tail"), 10, 64); err == nil && n > 0 {
			opts.Tail = min(n, maxLogLines)
		}

		// Without follow, the stream should end on its own; a timeout makes sure.
		ctx := r.Context()
		if !opts.Follow {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
		}

		// The stream client has no built-in timeout (see clusters.newClients).
		stream, err := resources.PodLogs(ctx, cluster.Stream, r.PathValue("namespace"), r.PathValue("name"), opts)
		if err != nil {
			if apierrors.IsBadRequest(err) {
				// e.g. "previous terminated container not found": the API's own
				// message is the clearest explanation.
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeClusterError(w, err, cluster)
			return
		}
		defer stream.Close()

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store") // logs can contain sensitive data
		w.Header().Set("X-Content-Type-Options", "nosniff")

		// Copy in chunks and flush each one, so the browser sees new lines
		// right away instead of when a buffer fills up.
		flusher := http.NewResponseController(w)
		buf := make([]byte, 32*1024)
		for {
			n, readErr := stream.Read(buf)
			if n > 0 {
				if _, err := w.Write(buf[:n]); err != nil {
					return // the browser went away
				}
				flusher.Flush()
			}
			if readErr != nil {
				return // io.EOF when the logs end, or the context was cancelled
			}
		}
	}
}
