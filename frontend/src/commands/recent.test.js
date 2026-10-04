import { afterEach, describe, expect, test, vi } from "vitest";
import { getRecent, remember } from "./recent.js";

// Tests run in Node, which has no localStorage; this stands in for it.
function fakeStorage() {
  const items = new Map();
  return {
    getItem: (key) => items.get(key) ?? null,
    setItem: (key, value) => items.set(key, String(value)),
  };
}

afterEach(() => vi.unstubAllGlobals());

describe("recent commands", () => {
  test("most recent first, each once", () => {
    vi.stubGlobal("localStorage", fakeStorage());
    remember("go.pods");
    remember("theme.dark");
    remember("go.pods");
    expect(getRecent()).toEqual(["go.pods", "theme.dark"]);
  });

  test("keeps the last 10", () => {
    vi.stubGlobal("localStorage", fakeStorage());
    for (let i = 0; i < 12; i++) remember(`cmd.${i}`);
    expect(getRecent()).toHaveLength(10);
    expect(getRecent()[0]).toBe("cmd.11");
  });

  test("ignores something odd in storage", () => {
    const storage = fakeStorage();
    vi.stubGlobal("localStorage", storage);
    storage.setItem("opscope-recent-commands", "{not json");
    expect(getRecent()).toEqual([]);
    storage.setItem("opscope-recent-commands", JSON.stringify(["go.pods", 42]));
    expect(getRecent()).toEqual(["go.pods"]);
  });

  test("works without storage", () => {
    vi.stubGlobal("localStorage", {
      getItem() {
        throw new Error("blocked");
      },
      setItem() {
        throw new Error("blocked");
      },
    });
    expect(() => remember("go.pods")).not.toThrow();
    expect(getRecent()).toEqual([]);
  });
});
