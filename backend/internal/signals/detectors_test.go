package signals

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"opscope/internal/collector"
)

// Every type in the vocabulary has a case that raises it and one that
// doesn't (the release plan's rule for v1.9's rules, started here). Each
// case is a set of objects; after three minutes, every type's wait is over.

type obj struct {
	resource string
	o        runtime.Object
}

type detectorCase struct {
	name    string
	objects []obj
	// want is "name:type" for every open signal, in any order; the case's
	// other objects must have none.
	want []string
	// fills is volume usage, keyed "namespace/claim".
	fills map[string]Fill
	// unreadable kinds aren't given to the world (a role without Secrets).
	unreadable []string
}

func runCase(t *testing.T, c detectorCase) []Signal {
	t.Helper()
	w := newWorld("lab", Default, t0.Add(-time.Hour))
	for _, k := range Default.Kinds() {
		w.setReadable(k, !slices.Contains(c.unreadable, k))
	}
	for _, o := range c.objects {
		w.upsert(o.resource, o.o, t0)
	}
	if c.fills != nil {
		w.setFills(c.fills, t0)
	}
	w.tick(t0.Add(3 * time.Minute))
	got := View{w, t0}.Signals(Filter{State: Open})
	var names []string
	for _, s := range got {
		names = append(names, s.Resource.Name+":"+s.Type)
		if s.Message == "" || len(s.Evidence) == 0 {
			t.Errorf("%s: no message or evidence: %+v", s.Type, s)
		}
		for _, e := range s.Evidence {
			if e.Source == "" || e.Detector == "" {
				t.Errorf("%s: evidence without its source: %+v", s.Type, e)
			}
		}
	}
	slices.Sort(names)
	want := slices.Clone(c.want)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Errorf("signals %v, want %v", names, want)
	}
	return got
}

func ago(d time.Duration) metav1.Time { return metav1.NewTime(t0.Add(-d)) }

func pod(name string, mutate func(p *corev1.Pod)) *corev1.Pod {
	p := running(name)
	p.Spec.Containers = []corev1.Container{{Name: "app", Image: "shop:1"}}
	mutate(p)
	return p
}

func waiting(reason, message string) corev1.ContainerState {
	return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}}
}

func terminated(reason string, code int32, at time.Duration) *corev1.ContainerStateTerminated {
	return &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code, FinishedAt: ago(at)}
}

func event(about runtime.Object, name, reason, message string, last time.Duration) obj {
	m := about.(metav1.Object)
	kind := map[string]string{"*v1.Pod": "Pod"}[strings.TrimSpace(typeName(about))]
	return obj{"events", &corev1.Event{
		ObjectMeta:     objMeta(m.GetNamespace(), name),
		InvolvedObject: corev1.ObjectReference{Kind: kind, Namespace: m.GetNamespace(), Name: m.GetName(), UID: m.GetUID()},
		Type:           corev1.EventTypeWarning, Reason: reason, Message: message, Count: 3,
		FirstTimestamp: ago(last + 5*time.Minute), LastTimestamp: ago(last),
	}}
}

func typeName(o runtime.Object) string {
	if _, ok := o.(*corev1.Pod); ok {
		return "*v1.Pod"
	}
	return ""
}

func TestPodDetectors(t *testing.T) {
	sidecar := corev1.ContainerRestartPolicyAlways
	notReady := func(p *corev1.Pod) { p.Status.ContainerStatuses[0].Ready = false }
	probing := pod("probing", notReady)
	mounting := pod("mounting", func(p *corev1.Pod) { p.Status.Phase = corev1.PodPending; p.Status.ContainerStatuses = nil })
	pending := pod("pending", func(p *corev1.Pod) {
		p.Status.Phase = corev1.PodPending
		p.Status.ContainerStatuses = nil
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse,
			Reason: corev1.PodReasonUnschedulable, Message: "0/3 nodes are available: 3 Insufficient memory.", LastTransitionTime: ago(time.Hour)}}
	})
	cases := []detectorCase{
		{name: "crash_loop", want: []string{"crash:crash_loop"}, objects: []obj{
			{"pods", pod("crash", func(p *corev1.Pod) {
				p.Status.ContainerStatuses[0].State = waiting("CrashLoopBackOff", "")
				p.Status.ContainerStatuses[0].RestartCount = 7
			})},
			{"pods", pod("restarted-once", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].RestartCount = 1 })},
		}},
		{name: "image_pull_failed", want: []string{"pull:image_pull_failed"}, objects: []obj{
			{"pods", pod("pull", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State = waiting("ImagePullBackOff", "") })},
			{"pods", pod("creating", func(p *corev1.Pod) { p.Status.ContainerStatuses[0].State = waiting("ContainerCreating", "") })},
		}},
		{name: "config_missing", want: []string{"config:config_missing"}, objects: []obj{
			{"pods", pod("config", func(p *corev1.Pod) {
				p.Status.ContainerStatuses[0].State = waiting("CreateContainerConfigError", `secret "db" not found`)
			})},
		}},
		{name: "init_failing", want: []string{"init:init_failing"}, objects: []obj{
			{"pods", pod("init", func(p *corev1.Pod) {
				p.Spec.InitContainers = []corev1.Container{{Name: "migrate"}}
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "migrate", State: waiting("CrashLoopBackOff", "")}}
			})},
			// A native sidecar that crashes is a crash loop, not an init failure.
			{"pods", pod("sidecar", func(p *corev1.Pod) {
				p.Spec.InitContainers = []corev1.Container{{Name: "proxy", RestartPolicy: &sidecar}}
				p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "proxy", Started: new(bool),
					State: corev1.ContainerState{Terminated: terminated("Completed", 0, time.Hour)}}}
			})},
		}},
		{name: "container_error", want: []string{"once:container_error"}, objects: []obj{
			{"pods", pod("once", func(p *corev1.Pod) {
				p.Spec.RestartPolicy = corev1.RestartPolicyNever
				p.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: terminated("Error", 2, time.Minute)}
			})},
			// A Job's pod is its Job's to judge.
			{"pods", pod("job-pod", func(p *corev1.Pod) {
				p.ObjectMeta = controlled(p.ObjectMeta, "batch/v1", "Job", "report")
				p.Spec.RestartPolicy = corev1.RestartPolicyNever
				p.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: terminated("Error", 2, time.Minute)}
			})},
			{"pods", pod("done", func(p *corev1.Pod) {
				p.Spec.RestartPolicy = corev1.RestartPolicyNever
				p.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: terminated("Completed", 0, time.Minute)}
			})},
		}},
		{name: "oom_killed", want: []string{"oom:oom_killed"}, objects: []obj{
			{"pods", pod("oom", func(p *corev1.Pod) {
				p.Spec.Containers[0].Resources.Limits = corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("16Mi")}
				p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{Terminated: terminated("OOMKilled", 137, 10*time.Minute)}
			})},
			// Killed two hours ago, and running since: no longer a signal.
			{"pods", pod("oom-long-ago", func(p *corev1.Pod) {
				p.Status.ContainerStatuses[0].LastTerminationState = corev1.ContainerState{Terminated: terminated("OOMKilled", 137, 2*time.Hour)}
			})},
		}},
		{name: "unschedulable", want: []string{"pending:unschedulable"}, objects: []obj{
			{"pods", pending},
			event(pending, "pending.1", "FailedScheduling", "0/3 nodes are available", time.Minute),
			// The scheduler gave up once, then placed it: that's history.
			{"pods", pod("placed", func(p *corev1.Pod) { p.Spec.NodeName = "n1" })},
			event(pod("placed", func(*corev1.Pod) {}), "placed.1", "FailedScheduling", "0/3 nodes are available", time.Minute),
		}},
		{name: "evicted", want: []string{"evicted:evicted"}, objects: []obj{
			{"pods", pod("evicted", func(p *corev1.Pod) {
				p.Status.Phase = corev1.PodFailed
				p.Status.Reason, p.Status.Message = "Evicted", "The node was low on resource: memory."
			})},
		}},
		{name: "probe_failing", want: []string{"probing:probe_failing"}, objects: []obj{
			{"pods", probing},
			event(probing, "probing.1", "Unhealthy", "Readiness probe failed: connection refused", time.Minute),
			// Old events, and a pod that's ready again, say nothing.
			{"pods", pod("old-probe", notReady)},
			event(pod("old-probe", notReady), "old-probe.1", "Unhealthy", "Readiness probe failed", time.Hour),
			{"pods", pod("ready-again", func(*corev1.Pod) {})},
			event(pod("ready-again", func(*corev1.Pod) {}), "ready-again.1", "Unhealthy", "Liveness probe failed", time.Minute),
		}},
		{name: "mount_failed", want: []string{"mounting:mount_failed"}, objects: []obj{
			{"pods", mounting},
			event(mounting, "mounting.1", "FailedMount", `MountVolume.SetUp failed for volume "data"`, time.Minute),
			{"pods", pod("mounted", func(*corev1.Pod) {})},
			event(pod("mounted", func(*corev1.Pod) {}), "mounted.1", "FailedMount", "it took a while", time.Minute),
		}},
		{name: "a pod being deleted", objects: []obj{
			{"pods", pod("leaving", func(p *corev1.Pod) {
				now := metav1.NewTime(t0)
				p.DeletionTimestamp = &now
				p.Status.ContainerStatuses[0].State = waiting("CrashLoopBackOff", "")
			})},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
}

func TestWorkloadDetectors(t *testing.T) {
	two := int32(2)
	stuck := deployment("stuck", 2, 2, appsv1.DeploymentCondition{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
		Reason: "ProgressDeadlineExceeded", Message: `ReplicaSet "stuck-2" has timed out progressing.`, LastTransitionTime: ago(time.Minute)})
	rs := &appsv1.ReplicaSet{ObjectMeta: controlled(objMeta("shop", "web-2"), "apps/v1", "Deployment", "web"), Spec: appsv1.ReplicaSetSpec{Replicas: &two}}
	quiet := &appsv1.ReplicaSet{ObjectMeta: controlled(objMeta("shop", "api-1"), "apps/v1", "Deployment", "api")}
	failedCreate := func(about metav1.Object, name string, last time.Duration) obj {
		return obj{"events", &corev1.Event{ObjectMeta: objMeta("shop", name), Type: corev1.EventTypeWarning, Reason: "FailedCreate",
			Message:        `pods "web-2-" is forbidden: exceeded quota`,
			InvolvedObject: corev1.ObjectReference{Kind: "ReplicaSet", Namespace: "shop", Name: about.GetName(), UID: about.GetUID()},
			Count:          4, FirstTimestamp: ago(last + time.Minute), LastTimestamp: ago(last)}}
	}
	cases := []detectorCase{
		{name: "replicas_unavailable", want: []string{"short:replicas_unavailable", "none:replicas_unavailable"}, objects: []obj{
			{"deployments", deployment("short", 3, 2)},
			{"deployments", deployment("none", 1, 0)},
			{"deployments", deployment("full", 2, 2)},
			{"deployments", deployment("zero", 0, 0)},
			{"statefulsets", &appsv1.StatefulSet{ObjectMeta: objMeta("shop", "db"), Spec: appsv1.StatefulSetSpec{Replicas: &two},
				Status: appsv1.StatefulSetStatus{ReadyReplicas: 2}}},
		}},
		{name: "rollout_stuck", want: []string{"stuck:rollout_stuck"}, objects: []obj{
			{"deployments", stuck},
			{"deployments", deployment("progressing", 2, 2, appsv1.DeploymentCondition{Type: appsv1.DeploymentProgressing,
				Status: corev1.ConditionTrue, Reason: "NewReplicaSetAvailable"})},
		}},
		{name: "job_failed", want: []string{"report:job_failed"}, objects: []obj{
			{"jobs", &batchv1.Job{ObjectMeta: objMeta("shop", "report"), Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded", Message: "Job has reached the specified backoff limit"}}}}},
			{"jobs", &batchv1.Job{ObjectMeta: objMeta("shop", "backup"), Status: batchv1.JobStatus{Conditions: []batchv1.JobCondition{{
				Type: batchv1.JobComplete, Status: corev1.ConditionTrue}}}}},
		}},
		{name: "create_failed", want: []string{"web-2:create_failed"}, objects: []obj{
			{"replicasets", rs}, failedCreate(rs, "web-2.1", time.Minute),
			{"replicasets", quiet}, failedCreate(quiet, "api-1.1", time.Hour), // an hour old: over
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
}

func service(name string, selector map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: objMeta("shop", name), Spec: corev1.ServiceSpec{Selector: selector}}
}

func labelled(name string, l map[string]string, ready bool) *corev1.Pod {
	p := running(name)
	p.Labels = l
	p.Status.ContainerStatuses[0].Ready = ready
	return p
}

func slice(name, service string, ready ...bool) *discoveryv1.EndpointSlice {
	s := &discoveryv1.EndpointSlice{ObjectMeta: objMeta("shop", name)}
	s.Labels = map[string]string{discoveryv1.LabelServiceName: service}
	for _, r := range ready {
		s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{Addresses: []string{"10.0.0.1"}, Conditions: discoveryv1.EndpointConditions{Ready: &r}})
	}
	return s
}

func gatewayObject(kind, name string, status map[string]any, spec map[string]any) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1", "kind": kind,
		"metadata": map[string]any{"name": name, "namespace": "shop", "uid": "uid-" + name},
		"spec":     spec, "status": status,
	}}
	if kind == "GatewayClass" {
		unstructured.RemoveNestedField(u.Object, "metadata", "namespace")
	}
	return u
}

func condition(typ, status, reason string) map[string]any {
	return map[string]any{"type": typ, "status": status, "reason": reason, "message": reason + " happened",
		"lastTransitionTime": "2026-11-13T09:00:00Z"}
}

func TestNetworkDetectors(t *testing.T) {
	app := map[string]string{"app": "web"}
	pathType := networkingv1.PathTypePrefix
	ingress := func(name, backend string) *networkingv1.Ingress {
		return &networkingv1.Ingress{ObjectMeta: objMeta("shop", name), Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
			IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{Paths: []networkingv1.HTTPIngressPath{{
				Path: "/", PathType: &pathType,
				Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{Name: backend, Port: networkingv1.ServiceBackendPort{Number: 80}}},
			}}}}}}}}
	}
	route := func(name, backend string, parents ...any) *unstructured.Unstructured {
		return gatewayObject("HTTPRoute", name, map[string]any{"parents": parents}, map[string]any{
			"parentRefs": []any{map[string]any{"name": "edge"}},
			"rules":      []any{map[string]any{"backendRefs": []any{map[string]any{"name": backend, "port": int64(80)}}}},
		})
	}
	parent := func(conds ...any) map[string]any {
		return map[string]any{"parentRef": map[string]any{"name": "edge"}, "controllerName": "x", "conditions": conds}
	}
	cases := []detectorCase{
		{name: "selector_matches_nothing", want: []string{"typo:selector_matches_nothing"}, objects: []obj{
			{"services", service("typo", map[string]string{"app": "web-frontend"})},
			{"services", service("manual", nil)}, // no selector: its endpoints are set by hand
			{"services", service("web", app)},
			{"pods", labelled("web-1", app, true)}, {"endpointslices", slice("web-x", "web", true)},
		}},
		{name: "no_ready_endpoints", want: []string{"down:no_ready_endpoints"}, objects: []obj{
			{"services", service("down", map[string]string{"app": "down"})},
			{"pods", labelled("down-1", map[string]string{"app": "down"}, false)},
			{"endpointslices", slice("down-x", "down", false)},
			// One of two ready is enough.
			{"services", service("half", map[string]string{"app": "half"})},
			{"pods", labelled("half-1", map[string]string{"app": "half"}, true)},
			{"pods", labelled("half-2", map[string]string{"app": "half"}, false)},
			{"endpointslices", slice("half-x", "half", true, false)},
		}},
		{name: "backend_missing", want: []string{"old:backend_missing", "store:backend_missing"}, objects: []obj{
			{"services", service("web", nil)},
			{"ingresses", ingress("old", "old-api")},
			{"ingresses", ingress("good", "web")},
			{"httproutes", route("store", "missing-svc")},
			{"httproutes", route("fine", "web")},
		}},
		{name: "route_not_accepted", want: []string{"rejected:route_not_accepted"}, objects: []obj{
			{"services", service("web", nil)},
			{"httproutes", route("rejected", "web", parent(condition("Accepted", "False", "NotAllowedByListeners")))},
			{"httproutes", route("accepted", "web", parent(condition("Accepted", "True", "Accepted"), condition("ResolvedRefs", "True", "ResolvedRefs")))},
		}},
		{name: "gateway_not_programmed", want: []string{"edge:gateway_not_programmed"}, objects: []obj{
			{"gateways", gatewayObject("Gateway", "edge", map[string]any{"conditions": []any{condition("Programmed", "False", "AddressNotAssigned")}}, nil)},
			{"gateways", gatewayObject("Gateway", "lb", map[string]any{"conditions": []any{condition("Programmed", "True", "Programmed")}}, nil)},
		}},
		{name: "gatewayclass_not_accepted", want: []string{"nobody:gatewayclass_not_accepted"}, objects: []obj{
			{"gatewayclasses", gatewayObject("GatewayClass", "nobody", map[string]any{"conditions": []any{condition("Accepted", "False", "InvalidParameters")}}, nil)},
			{"gatewayclasses", gatewayObject("GatewayClass", "nginx", map[string]any{"conditions": []any{condition("Accepted", "True", "Accepted")}}, nil)},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
}

// A pod's labels change which Services select it, and the Services are
// checked again: the trigger.
func TestServicesFollowTheirPods(t *testing.T) {
	w := testWorld()
	w.upsert("services", service("web", map[string]string{"app": "web"}), t0)
	if got := types_(open(w)); !slices.Equal(got, []string{"web:selector_matches_nothing:open"}) {
		t.Fatalf("signals = %v", got)
	}
	w.upsert("pods", labelled("web-1", map[string]string{"app": "web"}, true), t0.Add(time.Second))
	if got := open(w); len(got) != 1 || got[0].State != Resolving {
		t.Fatalf("after the pod came: %v", types_(got))
	}
	w.upsert("endpointslices", slice("web-x", "web", false), t0.Add(2*time.Second))
	if got := types_(open(w)); !slices.Contains(got, "web:no_ready_endpoints:open") {
		t.Errorf("after the slice came: %v", got)
	}
}

func TestStorageAndNodeDetectors(t *testing.T) {
	fast, local := "fast", "local"
	wait := storagev1.VolumeBindingWaitForFirstConsumer
	claim := func(name string, class *string, phase corev1.PersistentVolumeClaimPhase, age time.Duration) *corev1.PersistentVolumeClaim {
		c := &corev1.PersistentVolumeClaim{ObjectMeta: objMeta("shop", name), Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: class},
			Status: corev1.PersistentVolumeClaimStatus{Phase: phase}}
		c.CreationTimestamp = ago(age)
		return c
	}
	pv := func(name string, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
		return &corev1.PersistentVolume{ObjectMeta: objMeta("", name), Status: corev1.PersistentVolumeStatus{Phase: phase},
			Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: "shop", Name: "old"}, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain}}
	}
	node := func(name string, mutate func(n *corev1.Node)) *corev1.Node {
		n := &corev1.Node{ObjectMeta: objMeta("", name), Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
			{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
		}}}
		mutate(n)
		return n
	}
	cases := []detectorCase{
		{name: "pvc_unbound", want: []string{"stuck:pvc_unbound", "lost:pvc_unbound", "used-later:pvc_unbound"}, objects: []obj{
			{"storageclasses", &storagev1.StorageClass{ObjectMeta: objMeta("", "fast")}},
			{"storageclasses", &storagev1.StorageClass{ObjectMeta: objMeta("", "local"), VolumeBindingMode: &wait}},
			{"persistentvolumeclaims", claim("stuck", &fast, corev1.ClaimPending, time.Hour)},
			{"persistentvolumeclaims", claim("lost", &fast, corev1.ClaimLost, time.Hour)},
			{"persistentvolumeclaims", claim("bound", &fast, corev1.ClaimBound, time.Hour)},
			// Its class binds on first use, and no pod uses it yet: waiting, as it should.
			{"persistentvolumeclaims", claim("unused", &local, corev1.ClaimPending, time.Hour)},
			// The same, with a pod that uses it: now it's a problem.
			{"persistentvolumeclaims", claim("used-later", &local, corev1.ClaimPending, time.Hour)},
			{"pods", pod("user", func(p *corev1.Pod) {
				p.Spec.Volumes = []corev1.Volume{{Name: "d", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "used-later"}}}}
			})},
		}},
		{name: "pvc_unbound waits two minutes", objects: []obj{
			{"storageclasses", &storagev1.StorageClass{ObjectMeta: objMeta("", "fast")}},
			{"persistentvolumeclaims", claim("new", &fast, corev1.ClaimPending, -3*time.Minute+time.Second)}, // made at t0+2:59
		}},
		{name: "volume_released", want: []string{"kept:volume_released", "broken:volume_released"}, objects: []obj{
			{"persistentvolumes", pv("kept", corev1.VolumeReleased)},
			{"persistentvolumes", pv("broken", corev1.VolumeFailed)},
			{"persistentvolumes", pv("fine", corev1.VolumeBound)},
		}},
		{name: "volume_nearly_full", want: []string{"full:volume_nearly_full", "inodes:volume_nearly_full"},
			fills: map[string]Fill{
				"shop/full":   {Used: 95, Capacity: 100, InodesUsed: 1, Inodes: 100},
				"shop/inodes": {Used: 1, Capacity: 100, InodesUsed: 99, Inodes: 100},
				"shop/roomy":  {Used: 50, Capacity: 100, InodesUsed: 1, Inodes: 100},
			},
			objects: []obj{
				{"persistentvolumeclaims", claim("full", &fast, corev1.ClaimBound, time.Hour)},
				{"persistentvolumeclaims", claim("inodes", &fast, corev1.ClaimBound, time.Hour)},
				{"persistentvolumeclaims", claim("roomy", &fast, corev1.ClaimBound, time.Hour)},
				{"persistentvolumeclaims", claim("unmeasured", &fast, corev1.ClaimBound, time.Hour)},
			}},
		{name: "nodes", want: []string{"down:node_not_ready", "full:node_pressure", "cordoned:node_unschedulable", "silent:node_not_ready"}, objects: []obj{
			{"nodes", node("down", func(n *corev1.Node) {
				n.Status.Conditions[0] = corev1.NodeCondition{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, Reason: "NodeStatusUnknown", LastTransitionTime: ago(time.Hour)}
			})},
			{"nodes", node("full", func(n *corev1.Node) { n.Status.Conditions[1].Status = corev1.ConditionTrue })},
			{"nodes", node("cordoned", func(n *corev1.Node) { n.Spec.Unschedulable = true })},
			{"nodes", node("silent", func(n *corev1.Node) { n.Status.Conditions = nil })},
			{"nodes", node("fine", func(*corev1.Node) {})},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runCase(t, c) })
	}
}

// selfSigned makes a TLS Secret whose certificate expires at notAfter.
func selfSigned(t *testing.T, name string, notAfter time.Time) *corev1.Secret {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: t0.Add(-365 * 24 * time.Hour), NotAfter: notAfter}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &corev1.Secret{ObjectMeta: objMeta("shop", name), Type: corev1.SecretTypeTLS,
		Data: map[string][]byte{corev1.TLSCertKey: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}}
}

func TestConfigDetectors(t *testing.T) {
	day := 24 * time.Hour
	// The cache keeps only the expiry date (the collector's transform);
	// the detector reads it the same from either.
	cached := selfSigned(t, "cached", t0.Add(3*day))
	cached.Annotations = map[string]string{collector.CertNotAfterAnnotation: t0.Add(3 * day).Format(time.RFC3339)}
	cached.Data = nil

	ref := func(name string, mutate func(*corev1.PodSpec)) *appsv1.Deployment {
		d := deployment(name, 1, 1)
		mutate(&d.Spec.Template.Spec)
		return d
	}
	envFrom := func(kind, name string, optional bool) func(*corev1.PodSpec) {
		return func(s *corev1.PodSpec) {
			from := corev1.EnvFromSource{}
			if kind == "configmaps" {
				from.ConfigMapRef = &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Optional: &optional}
			} else {
				from.SecretRef = &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Optional: &optional}
			}
			s.Containers = []corev1.Container{{Name: "app", EnvFrom: []corev1.EnvFromSource{from}}}
		}
	}
	cases := []detectorCase{
		{name: "cert_expiring", want: []string{"soon:cert_expiring", "urgent:cert_expiring", "expired:cert_expiring", "cached:cert_expiring"}, objects: []obj{
			{"secrets", selfSigned(t, "soon", t0.Add(20*day))},
			{"secrets", selfSigned(t, "urgent", t0.Add(5*day))},
			{"secrets", selfSigned(t, "expired", t0.Add(-day))},
			{"secrets", cached},
			{"secrets", selfSigned(t, "fine", t0.Add(90*day))},
			{"secrets", &corev1.Secret{ObjectMeta: objMeta("shop", "opaque"), Type: corev1.SecretTypeOpaque}},
		}},
		{name: "reference_missing", want: []string{"gone:reference_missing", "lonely:reference_missing"}, objects: []obj{
			{"configmaps", &corev1.ConfigMap{ObjectMeta: objMeta("shop", "present")}},
			{"deployments", ref("gone", envFrom("configmaps", "absent", false))},
			{"deployments", ref("fine", envFrom("configmaps", "present", false))},
			{"deployments", ref("optional", envFrom("configmaps", "absent", true))},
			{"pods", pod("lonely", func(p *corev1.Pod) { envFrom("configmaps", "absent", false)(&p.Spec) })},
			// A Deployment's pod: its Deployment says so, once.
			{"pods", pod("owned", func(p *corev1.Pod) {
				p.ObjectMeta = controlled(p.ObjectMeta, "apps/v1", "ReplicaSet", "gone-1")
				envFrom("configmaps", "absent", false)(&p.Spec)
			})},
		}},
		{name: "reference_missing without the Secret role", unreadable: []string{"secrets"}, objects: []obj{
			{"deployments", ref("maybe", envFrom("secrets", "db", false))},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runCase(t, c)
			for _, s := range got {
				if s.Type == "cert_expiring" {
					want := map[string]string{"soon": Notice, "cached": Warning, "urgent": Warning, "expired": Critical}[s.Resource.Name]
					if s.Severity != want {
						t.Errorf("%s: severity %s, want %s", s.Resource.Name, s.Severity, want)
					}
				}
			}
		})
	}
}

// A missing object appearing resolves the signal: the trigger checks
// what refers to it.
func TestReferencesFollowWhatTheyNeed(t *testing.T) {
	w := testWorld()
	d := deployment("web", 1, 1)
	d.Spec.Template.Spec.Volumes = []corev1.Volume{{Name: "c", VolumeSource: corev1.VolumeSource{
		ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "cfg"}}}}}
	w.upsert("deployments", d, t0)
	if got := types_(open(w)); !slices.Equal(got, []string{"web:reference_missing:open"}) {
		t.Fatalf("signals = %v", got)
	}
	w.upsert("configmaps", &corev1.ConfigMap{ObjectMeta: objMeta("shop", "cfg")}, t0.Add(time.Second))
	if got := open(w); len(got) != 1 || got[0].State != Resolving {
		t.Errorf("after the ConfigMap came: %v", types_(got))
	}
}

// Every type in the vocabulary is raised by some detector, and has a case
// above.
func TestEveryTypeIsDetected(t *testing.T) {
	raised := map[string]bool{}
	for _, d := range Default.Detectors() {
		for _, typ := range d.Types {
			raised[typ] = true
		}
	}
	for _, typ := range Vocabulary {
		if !typ.Reserved && !raised[typ.Name] {
			t.Errorf("nothing raises %s", typ.Name)
		}
	}
}
