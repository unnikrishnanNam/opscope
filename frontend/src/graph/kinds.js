import * as icons from "../components/icons.jsx";

// The icon for each kind on a map: the sidebar's, where the kind has a page.
const ICONS = {
  nodes: icons.NodesIcon,
  pods: icons.PodsIcon,
  deployments: icons.DeploymentsIcon,
  statefulsets: icons.StatefulSetsIcon,
  daemonsets: icons.DaemonSetsIcon,
  jobs: icons.JobsIcon,
  cronjobs: icons.CronJobsIcon,
  replicasets: icons.ReplicaSetsIcon,
  configmaps: icons.ConfigMapsIcon,
  secrets: icons.SecretsIcon,
  serviceaccounts: icons.ServiceAccountsIcon,
  persistentvolumeclaims: icons.ClaimsIcon,
  persistentvolumes: icons.VolumesIcon,
  storageclasses: icons.StorageClassesIcon,
  resourcequotas: icons.QuotasIcon,
  services: icons.ServicesIcon,
  ingresses: icons.IngressesIcon,
  ingressclasses: icons.GatewayClassesIcon,
  gateways: icons.GatewaysIcon,
  httproutes: icons.HTTPRoutesIcon,
  gatewayclasses: icons.GatewayClassesIcon,
  referencegrants: icons.GrantsIcon,
  horizontalpodautoscalers: icons.AutoscalersIcon,
  poddisruptionbudgets: icons.DisruptionBudgetsIcon,
  namespaces: icons.NamespaceIcon,
  apps: icons.AppsIcon,
};

export function kindIcon(resource) {
  return ICONS[resource] ?? icons.FileTextIcon;
}

const OWNER_KINDS = {
  deployments: "Deployment",
  statefulsets: "StatefulSet",
  daemonsets: "DaemonSet",
  replicasets: "ReplicaSet",
  jobs: "Job",
};

// ownerKind is the kind of a group of pods' workload ("Deployment"), even
// when the workload itself isn't on the map.
export function ownerKind(group, nodes) {
  return nodes[group.pods.owner]?.kind ?? OWNER_KINDS[group.pods.owner.split("/")[0]] ?? "workload";
}

// How each kind of edge reads, from either end, for the node panel and the
// list: a Deployment "Owns" its pods, which are "Owned by" it.
export const RELATIONS = {
  owns: { out: "Owns", in: "Owned by" },
  "runs-on": { out: "Runs on", in: "Runs" },
  uses: { out: "Uses", in: "Used by" },
  selects: { out: "Selects", in: "Selected by" },
  "routes-to": { out: "Sends traffic to", in: "Gets traffic from" },
  attaches: { out: "Attached to", in: "Has attached" },
  permits: { out: "Allowed by", in: "Allows" },
  scales: { out: "Scales", in: "Scaled by" },
  blames: { out: "Held up by", in: "Holds up" },
};

// STATE_WORDS say each state in words, for accessible names and the list.
export const STATE_WORDS = {
  ok: "OK",
  progressing: "progressing",
  degraded: "degraded",
  failing: "failing",
  missing: "missing",
  unknown: "can't be read",
};
