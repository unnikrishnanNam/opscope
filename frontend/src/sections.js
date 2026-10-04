// Every page in the sidebar, in one list. The sidebar and the router both
// read from here, so adding a page means adding one entry.
//
// `path` is relative to the selected cluster: "workloads/pods" becomes
// /c/<cluster-id>/workloads/pods in the address bar.
// `resource` is the API name for pages that show a table (see columns.jsx).
// `clusterScoped` marks resources that don't live in a namespace (like nodes).
// `metrics` marks pages that also show live usage from metrics-server.
// `icon` is the page's icon in the sidebar.
// `aliases` are other names that find the page in the command palette:
// kubectl's short names and the singular ("po", "pod" for Pods).
// `kubectl` is the name kubectl knows the resource by, when it isn't
// `resource`: Gateway API kinds need their group, since other projects have
// a kind called Gateway too.

import * as icons from "./components/icons.jsx";

export const sections = [
  {
    group: "Cluster",
    items: [
      { path: "overview", icon: icons.OverviewIcon, label: "Overview", about: "Counts, node health and recent warnings at a glance." },
      { path: "nodes", icon: icons.NodesIcon, resource: "nodes", aliases: ["no", "node"], clusterScoped: true, metrics: "nodes", label: "Nodes", about: "Machines in the cluster, their status, capacity and usage." },
    ],
  },
  {
    group: "Workloads",
    items: [
      { path: "workloads/pods", icon: icons.PodsIcon, resource: "pods", aliases: ["po", "pod"], metrics: "pods", label: "Pods", about: "Running containers, their status, restarts and the node they run on." },
      { path: "workloads/deployments", icon: icons.DeploymentsIcon, resource: "deployments", aliases: ["deploy", "deployment"], label: "Deployments", about: "Stateless apps and how many of their replicas are ready." },
      { path: "workloads/statefulsets", icon: icons.StatefulSetsIcon, resource: "statefulsets", aliases: ["sts", "statefulset"], label: "StatefulSets", about: "Apps with stable names and storage, such as databases." },
      { path: "workloads/daemonsets", icon: icons.DaemonSetsIcon, resource: "daemonsets", aliases: ["ds", "daemonset"], label: "DaemonSets", about: "Pods that run on every node (or a chosen set of nodes)." },
      { path: "workloads/jobs", icon: icons.JobsIcon, resource: "jobs", aliases: ["job"], label: "Jobs", about: "One-off tasks that run until they finish." },
      { path: "workloads/cronjobs", icon: icons.CronJobsIcon, resource: "cronjobs", aliases: ["cj", "cronjob"], label: "CronJobs", about: "Jobs that run on a schedule." },
    ],
  },
  {
    group: "Config",
    items: [
      { path: "config/configmaps", icon: icons.ConfigMapsIcon, resource: "configmaps", aliases: ["cm", "configmap"], label: "ConfigMaps", about: "Plain configuration values used by pods." },
      { path: "config/secrets", icon: icons.SecretsIcon, resource: "secrets", aliases: ["secret"], label: "Secrets", about: "Sensitive values. Click a name to see its keys; each value stays hidden until you reveal it." },
    ],
  },
  {
    group: "Network",
    items: [
      { path: "network/services", icon: icons.ServicesIcon, resource: "services", aliases: ["svc", "service"], label: "Services", about: "Stable addresses in front of a set of pods." },
      { path: "network/ingresses", icon: icons.IngressesIcon, resource: "ingresses", aliases: ["ing", "ingress"], label: "Ingresses", about: "HTTP routes from outside the cluster to services." },
      { path: "network/gateways", icon: icons.GatewaysIcon, resource: "gateways", kubectl: "gateways.gateway.networking.k8s.io", aliases: ["gtw", "gateway"], label: "Gateways", about: "Gateway API entry points: where traffic comes in, on which ports." },
      { path: "network/httproutes", icon: icons.HTTPRoutesIcon, resource: "httproutes", kubectl: "httproutes.gateway.networking.k8s.io", aliases: ["httproute", "route"], label: "HTTPRoutes", about: "Gateway API rules that send HTTP traffic from a gateway to services." },
      { path: "network/gatewayclasses", icon: icons.GatewayClassesIcon, resource: "gatewayclasses", kubectl: "gatewayclasses.gateway.networking.k8s.io", aliases: ["gc", "gatewayclass"], clusterScoped: true, label: "GatewayClasses", about: "The controllers that run gateways, such as nginx or Envoy." },
    ],
  },
];

// The same pages as a flat list, handy for building routes.
export const allPages = sections.flatMap((section) =>
  section.items.map((item) => ({ ...item, group: section.group })),
);

// pageFor finds the page a URL belongs to, from the part after /c/<id>/:
// "workloads/pods" itself, or a detail page under it like
// "workloads/pods/web/api-1". Undefined for anything else.
export function pageFor(pagePath) {
  return allPages.find((p) => pagePath === p.path || pagePath?.startsWith(p.path + "/"));
}

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
