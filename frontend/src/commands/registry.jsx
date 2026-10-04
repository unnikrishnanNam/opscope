import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { globalSources } from "./index.js";
import { buildList, collect, collectItems } from "./list.js";
import { getRecent, remember } from "./recent.js";
import { createStore } from "./store.js";

// The command registry: every feature hands its commands to it, and the
// command palette (and keyboard shortcuts) read them from it. The palette
// itself knows nothing about pages, clusters or objects.
//
// A command is a plain object:
//
//   {
//     id: "go.pods",          unique and stable; used to remember recent commands
//     title: "Pods",          what the row says
//     group: "Go to",         the heading its row sits under
//     icon: PodsIcon,         optional, from icons.jsx
//     detail: "Workloads",    optional, quieter text after the title
//     keywords: ["po"],       optional, more words that find it ("po" finds Pods)
//     scope: ["pods", "po"],  optional, words that narrow a search to it but don't
//                             find it alone ("po api" finds pods called api)
//     boost: 3,               optional, a small lift in the ranking (an object in
//                             the selected namespace)
//     shortcut: "g p",        optional, a key or a sequence of keys
//     searchOnly: true,       optional, only shown once something is typed
//
//     and one of:
//     to: "/c/lab/workloads/pods",   a link (Cmd/Ctrl+Enter opens it in a new tab)
//     run: (ctx) => { ... },         an action: switch a tab, copy, change the theme
//     items: (ctx) => [...],         a sub-list, e.g. "Switch namespace…"
//   }
//
// Commands come in sources. A source is a list of commands, or a function
// that builds them from `ctx`:
//
//   {
//     id: "pages",
//     commands: (ctx) => [...],      or a plain array
//
//     optional:
//     searchOnly: true,              only shown once something is typed (objects:
//                                    too many to list, but good to find)
//     load: (ctx) => promise,        data the commands need, loaded when the palette
//                                    opens; commands(ctx, data) gets it
//     key: (ctx) => ctx.cluster?.id, which data that is; a new key loads again, and
//                                    null means there's nothing to load
//     maxAge: 30_000,                how long loaded data is used (milliseconds)
//     label: "objects",              names it in "Loading objects…"
//     notes: (ctx, data) => [...],   lines to show under the results about what was
//                                    loaded ("Secrets aren't searched: ..."); for a
//                                    searchOnly source, only once something is typed
//   }
//
// `ctx` is what commands may need to know and do, built by whoever shows
// the commands (the palette): the cluster, the namespace, the page,
// `navigate`, and so on. Commands get everything through it rather than
// reading React state, so they stay plain functions.
//
// Two ways to register, which cover every case:
//   - global sources, listed in commands/index.js, for commands that make
//     sense on any page
//   - page sources, with useCommands(...) in the component they belong to;
//     they're there while it's on screen

const CommandsContext = createContext(null);

export function CommandsProvider({ sources = globalSources, children }) {
  const [store] = useState(() => createStore(sources));
  return <CommandsContext.Provider value={store}>{children}</CommandsContext.Provider>;
}

// useCommands registers a page's commands while the component is on screen.
// `deps` works like useMemo's: list what the commands use, so they're
// rebuilt when it changes.
//
//   useCommands("pod-logs", [
//     { id: "logs.follow", title: following ? "Stop following" : "Follow logs", group: "Logs", run: toggle },
//   ], [following]);
export function useCommands(id, commands, deps) {
  const store = useContext(CommandsContext);
  // Two effects, so a change replaces the commands in place instead of
  // moving them to the top of the list.
  useEffect(() => {
    store.addPageSource(id);
    return () => store.removePageSource(id);
  }, [store, id]);
  useEffect(() => {
    store.updatePageSource(id, commands);
  }, [store, id, ...deps]);
}

// useCommandList returns what to show for a query: { groups, items, total,
// status } (see buildList in list.js; status says which sources are still
// loading or failed). With `within`, a command that has `items`, it lists
// that sub-list instead. Data that's missing or old starts loading.
//
// `ctx` should keep its identity between renders (useMemo), since a new
// ctx checks for loading again.
export function useCommandList(ctx, query, within = null) {
  const store = useContext(CommandsContext);
  const version = useSyncExternalStore(store.subscribe, store.version);

  useEffect(() => {
    if (!within) store.refresh(ctx);
  }, [store, ctx, within]);

  return useMemo(() => {
    const entries = within
      ? collectItems(within, ctx)
      : collect(store.sources(), ctx, (source) => store.data(source, ctx));
    // A sub-list keeps its own order (namespaces A to Z); "Recent" is for the top.
    const list = buildList({ entries, query, recent: within ? [] : getRecent() });
    return { ...list, status: within ? [] : store.status(ctx) };
  }, [store, version, ctx, query, within]);
}

// useShortcutCommands returns every command with a `shortcut`, one per
// shortcut (the first wins, as in keys.js), for the key bindings and the
// shortcuts help. Search-only sources are left out: their commands are
// found by typing, and building them (every object) on each page change
// would be wasted work.
export function useShortcutCommands(ctx) {
  const store = useContext(CommandsContext);
  const version = useSyncExternalStore(store.subscribe, store.version);
  return useMemo(() => {
    const sources = store.sources().filter((s) => !s.searchOnly);
    const seen = new Set();
    return collect(sources, ctx, (source) => store.data(source, ctx))
      .map((e) => e.command)
      .filter((c) => c.shortcut && !seen.has(c.shortcut) && seen.add(c.shortcut));
  }, [store, version, ctx]);
}

// perform runs a command that has `to` or `run` (one with `items` opens
// its sub-list instead; that's up to the palette) and remembers it as
// recent, unless `remember` is false (a shortcut: it's already at hand).
// `newTab` opens a link in a new browser tab.
export function perform(command, ctx, { newTab = false, remember: keep = true } = {}) {
  if (keep) remember(command.id);
  if (command.to) {
    if (newTab) window.open(command.to, "_blank", "noopener");
    else ctx.navigate(command.to);
    return;
  }
  try {
    command.run?.(ctx);
  } catch (error) {
    console.error(`Command "${command.id}" failed:`, error);
  }
}
