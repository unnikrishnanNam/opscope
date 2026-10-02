package resources

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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

	result, err := listPods(context.Background(), client, "web")
	if err != nil {
		t.Fatal(err)
	}
	pods := result.([]Pod)
	if len(pods) != 2 || pods[0].Name != "a" || pods[1].Name != "b" {
		t.Fatalf("expected web/a then web/b, got %+v", pods)
	}
	if b := pods[1]; b.Ready != 1 || b.Containers != 2 || b.Restarts != 3 {
		t.Errorf("unexpected counts for b: %+v", b)
	}

	all, _ := listPods(context.Background(), client, "")
	if n := len(all.([]Pod)); n != 3 {
		t.Errorf("all namespaces: got %d pods, want 3", n)
	}
}

func TestListDeploymentsDefaultsToOneReplica(t *testing.T) {
	client := fake.NewClientset(&appsv1.Deployment{
		ObjectMeta: meta("web", "api"),
		Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1, UpdatedReplicas: 1},
	})

	result, err := listDeployments(context.Background(), client, "")
	if err != nil {
		t.Fatal(err)
	}
	d := result.([]Deployment)[0]
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

	result, err := listCronJobs(context.Background(), client, "ops")
	if err != nil {
		t.Fatal(err)
	}
	cj := result.([]CronJob)[0]
	if cj.Schedule != "0 3 * * *" || !cj.Suspended || cj.Active != 1 || cj.TimeZone != tz || cj.LastSchedule != nil {
		t.Errorf("unexpected cronjob row: %+v", cj)
	}
}

func TestEveryListerWorksOnAnEmptyCluster(t *testing.T) {
	client := fake.NewClientset()
	for name, lister := range Listers {
		if _, err := lister(context.Background(), client, ""); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
