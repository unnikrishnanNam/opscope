import { describe, expect, it } from "vitest";
import { cardKind, shape, statusSummary, unreadable } from "./view.js";

// Answers shaped like GET /graph/…: node(id) for "resource/namespace/name".
const KINDS = {
  deployments: "Deployment",
  replicasets: "ReplicaSet",
  pods: "Pod",
  services: "Service",
  endpointslices: "EndpointSlice",
  configmaps: "ConfigMap",
  secrets: "Secret",
  nodes: "Node",
  statefulsets: "StatefulSet",
};

function node(id, extra = {}) {
  const parts = id.split("/");
  return {
    id,
    resource: parts[0],
    kind: KINDS[parts[0]],
    namespace: parts.length === 3 ? parts[1] : undefined,
    name: parts.at(-1),
    presence: "present",
    status: "",
    health: null,
    ...extra,
  };
}

let n = 0;
function edge(from, to, kind, note = "") {
  return { id: `e${n++}`, from, to, kind, evidence: [{ builder: "b", field: `f${n}`, note }] };
}

// A Deployment with an old and a current ReplicaSet, three pods behind a
// Service and its EndpointSlice, each using a ConfigMap, on a node.
function shop({ rollout = false } = {}) {
  const pods = ["pods/shop/web-a", "pods/shop/web-b", "pods/shop/web-c"];
  return {
    focus: "deployments/shop/web",
    nodes: [
      node("deployments/shop/web"),
      node("replicasets/shop/web-new"),
      node("replicasets/shop/web-old"),
      ...pods.map((p, i) => node(p, { status: i === 2 ? "Pending" : "Running" })),
      node("services/shop/web"),
      node("endpointslices/shop/web-x"),
      node("configmaps/shop/settings"),
      node("nodes/n1"),
    ],
    edges: [
      edge("deployments/shop/web", "replicasets/shop/web-new", "owns"),
      edge("deployments/shop/web", "replicasets/shop/web-old", "owns"),
      edge("replicasets/shop/web-new", "pods/shop/web-a", "owns"),
      edge("replicasets/shop/web-new", "pods/shop/web-b", "owns"),
      edge(rollout ? "replicasets/shop/web-old" : "replicasets/shop/web-new", "pods/shop/web-c", "owns"),
      ...pods.map((p) => edge("services/shop/web", p, "selects")),
      edge("services/shop/web", "endpointslices/shop/web-x", "owns"),
      edge("endpointslices/shop/web-x", "pods/shop/web-a", "endpoints", "ready"),
      edge("endpointslices/shop/web-x", "pods/shop/web-b", "endpoints", "ready"),
      edge("endpointslices/shop/web-x", "pods/shop/web-c", "endpoints", "not ready"),
      ...pods.map((p) => edge(p, "configmaps/shop/settings", "references", "volume config")),
      ...pods.map((p) => edge(p, "nodes/n1", "runs-on")),
    ],
  };
}

const ids = (v) => v.nodes.map((n) => n.id);
const between = (v, from, to) => v.edges.filter((e) => e.from === from && e.to === to);

describe("shape", () => {
  it("draws no EndpointSlices, and counts their pods on the Service", () => {
    const v = shape(shop());
    expect(ids(v)).not.toContain("endpointslices/shop/web-x");
    expect(v.edges.some((e) => e.kind === "endpoints")).toBe(false);
    expect(v.nodes.find((x) => x.id === "services/shop/web").endpoints).toEqual({ ready: 2, notReady: 1 });
    expect(v.folded.endpointslices).toBe(1);
  });

  it("folds a Deployment's ReplicaSets into it, and its pods into one card", () => {
    const v = shape(shop());
    expect(ids(v)).toEqual([
      "configmaps/shop/settings",
      "deployments/shop/web",
      "nodes/n1",
      "pods:deployments/shop/web",
      "services/shop/web",
    ]);
    const group = v.nodes.find((x) => x.pods);
    expect(group.pods.members.map((p) => p.name)).toEqual(["web-a", "web-b", "web-c"]);
    expect(group.status).toBe("2 Running, 1 Pending");
    expect(cardKind(group)).toBe("3 pods");
    // One edge for the three, with all their evidence.
    const owns = between(v, "deployments/shop/web", "pods:deployments/shop/web");
    expect(owns).toHaveLength(1);
    expect(owns[0].count).toBe(3);
    expect(owns[0].evidence[0].note).toBe("through ReplicaSet web-new");
    const uses = between(v, "pods:deployments/shop/web", "configmaps/shop/settings");
    expect(uses[0].count).toBe(3);
    expect(uses[0].evidence).toHaveLength(3);
    expect(v.folded.replicasets).toBe(2);
  });

  it("draws the ReplicaSets with pods during a rollout", () => {
    const v = shape(shop({ rollout: true }));
    expect(ids(v)).toContain("replicasets/shop/web-new");
    expect(ids(v)).toContain("replicasets/shop/web-old");
    // web-c, alone under the old one, isn't a group.
    expect(ids(v)).toContain("pods/shop/web-c");
    expect(ids(v)).toContain("pods:replicasets/shop/web-new");
  });

  it("shows each pod of an expanded owner", () => {
    const v = shape(shop(), { expanded: new Set(["deployments/shop/web"]) });
    expect(ids(v).filter((id) => id.startsWith("pods/"))).toHaveLength(3);
    expect(v.nodes.some((x) => x.pods)).toBe(false);
  });

  it("never folds the object the map is about", () => {
    const answer = { ...shop(), focus: "pods/shop/web-a" };
    const v = shape(answer);
    expect(ids(v)).toContain("pods/shop/web-a");
    expect(v.nodes.find((x) => x.pods).pods.members.map((p) => p.name)).toEqual(["web-b", "web-c"]);
  });

  it("keeps one pod as itself", () => {
    const answer = shop();
    answer.edges = answer.edges.filter((e) => !(e.kind === "owns" && e.to !== "pods/shop/web-a" && e.to.startsWith("pods/")));
    const v = shape(answer);
    expect(ids(v)).toContain("pods/shop/web-a");
    expect(v.nodes.some((x) => x.pods)).toBe(false);
  });

  it("keeps an edge optional only when every edge it stands for is", () => {
    const answer = shop();
    for (const e of answer.edges) if (e.kind === "references") e.optional = e.from !== "pods/shop/web-a";
    const uses = between(shape(answer), "pods:deployments/shop/web", "configmaps/shop/settings")[0];
    expect(uses.optional).toBe(false);
  });
});

describe("statusSummary", () => {
  it("counts statuses, most first", () => {
    expect(statusSummary([{ status: "Pending" }, { status: "Running" }, { status: "Running" }, { status: "" }])).toBe(
      "2 Running, 1 Pending, 1 Unknown",
    );
  });
});

describe("unreadable", () => {
  it("leaves out kinds the cluster doesn't have", () => {
    const partial = [
      { resource: "gateways", code: "not_installed", reason: "not installed on this cluster" },
      { resource: "secrets", code: "forbidden", reason: "listing across all namespaces isn't allowed" },
    ];
    expect(unreadable(partial).map((p) => p.resource)).toEqual(["secrets"]);
    expect(unreadable(undefined)).toEqual([]);
  });
});
