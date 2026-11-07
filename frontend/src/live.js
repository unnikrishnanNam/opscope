import { createContext, createElement, useContext, useEffect, useState, useSyncExternalStore } from "react";

// Live updates: the server tells the tab when something in the cluster
// changed (GET /api/clusters/{id}/live/stream), and pages fetch their data
// again instead of asking on a timer. See docs/api.md for the messages.
//
// The tab keeps one stream for the cluster on screen, shared by every
// component through LiveProvider: browsers allow only six connections per
// server, and a port-forward is one server. The stream is closed while the
// tab is hidden, and everything on screen is fetched again when it opens.

// The least time between two fetches of one path that a change can cause.
export const LIVE_MIN_MS = 2_000;
// A stream that sent nothing for this long (the server sends a heartbeat
// every 15 s) has died without closing, which happens through port-forwards.
const SILENT_MS = 45_000;
// After the stream fails for good (EventSource gives up on any answer but
// a stream), it's tried again after this, doubling up to the last.
const RETRY_FIRST_MS = 5_000;
const RETRY_MAX_MS = 30_000;

// Modes whose kinds come from the cache, which says when they change (see
// backend/internal/collector/kind.go). Pages showing them need no timer.
const STREAMED = new Set(["watch", "poll"]);
// Modes that give the same answer until a probe finds otherwise, which
// arrives as a new status: no timer either.
const SETTLED = new Set(["not_installed", "forbidden"]);

// Connection states:
//   connecting    opening the stream for the first time
//   open          receiving
//   reconnecting  lost it; pages ask on their timers until it's back
//   paused        the tab is hidden; nothing is fetched until it's shown
//   off           live data is turned off on the server (OPSCOPE_LIVE=false)

// LiveConnection is one cluster's stream for this tab. Everything the
// browser provides is passed in, so tests can use fakes.
export class LiveConnection {
  constructor(clusterId, env = {}) {
    this.url = `/api/clusters/${encodeURIComponent(clusterId)}/live`;
    this.env = {
      EventSource: globalThis.EventSource,
      document: globalThis.document,
      fetch: (url) => globalThis.fetch(url),
      setTimeout: (fn, ms) => globalThis.setTimeout(fn, ms),
      clearTimeout: (id) => globalThis.clearTimeout(id),
      ...env,
    };
    this.snapshot = { connection: "connecting", status: null };
    this.listeners = new Set(); // messages: { type: "changed", resource, namespaces } or { type: "resync" }
    this.watchers = new Set(); // snapshot changes
    this.source = null;
    this.opened = false; // opened at least once: the next open is a reconnect
    this.retryMs = RETRY_FIRST_MS;
    this.retryTimer = null;
    this.silentTimer = null;
    this.onVisibility = () => (this.env.document.hidden ? this.pause() : this.resume());
  }

  start() {
    this.env.document?.addEventListener("visibilitychange", this.onVisibility);
    if (this.env.document?.hidden) this.pause();
    else this.open();
  }

  stop() {
    this.env.document?.removeEventListener("visibilitychange", this.onVisibility);
    this.close();
    this.stopped = true;
  }

  // subscribe(fn) calls fn with every message; returns the unsubscribe.
  subscribe = (fn) => {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  };

  // For useSyncExternalStore.
  watch = (fn) => {
    this.watchers.add(fn);
    return () => this.watchers.delete(fn);
  };
  getSnapshot = () => this.snapshot;

  set(changes) {
    this.snapshot = { ...this.snapshot, ...changes };
    this.watchers.forEach((fn) => fn());
  }

  emit(message) {
    this.listeners.forEach((fn) => fn(message));
  }

  open() {
    this.close();
    const source = new this.env.EventSource(this.url + "/stream");
    this.source = source;
    this.heard();

    source.onopen = () => {
      // A reopened stream may have missed changes: everything is fetched again.
      if (this.opened) this.emit({ type: "resync" });
      this.opened = true;
      this.retryMs = RETRY_FIRST_MS;
    };
    source.addEventListener("status", (e) => {
      this.heard();
      this.set({ connection: "open", status: JSON.parse(e.data) });
    });
    source.addEventListener("changed", (e) => {
      this.heard();
      this.emit({ type: "changed", ...JSON.parse(e.data) });
    });
    source.addEventListener("resync", () => {
      this.heard();
      this.emit({ type: "resync" });
    });
    source.addEventListener("heartbeat", () => this.heard());
    source.onerror = () => {
      if (source !== this.source) return;
      if (source.readyState === 2) this.failed();
      else this.set({ connection: "reconnecting" }); // EventSource tries again itself
    };
  }

  close() {
    this.env.clearTimeout(this.retryTimer);
    this.env.clearTimeout(this.silentTimer);
    this.retryTimer = this.silentTimer = null;
    if (this.source) {
      this.source.onerror = null;
      this.source.close();
      this.source = null;
    }
  }

  // Something arrived: the stream is alive. One that goes silent is reopened.
  heard() {
    this.env.clearTimeout(this.silentTimer);
    this.silentTimer = this.env.setTimeout(() => {
      this.set({ connection: "reconnecting" });
      this.open();
    }, SILENT_MS);
  }

  // EventSource gave up: the server answered with something other than a
  // stream. 204 means live data is off; anything else is tried again later.
  async failed() {
    this.close();
    let enabled = true;
    try {
      const res = await this.env.fetch(this.url);
      if (res.ok) enabled = (await res.json()).enabled !== false;
    } catch {
      // The Opscope server itself is down; try again later.
    }
    if (this.stopped || this.snapshot.connection === "paused") return;
    if (!enabled) {
      this.set({ connection: "off", status: { state: "off", kinds: [] } });
      return;
    }
    this.set({ connection: "reconnecting" });
    this.retryTimer = this.env.setTimeout(() => this.open(), this.retryMs);
    this.retryMs = Math.min(this.retryMs * 2, RETRY_MAX_MS);
  }

  pause() {
    if (this.snapshot.connection === "off") return;
    this.close();
    this.set({ connection: "paused" });
  }

  resume() {
    if (this.snapshot.connection !== "paused") return;
    this.set({ connection: this.opened ? "reconnecting" : "connecting" });
    this.open();
  }
}

// The resources a `live` option names: "pods" or ["pods", "nodes"].
const listOf = (live) => [live].flat().filter(Boolean);

// needsTimer reports whether a page showing `live` still has to ask on its
// timer: when there's no stream, or one of its kinds isn't in the cache, so
// nothing says when it changes (read from the cluster on each request, or
// not probed yet). A hidden tab needs nothing.
export function needsTimer(snapshot, live) {
  if (!snapshot) return true;
  const { connection, status } = snapshot;
  if (connection === "paused") return false;
  if (connection !== "open" || !status || status.state === "unreachable") return true;
  return listOf(live).some((resource) => {
    const mode = status.kinds.find((k) => k.resource === resource)?.mode;
    return !STREAMED.has(mode) && !SETTLED.has(mode);
  });
}

// wants reports whether a stream message means `path`, which shows the
// `live` resources, has to be fetched again. A path limited to a namespace
// (?namespace=) ignores changes in others; cluster-wide kinds name none.
export function wants(message, live, path) {
  if (message.type === "resync") return true;
  if (message.type !== "changed" || !listOf(live).includes(message.resource)) return false;
  const namespace = new URLSearchParams(path.split("?")[1] ?? "").get("namespace");
  return !namespace || !message.namespaces?.length || message.namespaces.includes(namespace);
}

// describe turns a snapshot into what the top bar shows:
//   { label, tone: "ok" | "warn" | "bad" | "neutral", summary, groups: [{ title, kinds, reason }] }
// `labelOf` names a resource for people ("pods" -> "Pods"). `pageKinds` are
// the kinds the page on screen shows: the label is how fresh that page is,
// so a cluster where only Secrets can't be listed reads "Live" on Pods. An
// empty list is a page that isn't live at all (a detail page). Without it,
// every kind counts. The groups always list every kind.
export function describe(snapshot, labelOf = (r) => r, pageKinds) {
  if (!snapshot) return null;
  const { connection, status } = snapshot;
  if (connection === "off") {
    return {
      label: "Every 10 s",
      tone: "neutral",
      summary: "Live updates are turned off on this Opscope server, so pages ask the cluster again every 10 seconds.",
      groups: [],
    };
  }
  if (connection === "reconnecting") {
    return {
      label: "Reconnecting",
      tone: "warn",
      summary: "Lost the live updates from the Opscope server. Pages ask again every 10 seconds until they're back.",
      groups: [],
    };
  }
  if (connection === "connecting" || !status) {
    return { label: "Connecting", tone: "neutral", summary: "Connecting to the live updates.", groups: [] };
  }
  if (status.state === "unreachable") {
    return {
      label: "Unreachable",
      tone: "bad",
      summary: "The cluster isn't answering. Pages show what it answers when asked, and update live once it's back.",
      groups: [],
    };
  }

  // Kinds that aren't watched, grouped by how they're read and why.
  const groups = [];
  const titles = {
    poll: "Every 30 s",
    direct: "Read on each visit, every 10 s",
    pending: "Waiting for the cluster",
    forbidden: "Not shown",
    not_installed: "Not installed",
  };
  for (const kind of status.kinds) {
    if (kind.mode === "watch" || !titles[kind.mode]) continue;
    const title = titles[kind.mode];
    // "Not installed" says it all.
    const reason = kind.mode === "not_installed" ? "" : (kind.reason ?? "");
    let group = groups.find((g) => g.title === title && g.reason === reason);
    if (!group) groups.push((group = { title, reason, kinds: [] }));
    group.kinds.push(labelOf(kind.resource));
  }
  const order = Object.values(titles);
  groups.sort((a, b) => order.indexOf(a.title) - order.indexOf(b.title));

  if (status.state === "connecting") {
    return { label: "Connecting", tone: "neutral", summary: "Reading the cluster for the first time.", groups };
  }
  // A page that shows no live kinds, like a detail page, is read on its timer.
  if (pageKinds?.length === 0) {
    return {
      label: "Every 10 s",
      tone: "neutral",
      summary: "This page is read from the cluster every 10 seconds. Lists and the overview update live.",
      groups,
    };
  }
  // The slowest kind on the page decides.
  const shown = pageKinds ? status.kinds.filter((k) => pageKinds.includes(k.resource)) : status.kinds;
  const some = pageKinds ? "some kinds on this page" : "some kinds";
  if (shown.some((k) => k.mode === "poll")) {
    return {
      label: "Every 30 s",
      tone: "warn",
      summary: `Opscope isn't allowed to watch ${some}, so it lists them every 30 seconds.`,
      groups,
    };
  }
  if (shown.some((k) => k.mode === "direct")) {
    return {
      label: "Every 10 s",
      tone: "warn",
      summary: `Opscope isn't allowed to list ${some} across all namespaces, so they're read again every 10 seconds.`,
      groups,
    };
  }
  return {
    label: "Live",
    tone: "ok",
    summary: "Updates within a few seconds of a change in the cluster.",
    groups,
  };
}

// React: one LiveConnection per cluster for the tab.

const LiveContext = createContext(null);

export function LiveProvider({ clusterId, children }) {
  const [connection, setConnection] = useState(null);
  useEffect(() => {
    if (!clusterId || typeof EventSource === "undefined") {
      setConnection(null);
      return;
    }
    const c = new LiveConnection(clusterId);
    c.start();
    setConnection(c);
    return () => c.stop();
  }, [clusterId]);
  return createElement(LiveContext.Provider, { value: connection }, children);
}

const NONE = { watch: () => () => {}, getSnapshot: () => null };

// useLive returns the tab's connection (null outside a cluster) and its
// current snapshot: { connection, status }.
export function useLive() {
  const connection = useContext(LiveContext);
  const source = connection ?? NONE;
  const snapshot = useSyncExternalStore(source.watch, source.getSnapshot);
  return { connection, snapshot };
}
