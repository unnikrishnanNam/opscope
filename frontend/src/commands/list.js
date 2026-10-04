import { matchCommand, queryWords } from "./match.js";

// Turning sources into the rows the palette shows. Plain functions with no
// React, so they're easy to test; registry.jsx calls them.

// At most this many rows are drawn, so a cluster with thousands of objects
// stays quick to type in.
export const LIMIT = 50;

// Small lifts on top of the match score, enough to break near-ties.
const RECENT_BONUS = 6; // the most recent command; older ones get less
const PAGE_BONUS = 4; // commands from the page on screen

// collect asks every source for its commands, in source order.
//   sources  page sources first, then global ones (see registry.jsx)
//   dataFor  (source) => what the source's load() returned, or undefined
//            if it hasn't arrived (a source that loads is skipped until then)
// Returns [{ command, source }]. A source that throws is left out (and
// logged), so one broken feature can't take the palette down. If two
// commands share an id, the first one wins.
export function collect(sources, ctx, dataFor = () => undefined) {
  const seen = new Set();
  const entries = [];
  for (const source of sources) {
    let data;
    if (source.load) {
      data = dataFor(source);
      if (data === undefined) continue;
    }
    for (const command of commandsOf(source, () => source.commands(ctx, data))) {
      if (seen.has(command.id)) continue;
      seen.add(command.id);
      entries.push({ command, source });
    }
  }
  return entries;
}

// collectItems lists a sub-list: the commands of one command's `items`.
export function collectItems(parent, ctx) {
  const source = { id: parent.id };
  return commandsOf(source, () => parent.items(ctx)).map((command) => ({ command, source }));
}

function commandsOf(source, list) {
  if (Array.isArray(source.commands)) return source.commands;
  try {
    return list() ?? [];
  } catch (error) {
    console.error(`Commands from "${source.id}" failed:`, error);
    return [];
  }
}

// buildList ranks and groups entries for a query.
//   recent  command ids, most recent first (see recent.js)
// Returns:
//   groups  [{ title, items }] in the order they're shown
//   items   the same rows as one list, for moving with the arrow keys
//   total   how many matched, before the limit
// Each item is { command, source, group, match }, where match holds the
// positions to make bold ({ title, detail }), or is null for an empty query.
//
// With an empty query: recent commands first (under "Recent"), then every
// other command that isn't `searchOnly`, by group. With a query: the best
// matches first; groups are ordered by their best row.
export function buildList({ entries, query = "", recent = [], limit = LIMIT }) {
  const words = queryWords(query);
  if (!words.length) return browse(entries, recent, limit);

  const scored = [];
  entries.forEach((entry, order) => {
    const match = matchCommand(words, entry.command);
    if (!match) return;
    const r = recent.indexOf(entry.command.id);
    const score =
      match.score +
      (r === -1 ? 0 : RECENT_BONUS * (1 - r / recent.length)) +
      (entry.source.page ? PAGE_BONUS : 0);
    scored.push({ ...entry, match, score, order });
  });
  scored.sort((a, b) => b.score - a.score || a.order - b.order);
  return grouped(scored.slice(0, limit), scored.length);
}

function browse(entries, recent, limit) {
  const byId = new Map(entries.map((e) => [e.command.id, e]));
  // Recent rows may be search-only ones, like an object opened earlier.
  const recentRows = recent
    .map((id) => byId.get(id))
    .filter(Boolean)
    .map((e) => ({ ...e, group: "Recent" }));
  const rest = entries.filter((e) => !e.source.searchOnly && !recent.includes(e.command.id));
  const rows = [...recentRows, ...rest];
  return grouped(rows.slice(0, limit), rows.length);
}

function grouped(rows, total) {
  const groups = new Map();
  for (const row of rows) {
    const title = row.group ?? row.command.group ?? "Other";
    if (!groups.has(title)) groups.set(title, []);
    groups.get(title).push({ match: null, ...row, group: title });
  }
  const list = [...groups].map(([title, items]) => ({ title, items }));
  return { groups: list, items: list.flatMap((g) => g.items), total };
}
