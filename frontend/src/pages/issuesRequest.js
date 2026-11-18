// What the Issues page shows, from its URL (see Issues.jsx):
//
//   ?ns=shop                       the namespace picker's namespace
//   ?tab=resolved                  the last hour's resolved issues (open by default)
//   ?severity=critical             one severity
//   ?type=replicas_unavailable,rollout_stuck   one or more signal types
//   ?kind=deployments              one kind of object
//   ?signal=<id>                   a row to open and scroll to
//
// The page loads every issue of the tab (and namespace) and filters here,
// so the filters can offer what's there to pick from.

export function issuesRequest(params) {
  return {
    namespace: params.get("ns") ?? "",
    tab: params.get("tab") === "resolved" ? "resolved" : "open",
    severity: params.get("severity") ?? "",
    types: (params.get("type") ?? "").split(",").filter(Boolean),
    kind: params.get("kind") ?? "",
    signal: params.get("signal") ?? "",
  };
}

// issuesPath is the API path for a request: only what the server needs.
export function issuesPath(clusterId, r) {
  const q = new URLSearchParams();
  if (r.namespace) q.set("namespace", r.namespace);
  if (r.tab === "resolved") q.set("state", "resolved");
  const query = q.toString();
  return `/clusters/${encodeURIComponent(clusterId)}/issues${query ? "?" + query : ""}`;
}

// filtered keeps the groups the filters let through. A group's kind is
// the kind of its objects: pods, for a group of a workload's pods.
export function filtered(groups, r) {
  return groups.filter(
    (g) =>
      (!r.severity || g.severity === r.severity) &&
      (!r.types.length || r.types.includes(g.type)) &&
      (!r.kind || g.signals[0].resource.resource === r.kind),
  );
}

// choices lists what the filters can pick, with how many groups each
// would show: [{ value, label, count }], most first, then by label.
export function choices(groups) {
  const tally = (key, label) => {
    const m = new Map();
    for (const g of groups) {
      const k = key(g);
      m.set(k, { value: k, label: label(g), count: (m.get(k)?.count ?? 0) + 1 });
    }
    return [...m.values()].sort((a, b) => b.count - a.count || a.label.localeCompare(b.label));
  };
  return {
    types: tally((g) => g.type, (g) => g.title),
    kinds: tally((g) => g.signals[0].resource.resource, (g) => g.signals[0].resource.kind),
  };
}

// withFilter is the query string with one filter set (or cleared, with
// ""), keeping the rest and dropping an opened row.
export function withFilter(params, name, value) {
  const next = new URLSearchParams(params);
  if (value) next.set(name, value);
  else next.delete(name);
  next.delete("signal");
  return next;
}

// issuesLink is a link to the Issues page, e.g. from the overview's counts:
// issuesLink("lab", { kind: "deployments", types: ["replicas_unavailable"] }, "?ns=shop").
export function issuesLink(clusterId, { kind, types, severity, signal } = {}, search = "") {
  const q = new URLSearchParams(search);
  if (severity) q.set("severity", severity);
  if (kind) q.set("kind", kind);
  if (types?.length) q.set("type", types.join(","));
  if (signal) q.set("signal", signal);
  const query = q.toString();
  return `/c/${encodeURIComponent(clusterId)}/issues${query ? "?" + query : ""}`;
}
