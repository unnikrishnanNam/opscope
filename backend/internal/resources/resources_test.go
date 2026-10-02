package resources

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// meta is a shortcut for building object metadata in tests.
func meta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Namespace: namespace, Name: name}
}

func TestPodStatus(t *testing.T) {
	waiting := func(reason string) corev1.ContainerState {
		return corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason}}
	}
	terminated := func(reason string, code int32) corev1.ContainerState {
		return corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: reason, ExitCode: code}}
	}
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	always := corev1.ContainerRestartPolicyAlways
	yes := true
	now := metav1.Now()

	// Each case is a pod and the status we expect for it.
	tests := []struct {
		name string
		pod  corev1.Pod
		want string
	}{
		{
			name: "running and ready",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: running, Ready: true}},
			}},
			want: "Running",
		},
		{
			name: "crash loop wins over the Running phase",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{{State: waiting("CrashLoopBackOff")}},
			}},
			want: "CrashLoopBackOff",
		},
		{
			name: "finished job pod",
			pod: corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodSucceeded,
				ContainerStatuses: []corev1.ContainerStatus{{State: terminated("Completed", 0)}},
			}},
			want: "Completed",
		},
		{
			name: "evicted",
			pod:  corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted"}},
			want: "Evicted",
		},
		{
			name: "waiting on the second of two init containers",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "a"}, {Name: "b"}}},
				Status: corev1.PodStatus{
					Phase: corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{
						{Name: "a", State: terminated("Completed", 0)},
						{Name: "b", State: running},
					},
				},
			},
			want: "Init:1/2",
		},
		{
			name: "init container failed",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "a"}}},
				Status: corev1.PodStatus{
					Phase:                 corev1.PodPending,
					InitContainerStatuses: []corev1.ContainerStatus{{Name: "a", State: terminated("", 1)}},
				},
			},
			want: "Init:Error",
		},
		{
			name: "running native sidecar doesn't count as initialising",
			pod: corev1.Pod{
				Spec: corev1.PodSpec{InitContainers: []corev1.Container{{Name: "proxy", RestartPolicy: &always}}},
				Status: corev1.PodStatus{
					Phase:                 corev1.PodRunning,
					InitContainerStatuses: []corev1.ContainerStatus{{Name: "proxy", State: running, Started: &yes}},
					ContainerStatuses:     []corev1.ContainerStatus{{State: running, Ready: true}},
				},
			},
			want: "Running",
		},
		{
			name: "being deleted",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning},
			},
			want: "Terminating",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podStatus(&tt.pod); got != tt.want {
				t.Errorf("podStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListPodsFiltersByNamespaceAndSorts(t *testing.T) {
	client := fake.NewClientset(
		&corev1.Pod{ObjectMeta: meta("web", "b"), Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{Ready: true, RestartCount: 2}, {RestartCount: 1}},
		}, Spec: corev1.PodSpec{Containers: []corev1.Container{{}, {}}}},
		&corev1.Pod{ObjectMeta: meta("web", "a")},
		&corev1.Pod{ObjectMeta: meta("db", "c")},
	)

	pods, err := listPods(context.Background(), client, Query{Namespace: "web"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 2 || pods[0].Name != "a" || pods[1].Name != "b" {
		t.Fatalf("expected web/a then web/b, got %+v", pods)
	}
	if b := pods[1]; b.Ready != 1 || b.Containers != 2 || b.Restarts != 3 {
		t.Errorf("unexpected counts for b: %+v", b)
	}

	all, _ := listPods(context.Background(), client, Query{})
	if n := len(all); n != 3 {
		t.Errorf("all namespaces: got %d pods, want 3", n)
	}
}

func TestListDeploymentsDefaultsToOneReplica(t *testing.T) {
	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: meta("web", "api"),
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 1},
	})

	deployments, err := listDeployments(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	d := deployments[0]
	if d.Desired != 1 || d.Ready != 1 {
		t.Errorf("unexpected deployment row: %+v", d)
	}
}

func TestJobStatus(t *testing.T) {
	finishedAt := metav1.NewTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	suspend := true

	tests := []struct {
		name         string
		job          batchv1.Job
		want         string
		wantFinished bool
	}{
		{
			name: "complete",
			job: batchv1.Job{Status: batchv1.JobStatus{
				CompletionTime: &finishedAt,
				Conditions:     []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}},
			}},
			want: "Complete", wantFinished: true,
		},
		{
			name: "failed",
			job: batchv1.Job{Status: batchv1.JobStatus{
				Conditions: []batchv1.JobCondition{{Type: batchv1.JobFailed, Status: corev1.ConditionTrue, LastTransitionTime: finishedAt}},
			}},
			want: "Failed", wantFinished: true,
		},
		{name: "running", job: batchv1.Job{Status: batchv1.JobStatus{Active: 1}}, want: "Running"},
		{name: "suspended", job: batchv1.Job{Spec: batchv1.JobSpec{Suspend: &suspend}}, want: "Suspended"},
		{name: "not started", job: batchv1.Job{}, want: "Pending"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, finished := jobStatus(&tt.job)
			if status != tt.want {
				t.Errorf("status = %q, want %q", status, tt.want)
			}
			if (finished != nil) != tt.wantFinished {
				t.Errorf("finished = %v, want set: %v", finished, tt.wantFinished)
			}
		})
	}
}

func TestListCronJobs(t *testing.T) {
	suspend := true
	tz := "Asia/Kolkata"
	client := fake.NewClientset(&batchv1.CronJob{
		ObjectMeta: meta("ops", "backup"),
		Spec:       batchv1.CronJobSpec{Schedule: "0 3 * * *", Suspend: &suspend, TimeZone: &tz},
		Status:     batchv1.CronJobStatus{Active: []corev1.ObjectReference{{Name: "backup-1"}}},
	})

	cronJobs, err := listCronJobs(context.Background(), client, Query{Namespace: "ops"})
	if err != nil {
		t.Fatal(err)
	}
	cj := cronJobs[0]
	if cj.Schedule != "0 3 * * *" || !cj.Suspended || cj.Active != 1 || cj.TimeZone != tz || cj.LastSchedule != nil {
		t.Errorf("unexpected cronjob row: %+v", cj)
	}
}

func TestEveryListerWorksOnAnEmptyCluster(t *testing.T) {
	clients := Clients{Kube: fake.NewClientset(), Dynamic: fakeDynamic()}
	for name, lister := range Listers {
		if _, err := lister(context.Background(), clients, Query{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestListNodes(t *testing.T) {
	client := fake.NewClientset(&corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "master", Labels: map[string]string{
			"node-role.kubernetes.io/control-plane": "",
			"kubernetes.io/os":                      "linux",
		}},
		Spec: corev1.NodeSpec{Unschedulable: true},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			Addresses:  []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.2"}},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	})

	nodes, err := listNodes(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	n := nodes[0]
	if n.Status != "Ready" || n.Schedulable || n.InternalIP != "10.0.0.2" {
		t.Errorf("unexpected node row: %+v", n)
	}
	if len(n.Roles) != 1 || n.Roles[0] != "control-plane" {
		t.Errorf("roles = %v, want [control-plane]", n.Roles)
	}
	if n.CPU != 2000 || n.Memory != 4*1024*1024*1024 {
		t.Errorf("cpu = %d millicores, memory = %d bytes", n.CPU, n.Memory)
	}
}

func TestNodeWithoutReadyConditionIsUnknown(t *testing.T) {
	if got := nodeStatus(&corev1.Node{}); got != "Unknown" {
		t.Errorf("nodeStatus() = %q, want Unknown", got)
	}
}

func TestListEventsNewestFirst(t *testing.T) {
	older := metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newer := metav1.NewTime(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	client := fake.NewClientset(
		&corev1.Event{ObjectMeta: meta("web", "e1"), Type: "Warning", Reason: "BackOff", LastTimestamp: older, Count: 5,
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-1"}},
		&corev1.Event{ObjectMeta: meta("web", "e2"), Type: "Warning", Reason: "Failed", LastTimestamp: newer},
	)

	events, err := listEvents(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Reason != "Failed" {
		t.Fatalf("expected newest event first, got %+v", events)
	}
	if e := events[1]; e.Object != "Pod/api-1" || e.Count != 5 {
		t.Errorf("unexpected event row: %+v", e)
	}
	if events[0].Count != 1 {
		t.Errorf("an event without a count happened once, got %d", events[0].Count)
	}
}

func TestOverview(t *testing.T) {
	ready := corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}}
	two := int32(2)
	client := fake.NewClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "web"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "a"}, Status: ready},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "b"}},
		&corev1.Pod{ObjectMeta: meta("web", "p1"), Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: meta("web", "p2"), Status: corev1.PodStatus{Phase: corev1.PodRunning}},
		&corev1.Pod{ObjectMeta: meta("web", "p3"), Status: corev1.PodStatus{Phase: corev1.PodPending}},
		&appsv1.Deployment{ObjectMeta: meta("web", "ok"), Status: appsv1.DeploymentStatus{ReadyReplicas: 1}},
		&appsv1.Deployment{ObjectMeta: meta("web", "short"), Spec: appsv1.DeploymentSpec{Replicas: &two},
			Status: appsv1.DeploymentStatus{ReadyReplicas: 1}},
	)

	o, err := GetOverview(context.Background(), Clients{Kube: client, Dynamic: fakeDynamic()}, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if o.Namespaces != 1 || o.Nodes.Total != 2 || o.Nodes.Ready != 1 {
		t.Errorf("unexpected namespaces/nodes: %+v", o)
	}
	if o.Pods.Total != 3 || o.Pods.ByStatus["Running"] != 2 || o.Pods.ByStatus["Pending"] != 1 {
		t.Errorf("unexpected pods: %+v", o.Pods)
	}
	if d := o.Workloads[0]; d.Resource != "deployments" || d.Total != 2 || d.Unhealthy != 1 {
		t.Errorf("unexpected deployments count: %+v", d)
	}
}

func TestSecretListNeverContainsValues(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: meta("web", "db"),
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"password": []byte("hunter2"), "user": []byte("admin")},
	})

	secrets, err := listSecrets(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if s := secrets[0]; s.Type != "Opaque" || len(s.Keys) != 2 || s.Keys[0] != "password" {
		t.Errorf("unexpected secret row: %+v", s)
	}

	// Check the actual JSON the browser would receive.
	body, _ := json.Marshal(secrets)
	if strings.Contains(string(body), "hunter2") || strings.Contains(string(body), "aHVudGVyMg") {
		t.Fatalf("secret value leaked into the list: %s", body)
	}
}

func TestGetSecretValue(t *testing.T) {
	client := fake.NewClientset(&corev1.Secret{
		ObjectMeta: meta("web", "db"),
		Data:       map[string][]byte{"password": []byte("hunter2"), "keystore": {0xff, 0xfe, 0x00}},
	})
	ctx := context.Background()

	text, err := GetSecretValue(ctx, client, "web", "db", "password")
	if err != nil || text.Value != "hunter2" || text.Base64 {
		t.Errorf("password: got %+v, %v", text, err)
	}

	binary, err := GetSecretValue(ctx, client, "web", "db", "keystore")
	if err != nil || !binary.Base64 || binary.Value != "//4A" {
		t.Errorf("keystore: got %+v, %v", binary, err)
	}

	if _, err := GetSecretValue(ctx, client, "web", "db", "nope"); !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("missing key: got %v, want ErrKeyNotFound", err)
	}
	if _, err := GetSecretValue(ctx, client, "web", "gone", "password"); !apierrors.IsNotFound(err) {
		t.Errorf("missing secret: got %v, want a NotFound error", err)
	}
}

func TestListConfigMapsCountsBothKindsOfKeys(t *testing.T) {
	client := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: meta("web", "settings"),
		Data:       map[string]string{"b.conf": "x", "a.conf": "y"},
		BinaryData: map[string][]byte{"logo.png": {1}},
	})

	configMaps, err := listConfigMaps(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if keys := configMaps[0].Keys; strings.Join(keys, ",") != "a.conf,b.conf,logo.png" {
		t.Errorf("keys = %v", keys)
	}
}

func TestListServices(t *testing.T) {
	client := fake.NewClientset(
		&corev1.Service{
			ObjectMeta: meta("web", "api"),
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeNodePort,
				ClusterIP: "10.96.0.10",
				Ports: []corev1.ServicePort{
					{Port: 80, NodePort: 30080, Protocol: corev1.ProtocolTCP},
					{Port: 53, Protocol: corev1.ProtocolUDP},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: meta("web", "lb"),
			Spec:       corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
			Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{IP: "203.0.113.5"}},
			}},
		},
	)

	services, err := listServices(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	if ports := strings.Join(services[0].Ports, " "); ports != "80:30080/TCP 53/UDP" {
		t.Errorf("ports = %q", ports)
	}
	if ips := services[1].ExternalIPs; len(ips) != 1 || ips[0] != "203.0.113.5" {
		t.Errorf("external IPs = %v", ips)
	}
}

func TestListIngresses(t *testing.T) {
	client := fake.NewClientset(&networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   "web",
			Name:        "site",
			Annotations: map[string]string{"kubernetes.io/ingress.class": "nginx"},
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"example.com"}}},
			Rules: []networkingv1.IngressRule{
				{Host: "example.com"}, {Host: "example.com"}, {Host: ""},
			},
		},
	})

	ingresses, err := listIngresses(context.Background(), client, Query{})
	if err != nil {
		t.Fatal(err)
	}
	ing := ingresses[0]
	if ing.Class != "nginx" || !ing.TLS || strings.Join(ing.Hosts, ",") != "example.com,*" {
		t.Errorf("unexpected ingress row: %+v", ing)
	}
}
