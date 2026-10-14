package graph

import (
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
)

// appLabels are the labels that name an app, most specific grouping first.
// "part-of" names the larger application several components belong to;
// "instance" is a Helm release or similar; "name" and "app" name the
// component itself.
var appLabels = []string{
	"app.kubernetes.io/part-of",
	"app.kubernetes.io/instance",
	"app.kubernetes.io/name",
	"app",
}

// workloads are the kinds that start an app: an object of one of these not
// owned by another is an app's root, or an app of its own. An owner of
// another kind doesn't count: a Gateway controller owns the Deployment it
// runs the Gateway with, which is still an app's root.
var workloads = map[string]bool{
	"deployments": true, "statefulsets": true, "daemonsets": true, "cronjobs": true,
	"jobs": true, "replicasets": true, "pods": true,
}

// addApps gives each node the app it belongs to, within its namespace:
//
//  1. a workload not owned by another: its app label, or its pod template's, or its own name
//  2. what a workload owns (ReplicaSets, Jobs, pods): its owner's app
//  3. Services and PodDisruptionBudgets: the app of the pods they select
//  4. Ingresses and HTTPRoutes: the app of the Services they route to
//  5. autoscalers: the app of what they scale
//  6. ConfigMaps, Secrets, claims and service accounts: the app of what uses them
//  7. missing and unknown objects: the app of what needs them
//
// Steps 3–6 fall back to the object's own app label. Cluster-wide objects,
// Gateways and anything else used by no app keep no app; views reach them
// through edges.
func (b *builder) addApps() {
	g := b.g
	nodes := g.Nodes()

	// 1 and 2: roots, then down the owns edges.
	for _, n := range nodes {
		if !workloads[n.Resource] || n.Namespace == "" || b.hasWorkloadOwner(n.ID) {
			continue
		}
		n.App = firstOf(appLabel(n.labels), appLabel(b.templateLabels(n)), n.Name)
		b.spreadToOwned(n)
	}

	// 3–6: each from the apps of its neighbours.
	fromNeighbours := []struct {
		resources map[string]bool
		out       bool // look at what it points to (true) or what points to it (false)
		edge      string
	}{
		{set("services", "poddisruptionbudgets"), true, Selects},
		{set("ingresses", "httproutes"), true, RoutesTo},
		{set("horizontalpodautoscalers"), true, Scales},
		{set("configmaps", "secrets", "persistentvolumeclaims", "serviceaccounts"), false, Uses},
	}
	for _, step := range fromNeighbours {
		for _, n := range nodes {
			if !step.resources[n.Resource] || n.App != "" {
				continue
			}
			edges := g.In(n.ID)
			if step.out {
				edges = g.Out(n.ID)
			}
			counts := map[string]int{}
			for _, e := range edges {
				other := e.From
				if step.out {
					other = e.To
				}
				if e.Type == step.edge && g.nodes[other].App != "" && g.nodes[other].Namespace == n.Namespace {
					counts[g.nodes[other].App]++
				}
			}
			n.App = firstOf(mostCommon(counts, n.Name), appLabel(n.labels))
		}
	}

	// 7: missing objects have no labels; they belong with whatever needs them.
	for _, n := range g.Nodes() {
		if (n.State != Missing && n.State != Unknown) || n.Namespace == "" || n.App != "" {
			continue
		}
		counts := map[string]int{}
		for _, e := range g.In(n.ID) {
			if from := g.nodes[e.From]; from.App != "" && from.Namespace == n.Namespace {
				counts[from.App]++
			}
		}
		n.App = mostCommon(counts, n.Name)
	}

	// Services, routes and autoscalers that nothing else placed are an app of
	// their own: they're things people look for by name.
	for _, n := range g.Nodes() {
		if n.App == "" && n.Namespace != "" && standalone[n.Resource] {
			n.App = n.Name
		}
	}
}

var standalone = set("services", "ingresses", "httproutes", "horizontalpodautoscalers")

func (b *builder) hasWorkloadOwner(id string) bool {
	for _, e := range b.g.In(id) {
		if e.Type == Owns && workloads[b.g.nodes[e.From].Resource] {
			return true
		}
	}
	return false
}

// spreadToOwned gives the workloads n owns (ReplicaSets, Jobs, pods), and
// what they own, n's app.
func (b *builder) spreadToOwned(n *Node) {
	for _, e := range b.g.Out(n.ID) {
		if owned := b.g.nodes[e.To]; e.Type == Owns && workloads[owned.Resource] && owned.App == "" {
			owned.App = n.App
			b.spreadToOwned(owned)
		}
	}
}

// templateLabels returns the labels of a workload's pod template, which
// Helm charts and kubectl set even when the workload itself has none.
func (b *builder) templateLabels(n *Node) map[string]string {
	switch obj := b.object(n.ID).(type) {
	case *appsv1.Deployment:
		return obj.Spec.Template.Labels
	case *appsv1.StatefulSet:
		return obj.Spec.Template.Labels
	case *appsv1.DaemonSet:
		return obj.Spec.Template.Labels
	case *appsv1.ReplicaSet:
		return obj.Spec.Template.Labels
	case *batchv1.Job:
		return obj.Spec.Template.Labels
	case *batchv1.CronJob:
		return obj.Spec.JobTemplate.Spec.Template.Labels
	}
	return nil
}

func appLabel(labels map[string]string) string {
	for _, label := range appLabels {
		if value := labels[label]; value != "" {
			return value
		}
	}
	return ""
}

// mostCommon returns the app most of an object's neighbours are in. A tie
// goes to the app whose name is most like the object's own ("web-route"
// sends traffic to both "api" and "web-app": it's "web-app"), then to the
// first name in order, so the answer never changes from one build to the
// next.
func mostCommon(counts map[string]int, name string) string {
	names := make([]string, 0, len(counts))
	for app := range counts {
		names = append(names, app)
	}
	sort.Strings(names)
	best := ""
	for _, app := range names {
		switch {
		case best == "" || counts[app] > counts[best]:
			best = app
		case counts[app] == counts[best] && commonPrefix(app, name) > commonPrefix(best, name):
			best = app
		}
	}
	return best
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, item := range items {
		m[item] = true
	}
	return m
}
