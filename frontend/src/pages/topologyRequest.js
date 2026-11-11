// What the Topology page asks for, from its URL (see Topology.jsx):
//
//   topologyRequest(params) → { mode, path, namespace, object, q, depth, key }
//
//   mode   "namespace", "object", "search", or "pick" (no namespace: the list)
//   path   the graph endpoint under /clusters/{id}, or null for "pick"
//   key    what the view is about, without the depth: a new one starts it
//          afresh (no selection), a new depth keeps it

// The kinds the graph holds, for live updates: a change to any of them
// (in the page's namespace) loads the map again.
export const GRAPH_KINDS = [
  "configmaps",
  "cronjobs",
  "daemonsets",
  "deployments",
  "endpointslices",
  "gatewayclasses",
  "gateways",
  "httproutes",
  "ingresses",
  "jobs",
  "nodes",
  "persistentvolumeclaims",
  "persistentvolumes",
  "pods",
  "replicasets",
  "secrets",
  "serviceaccounts",
  "services",
  "statefulsets",
  "storageclasses",
];

export function topologyRequest(params) {
  const namespace = params.get("ns") ?? "";
  const q = params.get("q")?.trim();
  const object = parseObject(params.get("object"));
  const depth = clampDepth(params.get("depth"));
  const enc = encodeURIComponent;

  if (q) {
    const ns = namespace ? `&namespace=${enc(namespace)}` : "";
    return { mode: "search", path: `/graph/search?q=${enc(q)}${ns}`, namespace, q, depth, key: `q|${q}|${namespace}` };
  }
  if (object) {
    // The object's namespace is the one a graph built on request reads.
    const ns = object.namespace ? `&namespace=${enc(object.namespace)}` : "";
    return {
      mode: "object",
      path: `/graph/find?resource=${enc(object.resource)}${ns}&name=${enc(object.name)}&depth=${depth}`,
      namespace: object.namespace ?? namespace,
      object,
      depth,
      key: `object|${object.resource}|${object.namespace ?? ""}|${object.name}`,
    };
  }
  if (namespace) {
    return { mode: "namespace", path: `/graph/subgraph?namespace=${enc(namespace)}`, namespace, depth, key: `ns|${namespace}` };
  }
  return { mode: "pick", path: null, namespace, depth, key: "pick" };
}

// parseObject reads "deployments/shop/api", or "nodes/worker-1" for a
// cluster-wide kind.
export function parseObject(value) {
  const parts = value?.split("/").filter(Boolean) ?? [];
  if (parts.length === 3) return { resource: parts[0], namespace: parts[1], name: parts[2] };
  if (parts.length === 2) return { resource: parts[0], name: parts[1] };
  return null;
}

// objectParam is parseObject's other way: what a link to an object's map
// puts in ?object=.
export function objectParam(resource, namespace, name) {
  return namespace ? `${resource}/${namespace}/${name}` : `${resource}/${name}`;
}

function clampDepth(value) {
  const n = Number.parseInt(value ?? "", 10);
  return Number.isFinite(n) ? Math.min(3, Math.max(1, n)) : 2;
}
