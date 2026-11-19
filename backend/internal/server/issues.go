package server

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"opscope/internal/clusters"
	"opscope/internal/resources"
	"opscope/internal/signals"
)

// signalReader reads a cluster's signals: *signals.Engine, or a fake in
// tests.
type signalReader interface {
	Read(ctx context.Context, cluster string, src resources.Source, q resources.Query, fn func(signals.View, signals.Info) error) error
}

// attention is a View as the overview counts it (resources.Attention).
type attention struct {
	view      signals.View
	namespace string
}

func (a attention) Objects(resource string, types ...string) int {
	return a.view.Objects(a.namespace, resource, types...)
}

func (a attention) Types() map[string]int {
	return a.view.TypeCounts(a.namespace)
}

// healthCounts is what GET /health/counts returns: how many objects need
// a look, by kind and by signal type, and how the answer was made.
type healthCounts struct {
	signals.Info
	Kinds map[string]int `json:"kinds"`
	Types map[string]int `json:"types"`
}

// GET /api/clusters/{id}/health/counts?namespace=
//
// How many objects have an open signal, per kind and per signal type: what
// the overview's "needs attention" counts are made of.
func getHealthCounts(manager *clusters.Manager, issues signalReader, sources func(*clusters.Cluster) resources.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		namespace := r.URL.Query().Get("namespace")
		var out healthCounts
		err := issues.Read(r.Context(), cluster.ID, sources(cluster), resources.Query{Namespace: namespace}, func(v signals.View, info signals.Info) error {
			out = healthCounts{Info: info, Kinds: v.Counts(namespace), Types: v.TypeCounts(namespace)}
			return nil
		})
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		if out.Partial == nil {
			out.Partial = []signals.PartialKind{}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// issuesAnswer is what GET /issues returns.
type issuesAnswer struct {
	signals.Info
	Groups []signals.Group `json:"groups"`
	// Open and Resolved count the groups in the namespace on either tab,
	// whatever the other filters, for the tabs' counts.
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
}

// GET /api/clusters/{id}/issues?namespace=&severity=&type=&kind=&state=
//
// The Issues feed: open signals (state=open, the default) or those
// resolved in the last hour (state=resolved), grouped by workload. type
// takes several, comma-separated; kind is the URL name ("deployments").
func getIssues(manager *clusters.Manager, issues signalReader, sources func(*clusters.Cluster) resources.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		q := r.URL.Query()
		namespace, state := q.Get("namespace"), q.Get("state")
		if state == "" {
			state = signals.Open
		}
		if state != signals.Open && state != signals.Resolved {
			writeError(w, http.StatusBadRequest, "state must be open or resolved")
			return
		}
		var types []string
		if t := q.Get("type"); t != "" {
			types = strings.Split(t, ",")
		}
		var out issuesAnswer
		err := issues.Read(r.Context(), cluster.ID, sources(cluster), resources.Query{Namespace: namespace}, func(v signals.View, info signals.Info) error {
			out.Info = info
			out.Open = len(signals.GroupSignals(v.Signals(signals.Filter{Namespace: namespace, State: signals.Open})))
			out.Resolved = len(signals.GroupSignals(v.Signals(signals.Filter{Namespace: namespace, State: signals.Resolved})))
			var picked []signals.Signal
			for _, s := range v.Signals(signals.Filter{Namespace: namespace, State: state, Severity: q.Get("severity"), Resource: q.Get("kind")}) {
				if len(types) == 0 || slices.Contains(types, s.Type) {
					picked = append(picked, s)
				}
			}
			out.Groups = signals.GroupSignals(picked)
			return nil
		})
		if err != nil {
			writeClusterError(w, err, cluster)
			return
		}
		if out.Groups == nil {
			out.Groups = []signals.Group{}
		}
		if out.Partial == nil {
			out.Partial = []signals.PartialKind{}
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// GET /api/clusters/{id}/issues/{signal}
//
// One signal, open or resolved in the last hour, with all its evidence.
func getIssue(manager *clusters.Manager, issues signalReader, sources func(*clusters.Cluster) resources.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		var s signals.Signal
		found := false
		err := issues.Read(r.Context(), cluster.ID, sources(cluster), resources.Query{}, func(v signals.View, _ signals.Info) error {
			s, found = v.Signal(r.PathValue("signal"))
			return nil
		})
		switch {
		case err != nil:
			writeClusterError(w, err, cluster)
		case !found:
			writeError(w, http.StatusNotFound, "no such signal, open or resolved in the last hour")
		default:
			writeJSON(w, http.StatusOK, s)
		}
	}
}

// healthAnswer is one object's health, its signals and how its health
// changed since Opscope started.
type healthAnswer struct {
	signals.Info
	Health      *signals.Health      `json:"health"` // null for a kind no detector reads
	Signals     []signals.Signal     `json:"signals"`
	Transitions []signals.Transition `json:"transitions"`
}

// GET /api/clusters/{id}/health?resource=&namespace=&name=
//
// One object's health, as a detail page shows it.
func getHealth(manager *clusters.Manager, issues signalReader, sources func(*clusters.Cluster) resources.Source) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cluster, ok := findCluster(w, r, manager)
		if !ok {
			return
		}
		q := r.URL.Query()
		resource, namespace, name := q.Get("resource"), q.Get("namespace"), q.Get("name")
		k, known := resources.Lookup(resource)
		if !known || name == "" {
			writeError(w, http.StatusBadRequest, "resource and name are needed, and resource must be a kind Opscope knows")
			return
		}
		if !k.Namespaced {
			namespace = ""
		}
		var out healthAnswer
		found := false
		err := issues.Read(r.Context(), cluster.ID, sources(cluster), resources.Query{Namespace: namespace}, func(v signals.View, info signals.Info) error {
			out.Info = info
			uid, ok := v.Find(resource, namespace, name)
			if !ok {
				return nil
			}
			found = true
			if h, ok := v.Health(uid); ok {
				out.Health = &h
			}
			out.Signals = v.Signals(signals.Filter{UID: uid})
			out.Transitions = v.Transitions(uid)
			return nil
		})
		switch {
		case err != nil:
			writeClusterError(w, err, cluster)
		case !found:
			// Not held: a kind no detector reads, or an object that's gone.
			writeJSON(w, http.StatusOK, healthAnswer{Info: out.Info, Signals: []signals.Signal{}, Transitions: []signals.Transition{}})
		default:
			writeJSON(w, http.StatusOK, out)
		}
	}
}
