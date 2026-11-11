import * as icons from "../components/icons.jsx";

// What the map knows about kinds and edges: icons, names, and how each
// kind of edge reads from either end.

// The icon for each kind: the sidebar's, where the kind has a page.
const ICONS = {
  nodes: icons.NodesIcon,
  pods: icons.PodsIcon,
  deployments: icons.DeploymentsIcon,
  statefulsets: icons.StatefulSetsIcon,
  daemonsets: icons.DaemonSetsIcon,
  replicasets: icons.ReplicaSetsIcon,
  jobs: icons.JobsIcon,
  cronjobs: icons.CronJobsIcon,
  configmaps: icons.ConfigMapsIcon,
  secrets: icons.SecretsIcon,
  serviceaccounts: icons.ServiceAccountsIcon,
  persistentvolumeclaims: icons.PersistentVolumeClaimsIcon,
  persistentvolumes: icons.PersistentVolumesIcon,
  storageclasses: icons.StorageClassesIcon,
  services: icons.ServicesIcon,
  endpointslices: icons.EndpointSlicesIcon,
  ingresses: icons.IngressesIcon,
  gateways: icons.GatewaysIcon,
  httproutes: icons.HTTPRoutesIcon,
  gatewayclasses: icons.GatewayClassesIcon,
};

export function kindIcon(resource) {
  return ICONS[resource] ?? icons.FileTextIcon;
}

// pluralKind: "Pod" → "Pods", "Ingress" → "Ingresses", "StorageClass" →
// "StorageClasses".
export function pluralKind(kind) {
  if (!kind) return "";
  if (/(s|x|ch|sh)$/.test(kind)) return `${kind}es`;
  if (/[^aeiou]y$/.test(kind)) return `${kind.slice(0, -1)}ies`;
  return `${kind}s`;
}

// countOf: "1 pod", "3 pods".
export function countOf(n, kind) {
  return `${n} ${n === 1 ? kind : pluralKind(kind)}`;
}

// How each kind of edge reads from either end. An edge points from what
// needs to what it needs, so "out" is what this object needs ("Uses
// ConfigMap app-config") and "in" what needs it ("Used by Pod web-1").
export const RELATIONS = {
  owns: { out: "Owns", in: "Owned by" },
  references: { out: "Uses", in: "Used by" },
  binds: { out: "Bound to", in: "Bound from" },
  "runs-on": { out: "Runs on", in: "Runs" },
  selects: { out: "Selects", in: "Selected by" },
  endpoints: { out: "Lists as an endpoint", in: "Listed by" },
  routes: { out: "Sends traffic to", in: "Gets traffic from" },
  attaches: { out: "Attached to", in: "Has attached" },
};

export function relation(kind, out) {
  return RELATIONS[kind]?.[out ? "out" : "in"] ?? kind;
}

// The lines on a map, for the legend: which edge kinds each style draws.
export const LINE_STYLES = [
  { style: "solid", kinds: ["owns", "routes", "attaches"], label: "Owns, sends traffic to, attached to" },
  { style: "dashed", kinds: ["references", "binds"], label: "Uses, bound to" },
  { style: "thin", kinds: ["selects", "runs-on", "endpoints"], label: "Selects, runs on" },
];

export const LINE_STYLE = Object.fromEntries(LINE_STYLES.flatMap((s) => s.kinds.map((k) => [k, s.style])));
