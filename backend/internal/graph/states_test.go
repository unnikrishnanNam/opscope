package graph

import (
	"testing"
	"time"

	"opscope/internal/kube"
	"opscope/internal/snapshot"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodStates(t *testing.T) {
	notReady := readyPod(meta("a", "not-ready"), "")
	notReady.Status.Conditions[0] = corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour))}
	justStarted := readyPod(meta("a", "just-started"), "")
	justStarted.Status.Conditions[0] = corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-time.Minute))}
	completed := corev1.Pod{ObjectMeta: meta("a", "completed"), Status: corev1.PodStatus{Phase: corev1.PodSucceeded,
		ContainerStatuses: []corev1.ContainerStatus{{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}}}}}

	g := build(&snapshot.Snapshot{Pods: []corev1.Pod{
		readyPod(meta("a", "ready"), ""),
		notReady,
		justStarted,
		completed,
		waitingPod(meta("a", "crash"), "CrashLoopBackOff", "back-off 5m0s restarting failed container"),
		waitingPod(age(meta("a", "creating"), time.Minute), "ContainerCreating", ""),
		waitingPod(age(meta("a", "stuck"), time.Hour), "ContainerCreating", ""),
		waitingPod(meta("a", "config"), "CreateContainerConfigError", `secret "stripe" not found`),
	}})

	wantState(t, g, "pods/a/ready", OK)
	wantState(t, g, "pods/a/not-ready", Degraded)
	wantState(t, g, "pods/a/just-started", Progressing)
	wantState(t, g, "pods/a/completed", OK)
	wantState(t, g, "pods/a/crash", Failing)
	wantState(t, g, "pods/a/creating", Progressing)
	wantState(t, g, "pods/a/stuck", Failing)
	if n := node(t, g, "pods/a/config"); n.State != Failing || n.Status != "CreateContainerConfigError" || n.Message != `secret "stripe" not found` {
		t.Errorf("config: %+v", n)
	}
}

func TestWorkloadStates(t *testing.T) {
	healthy := meta("a", "healthy")
	rolling := meta("a", "rolling")
	broken := meta("a", "broken")
	half := meta("a", "half")
	blocked := meta("a", "blocked")
	stuck := meta("a", "stuck")
	stuckDown := meta("a", "stuck-down")

	blockedRS := replicaSet(ownedBy(meta("a", "blocked-1"), "Deployment", blocked), 2, 0)
	blockedRS.Status.Conditions = []appsv1.ReplicaSetCondition{{Type: appsv1.ReplicaSetReplicaFailure, Status: corev1.ConditionTrue,
		Message: `pods "blocked-1-x" is forbidden: exceeded quota: compute`}}
	stuckDeploy := deployment(stuck, 2, 1)
	stuckDeploy.Status.Conditions = []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing, Status: corev1.ConditionFalse,
		Reason: "ProgressDeadlineExceeded", Message: `ReplicaSet "stuck-2" has timed out progressing.`}}
	stuckDownDeploy := deployment(stuckDown, 2, 0)
	stuckDownDeploy.Status.Conditions = stuckDeploy.Status.Conditions

	brokenRS := ownedBy(meta("a", "broken-1"), "Deployment", broken)
	halfRS := ownedBy(meta("a", "half-1"), "Deployment", half)
	rollingRS := ownedBy(meta("a", "rolling-1"), "Deployment", rolling)

	g := build(&snapshot.Snapshot{
		Deployments: []appsv1.Deployment{
			deployment(healthy, 2, 2),
			deployment(rolling, 2, 1),
			deployment(broken, 2, 0),
			deployment(half, 2, 1),
			deployment(blocked, 2, 0),
			stuckDeploy,
			stuckDownDeploy,
		},
		ReplicaSets: []appsv1.ReplicaSet{
			replicaSet(brokenRS, 2, 0), replicaSet(halfRS, 2, 1), replicaSet(rollingRS, 2, 1), blockedRS,
		},
		Pods: []corev1.Pod{
			waitingPod(ownedBy(meta("a", "broken-1-x"), "ReplicaSet", brokenRS), "CrashLoopBackOff", ""),
			waitingPod(ownedBy(meta("a", "broken-1-y"), "ReplicaSet", brokenRS), "CrashLoopBackOff", ""),
			readyPod(ownedBy(meta("a", "half-1-x"), "ReplicaSet", halfRS), ""),
			waitingPod(ownedBy(meta("a", "half-1-y"), "ReplicaSet", halfRS), "ImagePullBackOff", ""),
			readyPod(ownedBy(meta("a", "rolling-1-x"), "ReplicaSet", rollingRS), ""),
			waitingPod(ownedBy(age(meta("a", "rolling-1-y"), time.Minute), "ReplicaSet", rollingRS), "ContainerCreating", ""),
		},
	})

	wantState(t, g, "deployments/a/healthy", OK)
	wantState(t, g, "deployments/a/rolling", Progressing) // a pod still starting
	wantState(t, g, "deployments/a/broken", Failing)      // pods failing, none ready
	wantState(t, g, "deployments/a/half", Degraded)       // one ready, one failing
	wantState(t, g, "deployments/a/blocked", Failing)     // its ReplicaSet can't create pods
	wantState(t, g, "deployments/a/stuck", Degraded)      // a stuck rollout, the old pods still serving
	wantState(t, g, "deployments/a/stuck-down", Failing)  // stuck, and nothing serving
	if n := node(t, g, "deployments/a/blocked"); n.Message != `pods "blocked-1-x" is forbidden: exceeded quota: compute` {
		t.Errorf("the ReplicaSet's reason should explain the Deployment, got %q", n.Message)
	}
	if n := node(t, g, "deployments/a/half"); n.Status != "1/2" {
		t.Errorf("status = %q, want 1/2", n.Status)
	}
	wantState(t, g, "replicasets/a/blocked-1", Failing)
}

func TestScaledToZeroIsOK(t *testing.T) {
	g := build(&snapshot.Snapshot{Deployments: []appsv1.Deployment{deployment(meta("a", "off"), 0, 0)}})
	wantState(t, g, "deployments/a/off", OK)
}

func TestJobAndCronJobStates(t *testing.T) {
	cj := meta("a", "nightly")
	failedJob := batchv1.Job{ObjectMeta: ownedBy(meta("a", "nightly-2"), "CronJob", cj), Status: batchv1.JobStatus{
		Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, Reason: "BackoffLimitExceeded"}},
	}}
	oldJob := batchv1.Job{ObjectMeta: age(ownedBy(meta("a", "nightly-1"), "CronJob", cj), 25*time.Hour), Status: batchv1.JobStatus{
		Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}},
	}}
	g := build(&snapshot.Snapshot{
		CronJobs: []batchv1.CronJob{{ObjectMeta: cj}, {ObjectMeta: meta("a", "paused"), Spec: batchv1.CronJobSpec{Suspend: ptr(true)}}},
		Jobs:     []batchv1.Job{failedJob, oldJob, {ObjectMeta: meta("a", "running")}},
	})
	wantState(t, g, "jobs/a/nightly-2", Failing)
	wantState(t, g, "jobs/a/nightly-1", OK)
	wantState(t, g, "jobs/a/running", Progressing)
	wantState(t, g, "cronjobs/a/nightly", Degraded) // its latest run failed
	wantState(t, g, "cronjobs/a/paused", OK)
}

func TestServiceStates(t *testing.T) {
	service := func(name string) corev1.Service {
		return corev1.Service{ObjectMeta: meta("a", name), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": name}}}
	}
	slice := func(service string, ready ...bool) discoveryv1.EndpointSlice {
		s := discoveryv1.EndpointSlice{ObjectMeta: meta("a", service+"-abc", "kubernetes.io/service-name", service)}
		for _, r := range ready {
			s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: ptr(r)}})
		}
		return s
	}
	leaving := slice("rolling", true)
	leaving.Endpoints = append(leaving.Endpoints, discoveryv1.Endpoint{Conditions: discoveryv1.EndpointConditions{Ready: ptr(false), Terminating: ptr(true)}})

	g := build(&snapshot.Snapshot{
		Services: []corev1.Service{
			service("healthy"), service("half"), service("down"), service("empty"), service("rolling"),
			{ObjectMeta: meta("a", "manual")},
			{ObjectMeta: meta("a", "dns"), Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "example.com"}},
		},
		EndpointSlices: []discoveryv1.EndpointSlice{
			slice("healthy", true, true), slice("half", true, false), slice("down", false), leaving,
		},
	})

	wantState(t, g, "services/a/healthy", OK)
	wantState(t, g, "services/a/half", Degraded)
	wantState(t, g, "services/a/down", Failing)
	wantState(t, g, "services/a/empty", Failing)
	wantState(t, g, "services/a/rolling", OK) // a terminating endpoint isn't a problem
	wantState(t, g, "services/a/manual", OK)  // no selector: endpoints are someone else's job
	wantState(t, g, "services/a/dns", OK)
	if n := node(t, g, "services/a/half"); n.Status != "1/2 ready" {
		t.Errorf("status = %q", n.Status)
	}
}

// Without permission to read EndpointSlices, a Service is judged by the
// readiness of the pods it selects.
func TestServiceStateWithoutEndpointSlices(t *testing.T) {
	s := &snapshot.Snapshot{
		Services: []corev1.Service{{ObjectMeta: meta("a", "web"), Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "web"}}}},
		Pods: []corev1.Pod{
			readyPod(meta("a", "web-1", "app", "web"), ""),
			waitingPod(meta("a", "web-2", "app", "web"), "CrashLoopBackOff", ""),
		},
	}
	unreadable(s, "forbidden", "endpointslices")
	g := build(s)
	if n := node(t, g, "services/a/web"); n.State != Degraded || n.Status != "1/2 ready" {
		t.Errorf("got %s %q", n.State, n.Status)
	}
}

func TestNodeStates(t *testing.T) {
	notReady := readyNode("down")
	notReady.Status.Conditions[0].Status = corev1.ConditionUnknown
	pressure := readyNode("full")
	pressure.Status.Conditions = append(pressure.Status.Conditions, corev1.NodeCondition{Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue})
	cordoned := readyNode("cordoned")
	cordoned.Spec.Unschedulable = true

	g := build(&snapshot.Snapshot{Nodes: []corev1.Node{readyNode("fine"), notReady, pressure, cordoned}})
	wantState(t, g, "nodes/fine", OK)
	wantState(t, g, "nodes/down", Failing)
	wantState(t, g, "nodes/full", Degraded)
	wantState(t, g, "nodes/cordoned", OK)
	if n := node(t, g, "nodes/cordoned"); n.Status != "Ready,SchedulingDisabled" {
		t.Errorf("status = %q", n.Status)
	}
}

func TestClaimStates(t *testing.T) {
	waitForPod := storagev1.VolumeBindingWaitForFirstConsumer
	pending := func(m metav1.ObjectMeta, class string) corev1.PersistentVolumeClaim {
		return corev1.PersistentVolumeClaim{ObjectMeta: m, Spec: corev1.PersistentVolumeClaimSpec{StorageClassName: ptr(class)},
			Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending}}
	}
	usesClaim := corev1.PodSpec{Volumes: []corev1.Volume{{Name: "data", VolumeSource: corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "used"}}}}}

	g := build(&snapshot.Snapshot{
		PersistentVolumeClaims: []corev1.PersistentVolumeClaim{
			pending(meta("a", "unused"), "local"),
			pending(meta("a", "used"), "local"),
			pending(age(meta("a", "new"), time.Minute), "fast"),
			pending(meta("a", "old"), "fast"),
		},
		StorageClasses: []storagev1.StorageClass{
			{ObjectMeta: meta("", "local"), VolumeBindingMode: &waitForPod},
			{ObjectMeta: meta("", "fast")},
		},
		Pods: []corev1.Pod{readyPod(meta("a", "db"), "", usesClaim)},
	})
	wantState(t, g, "persistentvolumeclaims/a/unused", OK) // waits for a pod, and none uses it yet
	wantState(t, g, "persistentvolumeclaims/a/used", Failing)
	wantState(t, g, "persistentvolumeclaims/a/new", Progressing)
	wantState(t, g, "persistentvolumeclaims/a/old", Failing)
}

func TestGatewayAPIStates(t *testing.T) {
	condition := func(t string, status metav1.ConditionStatus) metav1.Condition {
		return metav1.Condition{Type: t, Status: status}
	}
	programmed := kube.Gateway{ObjectMeta: meta("a", "ok")}
	programmed.Status.Conditions = []metav1.Condition{condition("Accepted", "True"), condition("Programmed", "True")}
	unresolved := kube.HTTPRoute{ObjectMeta: meta("a", "broken")}
	unresolved.Status.Parents = []kube.RouteParentStatus{{Conditions: []metav1.Condition{condition("Accepted", "True"), condition("ResolvedRefs", "False")}}}

	g := build(&snapshot.Snapshot{
		Gateways:       []kube.Gateway{programmed, {ObjectMeta: meta("a", "new")}},
		HTTPRoutes:     []kube.HTTPRoute{unresolved},
		GatewayClasses: []kube.GatewayClass{{ObjectMeta: age(meta("", "fresh"), time.Minute)}, {ObjectMeta: meta("", "orphan")}},
	})
	wantState(t, g, "gateways/a/ok", OK)
	wantState(t, g, "gateways/a/new", Progressing)
	wantState(t, g, "httproutes/a/broken", Failing)
	wantState(t, g, "gatewayclasses/fresh", Progressing)
	wantState(t, g, "gatewayclasses/orphan", Failing) // nobody has accepted it in an hour
}

// A pod that has been running but not ready for a long time makes its
// workload unhealthy, as a failing one does.
func TestWorkloadWithPodNotReadyForLong(t *testing.T) {
	ss := meta("a", "db")
	pod := readyPod(ownedBy(meta("a", "db-0"), "StatefulSet", ss), "")
	pod.Status.Conditions[0] = corev1.PodCondition{Type: corev1.PodReady, Status: corev1.ConditionFalse, LastTransitionTime: metav1.NewTime(now.Add(-time.Hour))}
	g := build(&snapshot.Snapshot{
		StatefulSets: []appsv1.StatefulSet{{ObjectMeta: ss, Spec: appsv1.StatefulSetSpec{Replicas: ptr(int32(1))}}},
		Pods:         []corev1.Pod{pod},
	})
	wantState(t, g, "statefulsets/a/db", Failing)
}
