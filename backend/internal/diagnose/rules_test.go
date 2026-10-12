package diagnose

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)

var uids atomic.Int64

func meta(namespace, name string, labelPairs ...string) metav1.ObjectMeta {
	m := metav1.ObjectMeta{Namespace: namespace, Name: name, UID: types.UID(fmt.Sprintf("uid-%d", uids.Add(1))),
		CreationTimestamp: metav1.NewTime(now.Add(-time.Hour))}
	for i := 0; i+1 < len(labelPairs); i += 2 {
		if m.Labels == nil {
			m.Labels = map[string]string{}
		}
		m.Labels[labelPairs[i]] = labelPairs[i+1]
	}
	return m
}

func waiting(m metav1.ObjectMeta, image, reason, message string) corev1.Pod {
	return corev1.Pod{ObjectMeta: m,
		Spec: corev1.PodSpec{NodeName: "worker-1", Containers: []corev1.Container{{Name: "app", Image: image}}},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{
			Name: "app", State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}},
		}}},
	}
}

func warning(about metav1.ObjectMeta, reason, message string) corev1.Event {
	return corev1.Event{ObjectMeta: meta(about.Namespace, about.Name+"."+reason), Type: corev1.EventTypeWarning, Reason: reason, Message: message,
		InvolvedObject: corev1.ObjectReference{Name: about.Name, Namespace: about.Namespace, UID: about.UID}, LastTimestamp: metav1.NewTime(now.Add(-time.Minute))}
}

func ready(node string) corev1.Node {
	return corev1.Node{ObjectMeta: meta("", node), Status: corev1.NodeStatus{
		Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4")},
	}}
}

func run(s *snapshot.Snapshot) *Diagnosis {
	s.Taken = now
	s.BuildIndex()
	return Run(s)
}

// only returns an object's single finding, failing the test otherwise.
func only(t *testing.T, d *Diagnosis, id string) *Finding {
	t.Helper()
	findings := d.Findings(id)
	if len(findings) != 1 {
		var titles []string
		for _, f := range findings {
			titles = append(titles, f.Rule+": "+f.Title)
		}
		t.Fatalf("%s: want one finding, got %d: %v", id, len(findings), titles)
	}
	return findings[0]
}

func TestNodeDown(t *testing.T) {
	node := ready("worker-2")
	node.Status.Conditions[0] = corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionUnknown,
		Reason: "NodeStatusUnknown", Message: "Kubelet stopped posting node status.", LastHeartbeatTime: metav1.NewTime(now.Add(-20 * time.Minute))}
	pod := corev1.Pod{ObjectMeta: meta("a", "web"), Spec: corev1.PodSpec{NodeName: "worker-2"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	d := run(&snapshot.Snapshot{Nodes: []corev1.Node{node}, Pods: []corev1.Pod{pod}})

	f := only(t, d, "pods/a/web")
	if f.Rule != "pod.node" || !slices.Equal(f.Blames, []string{"nodes/worker-2"}) {
		t.Errorf("pod: %+v", f)
	}
	n := only(t, d, "nodes/worker-2")
	if n.Title != "Node worker-2 stopped reporting" || !strings.Contains(n.Detail, "20m ago") {
		t.Errorf("node: %q %q", n.Title, n.Detail)
	}
	trace, _ := d.Trace("pods/a/web")
	if r := roots(trace.Steps); len(r) != 1 || r[0].Object != "nodes/worker-2" {
		t.Errorf("the node is the root cause:\n%s", render(trace.Steps, ""))
	}
}

func TestImagePullProblems(t *testing.T) {
	tests := []struct {
		message, want string
	}{
		{`Failed to pull image "x": rpc error: code = NotFound desc = failed to resolve reference "x": not found`, "notFound"},
		{`Failed to pull image "x": failed to resolve reference: dial tcp: lookup registry.example.com: no such host`, "unreachable"},
		{`Failed to pull image "x": failed to authorize: 401 Unauthorized`, "unauthorized"},
		{`Failed to pull image "x": pull access denied, repository does not exist or may require authorization`, "notFoundOrDenied"},
		{`Failed to pull image "x": something new`, ""},
	}
	for _, tt := range tests {
		if got := pullProblem(tt.message); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.message, got, tt.want)
		}
	}
	for image, want := range map[string]string{"nginx": "docker.io", "bitnami/redis:7": "docker.io", "ghcr.io/a/b:1": "ghcr.io", "localhost:5000/app": "localhost:5000"} {
		if got := registryOf(image); got != want {
			t.Errorf("registryOf(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestImageNeedsCredentials(t *testing.T) {
	image := "registry.example.com/team/app:1.2"
	withSecret := waiting(meta("a", "with-secret"), image, "ImagePullBackOff", `Back-off pulling image "`+image+`"`)
	withSecret.Spec.ImagePullSecrets = []corev1.LocalObjectReference{{Name: "regcred"}}
	without := waiting(meta("a", "without"), image, "ErrImagePull", "")
	denied := `Failed to pull image "` + image + `": failed to authorize: failed to fetch anonymous token: unexpected status: 401 Unauthorized`

	d := run(&snapshot.Snapshot{
		Pods:   []corev1.Pod{withSecret, without},
		Events: []corev1.Event{warning(withSecret.ObjectMeta, "Failed", denied), warning(without.ObjectMeta, "Failed", denied)},
	})

	f := only(t, d, "pods/a/with-secret")
	if f.Title != "The registry registry.example.com refuses to give image "+image || !slices.Equal(f.Blames, []string{"secrets/a/regcred"}) {
		t.Errorf("with a missing pull Secret: %q, blames %v", f.Title, f.Blames)
	}
	if f := only(t, d, "pods/a/without"); !strings.Contains(f.Detail, "no imagePullSecrets") || len(f.Blames) != 0 {
		t.Errorf("without one: %q", f.Detail)
	}
}

func TestInvalidImageName(t *testing.T) {
	d := run(&snapshot.Snapshot{Pods: []corev1.Pod{waiting(meta("a", "web"), "Nginx:Latest", "InvalidImageName", `Failed to apply default image tag "Nginx:Latest": couldn't parse image name`)}})
	if f := only(t, d, "pods/a/web"); f.Confidence != Certain || !strings.Contains(f.Title, `"Nginx:Latest" isn't valid`) {
		t.Errorf("got %q (%s)", f.Title, f.Confidence)
	}
}

func TestCrashExitCodes(t *testing.T) {
	crashed := func(name string, init bool, exit int32) corev1.Pod {
		pod := corev1.Pod{ObjectMeta: meta("a", name), Status: corev1.PodStatus{Phase: corev1.PodRunning}}
		status := corev1.ContainerStatus{Name: "app", RestartCount: 4,
			State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
			LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: exit, Reason: "Error"}}}
		if init {
			pod.Spec.InitContainers = []corev1.Container{{Name: "app"}}
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{status}
		} else {
			pod.Spec.Containers = []corev1.Container{{Name: "app"}}
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{status}
		}
		return pod
	}
	d := run(&snapshot.Snapshot{Pods: []corev1.Pod{crashed("missing-command", false, 127), crashed("done", false, 0), crashed("migrate", true, 2)}})

	if f := only(t, d, "pods/a/missing-command"); !strings.Contains(f.Detail, "isn't found in the image") {
		t.Errorf("127: %q", f.Detail)
	}
	if f := only(t, d, "pods/a/done"); f.Title != "Container app exits as soon as it starts" {
		t.Errorf("0: %q", f.Title)
	}
	if f := only(t, d, "pods/a/migrate"); f.Title != "Init container app exits with code 2" {
		t.Errorf("init: %q", f.Title)
	}
}

func TestUnschedulable(t *testing.T) {
	pending := func(name, message string) corev1.Pod {
		pod := corev1.Pod{ObjectMeta: meta("a", name), Status: corev1.PodStatus{Phase: corev1.PodPending, Conditions: []corev1.PodCondition{
			{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: message}}}}
		pod.Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}
		return pod
	}
	claim := corev1.PersistentVolumeClaim{ObjectMeta: meta("a", "data"), Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}

	d := run(&snapshot.Snapshot{
		Nodes: []corev1.Node{ready("worker-1")},
		Pods: []corev1.Pod{
			pending("claim", "0/3 nodes are available: pod has unbound immediate PersistentVolumeClaims. preemption: 0/3 nodes are available: 3 Preemption is not helpful for scheduling."),
			pending("taint", "0/3 nodes are available: 1 node(s) had untolerated taint {node-role.kubernetes.io/control-plane: }, 2 node(s) didn't match Pod's node affinity/selector. preemption: not helpful."),
		},
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{claim},
	})

	if f := only(t, d, "pods/a/claim"); !slices.Equal(f.Blames, []string{"persistentvolumeclaims/a/data"}) || !strings.Contains(f.Detail, "aren't bound") {
		t.Errorf("claim: %q, blames %v", f.Detail, f.Blames)
	}
	want := "1 node has a taint it doesn't tolerate: {node-role.kubernetes.io/control-plane: }. 2 nodes don't match its node selector or affinity."
	if f := only(t, d, "pods/a/taint"); !strings.HasPrefix(f.Detail, want) {
		t.Errorf("taint:\n got %q\nwant %q", f.Detail, want)
	}
}

func TestMountWaitsForClaim(t *testing.T) {
	pod := waiting(meta("a", "db"), "postgres", "ContainerCreating", "")
	pod.Spec.Volumes = []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}
	d := run(&snapshot.Snapshot{
		Pods:                   []corev1.Pod{pod},
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{{ObjectMeta: meta("a", "data"), Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}},
	})
	if f := only(t, d, "pods/a/db"); f.Title != "Volume data can't be mounted" || !strings.Contains(f.Detail, "isn't bound to a volume yet") {
		t.Errorf("got %q %q", f.Title, f.Detail)
	}
}

func TestCreateFailedMessages(t *testing.T) {
	tests := []struct {
		message, title string
		blames         []string
	}{
		{`pods "web-1" is forbidden: violates PodSecurity "restricted:latest": allowPrivilegeEscalation != false (container "app" must set securityContext.allowPrivilegeEscalation=false)`,
			"Namespace a's Pod Security level rejects its pods", []string{"namespaces/a"}},
		{`pods "web-1" is forbidden: failed quota: compute: must specify limits.cpu for: app; limits.memory for: app`,
			"ResourceQuota compute requires every pod to set limits.cpu for: app; limits.memory for: app", []string{"resourcequotas/a/compute"}},
		{`pods "web-1" is forbidden: maximum cpu usage per Container is 1, but limit is 2`,
			"A LimitRange in a rejects its pods", nil},
		{`pods "web-1" is forbidden: a webhook said no`, "Kubernetes refuses to create its pods", nil},
	}
	for _, tt := range tests {
		rs := appsv1.ReplicaSet{ObjectMeta: meta("a", "web"), Spec: appsv1.ReplicaSetSpec{Replicas: ptr(int32(1))},
			Status: appsv1.ReplicaSetStatus{Conditions: []appsv1.ReplicaSetCondition{
				{Type: appsv1.ReplicaSetReplicaFailure, Status: corev1.ConditionTrue, Reason: "FailedCreate", Message: tt.message}}}}
		d := run(&snapshot.Snapshot{
			Namespaces:     []corev1.Namespace{{ObjectMeta: meta("", "a")}},
			ResourceQuotas: []corev1.ResourceQuota{{ObjectMeta: meta("a", "compute")}},
			ReplicaSets:    []appsv1.ReplicaSet{rs},
		})
		f := only(t, d, "replicasets/a/web")
		if f.Title != tt.title || !slices.Equal(f.Blames, tt.blames) {
			t.Errorf("%s:\n got %q %v\nwant %q %v", tt.message, f.Title, f.Blames, tt.title, tt.blames)
		}
	}
}

func TestQuotaDetail(t *testing.T) {
	if got := quotaDetail("limits.memory=2Gi", "limits.memory=320Mi", "limits.memory=1Gi"); !strings.HasPrefix(got, "Each pod asks for 2Gi of limits.memory, and the namespace has used 320Mi of the 1Gi") {
		t.Errorf("got %q", got)
	}
	if got := quotaDetail("cpu=1,memory=1Gi", "cpu=1,memory=1Gi", "cpu=2,memory=2Gi"); !strings.Contains(got, "asks for cpu=1,memory=1Gi") {
		t.Errorf("several resources: %q", got)
	}
}

func TestRouteBackends(t *testing.T) {
	route := kube.HTTPRoute{ObjectMeta: meta("shop", "web")}
	route.Spec.Rules = []kube.HTTPRouteRule{
		{BackendRefs: []kube.BackendRef{{ObjectRef: kube.ObjectRef{Name: "gone"}}}},
		{BackendRefs: []kube.BackendRef{{ObjectRef: kube.ObjectRef{Name: "orders", Namespace: ptr("billing")}}}},
	}
	route.Status.Parents = []kube.RouteParentStatus{{Conditions: []metav1.Condition{
		{Type: "Accepted", Status: "True"}, {Type: "ResolvedRefs", Status: "False", Message: "Service shop/gone not found"}}}}
	d := run(&snapshot.Snapshot{
		HTTPRoutes: []kube.HTTPRoute{route},
		Services:   []corev1.Service{{ObjectMeta: meta("billing", "orders")}},
	})

	var titles []string
	for _, f := range d.Findings("httproutes/shop/web") {
		titles = append(titles, f.Title)
		if !slices.ContainsFunc(f.Evidence, func(e Evidence) bool { return strings.Contains(e.Text, "Service shop/gone not found") }) {
			t.Errorf("%q should carry the route's own condition as evidence", f.Title)
		}
	}
	slices.Sort(titles)
	want := []string{"Requests for Rule 1 can't be served", "Rule 2 isn't allowed to reach its backend in billing"}
	if !slices.Equal(titles, want) {
		t.Errorf("got %q", titles)
	}
	if f := only(t, d, "referencegrants/billing/(none)"); f.Title != "No ReferenceGrant in billing allows it" {
		t.Errorf("grant: %q", f.Title)
	}
}

func TestServicePartlyReady(t *testing.T) {
	readyPod := corev1.Pod{ObjectMeta: meta("a", "web-1", "app", "web"), Status: corev1.PodStatus{Phase: corev1.PodRunning,
		Conditions:        []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
		ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	s := &snapshot.Snapshot{
		Services: []corev1.Service{{ObjectMeta: meta("a", "web"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}}},
		Pods:     []corev1.Pod{readyPod, waiting(meta("a", "web-2", "app", "web"), "busybox", "CrashLoopBackOff", "")},
	}
	s.Skipped = []kube.Skipped{{Resource: "endpointslices", Code: "forbidden"}}
	d := run(s)
	if f := only(t, d, "services/a/web"); f.Title != "1 of the 2 pods it selects aren't ready" || f.Severity != Degraded || !slices.Equal(f.Blames, []string{"pods/a/web-2"}) {
		t.Errorf("got %q (%s), blames %v", f.Title, f.Severity, f.Blames)
	}
}

func TestUnknownSecret(t *testing.T) {
	pod := waiting(meta("a", "web"), "app", "CreateContainerConfigError", `secret "stripe" not found`)
	pod.Spec.Containers[0].EnvFrom = []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "stripe"}}}}
	s := &snapshot.Snapshot{Pods: []corev1.Pod{pod}}
	s.Skipped = []kube.Skipped{{Resource: "secrets", Code: "forbidden"}}
	d := run(s)

	trace, _ := d.Trace("pods/a/web")
	r := roots(trace.Steps)
	if len(r) != 1 || r[0].Rule != "unknown" || r[0].Severity != Info || r[0].Confidence != Possible {
		t.Errorf("without permission to read Secrets, the root is a note that Opscope can't check:\n%s", render(trace.Steps, ""))
	}
}

func TestNearestLabels(t *testing.T) {
	pods := []*corev1.Pod{{ObjectMeta: meta("a", "x", "app", "crash-loop")}, {ObjectMeta: meta("a", "y", "app", "crash-loop")}, {ObjectMeta: meta("a", "z", "app", "frontend")}}
	if got := nearestLabels(map[string]string{"app": "crashloop"}, pods); got != "Did you mean app=crash-loop? 2 pods have that label." {
		t.Errorf("got %q", got)
	}
	if got := nearestLabels(map[string]string{"app": "database"}, pods); got != "" {
		t.Errorf("nothing close: got %q", got)
	}
}

// A trace stops at a cycle instead of looping.
func TestTraceCycle(t *testing.T) {
	s := &snapshot.Snapshot{Pods: []corev1.Pod{{ObjectMeta: meta("a", "x")}, {ObjectMeta: meta("a", "y")}}}
	s.Taken = now
	s.BuildIndex()
	d := Run(s)
	d.byObject["pods/a/x"] = []*Finding{{Rule: "test", Object: "pods/a/x", Title: "x", Blames: []string{"pods/a/y"}}}
	d.byObject["pods/a/y"] = []*Finding{{Rule: "test", Object: "pods/a/y", Title: "y", Blames: []string{"pods/a/x"}}}

	trace, _ := d.Trace("pods/a/x")
	if len(trace.Steps) != 1 || len(trace.Steps[0].Causes) != 1 || !trace.Steps[0].Causes[0].Root {
		t.Errorf("got:\n%s", render(trace.Steps, ""))
	}
}

// Every command a finding suggests is read-only: Opscope never offers one
// that changes the cluster, not even as text to copy.
func TestCommandsAreReadOnly(t *testing.T) {
	d := Run(loadScenarios(t))
	commands := 0
	for _, findings := range d.byObject {
		for _, f := range findings {
			for _, step := range f.Next {
				if step.Command == "" {
					continue
				}
				commands++
				fields := strings.Fields(step.Command)
				if len(fields) < 2 || fields[0] != "kubectl" || !slices.Contains(readOnlyVerbs, fields[1]) {
					t.Errorf("%s suggests %q", f.Rule, step.Command)
				}
			}
		}
	}
	if commands == 0 {
		t.Error("no commands were checked")
	}
}

func ptr[T any](v T) *T { return &v }

func titles(d *Diagnosis, id string) []string {
	var out []string
	for _, f := range d.Findings(id) {
		out = append(out, f.Title)
	}
	return out
}

func TestNodePressureAndCordon(t *testing.T) {
	node := ready("worker-1")
	node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue, Message: "kubelet has disk pressure"})
	node.Spec.Unschedulable = true
	d := run(&snapshot.Snapshot{Nodes: []corev1.Node{node}})

	got := titles(d, "nodes/worker-1")
	want := []string{"Node worker-1 is low on disk space", "Node worker-1 is cordoned"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
	if f := d.Findings("nodes/worker-1")[1]; f.Severity != Info {
		t.Errorf("cordoned is a note, not a fault: %s", f.Severity)
	}
}

func TestTemplateChanges(t *testing.T) {
	rs := func(image string, env ...corev1.EnvVar) *appsv1.ReplicaSet {
		r := &appsv1.ReplicaSet{}
		r.Spec.Template.Spec.Containers = []corev1.Container{{Name: "app", Image: image, Env: env}, {Name: "proxy", Image: "envoy"}}
		return r
	}
	before := rs("api:1.4")
	after := rs("api:1.5", corev1.EnvVar{Name: "DEBUG", Value: "1"})
	after.Spec.Template.Spec.Containers = after.Spec.Template.Spec.Containers[:1] // proxy removed

	want := []string{"the image of container app (api:1.4 → api:1.5)", "the environment of container app", "removed container proxy"}
	if got := templateChanges(before, after); !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
	same := rs("api:1.4")
	same.Spec.Template.Spec.TerminationGracePeriodSeconds = ptr(int64(5))
	if got := templateChanges(before, same); !slices.Equal(got, []string{"other settings of its pods"}) {
		t.Errorf("got %q", got)
	}
}

// A Deployment created with a broken spec has one revision: it isn't a
// rollout, whatever its counts say.
func TestFirstRevisionIsNotARollout(t *testing.T) {
	d := meta("a", "web")
	rs := meta("a", "web-1")
	rs.OwnerReferences = []metav1.OwnerReference{{Kind: "Deployment", Name: "web", UID: d.UID}}
	rs.Annotations = map[string]string{revisionAnnotation: "1"}
	diag := run(&snapshot.Snapshot{
		Deployments: []appsv1.Deployment{{ObjectMeta: d, Spec: appsv1.DeploymentSpec{Replicas: ptr(int32(1))}}},
		ReplicaSets: []appsv1.ReplicaSet{{ObjectMeta: rs, Spec: appsv1.ReplicaSetSpec{Replicas: ptr(int32(1))}}},
	})
	for _, f := range diag.Findings("deployments/a/web") {
		if f.Rule == "deployment.rollout" {
			t.Errorf("got %q", f.Title)
		}
	}
}

func TestStatefulSetWaitsInOrder(t *testing.T) {
	ss := meta("a", "db")
	pod := waiting(meta("a", "db-0"), "postgres", "CrashLoopBackOff", "")
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: "StatefulSet", Name: "db", UID: ss.UID}}
	d := run(&snapshot.Snapshot{
		StatefulSets: []appsv1.StatefulSet{{ObjectMeta: ss, Spec: appsv1.StatefulSetSpec{Replicas: ptr(int32(3))}, Status: appsv1.StatefulSetStatus{Replicas: 1}}},
		Pods:         []corev1.Pod{pod},
	})
	f := d.Findings("statefulsets/a/db")[0]
	if !strings.Contains(f.Detail, "db-1 won't be created until db-0 is") {
		t.Errorf("got %q", f.Detail)
	}
}

func TestRouteNotAccepted(t *testing.T) {
	route := kube.HTTPRoute{ObjectMeta: meta("shop", "web")}
	route.Spec.ParentRefs = []kube.ObjectRef{{Name: "edge", Namespace: ptr("infra")}, {Name: "gone"}}
	route.Status.Parents = []kube.RouteParentStatus{{ParentRef: kube.ObjectRef{Name: "edge", Namespace: ptr("infra")},
		Conditions: []metav1.Condition{{Type: "Accepted", Status: "False", Reason: "NotAllowedByListeners", Message: "no listener allows it"}}}}
	d := run(&snapshot.Snapshot{HTTPRoutes: []kube.HTTPRoute{route}, Gateways: []kube.Gateway{{ObjectMeta: meta("infra", "edge")}}})

	got := map[string][]string{}
	for _, f := range d.Findings("httproutes/shop/web") {
		got[f.Title] = f.Blames
	}
	if !slices.Equal(got["Gateway edge doesn't accept it"], []string{"gateways/infra/edge"}) {
		t.Errorf("got %v", got)
	}
	if !slices.Equal(got["Its Gateway shop/gone doesn't exist"], []string{"gateways/shop/gone"}) {
		t.Errorf("got %v", got)
	}
}

func TestGatewayAndClass(t *testing.T) {
	orphan := kube.Gateway{ObjectMeta: meta("infra", "orphan")}
	orphan.Spec.GatewayClassName = "nobody"
	broken := kube.Gateway{ObjectMeta: meta("infra", "broken")}
	broken.Spec.GatewayClassName = "nginx"
	broken.Status.Conditions = []metav1.Condition{{Type: "Accepted", Status: "True"}, {Type: "Programmed", Status: "False", Reason: "AddressNotAssigned", Message: "no address"}}
	accepted := kube.GatewayClass{ObjectMeta: meta("", "nginx")}
	accepted.Status.Conditions = []metav1.Condition{{Type: "Accepted", Status: "True"}}
	stale := kube.GatewayClass{ObjectMeta: meta("", "stale")}
	stale.Spec.ControllerName = "example.com/gateway"

	d := run(&snapshot.Snapshot{Gateways: []kube.Gateway{orphan, broken}, GatewayClasses: []kube.GatewayClass{accepted, stale}})
	if got := titles(d, "gateways/infra/orphan"); !slices.Equal(got, []string{"Its GatewayClass nobody doesn't exist"}) {
		t.Errorf("orphan: %q", got)
	}
	if got := titles(d, "gateways/infra/broken"); !slices.Equal(got, []string{"It isn't serving: its controller couldn't set it up"}) {
		t.Errorf("broken: %q", got)
	}
	if f := d.Findings("gatewayclasses/stale"); len(f) != 1 || !strings.Contains(f[0].Detail, "example.com/gateway") {
		t.Errorf("stale: %+v", f)
	}
}

func TestClaimProblems(t *testing.T) {
	pending := func(name string, class *string) corev1.PersistentVolumeClaim {
		return corev1.PersistentVolumeClaim{ObjectMeta: meta("a", name), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: class},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	}
	lost := corev1.PersistentVolumeClaim{ObjectMeta: meta("a", "lost"), Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "pv-9"},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimLost}}
	failing := pending("failing", ptr("fast"))
	d := run(&snapshot.Snapshot{
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{pending("classless", nil), failing, lost},
		StorageClasses:         []storagev1.StorageClass{{ObjectMeta: meta("", "fast")}},
		Events:                 []corev1.Event{warning(failing.ObjectMeta, "ProvisioningFailed", "failed to provision volume: quota exceeded")},
	})
	if got := titles(d, "persistentvolumeclaims/a/classless"); !slices.Equal(got, []string{"It names no StorageClass, and the cluster has no default"}) {
		t.Errorf("classless: %q", got)
	}
	if f := d.Findings("persistentvolumeclaims/a/failing"); len(f) != 1 || f[0].Title != "Its volume can't be provisioned" || f[0].Confidence != Likely {
		t.Errorf("failing: %+v", f)
	}
	if got := titles(d, "persistentvolumeclaims/a/lost"); !slices.Equal(got, []string{"Its volume pv-9 is gone"}) {
		t.Errorf("lost: %q", got)
	}
}

func TestAutoscalerAtMaximum(t *testing.T) {
	hpa := autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: meta("a", "web"), Spec: autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 5,
		ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"}},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
			{Type: autoscalingv2.ScalingLimited, Status: corev1.ConditionTrue, Reason: "TooManyReplicas"}}}}
	d := run(&snapshot.Snapshot{HorizontalPodAutoscalers: []autoscalingv2.HorizontalPodAutoscaler{hpa}})
	got := titles(d, "horizontalpodautoscalers/a/web")
	if !slices.Equal(got, []string{"What it scales, Deployment web, doesn't exist", "It's at its maximum of 5 replicas"}) {
		t.Errorf("got %q", got)
	}
}

// What no rule explains is said so, with its unhealthy neighbours as
// possibly related, never as causes.
func TestUnexplained(t *testing.T) {
	node := ready("worker-1")
	node.Status.Conditions[0].Status = corev1.ConditionFalse
	pod := corev1.Pod{ObjectMeta: meta("a", "odd"), Spec: corev1.PodSpec{NodeName: "worker-1"}, Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Shutdown"}}
	svc := corev1.Service{ObjectMeta: meta("a", "odd", "app", "odd"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "odd"}}}
	pod.Labels = map[string]string{"app": "odd"}
	s := &snapshot.Snapshot{Nodes: []corev1.Node{node}, Pods: []corev1.Pod{pod}, Services: []corev1.Service{svc}}
	s.Skipped = []kube.Skipped{{Resource: "endpointslices", Code: "forbidden"}}
	d := run(s)

	var f *Finding
	for _, finding := range d.Findings("pods/a/odd") {
		if finding.Rule == "unexplained" {
			f = finding
		}
	}
	if f != nil {
		t.Fatalf("the pod's node is down: pod.node explains it, got %q", f.Title)
	}

	// Without the node down, nothing explains it.
	node.Status.Conditions[0].Status = corev1.ConditionTrue
	s = &snapshot.Snapshot{Nodes: []corev1.Node{node}, Pods: []corev1.Pod{pod}, Services: []corev1.Service{svc}}
	s.Skipped = []kube.Skipped{{Resource: "endpointslices", Code: "forbidden"}}
	d = run(s)
	f = only(t, d, "pods/a/odd")
	if f.Rule != "unexplained" || f.Confidence != Possible || len(f.Blames) != 0 || !slices.Equal(f.Related, []string{"services/a/odd"}) {
		t.Errorf("got %+v", f)
	}
}

// A node that's down is a problem in every namespace with pods on it.
func TestProblemsByNamespace(t *testing.T) {
	node := ready("worker-2")
	node.Status.Conditions[0].Status = corev1.ConditionFalse
	running := func(ns, name string) corev1.Pod {
		return corev1.Pod{ObjectMeta: meta(ns, name), Spec: corev1.PodSpec{NodeName: "worker-2"}, Status: corev1.PodStatus{Phase: corev1.PodRunning}}
	}
	unscheduled := waiting(meta("other", "x"), "nope", "InvalidImageName", "")
	unscheduled.Spec.NodeName = ""
	d := run(&snapshot.Snapshot{
		Nodes: []corev1.Node{node},
		Pods:  []corev1.Pod{running("shop", "web"), running("billing", "api"), unscheduled},
	})

	shop := d.Problems("shop")
	if len(shop) != 1 || shop[0].Rule != "node.not-ready" || len(shop[0].Affects) != 2 {
		t.Fatalf("shop: %+v", shop)
	}
	if all := d.Problems(""); len(all) != 2 {
		t.Errorf("the whole cluster has 2 problems, got %d", len(all))
	}
	if other := d.Problems("other"); len(other) != 1 || other[0].Rule != "pod.image" {
		t.Errorf("other: %+v", other)
	}
}

// A container that restarted long ago (a node reboot) and runs fine since
// isn't a problem; one that restarted minutes ago may be.
func TestOldRestartsAreIgnored(t *testing.T) {
	restarted := func(name string, ago time.Duration, reason string, code int32) corev1.Pod {
		return corev1.Pod{ObjectMeta: meta("a", name), Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
			Status: corev1.PodStatus{Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{{Name: "app", Ready: true, RestartCount: 1,
					State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
						ExitCode: code, Reason: reason, FinishedAt: metav1.NewTime(now.Add(-ago))}}}}}}
	}
	d := run(&snapshot.Snapshot{Pods: []corev1.Pod{
		restarted("long-ago", 11*time.Hour, "Error", 1),
		// The machine restarted three minutes ago: the runtime never saw
		// the container stop, so it says Unknown, 255. Not a crash.
		restarted("rebooted", 3*time.Minute, "Unknown", 255),
		restarted("flapping", 3*time.Minute, "Error", 1),
	}})
	for _, name := range []string{"long-ago", "rebooted"} {
		if f := d.Findings("pods/a/" + name); len(f) != 0 {
			t.Errorf("%s: %q", name, titles(d, "pods/a/"+name))
		}
	}
	if got := titles(d, "pods/a/flapping"); !slices.Equal(got, []string{"Container app exits with code 1"}) {
		t.Errorf("flapping: %q", got)
	}
}
