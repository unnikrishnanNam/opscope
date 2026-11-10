package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"opscope/internal/clusters"
	"opscope/internal/graph"
	"opscope/internal/resources"
)

// graphReader runs a query on a cluster's graph: *graph.Engine, or a fake
// in tests.
type graphReader interface {
	Read(ctx context.Context, cluster string, src resources.Source, namespace string, fn func(*graph.Graph, graph.Info) error) error
}

// graphAnswer is what every graph endpoint returns: how the graph was
// built, the kinds it couldn't read, and the nodes and edges asked for.
type graphAnswer struct {
	graph.Info
	graph.Subgraph
}

// graphQuery answers one kind of graph request from the graph it's given.
type graphQuery func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error)

// serveGraph runs a graph query for a request. ?namespace= is passed on: a
// graph built on request (one that can't be kept live) is built for that
// namespace, so a user who can read only some namespaces gets an answer.
func serveGraph(manager *clusters.Manager, graphs graphReader, sources func(*clusters.Cluster) resources.Source, query graphQuery) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		q, err := graphQueryFrom(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var answer graphAnswer
		var queryErr error
		err = graphs.Read(r.Context(), cluster.ID, sources(cluster), r.URL.Query().Get("namespace"), func(g *graph.Graph, info graph.Info) error {
			answer.Info = info
			answer.Subgraph, queryErr = query(r, g, q)
			return nil
		})
		switch {
		case err != nil:
			writeClusterError(w, err, cluster)
		case errors.Is(queryErr, graph.ErrNotFound), errors.Is(queryErr, graph.ErrNoPath):
			writeError(w, http.StatusNotFound, queryErr.Error())
		case queryErr != nil:
			writeError(w, http.StatusBadRequest, queryErr.Error())
		default:
			if answer.Partial == nil {
				answer.Partial = []graph.PartialKind{}
			}
			writeJSON(w, http.StatusOK, answer)
		}
	}
}

// graphQueryFrom reads ?depth= and ?kinds= (edge kinds, comma-separated).
func graphQueryFrom(r *http.Request) (graph.Query, error) {
	var q graph.Query
	if d := r.URL.Query().Get("depth"); d != "" {
		depth, err := strconv.Atoi(d)
		if err != nil || depth < 1 {
			return q, fmt.Errorf("depth %q: a number of steps, 1 or more", d)
		}
		q.Depth = depth
	}
	if k := r.URL.Query().Get("kinds"); k != "" {
		known := []string{graph.Owns, graph.References, graph.Binds, graph.RunsOn, graph.Selects, graph.Endpoints, graph.Routes, graph.Attaches}
		for _, kind := range strings.Split(k, ",") {
			if !slices.Contains(known, kind) {
				return q, fmt.Errorf("edge kind %q: one of %s", kind, strings.Join(known, ", "))
			}
			q.Kinds = append(q.Kinds, kind)
		}
	}
	return q, nil
}

func idParam(r *http.Request, name string) (string, error) {
	id := r.URL.Query().Get(name)
	if id == "" {
		return "", fmt.Errorf("?%s= is needed: a node's ID", name)
	}
	return id, nil
}

// registerGraph adds the graph endpoints. Node IDs go in the query string,
// not the path: a placeholder's ID has slashes ("missing:secrets/shop/x").
func registerGraph(mux *http.ServeMux, manager *clusters.Manager, graphs graphReader, sources func(*clusters.Cluster) resources.Source) {
	handle := func(path string, query graphQuery) {
		mux.HandleFunc("GET /api/clusters/{id}/graph/"+path, serveGraph(manager, graphs, sources, query))
	}

	// One object, named the way pages name it, and what's one edge away.
	handle("find", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		v := r.URL.Query()
		if v.Get("resource") == "" || v.Get("name") == "" {
			return graph.Subgraph{}, errors.New("?resource= and ?name= are needed, and ?namespace= for a namespaced kind")
		}
		n, err := g.Find(v.Get("resource"), v.Get("namespace"), v.Get("name"), q)
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Neighbors(n.ID, q)
	})
	// One node by ID, and what's one edge away.
	handle("node", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		id, err := idParam(r, "id")
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Neighbors(id, q)
	})
	handle("upstream", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		id, err := idParam(r, "id")
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Upstream(id, q)
	})
	handle("downstream", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		id, err := idParam(r, "id")
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Downstream(id, q)
	})
	handle("path", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		from, err := idParam(r, "from")
		if err != nil {
			return graph.Subgraph{}, err
		}
		to, err := idParam(r, "to")
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Path(from, to, q)
	})
	// With ?id=, one object's neighbourhood; without, ?namespace= as a whole.
	handle("subgraph", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		v := r.URL.Query()
		if v.Get("id") == "" {
			if v.Get("namespace") == "" {
				return graph.Subgraph{}, errors.New("?id= (a node) or ?namespace= is needed")
			}
			return g.Namespace(v.Get("namespace"), q)
		}
		direction := v.Get("direction")
		if direction == "" {
			direction = graph.Both
		}
		return g.Subgraph(v.Get("id"), direction, q)
	})
	handle("search", func(r *http.Request, g *graph.Graph, q graph.Query) (graph.Subgraph, error) {
		s, err := graph.ParseSearch(r.URL.Query().Get("q"))
		if err != nil {
			return graph.Subgraph{}, err
		}
		return g.Search(s, r.URL.Query().Get("namespace"), q)
	})
}
