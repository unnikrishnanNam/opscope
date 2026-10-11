// Following findings on a map: which card each finding belongs to, which
// cards are root causes, and the chain from one card down to its root
// causes, so the map can draw it.
//
// A view's findings are about objects; a card is an object or a group of
// pods. Findings about a pod in a group belong to the group's card.

const SEVERITY = { failing: 0, degraded: 1, info: 2 };

// worse returns the worse of two severities (either may be undefined).
export function worse(a, b) {
  if (!a) return b;
  if (!b) return a;
  return SEVERITY[b] < SEVERITY[a] ? b : a;
}

// indexFindings sorts a view's findings by object and by card.
export function indexFindings(view) {
  const drawnAs = new Map();
  const objects = new Map(); // card → the objects it stands for
  for (const n of view.nodes) {
    drawnAs.set(n.id, n.id);
    objects.set(n.id, [n.id, ...(n.pods?.members ?? [])]);
    for (const member of n.pods?.members ?? []) drawnAs.set(member, n.id);
  }
  const byObject = new Map();
  const byNode = new Map();
  for (const f of view.findings ?? []) {
    push(byObject, f.object, f);
    const card = drawnAs.get(f.object);
    if (card) push(byNode, card, f);
  }
  return { drawnAs, objects, byObject, byNode };
}

// rootCauses lists the cards that hold a root cause: a finding that isn't
// a note, about something none of whose culprits has a problem of its own
// (the same test as the problems list). The value is the worst severity.
export function rootCauses(index) {
  const roots = new Map();
  for (const [card, findings] of index.byNode) {
    for (const f of findings) {
      if (f.severity === "info") continue;
      const caused = f.blames.some((b) => (index.byObject.get(b) ?? []).some((other) => other.severity !== "info"));
      if (!caused) roots.set(card, worse(roots.get(card), f.severity));
    }
  }
  return roots;
}

// causeChain follows a card's findings through what they blame, as far as
// it goes, as the detail page's trace does.
//
//   cards  card → { severity, root }: every card on the way
//   links  [{ from, to, severity }]: card to card, the way the blame goes
export function causeChain(index, start) {
  const cards = new Map();
  const links = [];
  if (!start) return { cards, links };

  const mark = (card, severity, root) => {
    const c = cards.get(card) ?? { severity: undefined, root: false };
    cards.set(card, { severity: worse(c.severity, severity), root: c.root || root });
  };
  const seen = new Set();
  const queue = [...(index.objects.get(start) ?? [start])];
  const linked = new Set();
  while (queue.length) {
    const object = queue.shift();
    if (seen.has(object)) continue;
    seen.add(object);
    for (const f of index.byObject.get(object) ?? []) {
      const from = index.drawnAs.get(f.object);
      const causes = f.blames.filter((b) => !seen.has(b) && (index.byObject.get(b) ?? []).length > 0);
      mark(from, f.severity, causes.length === 0);
      for (const blamed of f.blames) {
        const to = index.drawnAs.get(blamed);
        if (!to) continue;
        if (to !== from && !linked.has(`${from}>${to}`)) {
          linked.add(`${from}>${to}`);
          links.push({ from, to, severity: f.severity });
        }
        queue.push(blamed);
      }
    }
  }
  return { cards, links };
}

// chainEdges finds the drawn edges a chain's links run along, with the
// severity to draw them in and whether the blame runs against the edge's
// drawing (right to left). A link with no edge of its own (a Deployment
// blames the pods of its new ReplicaSet, drawn between them) follows the
// shortest way the map joins its ends, up to three edges.
export function chainEdges(chain, edges, placed) {
  const touching = new Map();
  for (const e of edges) {
    for (const end of [e.from, e.to]) {
      if (!touching.has(end)) touching.set(end, []);
      touching.get(end).push(e);
    }
  }
  const found = new Map(); // edge id → { severity, reverse }
  for (const link of chain.links) {
    let from = link.from;
    for (const e of shortestWay(link.from, link.to, touching)) {
      const to = e.from === from ? e.to : e.from;
      const reverse = (placed[to]?.x ?? 0) < (placed[from]?.x ?? 0);
      found.set(e.id, { severity: worse(found.get(e.id)?.severity, link.severity), reverse });
      from = to;
    }
  }
  return found;
}

// shortestWay is the edges of the shortest way between two cards, either
// way along each edge, or none if it takes more than three.
function shortestWay(start, goal, touching) {
  const cameBy = new Map([[start, null]]);
  let frontier = [start];
  for (let step = 0; step < 3 && !cameBy.has(goal); step++) {
    const next = [];
    for (const card of frontier) {
      for (const e of touching.get(card) ?? []) {
        const other = e.from === card ? e.to : e.from;
        if (cameBy.has(other)) continue;
        cameBy.set(other, e);
        next.push(other);
      }
    }
    frontier = next;
  }
  if (!cameBy.has(goal)) return [];
  const way = [];
  for (let card = goal; card !== start; ) {
    const e = cameBy.get(card);
    way.unshift(e);
    card = e.from === card ? e.to : e.from;
  }
  return way;
}

// shownState is the state a card shows: its own, or worse when its own
// findings say so. An Ingress has no status to read, so it's "ok" even when
// the Service it sends traffic to doesn't exist; its finding knows better.
export function shownState(n, index) {
  if (n.state !== "ok" && n.state !== "progressing") return n.state;
  let worst;
  for (const f of index.byNode.get(n.id) ?? []) {
    if (f.severity !== "info") worst = worse(worst, f.severity);
  }
  return worst ?? n.state;
}

function push(map, key, value) {
  if (!map.has(key)) map.set(key, []);
  map.get(key).push(value);
}
