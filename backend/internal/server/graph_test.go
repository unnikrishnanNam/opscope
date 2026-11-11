package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"opscope/internal/bus"
	"opscope/internal/collector"
	"opscope/internal/graph"
	"opscope/internal/metrics"
	"opscope/internal/resources"
)

// fakeGraphs answers every Read with one graph and info, and records the
// namespaces asked for.
type fakeGraphs struct {
	g          *graph.Graph
	info       graph.Info
	err        error
	namespaces []string
}

func (f *fakeGraphs) Read(_ context.Context, _ string, _ resources.Source, namespace string, fn func(*graph.Graph, graph.Info) error) error {
	f.namespaces = append(f.namespaces, namespace)
	if f.err != nil {
		return f.err
	}
	return fn(f.g, f.info)
}

// shopGraph: a pod that uses a Secret and a Secret that doesn't exist, on a
// node, behind a Service. readable leaves Secrets out when false.
func shopGraph(secretsReadable bool) *graph.Graph {
	at := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	meta := func(ns, name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Namespace: ns, Name: name, UID: types.UID("uid-" + name), ResourceVersion: "1"}
	}
	env := func(name, secret string) corev1.EnvVar {
		return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secret}, Key: "k",
		}}}
	}
	pod := &corev1.Pod{ObjectMeta: meta("shop", "web-1"), Spec: corev1.PodSpec{
		NodeName:   "n1",
		Containers: []corev1.Container{{Name: "web", Env: []corev1.EnvVar{env("DB", "db-creds"), env("STRIPE", "stripe-key")}}},
	}}
	pod.Labels = map[string]string{"app": "web"}
	objects := []struct {
		resource string
		obj      runtime.Object
	}{
		{"nodes", &corev1.Node{ObjectMeta: meta("", "n1")}},
		{"secrets", &corev1.Secret{ObjectMeta: meta("shop", "db-creds")}},
		{"pods", pod},
		{"services", &corev1.Service{ObjectMeta: meta("shop", "web"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}}},
	}
	readable := slices.Clone(graph.NodeKinds)
	if !secretsReadable {
		readable = slices.DeleteFunc(readable, func(k string) bool { return k == "secrets" })
	}
	var in []graph.Object
	for _, o := range objects {
		in = append(in, graph.Object{Resource: o.resource, Object: o.obj, Seen: at})
	}
	return graph.Build(readable, in)
}

func graphServer(t *testing.T, graphs graphReader) http.Handler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	live := collector.NewSet(t.Context(), false, bus.New(), logger)
	return New(Config{}, managerWithCluster(t), live, graphs, metrics.NewHistory(), logger)
}

type answer struct {
	Built   string              `json:"built"`
	Partial []graph.PartialKind `json:"partial"`
	Focus   string              `json:"focus"`
	Nodes   []graph.Node        `json:"nodes"`
	Edges   []graph.Edge        `json:"edges"`
	Matches []string            `json:"matches"`
	Error   string              `json:"error"`
}

func ask(t *testing.T, h http.Handler, path string, want int) answer {
	t.Helper()
	rec := get(h, "/api/clusters/lab/graph/"+path)
	if rec.Code != want {
		t.Fatalf("%s: %d %s, want %d", path, rec.Code, rec.Body.String(), want)
	}
	var a answer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return a
}

func nodeNames(a answer) []string {
	var out []string
	for _, n := range a.Nodes {
		out = append(out, n.Kind+" "+n.Name)
	}
	slices.Sort(out)
	return out
}

func TestGraphEndpoints(t *testing.T) {
	graphs := &fakeGraphs{g: shopGraph(true), info: graph.Info{Built: graph.BuiltLive}}
	h := graphServer(t, graphs)
	q := url.QueryEscape

	cases := []struct {
		path  string
		focus string
		nodes []string
		edges int
	}{
		{"find?resource=pods&namespace=shop&name=web-1", "uid-web-1", []string{"Node n1", "Pod web-1", "Secret db-creds", "Secret stripe-key", "Service web"}, 4},
		{"find?resource=secrets&namespace=shop&name=db-creds&depth=2", "uid-db-creds", []string{"Node n1", "Pod web-1", "Secret db-creds", "Secret stripe-key", "Service web"}, 4},
		{"find?resource=secrets&namespace=shop&name=db-creds", "uid-db-creds", []string{"Pod web-1", "Secret db-creds"}, 1},
		{"node?id=uid-web-1", "uid-web-1", []string{"Node n1", "Pod web-1", "Secret db-creds", "Secret stripe-key", "Service web"}, 4},
		{"upstream?id=uid-web-1&depth=1", "uid-web-1", []string{"Node n1", "Pod web-1", "Secret db-creds", "Secret stripe-key"}, 3},
		{"downstream?id=uid-db-creds&depth=3", "uid-db-creds", []string{"Pod web-1", "Secret db-creds", "Service web"}, 2},
		{"path?from=uid-web&to=uid-db-creds", "uid-web", []string{"Pod web-1", "Secret db-creds", "Service web"}, 2},
		{"subgraph?id=uid-web-1&direction=up&kinds=runs-on", "uid-web-1", []string{"Node n1", "Pod web-1"}, 1},
		{"subgraph?namespace=shop", "", []string{"Node n1", "Pod web-1", "Secret db-creds", "Secret stripe-key", "Service web"}, 4},
		{"find?resource=secrets&namespace=shop&name=stripe-key", "missing:secrets/shop/stripe-key", []string{"Pod web-1", "Secret stripe-key"}, 1},
		{"node?id=" + q("missing:secrets/shop/stripe-key"), "missing:secrets/shop/stripe-key", []string{"Pod web-1", "Secret stripe-key"}, 1},
	}
	for _, c := range cases {
		a := ask(t, h, c.path, http.StatusOK)
		if a.Built != graph.BuiltLive || a.Partial == nil {
			t.Errorf("%s: built %q, partial %v", c.path, a.Built, a.Partial)
		}
		if a.Focus != c.focus || !slices.Equal(nodeNames(a), c.nodes) || len(a.Edges) != c.edges {
			t.Errorf("%s: focus %q, nodes %v, %d edges", c.path, a.Focus, nodeNames(a), len(a.Edges))
		}
	}

	a := ask(t, h, "search?q="+q("pods using db-creds"), http.StatusOK)
	if !slices.Equal(a.Matches, []string{"uid-web-1"}) {
		t.Errorf("search matches %v", a.Matches)
	}
	// Each piece of evidence says where the edge came from.
	a = ask(t, h, "upstream?id=uid-web-1&kinds=references", http.StatusOK)
	for _, e := range a.Edges {
		if len(e.Evidence) == 0 || e.Evidence[0].Field == "" {
			t.Errorf("edge without evidence: %+v", e)
		}
	}
	if !slices.Contains(nodeNames(ask(t, h, "find?resource=pods&namespace=shop&name=web-1", 200)), "Pod web-1") {
		t.Error("find lost the object")
	}
}

func TestGraphEndpointErrors(t *testing.T) {
	h := graphServer(t, &fakeGraphs{g: shopGraph(true), info: graph.Info{Built: graph.BuiltLive}})
	for path, want := range map[string]int{
		"find?resource=pods&namespace=shop":           http.StatusBadRequest,
		"find?resource=pods&namespace=shop&name=nope": http.StatusNotFound,
		"node":                                        http.StatusBadRequest,
		"node?id=nope":                                http.StatusNotFound,
		"upstream?id=uid-web-1&depth=x":               http.StatusBadRequest,
		"upstream?id=uid-web-1&depth=0":               http.StatusBadRequest,
		"downstream?id=uid-web-1&kinds=owns,nope":     http.StatusBadRequest,
		"subgraph":                                    http.StatusBadRequest,
		"subgraph?id=uid-web-1&direction=sideways":    http.StatusBadRequest,
		"path?from=uid-web-1":                         http.StatusBadRequest,
		"path?from=uid-web-1&to=uid-n1&kinds=selects": http.StatusNotFound,
		"search?q=db-creds":                           http.StatusBadRequest,
	} {
		if a := ask(t, h, path, want); a.Error == "" {
			t.Errorf("%s: no error message", path)
		}
	}
	if rec := get(h, "/api/clusters/nope/graph/node?id=x"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown cluster: %d", rec.Code)
	}

	// The cluster's own errors keep their meaning, as on every other page.
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("RBAC"))
	h = graphServer(t, &fakeGraphs{err: forbidden})
	ask(t, h, "subgraph?namespace=shop", http.StatusForbidden)
}

// Without the Secret role and with live data off, the answer says so, and
// the pod's Secrets are unknown rather than missing.
func TestGraphSaysWhatItCouldntRead(t *testing.T) {
	graphs := &fakeGraphs{g: shopGraph(false), info: graph.Info{
		Built:   graph.BuiltOnRequest,
		Partial: []graph.PartialKind{{Resource: "secrets", Code: graph.PartialForbidden, Reason: "not allowed"}},
	}}
	h := graphServer(t, graphs)
	a := ask(t, h, "find?resource=pods&namespace=shop&name=web-1", http.StatusOK)
	if a.Built != graph.BuiltOnRequest || len(a.Partial) != 1 || a.Partial[0].Resource != "secrets" {
		t.Errorf("built %q, partial %+v", a.Built, a.Partial)
	}
	for _, n := range a.Nodes {
		if n.Kind == "Secret" && n.Presence != graph.Unknown {
			t.Errorf("Secret %s is %s", n.Name, n.Presence)
		}
	}
	// The namespace asked about is what a graph built on request reads.
	if !slices.Equal(graphs.namespaces, []string{"shop"}) {
		t.Errorf("read namespaces %v", graphs.namespaces)
	}
}
