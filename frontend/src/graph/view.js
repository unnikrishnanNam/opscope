import { countOf } from "./kinds.js";

// shape turns a graph answer (GET /api/clusters/{id}/graph/…) into what a
// map draws. The graph holds every object; a map is easier to read with a
// few of them folded away, and nothing it folds is lost: the panel and the
// list still say it.
//
//   shape(answer, { expanded }) → { nodes, edges, focus, folded }
//
//   - EndpointSlices aren't drawn. A Service's `selects` edges already join
//     it to its pods; the slices' count of ready and not-ready pods goes on
//     the Service instead (`endpoints`).
//   - A Deployment's ReplicaSets are folded into it: its pods are drawn as
//     its own. During a rollout, when more than one of them has pods, the
//     ones with pods are drawn; old ones without pods never are.
//   - Pods with the same owner are one card ("3 pods") when there are two
//     or more, unless the owner's ID is in `expanded`. The object the map is
//     about is never folded into a group.
//
// Nodes keep the API's fields. A group of pods is a node with `pods`:
// { owner, members }. An edge drawn for several edges (a group's, or one
// through a ReplicaSet) keeps all their evidence and says how many it
// stands for in `count`.
export function shape(answer, { expanded = new Set() } = {}) {
  const focus = answer.focus;
  const byId = new Map(answer.nodes.map((n) => [n.id, n]));
  let edges = answer.edges.filter((e) => byId.has(e.from) && byId.has(e.to));
  const folded = { endpointslices: 0, replicasets: 0 };

  // ---- EndpointSlices ----
  const slices = new Set(answer.nodes.filter((n) => n.resource === "endpointslices").map((n) => n.id));
  const sliceOwner = new Map(); // slice → its Service
  for (const e of edges) {
    if (e.kind === "owns" && slices.has(e.to) && byId.get(e.from).resource === "services") sliceOwner.set(e.to, e.from);
  }
  const endpoints = new Map(); // Service → { ready, notReady }
  for (const e of edges) {
    if (e.kind !== "endpoints" || !sliceOwner.has(e.from)) continue;
    const service = sliceOwner.get(e.from);
    const counts = endpoints.get(service) ?? { ready: 0, notReady: 0 };
    if (e.evidence?.some((ev) => ev.note === "not ready")) counts.notReady++;
    else counts.ready++;
    endpoints.set(service, counts);
  }
  folded.endpointslices = slices.size;
  edges = edges.filter((e) => !slices.has(e.from) && !slices.has(e.to));

  // ---- ReplicaSets ----
  const ownedBy = (id) => edges.filter((e) => e.kind === "owns" && e.from === id).map((e) => e.to);
  const hidden = new Set(slices);
  const deployments = answer.nodes.filter((n) => n.resource === "deployments");
  for (const d of deployments) {
    const sets = ownedBy(d.id).filter((id) => byId.get(id).resource === "replicasets");
    const withPods = sets.filter((rs) => ownedBy(rs).some((p) => byId.get(p).resource === "pods"));
    for (const rs of sets) {
      if (withPods.length > 1 && withPods.includes(rs)) continue; // a rollout: drawn
      hidden.add(rs);
      folded.replicasets++;
      // Its pods become the Deployment's, by way of it.
      edges = edges.flatMap((e) => {
        if (e.from === d.id && e.to === rs) return [];
        if (e.from === rs && e.kind === "owns") {
          const evidence = (e.evidence ?? []).map((ev) => ({ ...ev, note: `through ReplicaSet ${byId.get(rs).name}` }));
          return [{ ...e, id: `${e.id}~${d.id}`, from: d.id, evidence }];
        }
        if (e.from === rs || e.to === rs) return [];
        return [e];
      });
    }
  }

  // ---- Groups of pods ----
  const pods = answer.nodes.filter((n) => n.resource === "pods" && !hidden.has(n.id));
  const ownerOf = new Map(); // pod → its owner (the first, by ID, if several)
  for (const e of edges) {
    if (e.kind === "owns" && (!ownerOf.has(e.to) || e.from < ownerOf.get(e.to))) ownerOf.set(e.to, e.from);
  }
  const members = new Map(); // owner → its pods
  for (const pod of pods) {
    if (pod.id === focus) continue;
    const owner = ownerOf.get(pod.id);
    if (!owner || expanded.has(owner)) continue;
    if (!members.has(owner)) members.set(owner, []);
    members.get(owner).push(pod);
  }
  const groupOf = new Map(); // pod → its group's ID
  const groups = [];
  for (const [owner, list] of members) {
    if (list.length < 2) continue;
    const ownerNode = byId.get(owner);
    const id = `pods:${owner}`;
    list.sort((a, b) => compare(a.name, b.name));
    for (const pod of list) groupOf.set(pod.id, id);
    groups.push({
      id,
      resource: "pods",
      kind: "Pod",
      namespace: list[0].namespace,
      name: ownerNode.name,
      presence: "present",
      status: statusSummary(list),
      health: null,
      pods: { owner, ownerKind: ownerNode.kind, members: list },
    });
  }

  // Edges at a grouped pod move to its group; the same edge from several
  // pods is drawn once.
  const merged = new Map();
  for (const e of edges) {
    const from = groupOf.get(e.from) ?? e.from;
    const to = groupOf.get(e.to) ?? e.to;
    if (from === to) continue;
    const key = from === e.from && to === e.to ? e.id : `${e.kind}|${from}|${to}`;
    const seen = merged.get(key);
    if (seen) {
      seen.count++;
      seen.optional = seen.optional && Boolean(e.optional);
      seen.evidence.push(...(e.evidence ?? []));
    } else {
      merged.set(key, { ...e, id: key, from, to, count: 1, optional: Boolean(e.optional), evidence: [...(e.evidence ?? [])] });
    }
  }

  const nodes = [
    ...answer.nodes
      .filter((n) => !hidden.has(n.id) && !groupOf.has(n.id))
      .map((n) => (endpoints.has(n.id) ? { ...n, endpoints: endpoints.get(n.id) } : n)),
    ...groups,
  ].sort((a, b) => compare(a.id, b.id));
  return {
    nodes,
    edges: [...merged.values()].sort((a, b) => compare(a.id, b.id)),
    focus,
    folded,
  };
}

// statusSummary sums up a group's statuses: "3 Running", "2 Running, 1 Pending".
export function statusSummary(pods) {
  const counts = new Map();
  for (const p of pods) {
    const s = p.status || "Unknown";
    counts.set(s, (counts.get(s) ?? 0) + 1);
  }
  return [...counts]
    .sort((a, b) => b[1] - a[1] || compare(a[0], b[0]))
    .map(([s, n]) => `${n} ${s}`)
    .join(", ");
}

// cardKind is what a card says it is: its kind, or for a group "3 pods".
// unreadable is the kinds a map should say it couldn't read: those the
// cluster has but Opscope isn't allowed to list. A kind the cluster doesn't
// have (Gateway API) has nothing to read, so it isn't a gap.
export function unreadable(partial) {
  return (partial ?? []).filter((p) => p.code !== "not_installed");
}

export function cardKind(n) {
  return n.pods ? countOf(n.pods.members.length, "pod") : n.kind;
}

export function compare(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}
