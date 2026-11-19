import { describe, expect, test } from "vitest";
import { issues } from "./issues.js";

describe("issues", () => {
  test("the critical issues, and the namespace's when one is picked", () => {
    const all = issues.commands({ cluster: { id: "lab" }, namespace: "", nsSearch: "" });
    expect(all.map((c) => [c.title, c.to])).toEqual([["Critical issues", "/c/lab/issues?severity=critical"]]);
    const shop = issues.commands({ cluster: { id: "lab" }, namespace: "shop", nsSearch: "?ns=shop" });
    expect(shop.map((c) => [c.title, c.to])).toEqual([
      ["Issues in shop", "/c/lab/issues?ns=shop"],
      ["Critical issues in shop", "/c/lab/issues?ns=shop&severity=critical"],
    ]);
  });

  test("nothing without a cluster", () => {
    expect(issues.commands({})).toEqual([]);
  });
});
