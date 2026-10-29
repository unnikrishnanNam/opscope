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
}

type NodeSummary struct {
	Total int `json:"total"`
	Ready int `json:"ready"`
}

type PodSummary struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"byStatus"` // e.g. {"Running": 40, "CrashLoopBackOff": 2}
}

// WorkloadCount says how many of one workload type exist and how many need
// a look. "Unhealthy" means fewer replicas ready than wanted, or for jobs,
// a failed job. CronJobs are never counted as unhealthy here. The same shape
// counts other kinds too: claims that aren't bound, volumes released or
// failed, Gateways not programmed.
type WorkloadCount struct {
	Resource  string `json:"resource"` // matches the API name, e.g. "deployments"
	Total     int    `json:"total"`
	Unhealthy int    `json:"unhealthy"`
}

// GetOverview builds the summary by reusing the listers. Nodes, volumes and
// the namespace count always cover the whole cluster; everything else follows
// q.Namespace.
//
// The calls run one after another. That's a handful of requests, and it
// keeps the code easy to follow; running them in parallel would be the next
// step if this ever feels slow.
func GetOverview(ctx context.Context, clients Clients, q Query) (*Overview, error) {
	client := clients.Kube
	result := &Overview{}

	namespaces, err := listNamespaces(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Namespaces = len(namespaces)

	nodes, err := listNodes(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Nodes.Total = len(nodes)
	for _, n := range nodes {
		if n.Status == "Ready" {
			result.Nodes.Ready++
		}
	}

	pods, err := listPods(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Pods = PodSummary{Total: len(pods), ByStatus: map[string]int{}}
	for _, p := range pods {
		result.Pods.ByStatus[p.Status]++
	}

	deployments, err := listDeployments(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, countWorkloads("deployments", deployments,
		func(d Deployment) bool { return d.Ready < d.Desired }))

	statefulSets, err := listStatefulSets(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, countWorkloads("statefulsets", statefulSets,
		func(s StatefulSet) bool { return s.Ready < s.Desired }))

	daemonSets, err := listDaemonSets(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, countWorkloads("daemonsets", daemonSets,
		func(d DaemonSet) bool { return d.Ready < d.Desired }))

	jobs, err := listJobs(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, countWorkloads("jobs", jobs,
		func(j Job) bool { return j.Status == "Failed" }))

	cronJobs, err := listCronJobs(ctx, client, q)
	if err != nil {
		return nil, err
	}
	result.Workloads = append(result.Workloads, countWorkloads("cronjobs", cronJobs,
		func(CronJob) bool { return false }))

	result.Storage, err = storageCounts(ctx, clients, q)
	if err != nil {
		return nil, err
	}

	// Gateway API is optional: a cluster without it simply gets no counts.
	gateways, err := listGateways(ctx, clients.Dynamic, q)
	if errors.Is(err, ErrGatewayAPINotInstalled) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	httpRoutes, err := listHTTPRoutes(ctx, clients.Dynamic, q)
	if err != nil {
		return nil, err
	}
	result.GatewayAPI = []WorkloadCount{
		countWorkloads("gateways", gateways, func(g Gateway) bool { return g.Status != "Programmed" }),
		countWorkloads("httproutes", httpRoutes, func(r HTTPRoute) bool { return r.Status != "Accepted" }),
	}

	return result, nil
}

// storageCounts counts claims that aren't bound and volumes that were
// released or failed. Storage was added after the rest, so credentials that
// could read the overview before may not be allowed to list it; then it's
// left out rather than failing the whole page. A cluster with no claims and
// no volumes gets no counts either, like one without Gateway API.
func storageCounts(ctx context.Context, clients Clients, q Query) ([]WorkloadCount, error) {
	claims, err := listPersistentVolumeClaims(ctx, clients.Kube, q)
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	volumes, err := listPersistentVolumes(ctx, clients.Kube, q)
	if apierrors.IsForbidden(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(claims) == 0 && len(volumes) == 0 {
		return nil, nil
	}
	return []WorkloadCount{
		countWorkloads("persistentvolumeclaims", claims,
			func(c PersistentVolumeClaim) bool { return c.Status != "Bound" }),
		countWorkloads("persistentvolumes", volumes,
			func(v PersistentVolume) bool { return v.Status == "Released" || v.Status == "Failed" }),
	}, nil
}

// countWorkloads counts rows, and the rows for which unhealthy returns true.
func countWorkloads[T any](resource string, rows []T, unhealthy func(T) bool) WorkloadCount {
	count := WorkloadCount{Resource: resource, Total: len(rows)}
	for _, row := range rows {
		if unhealthy(row) {
			count.Unhealthy++
		}
	}
	return count
}
