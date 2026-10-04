import { describe, expect, test, vi } from "vitest";
import { createStore } from "./store.js";

const theme = { id: "theme", commands: [] };

// Lets the store's promise chain finish.
const settle = () => new Promise((resolve) => setTimeout(resolve));

describe("page sources", () => {
  test("come before global ones, newest first", () => {
    const store = createStore([theme]);
    store.addPageSource("detail");
    store.addPageSource("logs");
    expect(store.sources().map((s) => s.id)).toEqual(["logs", "detail", "theme"]);
    expect(store.sources()[0].page).toBe(true);
  });

  test("an update keeps the position", () => {
    const store = createStore([theme]);
    store.addPageSource("detail");
    store.addPageSource("logs");
    store.updatePageSource("detail", [{ id: "tab.yaml", title: "YAML" }]);
    expect(store.sources().map((s) => s.id)).toEqual(["logs", "detail", "theme"]);
    expect(store.sources()[1].commands[0].id).toBe("tab.yaml");
  });

  test("removed when the component goes", () => {
    const store = createStore([theme]);
    store.addPageSource("detail");
    store.removePageSource("detail");
    expect(store.sources().map((s) => s.id)).toEqual(["theme"]);
  });

  test("listeners hear about every change", () => {
    const store = createStore([]);
    const notify = vi.fn();
    const unsubscribe = store.subscribe(notify);
    store.addPageSource("detail");
    store.updatePageSource("detail", []);
    unsubscribe();
    store.removePageSource("detail");
    expect(notify).toHaveBeenCalledTimes(2);
    expect(store.version()).toBe(3);
  });
});

describe("loading", () => {
  function objectsSource(load) {
    return { id: "objects", label: "objects", key: (ctx) => ctx.cluster ?? null, load, commands: () => [] };
  }

  test("loads once per key and keeps the data", async () => {
    const load = vi.fn(async (ctx) => [`${ctx.cluster}-pod`]);
    const source = objectsSource(load);
    const store = createStore([source]);

    store.refresh({ cluster: "lab" });
    expect(store.status({ cluster: "lab" })).toEqual([
      { id: "objects", label: "objects", searchOnly: false, loading: true, error: null, notes: [] },
    ]);
    store.refresh({ cluster: "lab" }); // already loading
    await settle();
    store.refresh({ cluster: "lab" }); // still fresh
    expect(load).toHaveBeenCalledTimes(1);
    expect(store.data(source, { cluster: "lab" })).toEqual(["lab-pod"]);
    expect(store.status({ cluster: "lab" })[0].loading).toBe(false);

    // Another cluster is another key.
    store.refresh({ cluster: "prod" });
    await settle();
    expect(store.data(source, { cluster: "prod" })).toEqual(["prod-pod"]);
    expect(store.data(source, { cluster: "lab" })).toEqual(["lab-pod"]);
  });

  test("a null key loads nothing", () => {
    const load = vi.fn();
    const store = createStore([objectsSource(load)]);
    store.refresh({});
    expect(load).not.toHaveBeenCalled();
    expect(store.status({})).toEqual([]);
  });

  test("old data loads again, and stays in use meanwhile", async () => {
    let n = 0;
    const source = objectsSource(async () => ++n);
    const store = createStore([source]);
    const ctx = { cluster: "lab" };

    store.refresh(ctx);
    await settle();
    store.refresh(ctx, Date.now() + 31_000);
    expect(store.data(source, ctx)).toBe(1);
    await settle();
    expect(store.data(source, ctx)).toBe(2);
  });

  test("a failure is reported and keeps the old data", async () => {
    let fail = false;
    const source = objectsSource(async () => {
      if (fail) throw new Error("unreachable");
      return ["api-1"];
    });
    const store = createStore([source]);
    const ctx = { cluster: "lab" };

    store.refresh(ctx);
    await settle();
    fail = true;
    store.refresh(ctx, Date.now() + 31_000);
    await settle();
    expect(store.data(source, ctx)).toEqual(["api-1"]);
    expect(store.status(ctx)[0].error.message).toBe("unreachable");
  });

  test("notes describe what was loaded, once it has", async () => {
    const source = {
      ...objectsSource(async () => ({ skipped: ["secrets"] })),
      searchOnly: true,
      notes: (ctx, data) => data.skipped.map((kind) => `${kind} aren't searched`),
    };
    const store = createStore([source]);
    const ctx = { cluster: "lab" };

    expect(store.status(ctx)[0]).toMatchObject({ searchOnly: true, notes: [] });
    store.refresh(ctx);
    await settle();
    expect(store.status(ctx)[0].notes).toEqual(["secrets aren't searched"]);
  });

  test("notes that throw are left out", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const source = {
      ...objectsSource(async () => []),
      notes: () => {
        throw new Error("boom");
      },
    };
    const store = createStore([source]);
    store.refresh({ cluster: "lab" });
    await settle();
    expect(store.status({ cluster: "lab" })[0].notes).toEqual([]);
    expect(error).toHaveBeenCalledOnce();
    error.mockRestore();
  });

  test("a source without a key loads once for every ctx", async () => {
    const load = vi.fn(async () => "data");
    const source = { id: "static", load, commands: () => [] };
    const store = createStore([source]);
    store.refresh({ cluster: "lab" });
    await settle();
    store.refresh({ cluster: "prod" });
    expect(load).toHaveBeenCalledTimes(1);
    expect(store.data(source, {})).toBe("data");
  });
});
