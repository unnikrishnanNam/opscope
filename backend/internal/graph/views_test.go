package graph

import (
	"slices"
	"testing"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// shop is a namespace with two apps behind one Gateway in another namespace:
//
//	Gateway infra/edge ← HTTPRoute storefront → Service web → 2 pods (Deployment web) → ConfigMap web-config
//	                                         → Service api → 2 pods (Deployment api), one crash-looping,
//	                                                          → Secret stripe, which doesn't exist
//	Ingress web → Service web
//
// The web pods run on worker-1 (Ready), the api pods on worker-2 (NotReady).
// Another namespace, "other", has an app of its own.
func shop() *snapshot.Snapshot {
	web := meta("shop", "web", "app.kubernetes.io/name", "web", "app.kubernetes.io/part-of", "storefront")
	webRS := ownedBy(meta("shop", "web-1"), "Deployment", web)
	webDeploy := deployment(web, 2, 2)
	webDeploy.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "config", VolumeSource: corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "web-config"}}}}}

	api := meta("shop", "api")
	apiRS := ownedBy(meta("shop", "api-1"), "Deployment", api)
	apiDeploy := deployment(api, 2, 1)
	apiDeploy.Spec.Template.Labels = map[string]string{"app": "api"} // only the template is labelled
	needsStripe := corev1.PodSpec{Containers: []corev1.Container{{Name: "api", EnvFrom: []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}}}}}}}
	apiDeploy.Spec.Template.Spec = needsStripe

	crashing := waitingPod(ownedBy(meta("shop", "api-1-b", "app", "api"), "ReplicaSet", apiRS), "CreateContainerConfigError", `secret "stripe" not found`)
	crashing.Spec = needsStripe
	crashing.Spec.NodeName = "worker-2"

	down := readyNode("worker-2")
	down.Status.Conditions[0].Status = corev1.ConditionFalse

	route := kube.HTTPRoute{ObjectMeta: meta("shop", "storefront")}
	route.Spec.ParentRefs = []kube.ObjectRef{{Name: "edge", Namespace: ptr("infra")}}
	route.Spec.Rules = []kube.HTTPRouteRule{{BackendRefs: []kube.BackendRef{
		{ObjectRef: kube.ObjectRef{Name: "web"}}, {ObjectRef: kube.ObjectRef{Name: "api"}},
	}}}
	route.Status.Parents = []kube.RouteParentStatus{{Conditions: []metav1.Condition{{Type: "Accepted", Status: "True"}}}}
	gateway := kube.Gateway{ObjectMeta: meta("infra", "edge")}
	gateway.Spec.GatewayClassName = "nginx"
	gateway.Status.Conditions = []metav1.Condition{{Type: "Programmed", Status: "True"}}
	class := kube.GatewayClass{ObjectMeta: meta("", "nginx")}
	class.Status.Conditions = []metav1.Condition{{Type: "Accepted", Status: "True"}}

	s := &snapshot.Snapshot{
		Nodes:       []corev1.Node{readyNode("worker-1"), down},
		Deployments: []appsv1.Deployment{webDeploy, apiDeploy, deployment(meta("other", "x"), 1, 1)},
		ReplicaSets: []appsv1.ReplicaSet{replicaSet(webRS, 2, 2), replicaSet(apiRS, 2, 1)},
		Pods: []corev1.Pod{
			readyPod(ownedBy(meta("shop", "web-1-a", "app.kubernetes.io/name", "web"), "ReplicaSet", webRS), "worker-1"),
			readyPod(ownedBy(meta("shop", "web-1-b", "app.kubernetes.io/name", "web"), "ReplicaSet", webRS), "worker-1"),
			readyPod(ownedBy(meta("shop", "api-1-a", "app", "api"), "ReplicaSet", apiRS), "worker-2", needsStripe),
			crashing,
		},
		Services: []corev1.Service{
			{ObjectMeta: meta("shop", "web"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/name": "web"}}},
			{ObjectMeta: meta("shop", "api"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "api"}}},
			{ObjectMeta: meta("shop", "legacy"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "legacy"}}},
			{ObjectMeta: meta("other", "x"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "x"}}},
		},
		ConfigMaps: []snapshot.ConfigMap{{ObjectMeta: meta("shop", "web-config")}},
		Ingresses: []networkingv1.Ingress{{ObjectMeta: meta("shop", "web"), Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: "web"}},
		}}},
		HTTPRoutes:     []kube.HTTPRoute{route},
		Gateways:       []kube.Gateway{gateway},
		GatewayClasses: []kube.GatewayClass{class},
		Namespaces:     []corev1.Namespace{{ObjectMeta: meta("", "shop")}},
	}
	// Judge Services by their pods' readiness; the fixture has no EndpointSlices.
	unreadable(s, "forbidden", "endpointslices")
	return s
}

func TestApps(t *testing.T) {
	g := build(shop())
	want := map[string]string{
		"deployments/shop/web":       "storefront", // part-of comes before name
		"replicasets/shop/web-1":     "storefront", // what a workload owns is in its app
		"pods/shop/web-1-a":          "storefront",
		"deployments/shop/api":       "api", // from its pod template's labels
		"services/shop/web":          "storefront",
		"services/shop/api":          "api",
		"ingresses/shop/web":         "storefront", // from the Service it routes to
		"httproutes/shop/storefront": "storefront", // routes to both apps: a tie goes to the app named most like it
		"configmaps/shop/web-config": "storefront", // from what uses it
		"secrets/shop/stripe":        "api",        // missing: from what needs it
		"services/shop/legacy":       "legacy",     // selects nothing: an app of its own
		"gateways/infra/edge":        "",           // shared infrastructure
		"gatewayclasses/nginx":       "",
	}
	for id, app := range want {
		if got := node(t, g, id).App; got != app {
			t.Errorf("%s: app %q, want %q", id, got, app)
		}
	}
}

func TestNamespaceView(t *testing.T) {
	v := Of(prepared(shop())).Namespace("shop", Options{})

	for _, id := range []string{
		"deployments/shop/web", "pods:replicasets/shop/web-1", "services/shop/web", "configmaps/shop/web-config",
		"deployments/shop/api", "pods:replicasets/shop/api-1", "secrets/shop/stripe",
		"httproutes/shop/storefront", "ingresses/shop/web",
		"gateways/infra/edge",  // in another namespace, but the route needs it
		"gatewayclasses/nginx", // and the Gateway needs its class
		"nodes/worker-2",       // a machine that isn't Ready
	} {
		if !hasID(v.Nodes, id) {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{
		"replicasets/shop/web-1", // folded into its Deployment
		"pods/shop/web-1-a",      // in its group
		"nodes/worker-1",         // a healthy machine
		"deployments/other/x",    // another namespace
		"namespaces/shop",
	} {
		if hasID(v.Nodes, id) {
			t.Errorf("unexpected %s", id)
		}
	}

	group := findNode(v.Nodes, "pods:replicasets/shop/api-1")
	if group.Name != "api" || group.Pods.Owner != "deployments/shop/api" || group.Pods.Total != 2 || group.Pods.Ready != 1 || group.State != Failing {
		t.Errorf("api pods: %+v %+v", group, group.Pods)
	}
	if group.Status != "1/2 ready" || group.Message != `secret "stripe" not found` {
		t.Errorf("api pods: status %q, message %q", group.Status, group.Message)
	}
	if !slices.Equal(group.Pods.Members, []string{"pods/shop/api-1-a", "pods/shop/api-1-b"}) {
		t.Errorf("members = %v", group.Pods.Members)
	}

	wantViewEdge(t, v, "deployments/shop/api", "pods:replicasets/shop/api-1", Owns) // past the hidden ReplicaSet
	wantViewEdge(t, v, "services/shop/api", "pods:replicasets/shop/api-1", Selects)
	wantViewEdge(t, v, "pods:replicasets/shop/api-1", "secrets/shop/stripe", Uses)
	wantViewEdge(t, v, "pods:replicasets/shop/api-1", "nodes/worker-2", RunsOn)

	wantApps := []App{
		{Name: "api", State: Failing, Workloads: 1},
		{Name: "legacy", State: Failing, Workloads: 0},
		{Name: "storefront", State: OK, Workloads: 1},
	}
	if !slices.Equal(v.Apps, wantApps) {
		t.Errorf("apps = %+v", v.Apps)
	}
}

func TestExpandedPods(t *testing.T) {
	g := Of(prepared(shop()))

	v := g.Namespace("shop", Options{Expand: map[string]bool{"deployments/shop/api": true}})
	if !hasID(v.Nodes, "pods/shop/api-1-b") || hasID(v.Nodes, "pods:replicasets/shop/api-1") {
		t.Error("expanding a Deployment shows its pods one by one")
	}
	if !hasID(v.Nodes, "pods:replicasets/shop/web-1") {
		t.Error("other workloads stay grouped")
	}
	wantViewEdge(t, v, "deployments/shop/api", "pods/shop/api-1-b", Owns)

	if v := g.Namespace("shop", Options{All: true}); hasID(v.Nodes, "pods:replicasets/shop/web-1") {
		t.Error("All expands every workload")
	}
}

func TestRolloutShowsReplicaSets(t *testing.T) {
	d := meta("a", "web")
	oldRS, newRS := ownedBy(meta("a", "web-1"), "Deployment", d), ownedBy(meta("a", "web-2"), "Deployment", d)
	g := build(&snapshot.Snapshot{
		Deployments: []appsv1.Deployment{deployment(d, 2, 2)},
		ReplicaSets: []appsv1.ReplicaSet{replicaSet(oldRS, 1, 1), replicaSet(newRS, 1, 1), replicaSet(ownedBy(meta("a", "web-0"), "Deployment", d), 0, 0)},
		Pods: []corev1.Pod{
			readyPod(ownedBy(meta("a", "web-1-x"), "ReplicaSet", oldRS), ""),
			readyPod(ownedBy(meta("a", "web-2-x"), "ReplicaSet", newRS), ""),
		},
	})
	v := g.Namespace("a", Options{})
	for _, id := range []string{"replicasets/a/web-1", "replicasets/a/web-2", "pods:replicasets/a/web-1", "pods:replicasets/a/web-2"} {
		if !hasID(v.Nodes, id) {
			t.Errorf("during a rollout, %s is shown", id)
		}
	}
	if hasID(v.Nodes, "replicasets/a/web-0") {
		t.Error("an old ReplicaSet with no pods isn't")
	}
	if group := findNode(v.Nodes, "pods:replicasets/a/web-2"); group.Name != "web-2" {
		t.Errorf("a group under a visible ReplicaSet is named after it, got %q", group.Name)
	}
}

func TestProblemsOnly(t *testing.T) {
	v := Of(prepared(shop())).Namespace("shop", Options{Problems: true})
	for _, id := range []string{
		"deployments/shop/api", "pods:replicasets/shop/api-1", "secrets/shop/stripe", "services/shop/api", "nodes/worker-2",
		"httproutes/shop/storefront", // OK itself, but it points at a Service that isn't
	} {
		if !hasID(v.Nodes, id) {
			t.Errorf("missing %s", id)
		}
	}
	for _, id := range []string{"deployments/shop/web", "configmaps/shop/web-config", "gatewayclasses/nginx"} {
		if hasID(v.Nodes, id) {
			t.Errorf("unexpected %s", id)
		}
	}
}

func TestAppView(t *testing.T) {
	v := Of(prepared(shop())).App("shop", "storefront", Options{})
	for _, id := range []string{
		"deployments/shop/web", "pods:replicasets/shop/web-1", "services/shop/web", "configmaps/shop/web-config", "ingresses/shop/web",
		"httproutes/shop/storefront", // another app's route, but it sends traffic here
		"gateways/infra/edge", "gatewayclasses/nginx",
	} {
		if !hasID(v.Nodes, id) {
			t.Errorf("missing %s", id)
		}
	}
	if hasID(v.Nodes, "deployments/shop/api") || hasID(v.Nodes, "pods:replicasets/shop/api-1") {
		t.Error("the other app's workload isn't part of this one")
	}
	if v.Apps != nil {
		t.Error("only a namespace view lists apps")
	}
}

func TestNeighbourhood(t *testing.T) {
	g := Of(prepared(shop()))

	t.Run("a pod is shown as itself, with its machine", func(t *testing.T) {
		v, ok := g.Neighbourhood("pods/shop/web-1-a", 1, Options{})
		if !ok {
			t.Fatal("not found")
		}
		for _, id := range []string{"pods/shop/web-1-a", "deployments/shop/web", "services/shop/web", "nodes/worker-1"} {
			if !hasID(v.Nodes, id) {
				t.Errorf("missing %s", id)
			}
		}
		if hasID(v.Nodes, "pods:replicasets/shop/web-1") {
			t.Error("the focused pod's workload is expanded")
		}
	})

	t.Run("depth", func(t *testing.T) {
		v, _ := g.Neighbourhood("deployments/shop/web", 1, Options{})
		if !hasID(v.Nodes, "pods:replicasets/shop/web-1") || hasID(v.Nodes, "services/shop/web") {
			t.Errorf("depth 1: %v", ids(v.Nodes))
		}
		v, _ = g.Neighbourhood("deployments/shop/web", 2, Options{})
		if !hasID(v.Nodes, "services/shop/web") || hasID(v.Nodes, "ingresses/shop/web") {
			t.Errorf("depth 2: %v", ids(v.Nodes))
		}
	})

	t.Run("the cause of a problem is never cut off", func(t *testing.T) {
		v, _ := g.Neighbourhood("services/shop/api", 1, Options{})
		// The pods are one step away; what they need, which is failing, is two.
		if !hasID(v.Nodes, "secrets/shop/stripe") || !hasID(v.Nodes, "nodes/worker-2") {
			t.Errorf("got %v", ids(v.Nodes))
		}
	})

	t.Run("a missing object", func(t *testing.T) {
		v, ok := g.Neighbourhood("secrets/shop/stripe", 2, Options{})
		if !ok || !hasID(v.Nodes, "pods:replicasets/shop/api-1") || !hasID(v.Nodes, "deployments/shop/api") {
			t.Errorf("got %v", ids(v.Nodes))
		}
	})

	t.Run("a workload's template edges give way to its pods'", func(t *testing.T) {
		v := g.Namespace("shop", Options{})
		wantViewEdge(t, v, "pods:replicasets/shop/api-1", "secrets/shop/stripe", Uses)
		for _, e := range v.Edges {
			if e.From == "deployments/shop/api" && e.To == "secrets/shop/stripe" {
				t.Error("the Deployment's own edge repeats its pods'")
			}
		}
	})

	t.Run("a machine", func(t *testing.T) {
		v, ok := g.Neighbourhood("nodes/worker-1", 1, Options{})
		if !ok || !hasID(v.Nodes, "nodes/worker-1") || !hasID(v.Nodes, "pods:replicasets/shop/web-1") {
			t.Errorf("got %v", ids(v.Nodes))
		}
	})

	if _, ok := g.Neighbourhood("pods/shop/nope", 1, Options{}); ok {
		t.Error("an unknown object isn't found")
	}
}

func TestOfBuildsOnce(t *testing.T) {
	s := prepared(shop())
	if Of(s) != Of(s) {
		t.Error("Of should keep the graph with the snapshot")
	}
}

// prepared indexes a snapshot without building its graph, for tests that
// go through Of.
func prepared(s *snapshot.Snapshot) *snapshot.Snapshot {
	s.Taken = now
	s.BuildIndex()
	return s
}

func findNode(nodes []*Node, id string) *Node {
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	return &Node{Pods: &PodGroup{}}
}

func wantViewEdge(t *testing.T, v *View, from, to, typ string) {
	t.Helper()
	for _, e := range v.Edges {
		if e.From == from && e.To == to && e.Type == typ {
			return
		}
	}
	t.Errorf("no %s edge %s → %s in the view", typ, from, to)
}

// A ReplicaSet that can't create pods is the only place that says why a
// Deployment has none, so it's shown.
func TestReplicaSetThatCantCreatePods(t *testing.T) {
	d := meta("a", "web")
	rs := replicaSet(ownedBy(meta("a", "web-1"), "Deployment", d), 2, 0)
	rs.Status.Conditions = []appsv1.ReplicaSetCondition{{Type: appsv1.ReplicaSetReplicaFailure, Status: corev1.ConditionTrue, Message: "exceeded quota"}}
	g := build(&snapshot.Snapshot{Deployments: []appsv1.Deployment{deployment(d, 2, 0)}, ReplicaSets: []appsv1.ReplicaSet{rs}})

	v := g.Namespace("a", Options{})
	if !hasID(v.Nodes, "replicasets/a/web-1") {
		t.Errorf("got %v", ids(v.Nodes))
	}
	wantViewEdge(t, v, "deployments/a/web", "replicasets/a/web-1", Owns)
}

// A Deployment whose pods can't be created keeps its template's edges: they
// are the only way to see what it needs.
func TestTemplateEdgesWithoutPods(t *testing.T) {
	d := deployment(meta("a", "web"), 1, 0)
	d.Spec.Template.Spec.Containers = []corev1.Container{{Name: "app", EnvFrom: []corev1.EnvFromSource{
		{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}}}}}}
	v := build(&snapshot.Snapshot{Deployments: []appsv1.Deployment{d}}).Namespace("a", Options{})
	wantViewEdge(t, v, "deployments/a/web", "secrets/a/stripe", Uses)
}

func TestUnusedConfigIsLeftOut(t *testing.T) {
	g := build(&snapshot.Snapshot{
		ConfigMaps:      []snapshot.ConfigMap{{ObjectMeta: meta("a", "kube-root-ca.crt")}},
		ServiceAccounts: []corev1.ServiceAccount{{ObjectMeta: meta("a", "default")}},
		Services:        []corev1.Service{{ObjectMeta: meta("a", "web")}},
	})
	v := g.Namespace("a", Options{})
	if hasID(v.Nodes, "configmaps/a/kube-root-ca.crt") || hasID(v.Nodes, "serviceaccounts/a/default") || !hasID(v.Nodes, "services/a/web") {
		t.Errorf("got %v", ids(v.Nodes))
	}
	if v, _ := g.Neighbourhood("configmaps/a/kube-root-ca.crt", 1, Options{}); !hasID(v.Nodes, "configmaps/a/kube-root-ca.crt") {
		t.Error("an unused ConfigMap is still shown when it's what you asked for")
	}
}

// A Gateway's controller owns the Deployment it runs the Gateway with. That
// Deployment is still the root of an app.
func TestWorkloadOwnedByGateway(t *testing.T) {
	gw := kube.Gateway{ObjectMeta: meta("a", "edge")}
	d := deployment(ownedBy(meta("a", "edge-nginx", "app.kubernetes.io/instance", "ngf"), "Gateway", gw.ObjectMeta), 1, 1)
	g := build(&snapshot.Snapshot{Gateways: []kube.Gateway{gw}, Deployments: []appsv1.Deployment{d}})

	findEdge(t, g, "gateways/a/edge", "deployments/a/edge-nginx", Owns)
	if app := node(t, g, "deployments/a/edge-nginx").App; app != "ngf" {
		t.Errorf("app = %q, want ngf", app)
	}
}

// A static pod names its node as its owner; it stays a pod of its own.
func TestStaticPods(t *testing.T) {
	node := readyNode("master")
	g := build(&snapshot.Snapshot{
		Nodes: []corev1.Node{node},
		Pods:  []corev1.Pod{readyPod(ownedBy(meta("kube-system", "etcd-master"), "Node", node.ObjectMeta), "master")},
	})
	noEdge(t, g, "nodes/master", "pods/kube-system/etcd-master")
	findEdge(t, g, "pods/kube-system/etcd-master", "nodes/master", RunsOn)
	if v := g.Namespace("kube-system", Options{}); !hasID(v.Nodes, "pods/kube-system/etcd-master") {
		t.Errorf("got %v", ids(v.Nodes))
	}
}

func TestLongViaIsShortened(t *testing.T) {
	var env []corev1.EnvVar
	for _, name := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		env = append(env, corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "settings"}, Key: name}}})
	}
	g := build(&snapshot.Snapshot{
		Pods:       []corev1.Pod{readyPod(meta("a", "web"), "", corev1.PodSpec{Containers: []corev1.Container{{Name: "app", Env: env}}})},
		ConfigMaps: []snapshot.ConfigMap{{ObjectMeta: meta("a", "settings")}},
	})
	if e := findEdge(t, g, "pods/a/web", "configmaps/a/settings", Uses); len(e.Via) != 7 {
		t.Errorf("the graph keeps every reference, got %d", len(e.Via))
	}
	v := g.Namespace("a", Options{})
	for _, e := range v.Edges {
		if len(e.Via) != 6 || e.Via[5] != "and 2 more" {
			t.Errorf("via = %v", e.Via)
		}
	}
}
