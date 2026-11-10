package graph

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opscope/internal/resources"
)

func TestEveryEdgeKind(t *testing.T) {
	s := Build(nil, shop()).Snapshot()

	cases := []struct {
		from, to, kind string
		evidence       []string
	}{
		// owns: read from the owned object, drawn from the owner
		{"deployments/shop/api", "replicasets/shop/api-7d9f", Owns, []string{"owners metadata.ownerReferences[0] controller"}},
		{"replicasets/shop/api-7d9f", "pods/shop/api-7d9f-x1", Owns, nil},
		{"statefulsets/shop/db", "pods/shop/db-0", Owns, nil},
		{"cronjobs/shop/nightly", "jobs/shop/nightly-123", Owns, nil},
		{"services/shop/api", "endpointslices/shop/api-abcde", Owns, nil},
		{"gateways/shop/edge", "deployments/shop/edge-nginx", Owns, []string{"owners metadata.ownerReferences[0] "}},

		// references: one edge per pair, with every field that makes it
		{"pods/shop/api-7d9f-x1", "secrets/shop/db-creds", References, []string{
			"pods.spec spec.containers[0].env[0].valueFrom.secretKeyRef.name env DB_PASSWORD",
			"pods.spec spec.volumes[1].secret.secretName volume creds",
		}},
		{"pods/shop/api-7d9f-x1", "configmaps/shop/app-config", References, []string{"pods.spec spec.volumes[0].configMap.name volume config"}},
		{"pods/shop/api-7d9f-x1", "persistentvolumeclaims/shop/data", References, []string{"pods.spec spec.volumes[2].persistentVolumeClaim.claimName volume data"}},
		{"pods/shop/api-7d9f-x1", "serviceaccounts/shop/api", References, []string{"pods.spec spec.serviceAccountName service account"}},
		{"deployments/shop/api", "secrets/shop/db-creds", References, []string{
			"deployments.template spec.template.spec.containers[0].env[0].valueFrom.secretKeyRef.name env DB_PASSWORD",
			"deployments.template spec.template.spec.volumes[1].secret.secretName volume creds",
		}},
		{"statefulsets/shop/db", "persistentvolumeclaims/shop/data-db-0", References, []string{"statefulsets.claims spec.volumeClaimTemplates[0] claim template data"}},
		{"ingresses/shop/web", "secrets/shop/tls-cert", References, []string{"ingresses.tls spec.tls[0].secretName TLS"}},
		{"gateways/shop/edge", "secrets/shop/tls-cert", References, []string{"gateways.certificates spec.listeners[0].tls.certificateRefs[0] listener https"}},

		// binds
		{"persistentvolumeclaims/shop/data", "persistentvolumes/pv-1", Binds, []string{"persistentvolumeclaims.binding spec.volumeName "}},
		{"persistentvolumeclaims/shop/data", "storageclasses/fast", Binds, []string{"persistentvolumeclaims.binding spec.storageClassName "}},
		{"persistentvolumes/pv-1", "storageclasses/fast", Binds, []string{"persistentvolumes.class spec.storageClassName "}},

		// runs-on, selects, endpoints
		{"pods/shop/api-7d9f-x1", "nodes/node-1", RunsOn, []string{"pods.node spec.nodeName "}},
		{"services/shop/api", "pods/shop/api-7d9f-x1", Selects, []string{"services.selector spec.selector "}},
		{"services/shop/api", "pods/shop/api-7d9f-x2", Selects, nil},
		{"endpointslices/shop/api-abcde", "pods/shop/api-7d9f-x1", Endpoints, []string{"endpointslices.endpoints endpoints[0].targetRef ready"}},
		{"endpointslices/shop/api-abcde", "pods/shop/api-7d9f-x2", Endpoints, []string{"endpointslices.endpoints endpoints[1].targetRef not ready"}},

		// routes, attaches
		{"ingresses/shop/web", "services/shop/api", Routes, []string{"ingresses.backends spec.rules[0].http.paths[0].backend.service.name shop.example/api"}},
		{"httproutes/shop/store", "services/shop/api", Routes, []string{"httproutes.backends spec.rules[0].backendRefs[0] port 80"}},
		{"httproutes/shop/store", "gateways/shop/edge", Attaches, []string{"httproutes.parents spec.parentRefs[0] listener https"}},
		{"gateways/shop/edge", "gatewayclasses/nginx", Attaches, []string{"gateways.class spec.gatewayClassName "}},
	}
	kinds := map[string]bool{}
	for _, c := range cases {
		e := findEdge(t, s, c.from, c.to, c.kind)
		kinds[e.Kind] = true
		if e.ID != EdgeID(e.From, e.To, e.Kind) {
			t.Errorf("%s → %s: ID %s isn't derived from its content", c.from, c.to, e.ID)
		}
		if c.evidence != nil && !slices.Equal(fields(e), c.evidence) {
			t.Errorf("%s → %s (%s): evidence\n  %q\nwant\n  %q", c.from, c.to, c.kind, fields(e), c.evidence)
		}
		for _, ev := range e.Evidence {
			if ev.ResourceVersion != "1" || !ev.Seen.Equal(seen) {
				t.Errorf("%s → %s: evidence %s without the object's version and time", c.from, c.to, ev.Field)
			}
		}
	}
	for _, k := range []string{Owns, References, Binds, RunsOn, Selects, Endpoints, Routes, Attaches} {
		if !kinds[k] {
			t.Errorf("no case for edge kind %s", k)
		}
	}
	if hasEdge(s, "statefulsets/shop/db", "persistentvolumeclaims/shop/data-db-old", References) {
		t.Error("a claim without an ordinal was taken as made from the StatefulSet's template")
	}
}

func TestOptionalReferences(t *testing.T) {
	s := Build(nil, shop()).Snapshot()
	if e := findEdge(t, s, "pods/shop/api-7d9f-x1", "configmaps/shop/flags", References); !e.Optional || !e.Evidence[0].Optional {
		t.Errorf("an optional configMapKeyRef isn't optional: %+v", e)
	}
	if e := findEdge(t, s, "pods/shop/api-7d9f-x1", "secrets/shop/db-creds", References); e.Optional {
		t.Error("a required reference is optional")
	}
}

func TestGaps(t *testing.T) {
	s := Build(nil, shop()).Snapshot()

	// Missing: referred to by name, and not there.
	for _, c := range []struct{ from, to, kind string }{
		{"pods/shop/api-7d9f-x1", "missing:secrets/shop/stripe-key", References},
		{"pods/shop/api-7d9f-x1", "missing:secrets/shop/regcred", References},
		{"ingresses/shop/web", "missing:services/shop/old-api", Routes},
		{"httproutes/shop/store", "missing:services/shop/missing-svc", Routes},
		{"persistentvolumeclaims/shop/pending", "missing:storageclasses/slow", Binds},
	} {
		findEdge(t, s, c.from, c.to, c.kind)
	}
	ids := named(s)
	if id := ids["missing:secrets/shop/stripe-key"]; id != "missing:secrets/shop/stripe-key" {
		t.Errorf("missing node's ID is %q", id)
	}

	// Dangling: a selector that matches nothing.
	dangling := map[string][]Dangling{}
	for _, n := range s.Nodes {
		if len(n.Dangling) > 0 {
			dangling[objectKey(n.Resource, n.Namespace, n.Name)] = n.Dangling
		}
	}
	want := map[string][]Dangling{"services/shop/ghost": {{Kind: Selects, Field: "spec.selector", Reason: "matches no pods"}}}
	if !equalJSON(dangling, want) {
		t.Errorf("dangling: %v, want %v", dangling, want)
	}

	// Unknown: a kind the graph isn't given, and a kind Opscope doesn't know.
	readable := slices.DeleteFunc(slices.Clone(NodeKinds), func(k string) bool { return k == "secrets" })
	objects := append(shop(), pod("shop", "canary", nil, corev1.PodSpec{},
		metav1.OwnerReference{APIVersion: "argoproj.io/v1alpha1", Kind: "Rollout", Name: "web", UID: "uid-rollout"}))
	s = Build(readable, objects).Snapshot()
	findEdge(t, s, "pods/shop/api-7d9f-x1", "unknown:secrets/shop/db-creds", References)
	findEdge(t, s, "unknown:Rollout.argoproj.io/shop/web", "pods/shop/canary", Owns)
	for _, n := range s.Nodes {
		if n.Presence == Missing && n.Resource == "secrets" {
			t.Errorf("%s: a Secret is missing when Secrets can't be read", n.ID)
		}
		if n.ID == "unknown:Rollout.argoproj.io/shop/web" && n.Kind != "Rollout" {
			t.Errorf("unknown owner's kind is %q", n.Kind)
		}
	}
}

// What G0–G7 learned on real clusters.
func TestLessonsKept(t *testing.T) {
	s := Build(nil, shop()).Snapshot()
	for _, n := range s.Nodes {
		if n.Name == kubeRootCA || (n.Resource == "serviceaccounts" && n.Name == "default") {
			for _, e := range s.Edges {
				if e.To == n.ID {
					t.Errorf("an edge to %s %s: it would tie every pod to it", n.Kind, n.Name)
				}
			}
		}
	}
	if hasEdge(s, "nodes/node-1", "pods/kube-system/etcd-node-1", Owns) {
		t.Error("a static pod is owned by its node")
	}
	findEdge(t, s, "pods/kube-system/etcd-node-1", "nodes/node-1", RunsOn)
	findEdge(t, s, "gateways/shop/edge", "deployments/shop/edge-nginx", Owns)
}

func TestMissingObjectComesAndGoes(t *testing.T) {
	g := Build(nil, shop())
	g.Upsert(secret("shop", "stripe-key"))
	s := g.Snapshot()
	findEdge(t, s, "pods/shop/api-7d9f-x1", "secrets/shop/stripe-key", References)
	if _, ok := named(s)["missing:secrets/shop/stripe-key"]; ok {
		t.Error("the placeholder stayed after the Secret was made")
	}

	g.Delete(string(uidOf("secrets", "stripe-key")))
	findEdge(t, g.Snapshot(), "pods/shop/api-7d9f-x1", "missing:secrets/shop/stripe-key", References)

	// Deleting the owner leaves what it owned pointing at a missing owner.
	g.Delete(string(uidOf("deployments", "api")))
	findEdge(t, g.Snapshot(), "missing:deployments/shop/api", "replicasets/shop/api-7d9f", Owns)
}

func TestOwnerMadeAgainIsntTheOwner(t *testing.T) {
	objects := shop()
	for i, o := range objects {
		if o.Resource == "deployments" && o.Object.(metav1.Object).GetName() == "api" {
			again := o.Object.DeepCopyObject()
			again.(metav1.Object).SetUID("uid-deployments-api-again")
			objects[i] = Object{Resource: o.Resource, Object: again, Seen: o.Seen}
		}
	}
	s := Build(nil, objects).Snapshot()
	findEdge(t, s, "missing:deployments/shop/api", "replicasets/shop/api-7d9f", Owns)
	if hasEdge(s, "deployments/shop/api", "replicasets/shop/api-7d9f", Owns) {
		t.Error("a Deployment made again under the same name owns the old one's ReplicaSet")
	}
}

func TestSelectorsFollowLabels(t *testing.T) {
	g := Build(nil, shop())
	relabel := func(name string, labels map[string]string) {
		for _, o := range shop() {
			if o.Resource == "pods" && o.Object.(metav1.Object).GetName() == name {
				o.Object.(metav1.Object).SetLabels(labels)
				o.Object.(metav1.Object).SetResourceVersion("2")
				g.Upsert(o)
			}
		}
	}
	relabel("api-7d9f-x2", map[string]string{"app": "nothing"})
	s := g.Snapshot()
	if hasEdge(s, "services/shop/api", "pods/shop/api-7d9f-x2", Selects) {
		t.Error("a pod whose labels changed is still selected")
	}
	findEdge(t, s, "services/shop/ghost", "pods/shop/api-7d9f-x2", Selects)
	for _, n := range s.Nodes {
		if n.Name == "ghost" && len(n.Dangling) > 0 {
			t.Error("ghost is still dangling after a pod matched it")
		}
	}
}

func TestReadableKinds(t *testing.T) {
	g := Build(nil, shop())
	g.SetReadable("secrets", false)
	s := g.Snapshot()
	findEdge(t, s, "pods/shop/api-7d9f-x1", "unknown:secrets/shop/db-creds", References)
	for _, n := range s.Nodes {
		if n.Resource == "secrets" && n.Presence != Unknown {
			t.Errorf("%s is %s after Secrets stopped being readable", n.ID, n.Presence)
		}
	}

	g.SetReadable("secrets", true)
	findEdge(t, g.Snapshot(), "pods/shop/api-7d9f-x1", "missing:secrets/shop/db-creds", References)
	g.Upsert(secret("shop", "db-creds"))
	findEdge(t, g.Snapshot(), "pods/shop/api-7d9f-x1", "secrets/shop/db-creds", References)
}

func TestDeletingEverythingLeavesNothing(t *testing.T) {
	objects := shop()
	g := Build(nil, objects)
	rand.New(rand.NewPCG(1, 2)).Shuffle(len(objects), func(i, j int) { objects[i], objects[j] = objects[j], objects[i] })
	for _, o := range objects {
		g.Delete(string(o.Object.(metav1.Object).GetUID()))
	}
	s := g.Snapshot()
	if len(s.Nodes) != 0 || len(s.Edges) != 0 {
		t.Errorf("%d nodes and %d edges left: %v", len(s.Nodes), len(s.Edges), s.Nodes)
	}
	if len(g.wanting) != 0 || len(g.watching) != 0 || len(g.degree) != 0 || len(g.groups) != 0 {
		t.Errorf("indexes left: wanting %d, watching %d, degree %d, groups %d", len(g.wanting), len(g.watching), len(g.degree), len(g.groups))
	}
}

// Adding objects one at a time, in any order, with earlier versions of
// some and objects that come and go along the way, gives the same graph,
// IDs and evidence included, as building from the final objects at once.
func TestOrderDoesntMatter(t *testing.T) {
	final := shop()
	want := snapshotJSON(t, Build(nil, final))

	type op struct {
		obj    Object
		delete bool
		at     float64
	}
	for seed := range uint64(40) {
		rnd := rand.New(rand.NewPCG(seed, 7))
		var ops []op
		for _, o := range final {
			at := rnd.Float64()
			if rnd.IntN(2) == 0 {
				ops = append(ops, op{obj: earlier(o), at: at * rnd.Float64()})
			}
			ops = append(ops, op{obj: o, at: at})
		}
		// Objects that come and go: a pod the api Service selects, using a
		// Secret that doesn't exist, and that Secret for a while.
		for _, temp := range []Object{
			pod("shop", "temp", map[string]string{"app": "api"}, corev1.PodSpec{NodeName: "node-2", Volumes: []corev1.Volume{{
				Name: "t", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: "temp-secret"}},
			}}}),
			secret("shop", "temp-secret"),
			secret("shop", "stripe-key"),
		} {
			from := rnd.Float64()
			ops = append(ops, op{obj: temp, at: from}, op{obj: temp, delete: true, at: from + rnd.Float64()*(1-from)})
		}
		sort.SliceStable(ops, func(i, j int) bool { return ops[i].at < ops[j].at })

		g := New(nil)
		for _, o := range ops {
			if o.delete {
				g.Delete(string(o.obj.Object.(metav1.Object).GetUID()))
			} else {
				g.Upsert(o.obj)
			}
		}
		if got := snapshotJSON(t, g); got != want {
			t.Fatalf("seed %d: the graph differs from one built at once:\n%s", seed, firstDifference(got, want))
		}
	}
}

// earlier makes an older version of an object: other labels, no volumes
// for a pod, another version and time.
func earlier(o Object) Object {
	c := o.Object.DeepCopyObject()
	m, _ := meta.Accessor(c)
	m.SetLabels(map[string]string{"app": "nothing"})
	m.SetResourceVersion("0")
	if p, ok := c.(*corev1.Pod); ok {
		p.Spec.Volumes = nil
		p.Spec.NodeName = "node-0"
	}
	return Object{Resource: o.Resource, Object: c, Seen: o.Seen.Add(-time.Minute)}
}

func TestEdgeID(t *testing.T) {
	a := EdgeID("x", "y", Owns)
	if a != EdgeID("x", "y", Owns) || len(a) != 32 {
		t.Errorf("EdgeID isn't stable, or isn't 16 bytes: %s", a)
	}
	for _, other := range []string{EdgeID("y", "x", Owns), EdgeID("x", "y", References), EdgeID("xy", "", Owns), EdgeID("", "xy", Owns)} {
		if other == a {
			t.Error("two different edges have the same ID")
		}
	}
	if EdgeID("ab", "c", Owns) == EdgeID("a", "bc", Owns) {
		t.Error("parts run together")
	}
}

func TestNodeKindsAreKnown(t *testing.T) {
	for _, k := range NodeKinds {
		if _, ok := resources.Lookup(k); !ok {
			t.Errorf("%s isn't a kind resources knows", k)
		}
	}
	for _, b := range builders {
		if b.Resource != "" && !slices.Contains(NodeKinds, b.Resource) {
			t.Errorf("builder %s reads %s, which the graph doesn't hold", b.Name, b.Resource)
		}
	}
}

// ---- Helpers ----

func snapshotJSON(t *testing.T, g *Graph) string {
	t.Helper()
	b, err := json.MarshalIndent(g.Snapshot(), "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func equalJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func firstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range min(len(g), len(w)) {
		if g[i] != w[i] {
			from := max(0, i-8)
			return "got:\n" + strings.Join(g[from:min(len(g), i+3)], "\n") + "\nwant:\n" + strings.Join(w[from:min(len(w), i+3)], "\n")
		}
	}
	return "lengths differ"
}
