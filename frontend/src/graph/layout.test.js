import { describe, expect, it } from "vitest";
import { HEADER_H, NODE_H, NODE_W, layout } from "./layout.js";

const KINDS = {
  gatewayclasses: "GatewayClass",
  gateways: "Gateway",
  httproutes: "HTTPRoute",
  ingresses: "Ingress",
  services: "Service",
  horizontalpodautoscalers: "HorizontalPodAutoscaler",
  deployments: "Deployment",
  cronjobs: "CronJob",
  jobs: "Job",
  replicasets: "ReplicaSet",
  pods: "Pod",
  configmaps: "ConfigMap",
  secrets: "Secret",
  nodes: "Node",
};

// node("deployments/shop/web") → a node as the API sends it.
function node(id, extra = {}) {
  const parts = id.replace(/^pods:/, "").split("/");
  const resource = id.startsWith("pods:") ? "pods" : parts[0];
  const name = parts.at(-1);
  return { id, resource, kind: KINDS[resource], namespace: parts.length === 3 ? parts[1] : "", name, state: "ok", status: "", ...extra };
}

const edge = (from, to, type = "uses") => ({ from, to, type });

// A shop: a Gateway and its class, a route to two Services, their
// Deployments and pods, a ConfigMap and a Secret both use, and a node.
const shop = {
  nodes: [
    node("gatewayclasses/nginx"),
    node("gateways/infra/main"),
    node("httproutes/shop/store"),
    node("services/shop/web"),
    node("services/shop/api"),
    node("deployments/shop/web"),
    node("deployments/shop/api"),
    node("pods:deployments/shop/web", { pods: { owner: "deployments/shop/web", ready: 3, total: 3, members: [] } }),
    node("pods:deployments/shop/api", { pods: { owner: "deployments/shop/api", ready: 1, total: 2, members: [] } }),
    node("configmaps/shop/settings"),
    node("secrets/shop/stripe", { state: "missing" }),
    node("nodes/worker-1", { state: "failing" }),
  ],
  edges: [
    edge("gateways/infra/main", "gatewayclasses/nginx", "attaches"),
    edge("httproutes/shop/store", "gateways/infra/main", "attaches"),
    edge("httproutes/shop/store", "services/shop/web", "routes-to"),
    edge("httproutes/shop/store", "services/shop/api", "routes-to"),
    edge("services/shop/web", "pods:deployments/shop/web", "selects"),
    edge("services/shop/api", "pods:deployments/shop/api", "selects"),
    edge("deployments/shop/web", "pods:deployments/shop/web", "owns"),
    edge("deployments/shop/api", "pods:deployments/shop/api", "owns"),
    edge("pods:deployments/shop/web", "configmaps/shop/settings"),
    edge("pods:deployments/shop/api", "configmaps/shop/settings"),
    edge("pods:deployments/shop/api", "secrets/shop/stripe"),
    edge("pods:deployments/shop/api", "nodes/worker-1", "runs-on"),
  ],
};

function shuffled(list, seed) {
  const copy = [...list];
  let s = seed;
  for (let i = copy.length - 1; i > 0; i--) {
    s = (s * 9301 + 49297) % 233280;
    const j = Math.floor((s / 233280) * (i + 1));
    [copy[i], copy[j]] = [copy[j], copy[i]];
  }
  return copy;
}

// crossingsOf counts edges that cross between neighbouring columns.
function crossingsOf(result) {
  const segments = result.edges
    .map((e) => [result.nodes[e.from], result.nodes[e.to]])
    .filter(([a, b]) => Math.abs(a.rank - b.rank) === 1)
    .map(([a, b]) => (a.rank < b.rank ? [a, b] : [b, a]));
  let count = 0;
  for (let i = 0; i < segments.length; i++) {
    for (let j = i + 1; j < segments.length; j++) {
      const [a1, b1] = segments[i];
      const [a2, b2] = segments[j];
      if (a1.rank === a2.rank && (a1.y - a2.y) * (b1.y - b2.y) < 0) count++;
    }
  }
  return count;
}

const columnOrder = (result, rank) =>
  Object.values(result.nodes)
    .filter((n) => n.rank === rank)
    .sort((a, b) => a.y - b.y)
    .map((n) => n.id);

describe("layout", () => {
  it("puts kinds in columns, left to right, leaving out empty ones", () => {
    const result = layout(shop);
    expect(result.columns.map((c) => c.label)).toEqual([
      "GatewayClasses",
      "Gateways",
      "HTTPRoutes",
      "Services",
      "Deployments",
      "Pods",
      "Config and claims",
      "Nodes",
    ]);
    const x = (id) => result.nodes[id].x;
    expect(x("gatewayclasses/nginx")).toBeLessThan(x("gateways/infra/main"));
    expect(x("services/shop/web")).toBeLessThan(x("deployments/shop/web"));
    expect(x("pods:deployments/shop/web")).toBeLessThan(x("secrets/shop/stripe"));
    expect(x("configmaps/shop/settings")).toBe(x("secrets/shop/stripe"));
  });

  it("never overlaps cards", () => {
    const big = {
      nodes: [...shop.nodes, ...Array.from({ length: 30 }, (_, i) => node(`configmaps/shop/extra-${i}`))],
      edges: [
        ...shop.edges,
        ...Array.from({ length: 30 }, (_, i) => edge(i % 2 ? "pods:deployments/shop/web" : "pods:deployments/shop/api", `configmaps/shop/extra-${i}`)),
      ],
    };
    const result = layout(big);
    for (let rank = 0; rank < result.columns.length; rank++) {
      const column = columnOrder(result, rank).map((id) => result.nodes[id]);
      for (let i = 1; i < column.length; i++) {
        expect(column[i].y - column[i - 1].y).toBeGreaterThanOrEqual(NODE_H + 16);
      }
    }
    for (const n of Object.values(result.nodes)) {
      expect(n.y).toBeGreaterThanOrEqual(HEADER_H);
      expect(n.y + n.h).toBeLessThanOrEqual(result.height);
      expect(n.x + n.w).toBeLessThanOrEqual(result.width);
    }
  });

  it("gives the same layout whatever order the view comes in", () => {
    const first = layout(shop);
    for (const seed of [1, 2, 3]) {
      expect(layout({ nodes: shuffled(shop.nodes, seed), edges: shuffled(shop.edges, seed + 10) })).toEqual(first);
    }
  });

  it("orders cards so edges don't cross", () => {
    // By name, web's Service would sit above api's while their pods are
    // the other way round.
    const view = {
      nodes: [node("services/shop/a"), node("services/shop/b"), node("deployments/shop/a"), node("deployments/shop/b"),
        node("pods/shop/y"), node("pods/shop/z")],
      edges: [
        edge("services/shop/a", "pods/shop/z", "selects"),
        edge("services/shop/b", "pods/shop/y", "selects"),
        edge("deployments/shop/a", "pods/shop/z", "owns"),
        edge("deployments/shop/b", "pods/shop/y", "owns"),
      ],
    };
    const result = layout(view);
    expect(result.crossings).toBe(0);
    expect(crossingsOf(result)).toBe(0);
    expect(layout(shop).crossings).toBe(0);
  });

  it("lines a card up with what it connects to", () => {
    // A Deployment beside its pods; the Service's edge to the same pods
    // runs past it, so they can't be exactly level.
    const result = layout(shop);
    const middle = (id) => result.nodes[id].y + NODE_H / 2;
    expect(Math.abs(middle("deployments/shop/web") - middle("pods:deployments/shop/web"))).toBeLessThan(NODE_H / 2);
    expect(middle("gateways/infra/main")).toBeCloseTo(middle("gatewayclasses/nginx"), 0);
  });

  it("runs an edge that skips columns between cards, not over them", () => {
    // The route's edge to its Gateway skips nothing; the pods' edge to the
    // node skips the config column, where it must pass between the cards.
    const result = layout(shop);
    const runsOn = result.edges.find((e) => e.type === "runs-on");
    const config = result.columns.findIndex((c) => c.key === "config");
    const cards = Object.values(result.nodes).filter((n) => n.rank === config);
    // The lane runs straight across the column: "L x+W y" right after reaching x.
    const lane = runsOn.path.match(new RegExp(`L ${cards[0].x + NODE_W} ([\\d.]+)`));
    expect(lane).not.toBeNull();
    const y = Number(lane[1]);
    for (const card of cards) {
      expect(y < card.y - 4 || y > card.y + card.h + 4).toBe(true);
    }
  });

  it("draws edges from left to right whichever way they point", () => {
    const result = layout(shop);
    const attach = result.edges.find((e) => e.from === "httproutes/shop/store" && e.to === "gateways/infra/main");
    const gateway = result.nodes["gateways/infra/main"];
    expect(attach.path.startsWith(`M ${gateway.x + NODE_W} `)).toBe(true);
  });

  it("puts a CronJob's Jobs between it and their pods", () => {
    const view = {
      nodes: [node("cronjobs/shop/report"), node("jobs/shop/report-1"), node("jobs/shop/migrate"), node("pods/shop/report-1-x")],
      edges: [edge("cronjobs/shop/report", "jobs/shop/report-1", "owns"), edge("jobs/shop/report-1", "pods/shop/report-1-x", "owns")],
    };
    const result = layout(view);
    expect(result.nodes["jobs/shop/report-1"].rank).toBe(1);
    expect(result.nodes["jobs/shop/migrate"].rank).toBe(0);
    expect(result.columns[1].label).toBe("Jobs");
  });

  it("keeps the order of cards that were there before", () => {
    const before = layout(shop);
    const grown = {
      nodes: [...shop.nodes, node("services/shop/aaa"), node("deployments/shop/aaa"), node("pods/shop/aaa-1")],
      edges: [...shop.edges, edge("services/shop/aaa", "pods/shop/aaa-1", "selects"), edge("deployments/shop/aaa", "pods/shop/aaa-1", "owns")],
    };
    const after = layout(grown, { previous: before.order });
    for (let rank = 0; rank < before.columns.length; rank++) {
      const old = columnOrder(before, rank);
      const now = columnOrder(after, rank).filter((id) => old.includes(id));
      expect(now).toEqual(old);
    }
  });

  it("draws an empty view as nothing", () => {
    expect(layout({ nodes: [], edges: [] })).toMatchObject({ width: 0, columns: [], nodes: {}, edges: [] });
  });
});
