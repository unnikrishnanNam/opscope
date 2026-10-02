// Every page in the sidebar, in one list. The sidebar and the router both
// read from here, so adding a page means adding one entry.
//
// `phase` is the build phase that brings the real page (see docs/PHASES.md).

export const sections = [
  {
    group: "Cluster",
    items: [
      { path: "/overview", label: "Overview", phase: 3, about: "Counts, node health and recent warnings at a glance." },
      { path: "/nodes", label: "Nodes", phase: 3, about: "Machines in the cluster, their status, capacity and usage." },
    ],
  },
  {
    group: "Workloads",
    items: [
      { path: "/workloads/pods", label: "Pods", phase: 2, about: "Running containers, their status, restarts and the node they run on." },
      { path: "/workloads/deployments", label: "Deployments", phase: 2, about: "Stateless apps and how many of their replicas are ready." },
      { path: "/workloads/statefulsets", label: "StatefulSets", phase: 2, about: "Apps with stable names and storage, such as databases." },
      { path: "/workloads/daemonsets", label: "DaemonSets", phase: 2, about: "Pods that run on every node (or a chosen set of nodes)." },
      { path: "/workloads/jobs", label: "Jobs", phase: 2, about: "One-off tasks that run until they finish." },
      { path: "/workloads/cronjobs", label: "CronJobs", phase: 2, about: "Jobs that run on a schedule." },
    ],
  },
  {
    group: "Config",
    items: [
      { path: "/config/configmaps", label: "ConfigMaps", phase: 4, about: "Plain configuration values used by pods." },
      { path: "/config/secrets", label: "Secrets", phase: 4, about: "Sensitive values. Hidden until you choose to reveal one." },
    ],
  },
  {
    group: "Network",
    items: [
      { path: "/network/services", label: "Services", phase: 4, about: "Stable addresses in front of a set of pods." },
      { path: "/network/ingresses", label: "Ingresses", phase: 4, about: "HTTP routes from outside the cluster to services." },
    ],
  },
];

// The same pages as a flat list, handy for building routes.
export const allPages = sections.flatMap((section) =>
  section.items.map((item) => ({ ...item, group: section.group })),
);
