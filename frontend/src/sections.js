// Every page in the sidebar, in one list. The sidebar and the router both
// read from here, so adding a page means adding one entry.
//
// `path` is relative to the selected cluster: "workloads/pods" becomes
// /c/<cluster-id>/workloads/pods in the address bar.
// `resource` is the API name for pages that show a table (see columns.jsx).
// `clusterScoped` marks resources that don't live in a namespace (like nodes).
// `phase` is the build phase that brings the real page (see docs/PHASES.md).

export const sections = [
  {
    group: "Cluster",
    items: [
      { path: "overview", label: "Overview", phase: 3, about: "Counts, node health and recent warnings at a glance." },
      { path: "nodes", resource: "nodes", clusterScoped: true, label: "Nodes", phase: 3, about: "Machines in the cluster, their status, capacity and usage." },
    ],
  },
  {
    group: "Workloads",
    items: [
      { path: "workloads/pods", resource: "pods", label: "Pods", phase: 2, about: "Running containers, their status, restarts and the node they run on." },
      { path: "workloads/deployments", resource: "deployments", label: "Deployments", phase: 2, about: "Stateless apps and how many of their replicas are ready." },
      { path: "workloads/statefulsets", resource: "statefulsets", label: "StatefulSets", phase: 2, about: "Apps with stable names and storage, such as databases." },
      { path: "workloads/daemonsets", resource: "daemonsets", label: "DaemonSets", phase: 2, about: "Pods that run on every node (or a chosen set of nodes)." },
      { path: "workloads/jobs", resource: "jobs", label: "Jobs", phase: 2, about: "One-off tasks that run until they finish." },
      { path: "workloads/cronjobs", resource: "cronjobs", label: "CronJobs", phase: 2, about: "Jobs that run on a schedule." },
    ],
  },
  {
    group: "Config",
    items: [
      { path: "config/configmaps", resource: "configmaps", label: "ConfigMaps", phase: 4, about: "Plain configuration values used by pods." },
      { path: "config/secrets", resource: "secrets", label: "Secrets", phase: 4, about: "Sensitive values. Click a name to see its keys; each value stays hidden until you reveal it." },
    ],
  },
  {
    group: "Network",
    items: [
      { path: "network/services", resource: "services", label: "Services", phase: 4, about: "Stable addresses in front of a set of pods." },
      { path: "network/ingresses", resource: "ingresses", label: "Ingresses", phase: 4, about: "HTTP routes from outside the cluster to services." },
      { path: "network/gateways", resource: "gateways", label: "Gateways", phase: 5, about: "Gateway API entry points: where traffic comes in, on which ports." },
      { path: "network/httproutes", resource: "httproutes", label: "HTTPRoutes", phase: 5, about: "Gateway API rules that send HTTP traffic from a gateway to services." },
      { path: "network/gatewayclasses", resource: "gatewayclasses", clusterScoped: true, label: "GatewayClasses", phase: 5, about: "The controllers that run gateways, such as nginx or Envoy." },
    ],
  },
];

// The same pages as a flat list, handy for building routes.
export const allPages = sections.flatMap((section) =>
  section.items.map((item) => ({ ...item, group: section.group })),
);

// detailPath builds the URL of one object's detail page, e.g.
// detailPath("lab", "pods", "web", "api-1") -> "/c/lab/workloads/pods/web/api-1".
// Cluster-wide kinds (like nodes) have no namespace in the URL.
// Returns null for kinds without a page.
export function detailPath(clusterId, resource, namespace, name) {
  const page = allPages.find((p) => p.resource === resource);
  if (!page) return null;
  const parts = page.clusterScoped ? [name] : [namespace, name];
  return `/c/${clusterId}/${page.path}/${parts.map(encodeURIComponent).join("/")}`;
}

// nsQuery keeps only the namespace from a query string, for links that
// should remember the selected namespace but nothing page-specific:
// nsQuery("?ns=web&tab=logs") -> "?ns=web".
export function nsQuery(search) {
  const ns = new URLSearchParams(search).get("ns");
  return ns ? `?ns=${encodeURIComponent(ns)}` : "";
}
