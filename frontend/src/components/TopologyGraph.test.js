import { describe, expect, it } from "vitest";
import { layout } from "../graph/layout.js";
import { cardLabel, firstView, fitAll, gapTag, gridStep, neighbour, statusText } from "./TopologyGraph.jsx";

const node = (id, resource, extra = {}) => ({ id, resource, kind: resource, name: id, namespace: "shop", presence: "present", status: "", ...extra });
const edge = (from, to, kind) => ({ id: `${kind}|${from}|${to}`, from, to, kind });

// A route to two Services, each selecting a pod, both pods on one node.
const view = {
  nodes: [
    node("route", "httproutes"),
    node("svc-a", "services"),
    node("svc-b", "services"),
    node("pod-a", "pods"),
    node("pod-b", "pods"),
    node("n1", "nodes"),
  ],
  edges: [
    edge("route", "svc-a", "routes"),
    edge("route", "svc-b", "routes"),
    edge("svc-a", "pod-a", "selects"),
    edge("svc-b", "pod-b", "selects"),
    edge("pod-a", "n1", "runs-on"),
    edge("pod-b", "n1", "runs-on"),
  ],
};

describe("neighbour", () => {
  const placed = layout(view);
  it("moves along edges left and right, to the nearest by height", () => {
    expect(neighbour(placed, "route", "ArrowRight")).toMatch(/^svc-/);
    expect(neighbour(placed, "svc-a", "ArrowRight")).toBe("pod-a");
    expect(neighbour(placed, "pod-a", "ArrowLeft")).toBe("svc-a");
    expect(neighbour(placed, "pod-b", "ArrowRight")).toBe("n1");
  });

  it("moves up and down a column, and stops at its ends", () => {
    const [top, bottom] = ["svc-a", "svc-b"].sort((a, b) => placed.nodes[a].y - placed.nodes[b].y);
    expect(neighbour(placed, top, "ArrowDown")).toBe(bottom);
    expect(neighbour(placed, bottom, "ArrowUp")).toBe(top);
    expect(neighbour(placed, top, "ArrowUp")).toBeUndefined();
    expect(neighbour(placed, "route", "ArrowLeft")).toBeUndefined();
  });
});

describe("first view", () => {
  const placed = layout(view);
  it("shows the whole map when it can be read that way", () => {
    const t = firstView(placed, { w: 4000, h: 2000 });
    expect(t).toEqual(fitAll(placed, { w: 4000, h: 2000 }));
    expect(t.k).toBe(1); // never larger than life
  });

  it("puts the object it's about in the middle when the whole map would be too small", () => {
    const size = { w: 500, h: 400 };
    const t = firstView(placed, size, "pod-b");
    const card = placed.nodes["pod-b"];
    const middle = (card.x + card.w / 2) * t.k + t.x;
    expect(t.k).toBeGreaterThan(fitAll(placed, size).k);
    expect(Math.abs(middle - size.w / 2)).toBeLessThan(size.w / 4);
  });

  it("starts with no card in sight cut off at the top", () => {
    // One route to eight Services: the route sits halfway down their column.
    const services = Array.from({ length: 8 }, (_, i) => node(`svc-${i}`, "services"));
    const tall = layout({
      nodes: [node("route", "httproutes"), ...services],
      edges: services.map((s) => edge("route", s.id, "routes")),
    });
    const size = { w: 500, h: 400 };
    const t = firstView(tall, size);
    const route = tall.nodes.route;
    const higher = Object.values(tall.nodes).filter((c) => c.y < route.y && c.x * t.k + t.x < size.w);
    expect(higher.length).toBeGreaterThan(0); // the case this is about
    for (const c of higher) expect(c.y * t.k + t.y).toBeGreaterThanOrEqual(24);
    expect(route.y * t.k + t.y).toBeLessThan(size.h); // and the first card is still in sight
  });

  it("starts at the first card of the first column without one", () => {
    const t = firstView(placed, { w: 500, h: 400 });
    const first = placed.nodes.route;
    expect(first.x * t.k + t.x).toBeGreaterThanOrEqual(0);
    expect(first.y * t.k + t.y).toBeLessThan(200);
  });
});

describe("gridStep", () => {
  it("keeps the dots at least 12 px apart however far out", () => {
    expect(gridStep(1)).toBe(24);
    expect(gridStep(0.5)).toBe(12);
    expect(gridStep(0.15)).toBeGreaterThanOrEqual(12);
    expect(gridStep(0.15)).toBeLessThan(24);
  });
});

describe("what a card says", () => {
  it("says what a gap is, in its tag, its status and its label", () => {
    const missing = node("x", "secrets", { kind: "Secret", name: "stripe-key", presence: "missing" });
    expect(gapTag(missing).label).toBe("Missing");
    expect(statusText(missing)).toBe("Doesn't exist");
    expect(cardLabel(missing)).toBe("Secret shop/stripe-key, missing");

    const unknown = node("y", "secrets", { kind: "Secret", presence: "unknown" });
    expect(statusText(unknown)).toBe("Not allowed to read");
    expect(statusText({ ...unknown, resource: "" })).toBe("Kind not read by Opscope");

    const dangling = node("z", "services", { kind: "Service", name: "ghost", dangling: [{ kind: "selects", field: "spec.selector" }] });
    expect(gapTag(dangling).label).toBe("Selects nothing");
    expect(cardLabel(dangling)).toBe("Service shop/ghost, selects nothing, Matches no pods");
  });

  it("gives a Service its endpoints, and a group of pods its owner", () => {
    expect(statusText(node("s", "services", { endpoints: { ready: 3, notReady: 0 } }))).toBe("3 ready endpoints");
    expect(statusText(node("s", "services", { endpoints: { ready: 2, notReady: 1 } }))).toBe("2 ready, 1 not ready");
    const group = node("g", "pods", { kind: "Pod", name: "web", status: "3 Running", pods: { ownerKind: "Deployment", members: [{}, {}, {}] } });
    expect(cardLabel(group, true)).toBe("3 pods of Deployment shop/web, 3 Running, this object");
  });
});
