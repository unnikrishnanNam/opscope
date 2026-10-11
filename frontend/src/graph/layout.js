import { pluralKind } from "../diagnosis.js";

// layout places a view of the graph (from GET /topology) for drawing.
//
//   layout(view, { previous }) → { width, height, columns, nodes, edges, order }
//
// Kubernetes kinds already have a natural left-to-right order, roughly the
// way traffic flows: a Gateway, its routes, Services, workloads, pods, then
// what pods need. So each group of kinds gets a column, and the work is in
// ordering the cards within each column so edges cross as little as
// possible, and lining them up with what they connect to.
//
// It's a layered layout in four steps:
//   1. columns: by kind; columns nothing is in are left out
//   2. lanes: an edge that skips columns gets a lane in each one it passes,
//      so it runs between cards rather than over them
//   3. order: a few sweeps of the barycenter heuristic (each card moves to
//      the average position of what it connects to in the next column),
//      each followed by swapping neighbours where that uncrosses edges,
//      keeping the order with the fewest crossings
//   4. rows: each card moves toward the middle of what it connects to,
//      keeping its order and its distance from its neighbours
//
// The same view always gets the same layout, whatever order its nodes and
// edges come in. Given the previous layout's `order`, cards that were there
// before keep their order in their column: a refresh that adds a pod doesn't
// shuffle the rest.

export const NODE_W = 208;
export const NODE_H = 64;
export const HEADER_H = 32; // column headings, above the cards
const COL_GAP = 72;
const ROW_GAP = 16;
const LANE_GAP = 10; // between a lane and anything next to it
const ORDER_SWEEPS = 12;
const ROW_SWEEPS = 16;

// COLUMNS, left to right. A column's label is used when it holds more than
// one kind; with one, the kind names it ("Deployments").
export const COLUMNS = [
  { key: "classes", label: "Classes", resources: ["gatewayclasses", "ingressclasses"] },
  { key: "entry", label: "Entry points", resources: ["gateways", "ingresses"] },
  { key: "routes", label: "Routes", resources: ["httproutes"] },
  {
    key: "services",
    label: "Services and policies",
    resources: ["services", "horizontalpodautoscalers", "poddisruptionbudgets", "referencegrants"],
  },
  { key: "workloads", label: "Workloads", resources: ["deployments", "statefulsets", "daemonsets", "cronjobs", "jobs"] },
  { key: "runs", label: "ReplicaSets and runs", resources: ["replicasets"] }, // and Jobs a CronJob started
  { key: "pods", label: "Pods", resources: ["pods"] },
  {
    key: "config",
    label: "Config and claims",
    resources: ["configmaps", "secrets", "persistentvolumeclaims", "serviceaccounts", "resourcequotas", "namespaces"],
  },
  { key: "volumes", label: "Volumes", resources: ["persistentvolumes"] },
  { key: "storage", label: "Storage classes", resources: ["storageclasses"] },
  { key: "nodes", label: "Nodes", resources: ["nodes"] },
];

const columnOf = new Map(COLUMNS.flatMap((c, i) => c.resources.map((r) => [r, i])));
const RUNS = COLUMNS.findIndex((c) => c.key === "runs");
const CONFIG = COLUMNS.findIndex((c) => c.key === "config");
// Within a column, kinds are kept in the order listed above to start with.
const resourceOrder = new Map(COLUMNS.flatMap((c) => c.resources.map((r, i) => [r, i])));

export function edgeId(e) {
  return `${e.from}>${e.to}>${e.type}`;
}

export function layout(view, { previous } = {}) {
  const nodes = [...view.nodes].sort((a, b) => compare(a.id, b.id));
  const byId = new Map(nodes.map((n) => [n.id, n]));
  const edges = view.edges
    .filter((e) => byId.has(e.from) && byId.has(e.to) && e.from !== e.to)
    .sort((a, b) => compare(edgeId(a), edgeId(b)));

  // 1. Columns. A Job a CronJob started sits between them, where a
  // Deployment's ReplicaSets do.
  const cronRuns = new Set(
    edges.filter((e) => e.type === "owns" && byId.get(e.from).resource === "cronjobs").map((e) => e.to),
  );
  const column = (n) => (n.resource === "jobs" && cronRuns.has(n.id) ? RUNS : (columnOf.get(n.resource) ?? CONFIG));
  const used = [...new Set(nodes.map(column))].sort((a, b) => a - b);
  const rankOf = new Map(used.map((c, r) => [c, r]));

  // Items are what a column holds: cards, and lanes for edges passing through.
  const items = new Map();
  for (const n of nodes) items.set(n.id, { id: n.id, node: n, rank: rankOf.get(column(n)), h: NODE_H, left: [], right: [] });

  // 2. Lanes. Each edge becomes a chain of items from its left end to its
  // right end, whichever way it points. Long edges that share an end share
  // their lanes, at whichever end more of them meet: a Gateway's edges to
  // the objects it runs on are one line until they part, and the edges from
  // thirty pods to their machine join into one before reaching it.
  const routes = [];
  const join = (a, b) => {
    if (items.get(a).right.includes(b)) return;
    items.get(a).right.push(b);
    items.get(b).left.push(a);
  };
  const ends = (e) => {
    const [a, b] = [items.get(e.from), items.get(e.to)];
    return a.rank < b.rank ? [a, b] : [b, a];
  };
  const long = new Map(); // how many long edges meet each card
  for (const e of edges) {
    const [l, r] = ends(e);
    if (r.rank - l.rank < 2) continue;
    long.set(l.id, (long.get(l.id) ?? 0) + 1);
    long.set(r.id, (long.get(r.id) ?? 0) + 1);
  }
  for (const e of edges) {
    const [l, r] = ends(e);
    if (l.rank === r.rank) {
      routes.push({ index: routes.length, edge: e, chain: [l.id, r.id], flat: true });
      continue;
    }
    const hub = long.get(r.id) > long.get(l.id) ? r : l;
    const chain = [l.id];
    for (let rank = l.rank + 1; rank < r.rank; rank++) {
      const id = `lane:${hub.id}:${rank}`;
      if (!items.has(id)) items.set(id, { id, lane: true, rank, h: 0, left: [], right: [], key: hub.id });
      chain.push(id);
    }
    chain.push(r.id);
    for (let i = 0; i + 1 < chain.length; i++) join(chain[i], chain[i + 1]);
    routes.push({ index: routes.length, edge: e, chain });
  }

  // 3. Order.
  const layers = used.map(() => []);
  for (const item of items.values()) layers[item.rank].push(item.id);
  const prev = previousOrder(previous, used);
  for (const layer of layers) {
    layer.sort((a, b) => startCompare(items.get(a), items.get(b), prev));
  }
  const { order, crossings: crossed } = orderLayers(layers, items, prev);

  // 4. Rows.
  const y = placeRows(order, items);

  // Coordinates: columns side by side, cards below the headings.
  let top = Infinity;
  let bottom = -Infinity;
  for (const item of items.values()) {
    top = Math.min(top, y.get(item.id) - item.h / 2);
    bottom = Math.max(bottom, y.get(item.id) + item.h / 2);
  }
  if (items.size === 0) top = bottom = 0;
  const shift = HEADER_H - top;
  const x = (rank) => rank * (NODE_W + COL_GAP);
  for (const item of items.values()) {
    item.x = x(item.rank);
    item.y = round(y.get(item.id) + shift); // its middle
  }

  const placed = {};
  for (const n of nodes) {
    const item = items.get(n.id);
    placed[n.id] = { id: n.id, x: item.x, y: item.y - NODE_H / 2, w: NODE_W, h: NODE_H, rank: item.rank };
  }

  return {
    width: used.length ? x(used.length - 1) + NODE_W : 0,
    height: round(bottom - top + HEADER_H),
    columns: used.map((c, rank) => ({ key: COLUMNS[c].key, label: columnLabel(c, layers[rank], items), x: x(rank) })),
    nodes: placed,
    edges: routeEdges(routes, items),
    crossings: crossed, // how many times edges cross, between neighbouring columns
    order: Object.fromEntries(
      order.flatMap((layer, rank) =>
        layer.filter((id) => !items.get(id).lane).map((id, index) => [id, { column: COLUMNS[used[rank]].key, index }]),
      ),
    ),
  };
}

// ---- Order ----

// previousOrder reads a previous layout's order: each card's place in its
// column, by column key.
function previousOrder(previous, used) {
  const prev = new Map();
  if (!previous) return prev;
  const keys = new Set(used.map((c) => COLUMNS[c].key));
  for (const [id, o] of Object.entries(previous)) {
    if (keys.has(o.column)) prev.set(id, o);
  }
  return prev;
}

// startCompare is the order before any sweeps: cards seen before in their
// old order, then the rest grouped by app and kind, then by name. Lanes
// start after the cards; the sweeps place them.
function startCompare(a, b, prev) {
  if (a.lane !== b.lane) return a.lane ? 1 : -1;
  if (a.lane) return compare(a.key, b.key) || compare(a.id, b.id);
  const pa = prev.get(a.id);
  const pb = prev.get(b.id);
  if (pa && pb) return pa.index - pb.index;
  if (pa || pb) return pa ? -1 : 1;
  return (
    compare(a.node.app ?? "", b.node.app ?? "") ||
    (resourceOrder.get(a.node.resource) ?? 99) - (resourceOrder.get(b.node.resource) ?? 99) ||
    compare(a.node.namespace ?? "", b.node.namespace ?? "") ||
    compare(a.node.name, b.node.name) ||
    compare(a.id, b.id)
  );
}

// orderLayers sweeps left to right and back, moving each item to the
// average position of its neighbours in the column it was just compared
// with, and keeps the order with the fewest crossings.
function orderLayers(layers, items, prev) {
  const pos = new Map();
  const index = (layer) => layer.forEach((id, i) => pos.set(id, i));
  layers.forEach(index);

  let best = layers.map((l) => [...l]);
  let fewest = crossings(layers, items, pos);
  for (let sweep = 0; sweep < ORDER_SWEEPS && fewest > 0; sweep++) {
    const rightward = sweep % 2 === 0;
    for (let k = 1; k < layers.length; k++) {
      const rank = rightward ? k : layers.length - 1 - k;
      const side = rightward ? "left" : "right";
      const bary = new Map();
      for (const id of layers[rank]) {
        const near = items.get(id)[side];
        bary.set(id, near.length ? near.reduce((sum, n) => sum + pos.get(n), 0) / near.length : pos.get(id));
      }
      layers[rank].sort((a, b) => bary.get(a) - bary.get(b) || pos.get(a) - pos.get(b));
      keepPrevious(layers[rank], prev);
      index(layers[rank]);
    }
    transpose(layers, items, pos, prev);
    const count = crossings(layers, items, pos);
    if (count < fewest) {
      fewest = count;
      best = layers.map((l) => [...l]);
    }
  }
  return { order: best, crossings: fewest };
}

// transpose swaps neighbours in a column wherever that makes fewer edges
// cross, until no swap helps (as Graphviz's dot does after each sweep).
// Cards from the previous layout keep their order.
function transpose(layers, items, pos, prev) {
  // crossed(u, v) counts crossings between u's and v's edges with u above v.
  const crossed = (u, v) => {
    let count = 0;
    for (const side of ["left", "right"]) {
      for (const a of items.get(u)[side]) {
        for (const b of items.get(v)[side]) if (pos.get(a) > pos.get(b)) count++;
      }
    }
    return count;
  };
  for (let pass = 0, improved = true; improved && pass < 8; pass++) {
    improved = false;
    for (const layer of layers) {
      for (let i = 0; i + 1 < layer.length; i++) {
        const [u, v] = [layer[i], layer[i + 1]];
        if (prev.has(u) && prev.has(v)) continue;
        if (crossed(v, u) < crossed(u, v)) {
          [layer[i], layer[i + 1]] = [v, u];
          pos.set(v, i);
          pos.set(u, i + 1);
          improved = true;
        }
      }
    }
  }
}

// keepPrevious puts cards from the previous layout back in their old order,
// in the places the sweep gave them: new cards find their place among them,
// but the old ones don't swap.
function keepPrevious(layer, prev) {
  if (prev.size === 0) return;
  const slots = [];
  const known = [];
  layer.forEach((id, i) => {
    if (prev.has(id)) {
      slots.push(i);
      known.push(id);
    }
  });
  known.sort((a, b) => prev.get(a).index - prev.get(b).index);
  slots.forEach((slot, i) => (layer[slot] = known[i]));
}

// crossings counts pairs of edge segments that cross between neighbouring
// columns: for segments sorted by their left end, every pair whose right
// ends are the other way round.
export function crossings(layers, items, pos) {
  let total = 0;
  for (let rank = 0; rank + 1 < layers.length; rank++) {
    const ends = [];
    for (const id of layers[rank]) {
      for (const right of items.get(id).right) ends.push([pos.get(id), pos.get(right)]);
    }
    ends.sort((a, b) => a[0] - b[0] || a[1] - b[1]);
    total += inversions(ends.map((e) => e[1]), layers[rank + 1].length);
  }
  return total;
}

// inversions counts pairs i < j with values[i] > values[j], in O(n log n),
// with a Fenwick tree over values 0..size-1.
function inversions(values, size) {
  const tree = new Array(size + 1).fill(0);
  let count = 0;
  for (let i = values.length - 1; i >= 0; i--) {
    for (let k = values[i]; k > 0; k -= k & -k) count += tree[k]; // smaller values seen to the right
    for (let k = values[i] + 1; k <= size; k += k & -k) tree[k]++;
  }
  return count;
}

// ---- Rows ----

// placeRows gives each item its vertical middle. It starts with every
// column packed from the top, then repeatedly moves each item toward the
// average middle of what it connects to, as close as it can get while
// keeping its order and its distance from its neighbours (a least-squares
// fit, solved exactly by isotonic regression).
function placeRows(layers, items) {
  const y = new Map();
  const seps = layers.map((layer) => layer.map((id, i) => (i === 0 ? 0 : separation(items.get(layer[i - 1]), items.get(id)))));
  layers.forEach((layer, rank) => {
    let offset = 0;
    layer.forEach((id, i) => {
      offset += seps[rank][i];
      y.set(id, offset);
    });
  });

  for (let sweep = 0; sweep < ROW_SWEEPS; sweep++) {
    const rightward = sweep % 2 === 0;
    for (let k = 0; k < layers.length; k++) {
      const rank = rightward ? k : layers.length - 1 - k;
      const layer = layers[rank];
      const wanted = layer.map((id) => {
        const item = items.get(id);
        const near = [...item.left, ...item.right];
        return near.length ? near.reduce((sum, n) => sum + y.get(n), 0) / near.length : y.get(id);
      });
      // Lanes weigh more, so long edges run straight.
      const weights = layer.map((id) => (items.get(id).lane ? 2 : 1));
      const placed = fit(wanted, seps[rank], weights);
      layer.forEach((id, i) => y.set(id, placed[i]));
    }
  }
  return y;
}

// separation is the least distance between two neighbours' middles.
function separation(a, b) {
  const gap = a.lane || b.lane ? LANE_GAP : ROW_GAP;
  return a.h / 2 + gap + b.h / 2;
}

// fit places items as close to where they want to be as their order and
// separations allow: minimise Σ w(y - wanted)² with y[i+1] - y[i] ≥ seps[i+1].
// With s the running sum of separations, z = y - s must not decrease, so
// it's isotonic regression of wanted - s (pool adjacent violators).
function fit(wanted, seps, weights) {
  const s = [];
  let sum = 0;
  for (const sep of seps) s.push((sum += sep));
  const blocks = [];
  wanted.forEach((w, i) => {
    blocks.push({ value: w - s[i], weight: weights[i], count: 1 });
    while (blocks.length > 1 && blocks.at(-2).value > blocks.at(-1).value) {
      const b = blocks.pop();
      const a = blocks.pop();
      const weight = a.weight + b.weight;
      blocks.push({ value: (a.value * a.weight + b.value * b.weight) / weight, weight, count: a.count + b.count });
    }
  });
  const y = [];
  for (const block of blocks) {
    for (let k = 0; k < block.count; k++) y.push(block.value + s[y.length]);
  }
  return y;
}

// ---- Edges ----

// routeEdges draws each edge from the right side of its left card to the
// left side of its right card, straight through its lanes and curving
// between columns. Where edges leave a side of a card toward different
// places, they're spread along it in the order of where they go, so they
// don't cross there; edges toward the same place leave together.
function routeEdges(routes, items) {
  const ports = new Map(); // "id|side" → the items its edges go toward
  const attach = (id, side, toward) => {
    const key = `${id}|${side}`;
    if (!ports.has(key)) ports.set(key, new Set());
    ports.get(key).add(toward);
  };
  for (const route of routes) {
    const [first, last] = [route.chain[0], route.chain.at(-1)];
    if (route.flat) {
      attach(first, "right", last);
      attach(last, "right", first);
    } else {
      attach(first, "right", route.chain[1]);
      attach(last, "left", route.chain.at(-2));
    }
  }
  const portY = new Map(); // "id|side|toward" → y
  for (const [key, towards] of ports) {
    const item = items.get(key.split("|")[0]);
    const list = [...towards].sort((a, b) => items.get(a).y - items.get(b).y || compare(a, b));
    const spread = Math.min(item.h * 0.5, (list.length - 1) * 8);
    list.forEach((toward, i) => {
      const offset = list.length === 1 ? 0 : -spread / 2 + (spread * i) / (list.length - 1);
      portY.set(`${key}|${toward}`, round(item.y + offset));
    });
  }

  return routes.map((route) => {
    const e = route.edge;
    const { chain } = route;
    const first = items.get(chain[0]);
    const last = items.get(chain.at(-1));
    let path;
    if (route.flat) {
      const x = first.x + NODE_W;
      const y1 = portY.get(`${first.id}|right|${last.id}`);
      const y2 = portY.get(`${last.id}|right|${first.id}`);
      const bulge = 24 + Math.min(32, Math.abs(y2 - y1) / 6);
      path = `M ${x} ${y1} C ${x + bulge} ${y1} ${x + bulge} ${y2} ${x} ${y2}`;
    } else {
      let px = first.x + NODE_W;
      let py = portY.get(`${first.id}|right|${chain[1]}`);
      path = `M ${px} ${py}`;
      for (const id of chain.slice(1, -1)) {
        const lane = items.get(id);
        path += curve(px, py, lane.x, lane.y) + ` L ${lane.x + NODE_W} ${lane.y}`;
        px = lane.x + NODE_W;
        py = lane.y;
      }
      path += curve(px, py, last.x, portY.get(`${last.id}|left|${chain.at(-2)}`));
    }
    return { id: edgeId(e), from: e.from, to: e.to, type: e.type, optional: Boolean(e.optional), via: e.via ?? [], path };
  });
}

function curve(x1, y1, x2, y2) {
  const dx = (x2 - x1) / 2;
  return ` C ${x1 + dx} ${y1} ${x2 - dx} ${y2} ${x2} ${y2}`;
}

// ---- Helpers ----

function columnLabel(c, layer, items) {
  const kinds = new Set(layer.filter((id) => !items.get(id).lane).map((id) => items.get(id).node.kind));
  return kinds.size === 1 ? pluralKind([...kinds][0]) : COLUMNS[c].label;
}

function compare(a, b) {
  return a < b ? -1 : a > b ? 1 : 0;
}

function round(v) {
  return Math.round(v * 2) / 2;
}
