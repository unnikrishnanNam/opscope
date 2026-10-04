import { describe, expect, test, vi } from "vitest";
import { buildList, collect, collectItems } from "./list.js";

const pages = {
  id: "pages",
  commands: (ctx) => [
    { id: "go.overview", title: "Overview", group: "Go to", to: `/c/${ctx.cluster}/overview` },
    { id: "go.pods", title: "Pods", group: "Go to", to: `/c/${ctx.cluster}/workloads/pods` },
    { id: "go.deployments", title: "Deployments", group: "Go to", to: `/c/${ctx.cluster}/workloads/deployments` },
  ],
};
const theme = {
  id: "theme",
  commands: [{ id: "theme.dark", title: "Dark theme", group: "Theme", run: () => {} }],
};
const objects = {
  id: "objects",
  searchOnly: true,
  load: () => {},
  commands: (ctx, data) => data.map((name) => ({ id: `pod.${name}`, title: name, group: "Pods" })),
};
const ctx = { cluster: "lab" };
const ids = (list) => list.items.map((item) => item.command.id);

describe("collect", () => {
  test("lists every source's commands in order, built from ctx", () => {
    const entries = collect([pages, theme], ctx);
    expect(entries.map((e) => e.command.id)).toEqual(["go.overview", "go.pods", "go.deployments", "theme.dark"]);
    expect(entries[1].command.to).toBe("/c/lab/workloads/pods");
    expect(entries[3].source).toBe(theme);
  });

  test("a source that loads waits for its data", () => {
    expect(collect([objects], ctx)).toEqual([]);
    expect(collect([objects], ctx, () => ["api-1"]).map((e) => e.command.id)).toEqual(["pod.api-1"]);
  });

  test("the first command with an id wins", () => {
    const page = { id: "page", page: true, commands: [{ id: "go.pods", title: "Pods (from the page)" }] };
    const entries = collect([page, pages], ctx);
    expect(entries.filter((e) => e.command.id === "go.pods")).toHaveLength(1);
    expect(entries[0].command.title).toBe("Pods (from the page)");
  });

  test("a source that throws is left out", () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const broken = {
      id: "broken",
      commands: () => {
        throw new Error("boom");
      },
    };
    expect(collect([broken, theme], ctx).map((e) => e.command.id)).toEqual(["theme.dark"]);
    expect(error).toHaveBeenCalledOnce();
    error.mockRestore();
  });
});

describe("collectItems", () => {
  test("lists a command's sub-list", () => {
    const parent = { id: "ns", title: "Switch namespace…", items: (c) => [{ id: `ns.${c.cluster}`, title: "default" }] };
    expect(collectItems(parent, ctx).map((e) => e.command.id)).toEqual(["ns.lab"]);
  });
});

describe("buildList with an empty query", () => {
  const entries = collect([pages, theme, objects], ctx, () => ["api-1", "web-2"]);

  test("shows everything but search-only commands, by group", () => {
    const list = buildList({ entries });
    expect(list.groups.map((g) => g.title)).toEqual(["Go to", "Theme"]);
    expect(ids(list)).toEqual(["go.overview", "go.pods", "go.deployments", "theme.dark"]);
    expect(list.items[0].match).toBeNull();
  });

  test("a single command can be search-only too", () => {
    const ns = {
      id: "namespaces",
      commands: [
        { id: "namespace.switch", title: "Switch namespace…", group: "Namespace" },
        { id: "namespace.web", title: "web", group: "Namespaces", searchOnly: true },
      ],
    };
    const list = collect([ns], ctx);
    expect(ids(buildList({ entries: list }))).toEqual(["namespace.switch"]);
    expect(ids(buildList({ entries: list, query: "web" }))).toEqual(["namespace.web"]);
  });

  test("recent commands come first, search-only ones included", () => {
    const list = buildList({ entries, recent: ["pod.web-2", "theme.dark", "gone"] });
    expect(list.groups[0]).toMatchObject({ title: "Recent" });
    expect(list.groups[0].items.map((i) => i.command.id)).toEqual(["pod.web-2", "theme.dark"]);
    // Not listed twice.
    expect(ids(list).filter((id) => id === "theme.dark")).toHaveLength(1);
  });
});

describe("buildList with a query", () => {
  const entries = collect([pages, theme, objects], ctx, () => ["api-1", "dashboard-api"]);

  test("only matches, best first", () => {
    expect(ids(buildList({ entries, query: "dep" }))).toEqual(["go.deployments"]);
    expect(ids(buildList({ entries, query: "api" }))).toEqual(["pod.api-1", "pod.dashboard-api"]);
  });

  test("search-only commands are found", () => {
    expect(ids(buildList({ entries, query: "api-1" }))).toEqual(["pod.api-1"]);
  });

  test("groups are ordered by their best row", () => {
    const list = buildList({ entries, query: "da" });
    // Both are prefix matches; "Dark theme" is shorter, so it fits better.
    expect(list.groups.map((g) => g.title)).toEqual(["Theme", "Pods"]);
  });

  test("items carry the positions to make bold", () => {
    expect(buildList({ entries, query: "pods" }).items[0].match.title).toEqual([0, 1, 2, 3]);
  });

  test("the limit cuts the rows but not the total", () => {
    const many = collect([objects], ctx, () => Array.from({ length: 80 }, (_, i) => `api-${i}`));
    const list = buildList({ entries: many, query: "api" });
    expect(list.items).toHaveLength(50);
    expect(list.total).toBe(80);
  });

  test("recent and page commands win near-ties", () => {
    const twins = collect([{ id: "x", commands: [{ id: "a", title: "web" }, { id: "b", title: "web" }] }], ctx);
    expect(ids(buildList({ entries: twins, query: "web" }))).toEqual(["a", "b"]);
    expect(ids(buildList({ entries: twins, query: "web", recent: ["b"] }))).toEqual(["b", "a"]);

    const page = { id: "page", page: true, commands: [{ id: "c", title: "web" }] };
    const withPage = collect([{ id: "x", commands: [{ id: "a", title: "web" }] }, page], ctx);
    expect(ids(buildList({ entries: withPage, query: "web" }))).toEqual(["c", "a"]);
  });

  test("a scope word first searches only that kind", () => {
    const mixed = collect(
      [
        {
          id: "x",
          commands: [
            { id: "gateway", title: "main", scope: ["gateways", "gtw"] },
            { id: "deployment", title: "main-gateway-nginx", scope: ["deployments", "deploy"] },
            { id: "page", title: "Gateways", keywords: ["gtw"] },
          ],
        },
      ],
      ctx,
    );
    expect(ids(buildList({ entries: mixed, query: "gtw main" }))).toEqual(["gateway"]);
    // Alone, the word is an ordinary search (and finds the page).
    expect(ids(buildList({ entries: mixed, query: "gtw" }))).toEqual(["page", "deployment"]);
    // Not a scope word: everything is searched.
    expect(ids(buildList({ entries: mixed, query: "main" }))).toEqual(["gateway", "deployment"]);
  });

  test("a boost lifts a command over an equal match", () => {
    const twins = collect([{ id: "x", commands: [{ id: "a", title: "web" }, { id: "b", title: "web", boost: 3 }] }], ctx);
    expect(ids(buildList({ entries: twins, query: "web" }))).toEqual(["b", "a"]);
  });

  test("a recent command doesn't beat a clearly better match", () => {
    expect(ids(buildList({ entries, query: "pods", recent: ["go.deployments"] }))[0]).toBe("go.pods");
  });
});
