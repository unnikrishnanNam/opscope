package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/cache"
)

// BenchmarkCacheMemory reports how much memory the watch cache holds per
// 1,000 objects of each kind, after the transform: the numbers behind the
// sizing table in docs/deployment.md.
//
//	go test ./internal/collector -run '^$' -bench CacheMemory -benchtime 3x
//
// The objects are made up to look like a typical production cluster's (see
// the make* functions), then written as JSON and read back, so every string
// is its own allocation as it is when client-go decodes a response.
//
// To measure a real cluster's objects instead, save them with kubectl and
// point OPSCOPE_BENCH_DIR at the folder:
//
//	kubectl get pods -A -o json > dir/pods.json   # likewise for the other kinds
func BenchmarkCacheMemory(b *testing.B) {
	for _, k := range memoryKinds {
		b.Run(k.resource, func(b *testing.B) {
			objects := k.objects(b)
			var total float64
			for range b.N {
				total += cachedBytes(b, objects)
			}
			b.ReportMetric(total/float64(b.N)/float64(len(objects))*1000/1024, "KiB/1000")
			b.ReportMetric(0, "ns/op") // time isn't what's measured
		})
	}
}

// cachedBytes fills a fresh cache with copies of the objects, as an
// informer does, and returns how much the heap grew.
func cachedBytes(b *testing.B, objects [][]byte) float64 {
	b.Helper()
	store := cache.NewIndexer(cache.MetaNamespaceKeyFunc, namespaceIndex)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for _, data := range objects {
		obj := decode(b, data)
		_, _ = trim(obj)
		if err := store.Add(obj); err != nil {
			b.Fatal(err)
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(store)
	return float64(after.HeapAlloc) - float64(before.HeapAlloc)
}

// decode reads one object's JSON into its typed struct, as client-go does.
func decode(b *testing.B, data []byte) k8sruntime.Object {
	var head metav1.TypeMeta
	if err := json.Unmarshal(data, &head); err != nil {
		b.Fatal(err)
	}
	obj := newObject(head.Kind)
	if obj == nil {
		b.Fatalf("no type for kind %q", head.Kind)
	}
	if err := json.Unmarshal(data, obj); err != nil {
		b.Fatal(err)
	}
	return obj
}

func newObject(kind string) k8sruntime.Object {
	switch kind {
	case "Pod":
		return &corev1.Pod{}
	case "Event":
		return &corev1.Event{}
	case "ConfigMap":
		return &corev1.ConfigMap{}
	case "Secret":
		return &corev1.Secret{}
	case "Service":
		return &corev1.Service{}
	case "Node":
		return &corev1.Node{}
	case "Namespace":
		return &corev1.Namespace{}
	case "PersistentVolumeClaim":
		return &corev1.PersistentVolumeClaim{}
	case "Deployment":
		return &appsv1.Deployment{}
	case "Job":
		return &batchv1.Job{}
	}
	return nil
}

type memoryKind struct {
	resource string
	kind     string
	make     func(i int) k8sruntime.Object
}

// The kinds measured: the ones a cluster has most of, and one of each
// shape. StatefulSets and DaemonSets are close to Deployments, CronJobs to
// Jobs, PersistentVolumes to claims.
var memoryKinds = []memoryKind{
	{"pods", "Pod", makePod},
	{"events", "Event", makeEvent},
	{"configmaps", "ConfigMap", makeConfigMap},
	{"secrets", "Secret", makeSecret},
	{"deployments", "Deployment", makeDeployment},
	{"services", "Service", makeService},
	{"jobs", "Job", makeJob},
	{"persistentvolumeclaims", "PersistentVolumeClaim", makeClaim},
	{"namespaces", "Namespace", makeNamespace},
	{"nodes", "Node", makeNode},
}

// objects returns the JSON of 2,000 made-up objects, or of every object in
// OPSCOPE_BENCH_DIR/<resource>.json when that's set.
func (k memoryKind) objects(b *testing.B) [][]byte {
	if dir := os.Getenv("OPSCOPE_BENCH_DIR"); dir != "" {
		return realObjects(b, filepath.Join(dir, k.resource+".json"), k.kind)
	}
	out := make([][]byte, 2000)
	for i := range out {
		obj := k.make(i)
		obj.GetObjectKind().SetGroupVersionKind(gvkOf(k.kind))
		data, err := json.Marshal(obj)
		if err != nil {
			b.Fatal(err)
		}
		out[i] = data
	}
	return out
}

func gvkOf(kind string) schema.GroupVersionKind {
	switch kind {
	case "Deployment":
		return appsv1.SchemeGroupVersion.WithKind(kind)
	case "Job":
		return batchv1.SchemeGroupVersion.WithKind(kind)
	}
	return corev1.SchemeGroupVersion.WithKind(kind)
}

// realObjects splits the items out of `kubectl get <kind> -A -o json`.
func realObjects(b *testing.B, path, kind string) [][]byte {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b.Skipf("no %s", path)
	}
	if err != nil {
		b.Fatal(err)
	}
	var list struct{ Items []json.RawMessage }
	if err := json.Unmarshal(data, &list); err != nil {
		b.Fatal(err)
	}
	if len(list.Items) == 0 {
		b.Skipf("%s has no items", path)
	}
	out := make([][]byte, len(list.Items))
	for i, item := range list.Items {
		// kubectl leaves the kind off items of a typed list.
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err != nil {
			b.Fatal(err)
		}
		obj["kind"] = kind
		out[i], _ = json.Marshal(obj)
	}
	return out
}

// What follows makes objects like those of a typical production cluster:
// workloads with a couple of containers, a service mesh's or monitoring's
// labels and annotations, three or four field managers each, and the
// annotation `kubectl apply` leaves on what it applied.

var (
	created      = metav1.NewTime(time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC))
	apps         = []string{"checkout", "cart", "catalog", "payments", "search", "accounts", "orders", "shipping"}
	nsOf         = func(i int) string { return fmt.Sprintf("team-%02d", i%40) }
	appOf        = func(i int) string { return apps[i%len(apps)] }
	templateHash = func(i int) string { return fmt.Sprintf("%010x", 0x5d7f8b6c4+i*7919)[:10] }
	suffixOf     = func(i int) string { return fmt.Sprintf("%05x", 0x2b9c1+i*104729)[:5] }
)

func managed(managers ...string) []metav1.ManagedFieldsEntry {
	out := make([]metav1.ManagedFieldsEntry, len(managers))
	for i, m := range managers {
		op, sub := metav1.ManagedFieldsOperationUpdate, ""
		if strings.HasSuffix(m, "/status") {
			m, sub = strings.TrimSuffix(m, "/status"), "status"
		}
		out[i] = metav1.ManagedFieldsEntry{
			Manager: m, Operation: op, APIVersion: "v1", Time: &created, Subresource: sub,
			FieldsType: "FieldsV1",
			// Dropped by the transform; here so the input is the right size.
			FieldsV1: &metav1.FieldsV1{Raw: []byte(`{"f:metadata":{"f:labels":{".":{},"f:app":{},"f:version":{}}},"f:spec":{"f:replicas":{},"f:template":{"f:spec":{"f:containers":{"k:{\"name\":\"app\"}":{".":{},"f:image":{},"f:env":{}}}}}}}`)},
		}
	}
	return out
}

func objectMeta(name, namespace string, i int, labels map[string]string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name: name, Namespace: namespace,
		UID:               types.UID(fmt.Sprintf("6f1c%04x-2b7e-4c1a-9d3f-%012x", i%65536, i*2654435761)),
		ResourceVersion:   fmt.Sprint(48213977 + i),
		Generation:        3,
		CreationTimestamp: created,
		Labels:            labels,
	}
}

// lastApplied is the annotation `kubectl apply` (and Argo CD, by default)
// leaves: the whole object as it was applied, in one string.
func lastApplied(obj any) map[string]string {
	data, _ := json.Marshal(obj)
	return map[string]string{lastAppliedAnnotation: string(data) + "\n"}
}

func appLabels(i int) map[string]string {
	return map[string]string{
		"app":                          appOf(i),
		"app.kubernetes.io/name":       appOf(i),
		"app.kubernetes.io/part-of":    "storefront",
		"app.kubernetes.io/managed-by": "kustomize",
		"version":                      "v2.14.3",
	}
}

func container(name, app string, i int) corev1.Container {
	return corev1.Container{
		Name:  name,
		Image: fmt.Sprintf("registry.example.com/storefront/%s:2.14.3", app),
		Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: "TCP"}, {Name: "metrics", ContainerPort: 9090, Protocol: "TCP"}},
		Env: []corev1.EnvVar{
			{Name: "LOG_LEVEL", Value: "info"},
			{Name: "PORT", Value: "8080"},
			{Name: "DATABASE_HOST", Value: app + "-db.data.svc.cluster.local"},
			{Name: "CACHE_URL", Value: "redis://redis.data.svc.cluster.local:6379/0"},
			{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: "http://otel-collector.observability:4317"},
			{Name: "POD_NAME", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"}}},
			{Name: "DATABASE_PASSWORD", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: app + "-db"}, Key: "password"}}},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("512Mi")},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: "/etc/" + app},
			{Name: "kube-api-access-" + suffixOf(i), MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
		},
		LivenessProbe:            &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromString("http"), Scheme: "HTTP"}}, PeriodSeconds: 10, TimeoutSeconds: 1, SuccessThreshold: 1, FailureThreshold: 3},
		ReadinessProbe:           &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/ready", Port: intstr.FromString("http"), Scheme: "HTTP"}}, PeriodSeconds: 5, TimeoutSeconds: 1, SuccessThreshold: 1, FailureThreshold: 3},
		TerminationMessagePath:   "/dev/termination-log",
		TerminationMessagePolicy: "File",
		ImagePullPolicy:          "IfNotPresent",
	}
}

func podSpec(app string, i int) corev1.PodSpec {
	sidecar := container("proxy", "proxy", i)
	sidecar.Image = "registry.example.com/mesh/proxy:1.22.0"
	sidecar.Env = sidecar.Env[:3]
	ten := int64(30)
	return corev1.PodSpec{
		Containers:                    []corev1.Container{container("app", app, i), sidecar},
		RestartPolicy:                 "Always",
		TerminationGracePeriodSeconds: &ten,
		DNSPolicy:                     "ClusterFirst",
		ServiceAccountName:            app,
		SchedulerName:                 "default-scheduler",
		Volumes: []corev1.Volume{
			{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: app + "-config"}}}},
			{Name: "kube-api-access-" + suffixOf(i), VolumeSource: corev1.VolumeSource{Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
				{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token", ExpirationSeconds: &[]int64{3607}[0]}},
				{ConfigMap: &corev1.ConfigMapProjection{LocalObjectReference: corev1.LocalObjectReference{Name: "kube-root-ca.crt"}, Items: []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}}},
				{DownwardAPI: &corev1.DownwardAPIProjection{Items: []corev1.DownwardAPIVolumeFile{{Path: "namespace", FieldRef: &corev1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.namespace"}}}}},
			}}}},
		},
		Tolerations: []corev1.Toleration{
			{Key: "node.kubernetes.io/not-ready", Operator: "Exists", Effect: "NoExecute", TolerationSeconds: &[]int64{300}[0]},
			{Key: "node.kubernetes.io/unreachable", Operator: "Exists", Effect: "NoExecute", TolerationSeconds: &[]int64{300}[0]},
		},
	}
}

func makePod(i int) k8sruntime.Object {
	app := appOf(i)
	labels := appLabels(i)
	labels["pod-template-hash"] = templateHash(i)
	name := fmt.Sprintf("%s-%s-%s", app, templateHash(i), suffixOf(i))
	pod := &corev1.Pod{ObjectMeta: objectMeta(name, nsOf(i), i, labels), Spec: podSpec(app, i)}
	pod.Annotations = map[string]string{
		"kubectl.kubernetes.io/restartedAt": "2026-10-28T14:02:11Z",
		"prometheus.io/scrape":              "true",
		"prometheus.io/port":                "9090",
	}
	pod.OwnerReferences = []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: app + "-" + templateHash(i), UID: types.UID(fmt.Sprintf("1a2b3c4d-0000-4000-8000-%012x", i)), Controller: &[]bool{true}[0], BlockOwnerDeletion: &[]bool{true}[0]}}
	pod.ManagedFields = managed("kube-controller-manager", "kubelet/status")
	pod.Spec.NodeName = fmt.Sprintf("ip-10-0-%d-%d.eu-west-1.compute.internal", i%4, i%250)
	started := metav1.NewTime(created.Add(time.Minute))
	pod.Status = corev1.PodStatus{
		Phase:     "Running",
		QOSClass:  "Burstable",
		HostIP:    "10.0.1.17",
		HostIPs:   []corev1.HostIP{{IP: "10.0.1.17"}},
		PodIP:     fmt.Sprintf("10.42.%d.%d", i%250, i%200),
		PodIPs:    []corev1.PodIP{{IP: fmt.Sprintf("10.42.%d.%d", i%250, i%200)}},
		StartTime: &created,
	}
	for _, t := range []corev1.PodConditionType{"PodReadyToStartContainers", "Initialized", "Ready", "ContainersReady", "PodScheduled"} {
		pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{Type: t, Status: "True", LastTransitionTime: started})
	}
	for _, c := range pod.Spec.Containers {
		pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{
			Name: c.Name, Image: c.Image, Ready: true, Started: &[]bool{true}[0], RestartCount: int32(i % 3),
			ImageID:     c.Image + "@sha256:" + strings.Repeat(templateHash(i), 7)[:64],
			ContainerID: "containerd://" + strings.Repeat(suffixOf(i)+templateHash(i), 5)[:64],
			State:       corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
		})
	}
	return pod
}

func makeEvent(i int) k8sruntime.Object {
	pod := makePod(i).(*corev1.Pod)
	reasons := []struct{ reason, kind, message string }{
		{"Pulled", "Normal", `Container image "` + pod.Spec.Containers[0].Image + `" already present on machine`},
		{"Started", "Normal", "Started container app"},
		{"Unhealthy", "Warning", "Readiness probe failed: Get \"http://10.42.3.17:8080/ready\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)"},
		{"BackOff", "Warning", "Back-off restarting failed container app in pod " + pod.Name + "_" + pod.Namespace + "(" + string(pod.UID) + ")"},
	}
	r := reasons[i%len(reasons)]
	return &corev1.Event{
		ObjectMeta: func() metav1.ObjectMeta {
			m := objectMeta(fmt.Sprintf("%s.%x", pod.Name, 0x1879a3c2d4e5f600+i), pod.Namespace, i, nil)
			m.ManagedFields = managed("kubelet")
			return m
		}(),
		InvolvedObject:      corev1.ObjectReference{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID, APIVersion: "v1", ResourceVersion: pod.ResourceVersion, FieldPath: "spec.containers{app}"},
		Reason:              r.reason,
		Message:             r.message,
		Type:                r.kind,
		Source:              corev1.EventSource{Component: "kubelet", Host: pod.Spec.NodeName},
		FirstTimestamp:      created,
		LastTimestamp:       created,
		Count:               int32(1 + i%5),
		ReportingController: "kubelet",
		ReportingInstance:   pod.Spec.NodeName,
	}
}

func makeConfigMap(i int) k8sruntime.Object {
	app := appOf(i)
	cm := &corev1.ConfigMap{ObjectMeta: objectMeta(fmt.Sprintf("%s-config-%d", app, i), nsOf(i), i, appLabels(i))}
	cm.Data = map[string]string{
		"application.yaml":   strings.Repeat("server:\n  port: 8080\n  shutdown: graceful\nfeatures:\n  checkout-v2: true\n", 30), // ~2 KB
		"logging.properties": strings.Repeat("log.level.root=INFO\n", 10),
		"FEATURE_FLAGS":      "checkout-v2,new-search,fast-cart",
	}
	cm.Annotations = lastApplied(cm)
	cm.ManagedFields = managed("kubectl-client-side-apply")
	return cm
}

func makeSecret(i int) k8sruntime.Object {
	app := appOf(i)
	s := &corev1.Secret{ObjectMeta: objectMeta(fmt.Sprintf("%s-db-%d", app, i), nsOf(i), i, appLabels(i)), Type: "Opaque"}
	s.Data = map[string][]byte{
		"username": []byte(app),
		"password": []byte(strings.Repeat("x", 32)),
		"tls.crt":  []byte(strings.Repeat("MIIDdzCCAl+gAwIBAgIUY", 70)), // a certificate's size
		"tls.key":  []byte(strings.Repeat("MIIEvQIBADANBgkqhkiG9", 80)),
	}
	s.Annotations = lastApplied(s)
	s.ManagedFields = managed("kubectl-client-side-apply")
	return s
}

func makeDeployment(i int) k8sruntime.Object {
	app := appOf(i)
	d := &appsv1.Deployment{ObjectMeta: objectMeta(fmt.Sprintf("%s-%d", app, i), nsOf(i), i, appLabels(i))}
	d.Spec = appsv1.DeploymentSpec{
		Replicas: &[]int32{3}[0],
		Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": app}},
		Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: appLabels(i)}, Spec: podSpec(app, i)},
		Strategy: appsv1.DeploymentStrategy{Type: "RollingUpdate"},
	}
	d.Annotations = lastApplied(d)
	d.Annotations["deployment.kubernetes.io/revision"] = "14"
	d.ManagedFields = managed("kubectl-client-side-apply", "kube-controller-manager/status")
	d.Status = appsv1.DeploymentStatus{ObservedGeneration: 3, Replicas: 3, UpdatedReplicas: 3, ReadyReplicas: 3, AvailableReplicas: 3, Conditions: []appsv1.DeploymentCondition{
		{Type: "Available", Status: "True", Reason: "MinimumReplicasAvailable", Message: "Deployment has minimum availability.", LastUpdateTime: created, LastTransitionTime: created},
		{Type: "Progressing", Status: "True", Reason: "NewReplicaSetAvailable", Message: `ReplicaSet "` + app + "-" + templateHash(i) + `" has successfully progressed.`, LastUpdateTime: created, LastTransitionTime: created},
	}}
	return d
}

func makeService(i int) k8sruntime.Object {
	app := appOf(i)
	s := &corev1.Service{ObjectMeta: objectMeta(fmt.Sprintf("%s-%d", app, i), nsOf(i), i, appLabels(i))}
	s.Spec = corev1.ServiceSpec{
		Type:            "ClusterIP",
		Selector:        map[string]string{"app": app},
		Ports:           []corev1.ServicePort{{Name: "http", Port: 80, TargetPort: intstr.FromString("http"), Protocol: "TCP"}, {Name: "metrics", Port: 9090, TargetPort: intstr.FromInt32(9090), Protocol: "TCP"}},
		ClusterIP:       fmt.Sprintf("10.96.%d.%d", i%250, i%200),
		ClusterIPs:      []string{fmt.Sprintf("10.96.%d.%d", i%250, i%200)},
		IPFamilies:      []corev1.IPFamily{"IPv4"},
		SessionAffinity: "None",
	}
	s.Annotations = lastApplied(s)
	s.ManagedFields = managed("kubectl-client-side-apply")
	return s
}

func makeJob(i int) k8sruntime.Object {
	app := appOf(i)
	labels := map[string]string{"batch.kubernetes.io/job-name": fmt.Sprintf("%s-report-%d", app, 29000000+i), "batch.kubernetes.io/controller-uid": fmt.Sprintf("8c1d%04x-aaaa-4bbb-8ccc-%012x", i, i)}
	j := &batchv1.Job{ObjectMeta: objectMeta(fmt.Sprintf("%s-report-%d", app, 29000000+i), nsOf(i), i, labels)}
	j.OwnerReferences = []metav1.OwnerReference{{APIVersion: "batch/v1", Kind: "CronJob", Name: app + "-report", UID: "4d5e6f70-1111-4222-8333-444455556666", Controller: &[]bool{true}[0]}}
	spec := podSpec(app, i)
	spec.Containers = spec.Containers[:1]
	spec.RestartPolicy = "OnFailure"
	j.Spec = batchv1.JobSpec{Completions: &[]int32{1}[0], Parallelism: &[]int32{1}[0], BackoffLimit: &[]int32{6}[0], Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: spec}}
	j.ManagedFields = managed("kube-controller-manager", "kube-controller-manager/status")
	done := metav1.NewTime(created.Add(2 * time.Minute))
	j.Status = batchv1.JobStatus{Succeeded: 1, StartTime: &created, CompletionTime: &done, Conditions: []batchv1.JobCondition{
		{Type: "SuccessCriteriaMet", Status: "True", LastProbeTime: done, LastTransitionTime: done},
		{Type: "Complete", Status: "True", LastProbeTime: done, LastTransitionTime: done},
	}}
	return j
}

func makeClaim(i int) k8sruntime.Object {
	app := appOf(i)
	c := &corev1.PersistentVolumeClaim{ObjectMeta: objectMeta(fmt.Sprintf("data-%s-%d", app, i), nsOf(i), i, map[string]string{"app": app})}
	c.Annotations = map[string]string{
		"pv.kubernetes.io/bind-completed":               "yes",
		"pv.kubernetes.io/bound-by-controller":          "yes",
		"volume.beta.kubernetes.io/storage-provisioner": "ebs.csi.aws.com",
		"volume.kubernetes.io/storage-provisioner":      "ebs.csi.aws.com",
		"volume.kubernetes.io/selected-node":            fmt.Sprintf("ip-10-0-%d-%d.eu-west-1.compute.internal", i%4, i%250),
	}
	c.Finalizers = []string{"kubernetes.io/pvc-protection"}
	c.ManagedFields = managed("kube-controller-manager", "kube-scheduler", "kube-controller-manager/status")
	class := "gp3"
	c.Spec = corev1.PersistentVolumeClaimSpec{
		AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
		Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("20Gi")}},
		StorageClassName: &class,
		VolumeName:       fmt.Sprintf("pvc-7e8f%04x-1234-4abc-9def-%012x", i%65536, i),
		VolumeMode:       &[]corev1.PersistentVolumeMode{"Filesystem"}[0],
	}
	c.Status = corev1.PersistentVolumeClaimStatus{Phase: "Bound", AccessModes: c.Spec.AccessModes, Capacity: c.Spec.Resources.Requests}
	return c
}

func makeNamespace(i int) k8sruntime.Object {
	n := &corev1.Namespace{ObjectMeta: objectMeta(fmt.Sprintf("team-%03d", i), "", i, map[string]string{
		"kubernetes.io/metadata.name":        fmt.Sprintf("team-%03d", i),
		"istio-injection":                    "enabled",
		"pod-security.kubernetes.io/enforce": "baseline",
	})}
	n.ManagedFields = managed("kubectl-create", "kubectl-label")
	n.Spec.Finalizers = []corev1.FinalizerName{"kubernetes"}
	n.Status.Phase = "Active"
	return n
}

// makeNode is a cloud node: its labels, the images it has pulled (the
// kubelet reports up to 50), and its conditions are most of it.
func makeNode(i int) k8sruntime.Object {
	name := fmt.Sprintf("ip-10-0-%d-%d.eu-west-1.compute.internal", i%4, i)
	labels := map[string]string{
		"kubernetes.io/hostname": name, "kubernetes.io/os": "linux", "kubernetes.io/arch": "amd64",
		"beta.kubernetes.io/os": "linux", "beta.kubernetes.io/arch": "amd64", "beta.kubernetes.io/instance-type": "m6i.xlarge",
		"node.kubernetes.io/instance-type": "m6i.xlarge", "topology.kubernetes.io/region": "eu-west-1", "topology.kubernetes.io/zone": "eu-west-1a",
		"failure-domain.beta.kubernetes.io/region": "eu-west-1", "failure-domain.beta.kubernetes.io/zone": "eu-west-1a",
		"eks.amazonaws.com/nodegroup": "general", "eks.amazonaws.com/capacityType": "ON_DEMAND", "topology.ebs.csi.aws.com/zone": "eu-west-1a",
	}
	n := &corev1.Node{ObjectMeta: objectMeta(name, "", i, labels)}
	n.Annotations = map[string]string{
		"alpha.kubernetes.io/provided-node-ip":                   "10.0.1.17",
		"csi.volume.kubernetes.io/nodeid":                        `{"ebs.csi.aws.com":"i-0abc123def4567890"}`,
		"node.alpha.kubernetes.io/ttl":                           "0",
		"volumes.kubernetes.io/controller-managed-attach-detach": "true",
	}
	n.ManagedFields = managed("kubelet", "kube-controller-manager", "kubelet/status", "aws-node")
	n.Spec = corev1.NodeSpec{ProviderID: "aws:///eu-west-1a/i-0abc123def4567890", PodCIDR: "10.42.3.0/24", PodCIDRs: []string{"10.42.3.0/24"}}
	capacity := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16019640Ki"), corev1.ResourcePods: resource.MustParse("58"), corev1.ResourceEphemeralStorage: resource.MustParse("83873772Ki")}
	n.Status = corev1.NodeStatus{
		Capacity: capacity, Allocatable: capacity,
		Addresses: []corev1.NodeAddress{{Type: "InternalIP", Address: "10.0.1.17"}, {Type: "InternalDNS", Address: name}, {Type: "Hostname", Address: name}},
		NodeInfo: corev1.NodeSystemInfo{
			MachineID: strings.Repeat("ec2", 11)[:32], SystemUUID: "ec2" + strings.Repeat("a", 33)[:33], BootID: strings.Repeat("b", 36),
			KernelVersion: "6.1.112-122.189.amzn2023.x86_64", OSImage: "Amazon Linux 2023.6.20241010", ContainerRuntimeVersion: "containerd://1.7.22",
			KubeletVersion: "v1.33.5-eks-1234567", OperatingSystem: "linux", Architecture: "amd64",
		},
		DaemonEndpoints: corev1.NodeDaemonEndpoints{KubeletEndpoint: corev1.DaemonEndpoint{Port: 10250}},
	}
	for _, c := range []struct{ t, status, reason, message string }{
		{"MemoryPressure", "False", "KubeletHasSufficientMemory", "kubelet has sufficient memory available"},
		{"DiskPressure", "False", "KubeletHasNoDiskPressure", "kubelet has no disk pressure"},
		{"PIDPressure", "False", "KubeletHasSufficientPID", "kubelet has sufficient PID available"},
		{"Ready", "True", "KubeletReady", "kubelet is posting ready status"},
	} {
		n.Status.Conditions = append(n.Status.Conditions, corev1.NodeCondition{Type: corev1.NodeConditionType(c.t), Status: corev1.ConditionStatus(c.status), Reason: c.reason, Message: c.message, LastHeartbeatTime: created, LastTransitionTime: created})
	}
	for j := range 50 {
		image := fmt.Sprintf("registry.example.com/storefront/service-%02d", j)
		n.Status.Images = append(n.Status.Images, corev1.ContainerImage{
			Names:     []string{image + "@sha256:" + strings.Repeat(fmt.Sprintf("%08x", j*977), 8), image + ":2.14.3"},
			SizeBytes: 104857600 + int64(j)*1048576,
		})
	}
	return n
}
