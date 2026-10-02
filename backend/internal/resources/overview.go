package resources

import (
	"context"
	"errors"
)

// Overview is the summary shown on a cluster's front page.
type Overview struct {
	Namespaces int             `json:"namespaces"`
	Nodes      NodeSummary     `json:"nodes"`
	Pods       PodSummary      `json:"pods"`
	Workloads  []WorkloadCount `json:"workloads"`
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
// a failed job. CronJobs are never counted as unhealthy here.
type WorkloadCount struct {
	Resource  string `json:"resource"` // matches the API name, e.g. "deployments"
	Total     int    `json:"total"`
	Unhealthy int    `json:"unhealthy"`
}

// GetOverview builds the summary by reusing the listers. Nodes and the
// namespace count always cover the whole cluster; everything else follows
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
