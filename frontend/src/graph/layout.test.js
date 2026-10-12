import { describe, expect, it } from "vitest";
import { BOX_TOP, BOX_X, HEADER_H, NODE_H, NODE_W, layout } from "./layout.js";

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
  apps: "App",
  namespaces: "Namespace",
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

describe("the cluster view", () => {
  // Apps in their namespaces, folded namespaces, a Gateway two apps use,
  // and three machines.
  const app = (ns, name, extra = {}) => node(`apps/${ns}/${name}`, { group: `namespaces/${ns}`, namespace: ns, ...extra });
  const cluster = {
    nodes: [
      node("gateways/infra/main"),
      app("shop", "web"),
      app("shop", "api", { state: "failing" }),
      app("blog", "blog"),
      app("blog", "db"),
      node("namespaces/calm"),
      node("namespaces/quiet"),
      node("nodes/worker-1"),
      node("nodes/worker-2", { state: "failing" }),
      node("nodes/worker-3"),
    ],
    edges: [edge("apps/shop/web", "gateways/infra/main", "attaches"), edge("apps/blog/blog", "gateways/infra/main", "attaches")],
    groups: [
      { id: "namespaces/blog", name: "blog", state: "ok" },
      { id: "namespaces/shop", name: "shop", state: "failing" },
    ],
  };

  it("keeps each namespace's apps together, in a box around them", () => {
    const result = layout(cluster);
    const apps = Object.values(result.nodes)
      .filter((n) => n.id.startsWith("apps/") || n.id.startsWith("namespaces/"))
      .sort((a, b) => a.y - b.y)
      .map((n) => n.id.split("/")[1]);
    // Each namespace's apps are next to each other.
    expect(apps.join(",")).toMatch(/^(?:(?!shop).)*shop,shop(?:(?!shop).)*$/);
    expect(apps.join(",")).toMatch(/^(?:(?!blog).)*blog,blog(?:(?!blog).)*$/);

    expect(result.groups.map((g) => g.label).sort()).toEqual(["blog", "shop"]);
    for (const group of result.groups) {
      for (const n of Object.values(result.nodes).filter((n) => !n.section)) {
        const inside = n.x >= group.x && n.x + n.w <= group.x + group.w && n.y >= group.y && n.y + n.h <= group.y + group.h;
        const member = n.id.startsWith(`apps/${group.label}/`);
        expect(inside).toBe(member);
      }
      expect(group.x).toBe(result.nodes[`apps/${group.label}/${group.label === "shop" ? "web" : "db"}`].x - BOX_X);
    }
    const shop = result.groups.find((g) => g.label === "shop");
    expect(shop.state).toBe("failing");
    const firstShop = Math.min(result.nodes["apps/shop/web"].y, result.nodes["apps/shop/api"].y);
    expect(firstShop - shop.y).toBe(BOX_TOP);
  });

  it("lays out what nothing joins as a grid, the worst first, and the machines in a strip under it", () => {
    const result = layout(cluster, { grid: ["apps", "namespaces"], strip: ["nodes"] });
    // The apps joined to the Gateway stay in the columns, with their whole
    // namespace; the rest are in the grid.
    expect(result.columns.map((c) => c.key)).toEqual(["entry", "apps"]);
    for (const id of ["apps/shop/web", "apps/shop/api", "apps/blog/blog", "apps/blog/db"]) {
      expect(result.nodes[id].section).toBeUndefined();
    }
    const grid = result.sections.find((s) => s.key === "grid");
    expect(grid.ids).toEqual(["namespaces/calm", "namespaces/quiet"]);
    const strip = result.sections.find((s) => s.key === "strip");
    expect(strip).toMatchObject({ label: "Nodes", ids: ["nodes/worker-1", "nodes/worker-2", "nodes/worker-3"] });

    const columnsBottom = Math.max(...Object.values(result.nodes).filter((n) => !n.section).map((n) => n.y + n.h));
    const gridBottom = Math.max(...grid.ids.map((id) => result.nodes[id].y + NODE_H));
    expect(grid.y).toBeGreaterThan(columnsBottom);
    expect(strip.y).toBeGreaterThan(gridBottom);
    const machines = strip.ids.map((id) => result.nodes[id]);
    for (const m of machines) expect(m.y).toBe(strip.y + HEADER_H);
    expect(machines[1].x).toBeGreaterThanOrEqual(machines[0].x + NODE_W);
    expect(result.height).toBe(machines[0].y + NODE_H);
  });

  it("puts a namespace in the grid as its box, the worst first", () => {
    const quiet = { ...cluster, edges: [] }; // nothing joined: everything in the grid
    const result = layout(quiet, { grid: ["apps", "namespaces"], strip: ["nodes"] });
    expect(result.columns.map((c) => c.key)).toEqual(["entry"]); // the Gateway, on its own
    const grid = result.sections.find((s) => s.key === "grid");
    expect(grid.ids.slice(0, 2)).toEqual(["apps/shop/api", "apps/shop/web"]); // shop is failing: first, its failing app on top
    const shop = result.groups.find((g) => g.label === "shop");
    for (const id of ["apps/shop/api", "apps/shop/web"]) {
      const n = result.nodes[id];
      expect(n.x).toBe(shop.x + BOX_X);
      expect(n.y).toBeGreaterThanOrEqual(shop.y + BOX_TOP);
      expect(n.y + NODE_H).toBeLessThanOrEqual(shop.y + shop.h);
    }
    // Blocks never overlap.
    const cards = grid.ids.map((id) => result.nodes[id]);
    for (const a of cards) {
      for (const b of cards) {
        if (a === b) continue;
        expect(a.x + a.w <= b.x || b.x + b.w <= a.x || a.y + a.h <= b.y || b.y + b.h <= a.y).toBe(true);
      }
    }
  });

  it("is columns like any other map without the grid and the strip", () => {
    const result = layout(cluster);
    expect(result.sections).toEqual([]);
    expect(result.columns.map((c) => c.key)).toEqual(["entry", "apps", "nodes"]);
  });
});
