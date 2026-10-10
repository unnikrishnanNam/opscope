package snapshot

import (
	"context"
	"sort"
	"sync"
	"time"

	"opscope/internal/kube"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// pageSize is how many objects one list call asks for. Large lists come in
// pages, so no single request runs into the client's timeout and the API
// server never has to build one huge answer.
const pageSize = 500

// lastApplied is the annotation `kubectl apply` keeps a full copy of the
// object in. It's often the largest part of an object and nothing here
// needs it.
const lastApplied = "kubectl.kubernetes.io/last-applied-configuration"

// loader reads one kind into its list in the snapshot.
type loader struct {
	resource string
	load     func(ctx context.Context, c kube.Clients, s *Snapshot) error
}

// loaders lists every kind a snapshot holds. Adding a kind means a field on
// Snapshot, a line here and a line in buildIndex.
var loaders = []loader{
	{"namespaces", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Namespaces, err = listAll(ctx, c.Kube.CoreV1().Namespaces().List, func(l *corev1.NamespaceList) []corev1.Namespace { return l.Items })
		return err
	}},
	{"nodes", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Nodes, err = listAll(ctx, c.Kube.CoreV1().Nodes().List, func(l *corev1.NodeList) []corev1.Node { return l.Items })
		return err
	}},
	{"pods", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Pods, err = listAll(ctx, c.Kube.CoreV1().Pods("").List, func(l *corev1.PodList) []corev1.Pod { return l.Items })
		return err
	}},
	{"replicasets", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.ReplicaSets, err = listAll(ctx, c.Kube.AppsV1().ReplicaSets("").List, func(l *appsv1.ReplicaSetList) []appsv1.ReplicaSet { return l.Items })
		return err
	}},
	{"deployments", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Deployments, err = listAll(ctx, c.Kube.AppsV1().Deployments("").List, func(l *appsv1.DeploymentList) []appsv1.Deployment { return l.Items })
		return err
	}},
	{"statefulsets", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.StatefulSets, err = listAll(ctx, c.Kube.AppsV1().StatefulSets("").List, func(l *appsv1.StatefulSetList) []appsv1.StatefulSet { return l.Items })
		return err
	}},
	{"daemonsets", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.DaemonSets, err = listAll(ctx, c.Kube.AppsV1().DaemonSets("").List, func(l *appsv1.DaemonSetList) []appsv1.DaemonSet { return l.Items })
		return err
	}},
	{"jobs", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Jobs, err = listAll(ctx, c.Kube.BatchV1().Jobs("").List, func(l *batchv1.JobList) []batchv1.Job { return l.Items })
		return err
	}},
	{"cronjobs", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.CronJobs, err = listAll(ctx, c.Kube.BatchV1().CronJobs("").List, func(l *batchv1.CronJobList) []batchv1.CronJob { return l.Items })
		return err
	}},
	{"services", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Services, err = listAll(ctx, c.Kube.CoreV1().Services("").List, func(l *corev1.ServiceList) []corev1.Service { return l.Items })
		return err
	}},
	{"endpointslices", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.EndpointSlices, err = listAll(ctx, c.Kube.DiscoveryV1().EndpointSlices("").List, func(l *discoveryv1.EndpointSliceList) []discoveryv1.EndpointSlice { return l.Items })
		return err
	}},
	{"ingresses", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Ingresses, err = listAll(ctx, c.Kube.NetworkingV1().Ingresses("").List, func(l *networkingv1.IngressList) []networkingv1.Ingress { return l.Items })
		return err
	}},
	{"ingressclasses", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.IngressClasses, err = listAll(ctx, c.Kube.NetworkingV1().IngressClasses().List, func(l *networkingv1.IngressClassList) []networkingv1.IngressClass { return l.Items })
		return err
	}},
	{"configmaps", loadConfigMaps},
	{"secrets", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		// The metadata client asks only for metadata, so the values of
		// Secrets never leave the API server.
		secrets := schema.GroupVersionResource{Version: "v1", Resource: "secrets"}
		s.Secrets, err = listAll(ctx, c.Metadata.Resource(secrets).Namespace("").List, func(l *metav1.PartialObjectMetadataList) []metav1.PartialObjectMetadata { return l.Items })
		return err
	}},
	{"persistentvolumeclaims", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.PersistentVolumeClaims, err = listAll(ctx, c.Kube.CoreV1().PersistentVolumeClaims("").List, func(l *corev1.PersistentVolumeClaimList) []corev1.PersistentVolumeClaim { return l.Items })
		return err
	}},
	{"persistentvolumes", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.PersistentVolumes, err = listAll(ctx, c.Kube.CoreV1().PersistentVolumes().List, func(l *corev1.PersistentVolumeList) []corev1.PersistentVolume { return l.Items })
		return err
	}},
	{"storageclasses", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.StorageClasses, err = listAll(ctx, c.Kube.StorageV1().StorageClasses().List, func(l *storagev1.StorageClassList) []storagev1.StorageClass { return l.Items })
		return err
	}},
	{"serviceaccounts", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.ServiceAccounts, err = listAll(ctx, c.Kube.CoreV1().ServiceAccounts("").List, func(l *corev1.ServiceAccountList) []corev1.ServiceAccount { return l.Items })
		return err
	}},
	{"resourcequotas", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.ResourceQuotas, err = listAll(ctx, c.Kube.CoreV1().ResourceQuotas("").List, func(l *corev1.ResourceQuotaList) []corev1.ResourceQuota { return l.Items })
		return err
	}},
	{"horizontalpodautoscalers", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.HorizontalPodAutoscalers, err = listAll(ctx, c.Kube.AutoscalingV2().HorizontalPodAutoscalers("").List, func(l *autoscalingv2.HorizontalPodAutoscalerList) []autoscalingv2.HorizontalPodAutoscaler {
			return l.Items
		})
		return err
	}},
	{"poddisruptionbudgets", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.PodDisruptionBudgets, err = listAll(ctx, c.Kube.PolicyV1().PodDisruptionBudgets("").List, func(l *policyv1.PodDisruptionBudgetList) []policyv1.PodDisruptionBudget { return l.Items })
		return err
	}},
	{"gateways", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.Gateways, err = kube.ListCustom[kube.Gateway](ctx, c.Dynamic, kube.GatewayAPI("gateways"), "")
		slim(s.Gateways)
		return err
	}},
	{"httproutes", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.HTTPRoutes, err = kube.ListCustom[kube.HTTPRoute](ctx, c.Dynamic, kube.GatewayAPI("httproutes"), "")
		slim(s.HTTPRoutes)
		return err
	}},
	{"gatewayclasses", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.GatewayClasses, err = kube.ListCustom[kube.GatewayClass](ctx, c.Dynamic, kube.GatewayAPI("gatewayclasses"), "")
		slim(s.GatewayClasses)
		return err
	}},
	{"referencegrants", func(ctx context.Context, c kube.Clients, s *Snapshot) (err error) {
		s.ReferenceGrants, err = kube.ListReferenceGrants(ctx, c.Dynamic)
		slim(s.ReferenceGrants)
		return err
	}},
	{"events", loadEvents},
}

// Resources lists the kinds a snapshot reads, by their API names.
func Resources() []string {
	names := make([]string, len(loaders))
	for i, l := range loaders {
		names[i] = l.resource
	}
	return names
}

// Load reads every kind at once and builds the lookups.
//
// One kind failing doesn't fail the snapshot: it's named in Skipped and the
// others are kept. Only when nothing at all could be read is it an error,
// since then the cluster itself is the problem (unreachable, credentials
// rejected).
func Load(ctx context.Context, clients kube.Clients) (*Snapshot, error) {
	s := &Snapshot{Taken: time.Now()}

	// Each kind is its own requests to the API server, so they run in
	// parallel. Each goroutine writes only its own list and its own error.
	errs := make([]error, len(loaders))
	var wg sync.WaitGroup
	for i, l := range loaders {
		wg.Go(func() { errs[i] = l.load(ctx, clients, s) })
	}
	wg.Wait()

	s.Skipped = []kube.Skipped{}
	var firstErr error
	read := 0
	for i, l := range loaders {
		if errs[i] == nil {
			read++
			continue
		}
		skipped := kube.Skip(l.resource, errs[i])
		s.Skipped = append(s.Skipped, skipped)
		if skipped.Code == "failed" && firstErr == nil {
			firstErr = errs[i]
		}
	}
	if read == 0 && firstErr != nil {
		return nil, firstErr
	}

	s.buildIndex()
	return s, nil
}

// listAll reads every page of one list and slims each object.
//
// list is a client's List method, like client.CoreV1().Pods("").List, and
// items picks the objects out of one page. L is the page type (*PodList),
// T the object type (Pod) and PT a pointer to it (*Pod); Go works out all
// three from the two functions.
func listAll[L interface{ GetContinue() string }, T any, PT interface {
	*T
	metav1.Object
}](ctx context.Context, list func(context.Context, metav1.ListOptions) (L, error), items func(L) []T) ([]T, error) {
	options := metav1.ListOptions{Limit: pageSize}
	var all []T
	for {
		page, err := list(ctx, options)
		if err != nil {
			return nil, err
		}
		all = append(all, items(page)...)
		if options.Continue = page.GetContinue(); options.Continue == "" {
			break
		}
	}
	slim[T, PT](all)
	return all, nil
}

// slim drops what nothing here reads and what makes objects large:
// managedFields (server-side apply bookkeeping) and kubectl's
// last-applied-configuration annotation, a full copy of the object.
func slim[T any, PT interface {
	*T
	metav1.Object
}](items []T) {
	for i := range items {
		obj := PT(&items[i])
		obj.SetManagedFields(nil)
		if annotations := obj.GetAnnotations(); annotations[lastApplied] != "" {
			delete(annotations, lastApplied)
		}
	}
}

// loadConfigMaps keeps each ConfigMap's metadata and key names, and lets
// the values go as soon as the page is read.
func loadConfigMaps(ctx context.Context, c kube.Clients, s *Snapshot) error {
	list, err := listAll(ctx, c.Kube.CoreV1().ConfigMaps("").List, func(l *corev1.ConfigMapList) []ConfigMap {
		maps := make([]ConfigMap, 0, len(l.Items))
		for _, cm := range l.Items {
			keys := make([]string, 0, len(cm.Data)+len(cm.BinaryData))
			for key := range cm.Data {
				keys = append(keys, key)
			}
			for key := range cm.BinaryData {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			maps = append(maps, ConfigMap{ObjectMeta: cm.ObjectMeta, Keys: keys})
		}
		return maps
	})
	s.ConfigMaps = list
	return err
}

// loadEvents reads Warning events only. Normal events ("Scheduled",
// "Pulled", "Started") are most of a cluster's events and explain nothing
// that's wrong.
func loadEvents(ctx context.Context, c kube.Clients, s *Snapshot) error {
	warnings := fields.OneTermEqualSelector("type", corev1.EventTypeWarning).String()
	list := func(ctx context.Context, options metav1.ListOptions) (*corev1.EventList, error) {
		options.FieldSelector = warnings
		return c.Kube.CoreV1().Events("").List(ctx, options)
	}
	events, err := listAll(ctx, list, func(l *corev1.EventList) []corev1.Event { return l.Items })
	if err != nil {
		return err
	}
	// The API server filters by type already; this keeps the promise even
	// if something in between doesn't.
	s.Events = events[:0]
	for _, e := range events {
		if e.Type == corev1.EventTypeWarning {
			s.Events = append(s.Events, e)
		}
	}
	return nil
}
