package server

import (
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"opscope/internal/resources"
)

// The pages v1.6 gives the kinds v1.5 cached only for the graph:
// ServiceAccounts, a Deployment's ReplicaSets, a Service's endpoints.

func controlledBy(m metav1.ObjectMeta, apiVersion, kind, name string) metav1.ObjectMeta {
	yes := true
	m.OwnerReferences = []metav1.OwnerReference{{APIVersion: apiVersion, Kind: kind, Name: name,
		UID: types.UID("uid-" + m.Namespace + "-" + name), Controller: &yes}}
	return m
}

func rows(t *testing.T, d *resources.Detail, title string) [][]string {
	t.Helper()
	table := tableOf(d, title)
	if table == nil {
		t.Fatalf("no %q table; tables: %+v", title, d.Tables)
	}
	return table.Rows
}

func TestServiceAccountDetail(t *testing.T) {
	no := false
	ci := &corev1.ServiceAccount{ObjectMeta: smeta("shop", "ci"), AutomountServiceAccountToken: &no,
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "regcred"}},
		Secrets:          []corev1.ObjectReference{{Name: "ci-token"}}}
	runsAs := func(account string) corev1.PodSpec { return corev1.PodSpec{ServiceAccountName: account} }
	builder := &appsv1.Deployment{ObjectMeta: smeta("shop", "builder")}
	builder.Spec.Template.Spec = runsAs("ci")
	objects := []runtime.Object{
		ci,
		&corev1.ServiceAccount{ObjectMeta: smeta("shop", "default")},
		&corev1.Secret{ObjectMeta: smeta("shop", "regcred")},
		builder,
		&appsv1.ReplicaSet{ObjectMeta: controlledBy(smeta("shop", "builder-1"), "apps/v1", "Deployment", "builder")},
		&corev1.Pod{ObjectMeta: controlledBy(smeta("shop", "builder-1-a"), "apps/v1", "ReplicaSet", "builder-1"), Spec: runsAs("ci")},
		&corev1.Pod{ObjectMeta: smeta("shop", "one-off"), Spec: runsAs("ci")},
		&corev1.Pod{ObjectMeta: smeta("shop", "plain"), Spec: runsAs("")},
	}

	d := detailFromGraph(t, objects, "serviceaccounts", "shop", "ci")
	if f := fieldOf(d, "Token mounted in pods"); f == nil || f.Value != "no, unless a pod asks for it" {
		t.Errorf("automount field = %+v", f)
	}
	want := [][]string{{"regcred", "pulling images"}, {"ci-token (not found)", "token (older clusters)"}}
	if got := rows(t, d, "Secrets"); !reflect.DeepEqual(got, want) {
		t.Errorf("secrets = %v, want %v", got, want)
	}
	want = [][]string{{"Deployment", "builder", "service account", "1"}, {"Pod", "one-off", "service account", ""}}
	if got := rows(t, d, "Used by"); !reflect.DeepEqual(got, want) {
		t.Errorf("used by = %v, want %v", got, want)
	}

	// The graph has no edges to "default", so its page says what uses it
	// in words rather than showing an empty table.
	d = detailFromGraph(t, objects, "serviceaccounts", "shop", "default")
	if tableOf(d, "Used by") != nil {
		t.Error("default has a Used by table")
	}
	if f := fieldOf(d, "Used by"); f == nil || f.Value != "every pod in shop that names no other account" {
		t.Errorf("default's used by = %+v", f)
	}
	if f := fieldOf(d, "Token mounted in pods"); f == nil || f.Value != "yes (not set)" {
		t.Errorf("default's automount = %+v", f)
	}
}

// made sets when a ReplicaSet was created, that long ago.
func made(rs *appsv1.ReplicaSet, ago time.Duration) *appsv1.ReplicaSet {
	rs.CreationTimestamp = metav1.NewTime(time.Now().Add(-ago))
	return rs
}

func TestDeploymentRevisions(t *testing.T) {
	two, zero := int32(2), int32(0)
	revision := func(m metav1.ObjectMeta, n string) metav1.ObjectMeta {
		m.Annotations = map[string]string{"deployment.kubernetes.io/revision": n}
		return m
	}
	rs := func(name, rev string, desired, running, ready int32) *appsv1.ReplicaSet {
		return &appsv1.ReplicaSet{
			ObjectMeta: revision(controlledBy(smeta("shop", name), "apps/v1", "Deployment", "web"), rev),
			Spec:       appsv1.ReplicaSetSpec{Replicas: &desired},
			Status:     appsv1.ReplicaSetStatus{Replicas: running, ReadyReplicas: ready},
		}
	}
	objects := []runtime.Object{
		&appsv1.Deployment{ObjectMeta: revision(smeta("shop", "web"), "10"), Spec: appsv1.DeploymentSpec{Replicas: &two}},
		made(rs("web-9", "9", 1, 1, 1), 2*time.Hour+time.Minute),       // the rollout to 10 is under way
		made(rs("web-10", "10", 2, 1, 0), 3*time.Minute+5*time.Second), // the current one
		made(rs("web-2", "2", 0, 0, 0), 40*24*time.Hour),
		// Another Deployment's, and one nothing owns: not this one's revisions.
		rs("api-1", "1", 1, 1, 1),
		&appsv1.ReplicaSet{ObjectMeta: smeta("shop", "loose"), Spec: appsv1.ReplicaSetSpec{Replicas: &zero}},
	}
	objects[4].(*appsv1.ReplicaSet).OwnerReferences[0].UID = "uid-shop-api"

	d := detailFromGraph(t, objects, "deployments", "shop", "web")
	want := [][]string{
		{"10", "web-10", "0/2", "current", "3m"},
		{"9", "web-9", "1/1", "old, still running", "2h"},
		{"2", "web-2", "0/0", "old", "40d"},
	}
	if got := rows(t, d, "Revisions"); !reflect.DeepEqual(got, want) {
		t.Errorf("revisions = %v, want %v", got, want)
	}
	if l := tableOf(d, "Revisions").Links[0][1]; l == nil || l.Resource != "replicasets" || l.Name != "web-10" {
		t.Errorf("revision link = %+v", l)
	}
}

func TestReplicaSetDetail(t *testing.T) {
	three := int32(3)
	set := &appsv1.ReplicaSet{
		ObjectMeta: controlledBy(smeta("shop", "web-10"), "apps/v1", "Deployment", "web"),
		Spec: appsv1.ReplicaSetSpec{Replicas: &three, Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "web", Image: "web:10"}}}}},
		Status: appsv1.ReplicaSetStatus{Replicas: 1, ReadyReplicas: 1},
	}
	set.Annotations = map[string]string{"deployment.kubernetes.io/revision": "10"}
	pod := &corev1.Pod{ObjectMeta: controlledBy(smeta("shop", "web-10-a"), "apps/v1", "ReplicaSet", "web-10"),
		Spec: corev1.PodSpec{NodeName: "n1"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}

	d := detailFromGraph(t, []runtime.Object{
		&appsv1.Deployment{ObjectMeta: smeta("shop", "web")}, set, pod, &corev1.Node{ObjectMeta: smeta("", "n1")},
	}, "replicasets", "shop", "web-10")

	if f := fieldOf(d, "Revision"); f == nil || f.Value != "10" {
		t.Errorf("revision = %+v", f)
	}
	if len(d.Containers) != 1 || d.Containers[0].Image != "web:10" {
		t.Errorf("containers = %+v; the page reads the template from the cluster", d.Containers)
	}
	if len(d.Owners) != 1 || d.Owners[0].Resource != "deployments" {
		t.Errorf("owners = %+v", d.Owners)
	}
	if got, want := rows(t, d, "Pods"), [][]string{{"web-10-a", "Running", "n1"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("pods = %v, want %v", got, want)
	}
}

func TestServiceEndpoints(t *testing.T) {
	yes, no := true, false
	tcp := corev1.ProtocolTCP
	port, name := int32(8080), "http"
	node1, zone := "n1", "a"
	sliceMeta := func(name, service string) metav1.ObjectMeta {
		m := controlledBy(smeta("shop", name), "v1", "Service", service)
		m.Labels = map[string]string{"kubernetes.io/service-name": service, "endpointslice.kubernetes.io/managed-by": "endpointslice-controller.k8s.io"}
		return m
	}
	target := func(pod string) *corev1.ObjectReference {
		return &corev1.ObjectReference{Kind: "Pod", Namespace: "shop", Name: pod}
	}
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta:  sliceMeta("web-x1", "web"),
		AddressType: discoveryv1.AddressTypeIPv4,
		Ports:       []discoveryv1.EndpointPort{{Name: &name, Port: &port, Protocol: &tcp}},
		Endpoints: []discoveryv1.Endpoint{
			{Addresses: []string{"10.0.0.1"}, TargetRef: target("web-a"), NodeName: &node1, Zone: &zone,
				Conditions: discoveryv1.EndpointConditions{Ready: &yes}},
			{Addresses: []string{"10.0.0.2"}, TargetRef: target("web-b"),
				Conditions: discoveryv1.EndpointConditions{Ready: &no, Serving: &no}},
			{Addresses: []string{"10.0.0.3"}, TargetRef: target("web-c"),
				Conditions: discoveryv1.EndpointConditions{Ready: &no, Serving: &yes, Terminating: &yes}},
		},
	}
	objects := []runtime.Object{
		&corev1.Service{ObjectMeta: smeta("shop", "web")},
		&corev1.Service{ObjectMeta: smeta("shop", "api")},
		slice,
		// Another Service's slice isn't shown.
		&discoveryv1.EndpointSlice{ObjectMeta: sliceMeta("api-x1", "api"), AddressType: discoveryv1.AddressTypeIPv4,
			Endpoints: []discoveryv1.Endpoint{{Addresses: []string{"10.0.0.9"}}}},
	}

	d := detailFromGraph(t, objects, "services", "shop", "web")
	want := [][]string{
		{"10.0.0.1", "web-a", "Ready", "n1", "a", "http 8080/TCP"},
		{"10.0.0.2", "web-b", "Not ready", "", "", "http 8080/TCP"},
		{"10.0.0.3", "web-c", "Terminating, still serving", "", "", "http 8080/TCP"},
	}
	if got := rows(t, d, "Endpoints"); !reflect.DeepEqual(got, want) {
		t.Errorf("service endpoints = %v, want %v", got, want)
	}
	if l := tableOf(d, "Endpoints").Links[0][1]; l == nil || l.Resource != "pods" || l.Name != "web-a" {
		t.Errorf("endpoint link = %+v", l)
	}

	// The slice's own page: its Service, its endpoints without the ports
	// column, and its ports.
	d = detailFromGraph(t, objects, "endpointslices", "shop", "web-x1")
	if f := fieldOf(d, "Service"); f == nil || f.Link == nil || f.Link.Resource != "services" || f.Link.Name != "web" {
		t.Errorf("service field = %+v", f)
	}
	if f := fieldOf(d, "Address type"); f == nil || f.Value != "IPv4" {
		t.Errorf("address type = %+v", f)
	}
	if got := rows(t, d, "Endpoints"); len(got) != 3 || len(got[0]) != 5 {
		t.Errorf("slice endpoints = %v", got)
	}
	if got, want := rows(t, d, "Ports"), [][]string{{"http", "8080", "TCP", ""}}; !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %v, want %v", got, want)
	}
	if len(d.Owners) != 1 || d.Owners[0].Resource != "services" {
		t.Errorf("owners = %+v", d.Owners)
	}
}
