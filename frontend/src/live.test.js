import { afterEach, beforeEach, describe as group, expect, test, vi } from "vitest";
import { LiveConnection, describe, needsTimer, wants } from "./live.js";

group("wants", () => {
  const pods = (namespaces) => ({ type: "changed", resource: "pods", namespaces });

  test("a change to the page's resource", () => {
    expect(wants(pods(["shop"]), "pods", "/clusters/a/pods")).toBe(true);
    expect(wants(pods(["shop"]), "services", "/clusters/a/services")).toBe(false);
  });

  test("a page limited to a namespace ignores the others", () => {
    expect(wants(pods(["shop", "web"]), "pods", "/clusters/a/pods?namespace=web")).toBe(true);
    expect(wants(pods(["shop"]), "pods", "/clusters/a/pods?namespace=web")).toBe(false);
    // A namespace whose name needs escaping.
    expect(wants(pods(["a b"]), "pods", "/clusters/a/pods?namespace=a%20b")).toBe(true);
  });

  test("cluster-wide kinds name no namespace, and match any page", () => {
    const nodes = { type: "changed", resource: "nodes" };
    expect(wants(nodes, ["pods", "nodes"], "/clusters/a/overview?namespace=web")).toBe(true);
  });

  test("a page showing several kinds", () => {
    expect(wants(pods(["web"]), ["nodes", "pods"], "/clusters/a/overview")).toBe(true);
    expect(wants(pods(["web"]), ["nodes", "jobs"], "/clusters/a/overview")).toBe(false);
  });

  test("resync means everything", () => {
    expect(wants({ type: "resync" }, "pods", "/clusters/a/pods?namespace=web")).toBe(true);
  });
});

group("needsTimer", () => {
  const open = (state, kinds) => ({ connection: "open", status: { state, kinds } });
  const live = open("live", [
    { resource: "pods", mode: "watch" },
    { resource: "services", mode: "poll" },
    { resource: "configmaps", mode: "direct" },
    { resource: "secrets", mode: "forbidden" },
    { resource: "gateways", mode: "not_installed" },
    { resource: "jobs", mode: "pending" },
  ]);

  test("kinds the cache says changed need no timer", () => {
    expect(needsTimer(live, "pods")).toBe(false);
    expect(needsTimer(live, "services")).toBe(false); // polled by the server, which says what changed
    expect(needsTimer(live, ["pods", "secrets", "gateways"])).toBe(false); // same answer until a probe says otherwise
  });

  test("kinds read from the cluster each time need it", () => {
    expect(needsTimer(live, "configmaps")).toBe(true);
    expect(needsTimer(live, "jobs")).toBe(true);
    expect(needsTimer(live, ["pods", "configmaps"])).toBe(true);
    expect(needsTimer(live, "unknown")).toBe(true);
  });

  test("without a working stream, everything needs it", () => {
    expect(needsTimer(null, "pods")).toBe(true);
    expect(needsTimer({ connection: "connecting", status: null }, "pods")).toBe(true);
    expect(needsTimer({ ...live, connection: "reconnecting" }, "pods")).toBe(true);
    expect(needsTimer({ connection: "off", status: { state: "off", kinds: [] } }, "pods")).toBe(true);
    expect(needsTimer(open("unreachable", live.status.kinds), "pods")).toBe(true);
  });

  test("a hidden tab needs nothing", () => {
    expect(needsTimer({ ...live, connection: "paused" }, "configmaps")).toBe(false);
  });
});

group("describe", () => {
  test("each state reads as the top bar shows it", () => {
    const status = (state, kinds = []) => ({ connection: "open", status: { state, kinds } });
    expect(describe(null)).toBe(null);
    expect(describe({ connection: "connecting", status: null }).label).toBe("Connecting");
    expect(describe(status("connecting")).label).toBe("Connecting");
    expect(describe(status("live")).label).toBe("Live");
    expect(describe(status("partial", [{ resource: "pods", mode: "poll" }])).label).toBe("Every 30 s");
    expect(describe(status("unreachable")).label).toBe("Unreachable");
    expect(describe({ connection: "reconnecting", status: null }).label).toBe("Reconnecting");
    expect(describe({ connection: "off", status: null }).label).toBe("Every 10 s");
    // Kinds read from the cluster on each visit are on the page's timer.
    expect(describe(status("live", [{ resource: "pods", mode: "direct" }])).label).toBe("Every 10 s");
    expect(describe(status("live", [{ resource: "gateways", mode: "not_installed" }])).label).toBe("Live");
  });

  test("the label is about the page on screen; the groups about every kind", () => {
    const snapshot = {
      connection: "open",
      status: {
        state: "partial",
        kinds: [
          { resource: "pods", mode: "watch" },
          { resource: "services", mode: "poll", reason: "watching isn't allowed" },
          { resource: "secrets", mode: "direct", reason: "listing across all namespaces isn't allowed" },
        ],
      },
    };
    const on = (kinds) => describe(snapshot, (r) => r, kinds);
    expect(on(["pods"]).label).toBe("Live"); // the default install, without Secrets access
    expect(on(["pods"]).groups.map((g) => g.kinds)).toEqual([["services"], ["secrets"]]);
    expect(on(["secrets"]).label).toBe("Every 10 s");
    expect(on(["services"]).label).toBe("Every 30 s");
    expect(on(["pods", "services", "secrets"]).label).toBe("Every 30 s"); // the slowest
    expect(on(undefined).label).toBe("Every 30 s"); // no page: every kind
    // A detail page shows no live kinds: it's read on its timer, which isn't a warning.
    expect(on([])).toMatchObject({ label: "Every 10 s", tone: "neutral" });
    expect(on([]).groups).toEqual(on(["pods"]).groups);
  });

  test("kinds that aren't watched are grouped by how and why", () => {
    const info = describe(
      {
        connection: "open",
        status: {
          state: "partial",
          kinds: [
            { resource: "secrets", mode: "forbidden", reason: "listing isn't allowed" },
            { resource: "pods", mode: "poll", reason: "watching isn't allowed" },
            { resource: "nodes", mode: "watch" },
            { resource: "services", mode: "poll", reason: "watching isn't allowed" },
          ],
        },
      },
      (r) => r.toUpperCase(),
    );
    expect(info.groups).toEqual([
      { title: "Every 30 s", reason: "watching isn't allowed", kinds: ["PODS", "SERVICES"] },
      { title: "Not shown", reason: "listing isn't allowed", kinds: ["SECRETS"] },
    ]);
  });
});

// Fakes for what the browser provides.

class FakeEventSource {
  static all = [];
  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this.handlers = {};
    FakeEventSource.all.push(this);
  }
  addEventListener(type, fn) {
    (this.handlers[type] ??= []).push(fn);
  }
  close() {
    this.readyState = 2;
    this.closed = true;
  }
  // The server's side:
  connect() {
    this.readyState = 1;
    this.onopen?.();
  }
  send(type, data = {}) {
    this.handlers[type]?.forEach((fn) => fn({ data: JSON.stringify(data) }));
  }
  fail(readyState) {
    this.readyState = readyState;
    this.onerror?.();
  }
}

function fakeDocument() {
  const listeners = new Set();
  return {
    hidden: false,
    addEventListener: (_, fn) => listeners.add(fn),
    removeEventListener: (_, fn) => listeners.delete(fn),
    setHidden(hidden) {
      this.hidden = hidden;
      listeners.forEach((fn) => fn());
    },
  };
}

group("LiveConnection", () => {
  let doc, messages, conn, liveAnswer;
  const latest = () => FakeEventSource.all.at(-1);
  const LIVE = { state: "live", kinds: [{ resource: "pods", mode: "watch" }] };

  beforeEach(() => {
    vi.useFakeTimers();
    FakeEventSource.all = [];
    doc = fakeDocument();
    messages = [];
    liveAnswer = { enabled: true };
    conn = new LiveConnection("lab", {
      EventSource: FakeEventSource,
      document: doc,
      fetch: async () => ({ ok: true, json: async () => liveAnswer }),
    });
    conn.subscribe((m) => messages.push(m));
    conn.start();
  });

  afterEach(() => {
    conn.stop();
    vi.useRealTimers();
  });

  test("opens one stream and passes its messages on", () => {
    expect(FakeEventSource.all).toHaveLength(1);
    expect(latest().url).toBe("/api/clusters/lab/live/stream");
    expect(conn.getSnapshot().connection).toBe("connecting");

    latest().connect();
    latest().send("status", LIVE);
    expect(conn.getSnapshot()).toEqual({ connection: "open", status: LIVE });

    latest().send("changed", { resource: "pods", namespaces: ["web"] });
    expect(messages).toEqual([{ type: "changed", resource: "pods", namespaces: ["web"] }]);
  });

  test("a reconnect fetches everything again; the first open doesn't", () => {
    latest().connect();
    latest().send("status", LIVE);
    latest().fail(0); // EventSource reconnects by itself
    expect(conn.getSnapshot().connection).toBe("reconnecting");
    latest().connect();
    expect(messages).toEqual([{ type: "resync" }]);
  });

  test("hidden tabs close the stream; showing it opens a new one and fetches everything", () => {
    latest().connect();
    latest().send("status", LIVE);
    doc.setHidden(true);
    expect(latest().closed).toBe(true);
    expect(conn.getSnapshot().connection).toBe("paused");

    doc.setHidden(false);
    expect(FakeEventSource.all).toHaveLength(2);
    latest().connect();
    expect(messages).toEqual([{ type: "resync" }]);
  });

  test("a stream that goes silent is opened again", () => {
    latest().connect();
    latest().send("status", LIVE);
    vi.advanceTimersByTime(30_000);
    latest().send("heartbeat");
    vi.advanceTimersByTime(30_000);
    expect(FakeEventSource.all).toHaveLength(1); // the heartbeat kept it
    vi.advanceTimersByTime(20_000);
    expect(FakeEventSource.all).toHaveLength(2);
    expect(FakeEventSource.all[0].closed).toBe(true);
  });

  test("live data turned off on the server: no more tries", async () => {
    liveAnswer = { enabled: false, state: "off" };
    latest().fail(2); // a 204 closes EventSource for good
    await vi.runAllTimersAsync();
    expect(conn.getSnapshot().connection).toBe("off");
    expect(FakeEventSource.all).toHaveLength(1);
  });

  test("other failures are tried again, less and less often", async () => {
    latest().fail(2);
    await vi.advanceTimersByTimeAsync(0);
    expect(conn.getSnapshot().connection).toBe("reconnecting");
    await vi.advanceTimersByTimeAsync(5_000);
    expect(FakeEventSource.all).toHaveLength(2);

    latest().fail(2);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(FakeEventSource.all).toHaveLength(2); // waits 10 s the second time
    await vi.advanceTimersByTimeAsync(5_000);
    expect(FakeEventSource.all).toHaveLength(3);
  });
});
