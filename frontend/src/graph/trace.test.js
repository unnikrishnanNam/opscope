import { describe, expect, it } from "vitest";
import { causeChain, chainEdges, indexFindings, rootCauses, shownState } from "./trace.js";

const finding = (object, severity, blames = []) => ({ id: `${object}|${blames}`, object, severity, blames, title: object });

// A Deployment whose two pods (one card) can't start because a Secret is
// missing; a Service selecting them; a rollout note on another Deployment.
const view = {
  nodes: [
    { id: "services/shop/web" },
    { id: "deployments/shop/web" },
    { id: "pods:deployments/shop/web", pods: { members: ["pods/shop/web-a", "pods/shop/web-b"] } },
    { id: "secrets/shop/stripe" },
    { id: "deployments/shop/other" },
  ],
  edges: [
    { id: "s", from: "services/shop/web", to: "pods:deployments/shop/web", type: "selects" },
    { id: "o", from: "deployments/shop/web", to: "pods:deployments/shop/web", type: "owns" },
    { id: "u", from: "pods:deployments/shop/web", to: "secrets/shop/stripe", type: "uses" },
  ],
  findings: [
    finding("services/shop/web", "failing", ["pods/shop/web-a", "pods/shop/web-b"]),
    finding("deployments/shop/web", "failing", ["pods/shop/web-a", "pods/shop/web-b"]),
    finding("pods/shop/web-a", "failing", ["secrets/shop/stripe"]),
    finding("pods/shop/web-b", "failing", ["secrets/shop/stripe"]),
    finding("secrets/shop/stripe", "failing"),
    finding("deployments/shop/other", "info"),
  ],
};
const placed = {
  "services/shop/web": { x: 0 },
  "deployments/shop/web": { x: 100 },
  "pods:deployments/shop/web": { x: 200 },
  "secrets/shop/stripe": { x: 300 },
};

describe("indexFindings", () => {
  it("files findings about grouped pods under their card", () => {
    const index = indexFindings(view);
    expect(index.byNode.get("pods:deployments/shop/web")).toHaveLength(2);
    expect(index.drawnAs.get("pods/shop/web-b")).toBe("pods:deployments/shop/web");
  });
});

describe("rootCauses", () => {
  it("finds the cards whose problems nothing else explains, leaving out notes", () => {
    expect([...rootCauses(indexFindings(view))]).toEqual([["secrets/shop/stripe", "failing"]]);
  });
});

describe("causeChain", () => {
  it("follows blame from a card to its root cause", () => {
    const chain = causeChain(indexFindings(view), "deployments/shop/web");
    expect([...chain.cards.keys()]).toEqual(["deployments/shop/web", "pods:deployments/shop/web", "secrets/shop/stripe"]);
    expect(chain.cards.get("secrets/shop/stripe")).toEqual({ severity: "failing", root: true });
    expect(chain.cards.get("deployments/shop/web").root).toBe(false);
    expect(chain.links).toEqual([
      { from: "deployments/shop/web", to: "pods:deployments/shop/web", severity: "failing" },
      { from: "pods:deployments/shop/web", to: "secrets/shop/stripe", severity: "failing" },
    ]);
  });

  it("starts from every pod in a group", () => {
    const chain = causeChain(indexFindings(view), "pods:deployments/shop/web");
    expect(chain.links).toEqual([{ from: "pods:deployments/shop/web", to: "secrets/shop/stripe", severity: "failing" }]);
  });

  it("is empty for a card with nothing wrong, or none", () => {
    expect(causeChain(indexFindings(view), "deployments/shop/missing-nothing").links).toEqual([]);
    expect(causeChain(indexFindings(view), null).cards.size).toBe(0);
  });
});

describe("chainEdges", () => {
  it("finds the edges a chain runs along, and which way", () => {
    const index = indexFindings(view);
    const edges = chainEdges(causeChain(index, "deployments/shop/web"), view.edges, placed);
    expect([...edges.keys()].sort()).toEqual(["o", "u"]);
    expect(edges.get("o")).toEqual({ severity: "failing", reverse: false });
  });

  it("follows the map where a link has no edge of its own", () => {
    // A rollout: the Deployment blames the new ReplicaSet's pods.
    const v = {
      nodes: [{ id: "deployments/a/web" }, { id: "replicasets/a/web-2" }, { id: "pods/a/web-2-x" }],
      edges: [
        { id: "d", from: "deployments/a/web", to: "replicasets/a/web-2", type: "owns" },
        { id: "r", from: "replicasets/a/web-2", to: "pods/a/web-2-x", type: "owns" },
      ],
      findings: [finding("deployments/a/web", "degraded", ["pods/a/web-2-x"]), finding("pods/a/web-2-x", "failing")],
    };
    const edges = chainEdges(causeChain(indexFindings(v), "deployments/a/web"), v.edges, {
      "deployments/a/web": { x: 0 },
      "replicasets/a/web-2": { x: 100 },
      "pods/a/web-2-x": { x: 200 },
    });
    expect([...edges]).toEqual([
      ["d", { severity: "degraded", reverse: false }],
      ["r", { severity: "degraded", reverse: false }],
    ]);
  });

  it("marks a chain that runs right to left", () => {
    // A route's Gateway sits left of it; the route blames the Gateway.
    const v = {
      nodes: [{ id: "gateways/a/main" }, { id: "httproutes/a/r" }],
      edges: [{ id: "a", from: "httproutes/a/r", to: "gateways/a/main", type: "attaches" }],
      findings: [finding("httproutes/a/r", "failing", ["gateways/a/main"]), finding("gateways/a/main", "failing")],
    };
    const edges = chainEdges(causeChain(indexFindings(v), "httproutes/a/r"), v.edges, {
      "gateways/a/main": { x: 0 },
      "httproutes/a/r": { x: 100 },
    });
    expect(edges.get("a").reverse).toBe(true);
  });
});

describe("shownState", () => {
  it("is a card's own state, or worse when its findings say so", () => {
    const v = {
      nodes: [{ id: "ingresses/a/shop", state: "ok" }, { id: "deployments/a/web", state: "progressing" }, { id: "secrets/a/x", state: "missing" }],
      edges: [],
      findings: [finding("ingresses/a/shop", "failing", ["services/a/api"]), finding("deployments/a/web", "info")],
    };
    const index = indexFindings(v);
    expect(v.nodes.map((n) => shownState(n, index))).toEqual(["failing", "progressing", "missing"]);
  });
});
