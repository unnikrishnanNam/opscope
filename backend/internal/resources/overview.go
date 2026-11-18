package resources

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Overview is the summary shown on a cluster's front page.
type Overview struct {
	Namespaces int             `json:"namespaces"`
	Nodes      NodeSummary     `json:"nodes"`
	Pods       PodSummary      `json:"pods"`
	Workloads  []WorkloadCount `json:"workloads"`
	// PersistentVolumeClaims and PersistentVolumes; null when there are
	// neither, or Opscope isn't allowed to list them.
	Storage []WorkloadCount `json:"storage"`
	// Gateways and HTTPRoutes; null when the cluster doesn't have Gateway API.
	GatewayAPI []WorkloadCount `json:"gatewayAPI"`
	// Signals counts the objects with an open signal of each type, from
	// the Signal engine: what the Issues feed lists.
	Signals map[string]int `json:"signals"`
}

// Attention is how many objects need a look, from the Signal engine
// (internal/signals), so the overview and the Issues feed can't disagree.
// It's an interface here because signals imports this package.
type Attention interface {
	// Objects counts the objects of a kind with an open signal of one of
	// these types, or of any type when none are given.
	Objects(resource string, types ...string) int
	// Types counts the objects with an open signal, by signal type.
	Types() map[string]int
}

// noAttention counts nothing, for callers without a Signal engine.
type noAttention struct{}

func (noAttention) Objects(string, ...string) int { return 0 }
func (noAttention) Types() map[string]int         { return map[string]int{} }

type NodeSummary struct {
	Total int `json:"total"`
	Ready int `json:"ready"`
}

type PodSummary struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"byStatus"`  // e.g. {"Running": 40, "CrashLoopBackOff": 2}
	Unhealthy int            `json:"unhealthy"` // pods with an open signal
}

// WorkloadCount says how many of one workload type exist and how many need
// a look: those with an open signal of the types its row on the overview
// names (attentionTypes). CronJobs are never counted as unhealthy here. The
// same shape counts other kinds too: claims that aren't bound, volumes
// released or failed, Gateways not programmed.
type WorkloadCount struct {
	Resource  string `json:"resource"` // matches the API name, e.g. "deployments"
	Total     int    `json:"total"`
	Unhealthy int    `json:"unhealthy"`
	// Types are the signal types Unhealthy counts, for a link to the
	// Issues feed filtered the same way.
	Types []string `json:"types,omitempty"`
}

// attentionTypes are the signal types each kind's "needs attention" count
// is made of, as the overview's labels say: "2 Deployments not ready".
var attentionTypes = map[string][]string{
	"deployments":            {"replicas_unavailable", "rollout_stuck"},
	"statefulsets":           {"replicas_unavailable"},
	"daemonsets":             {"replicas_unavailable"},
	"jobs":                   {"job_failed"},
	"persistentvolumeclaims": {"pvc_unbound"},
	"persistentvolumes":      {"volume_released"},
	"gateways":               {"gateway_not_programmed"},
	"httproutes":             {"route_not_accepted"},
}

// GetOverview builds the summary by reusing the listers, with what needs
// attention from att (nil counts nothing). Nodes, volumes and the
// namespace count always cover the whole cluster; everything else follows
// q.Namespace; att must be read for the same namespace.
//
// The calls run one after another. From the watch cache they're cheap; read
// directly from the cluster they're a handful of requests, and running them
// in parallel would be the next step if that ever feels slow.
func GetOverview(ctx context.Context, src Source, q Query, att Attention) (*Overview, error) {
	if att == nil {
		att = noAttention{}
	}
	result := &Overview{Signals: att.Types()}
	count := func(resource string, total int) WorkloadCount {
		c := WorkloadCount{Resource: resource, Total: total}
		if types, ok := attentionTypes[resource]; ok {
			c.Unhealthy = att.Objects(resource, types...)
			c.Types = types
		}
		return c
	}

	namespaces, err := listNamespaces(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Namespaces = len(namespaces)

	nodes, err := listNodes(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Nodes.Total = len(nodes)
	for _, n := range nodes {
		if n.Status == "Ready" {
			result.Nodes.Ready++
		}
	}

	pods, err := listPods(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Pods = PodSummary{Total: len(pods), ByStatus: map[string]int{}, Unhealthy: att.Objects("pods")}
	for _, p := range pods {
		result.Pods.ByStatus[p.Status]++
	}

	deployments, err := listDeployments(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, count("deployments", len(deployments)))

	statefulSets, err := listStatefulSets(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, count("statefulsets", len(statefulSets)))

	daemonSets, err := listDaemonSets(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, count("daemonsets", len(daemonSets)))

	jobs, err := listJobs(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, count("jobs", len(jobs)))

	cronJobs, err := listCronJobs(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, count("cronjobs", len(cronJobs)))

	result.Storage, err = storageCounts(ctx, src, q, count)
	if err != nil {
		return nil, err
	}

	// Gateway API is optional: a cluster without it simply gets no counts.
	gateways, err := listGateways(ctx, src, q)
	if errors.Is(err, ErrGatewayAPINotInstalled) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	httpRoutes, err := listHTTPRoutes(ctx, src, q)
	if err != nil {
		return nil, err
	}
	result.GatewayAPI = []WorkloadCount{count("gateways", len(gateways)), count("httproutes", len(httpRoutes))}

	return result, nil
}

// storageCounts counts claims and volumes, and those that aren't bound or
// were released or failed (pvc_unbound, volume_released). Storage was added
// after the rest, so credentials that could read the overview before may
// not be allowed to list it; then it's left out rather than failing the
// whole page. A cluster with no claims and no volumes gets no counts
// either, like one without Gateway API.
func storageCounts(ctx context.Context, src Source, q Query, count func(string, int) WorkloadCount) ([]WorkloadCount, error) {
	claims, err := listPersistentVolumeClaims(ctx, src, q)
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	volumes, err := listPersistentVolumes(ctx, src, q)
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 && len(volumes) == 0 {
		return nil, nil
	}
	return []WorkloadCount{count("persistentvolumeclaims", len(claims)), count("persistentvolumes", len(volumes))}, nil
}
