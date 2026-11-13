import { describe, expect, test } from "vitest";
import { pages } from "./commands/sources/pages.js";
import { objects } from "./commands/sources/objects.js";
import { allPages, detailPath, hiddenPages, pageFor, sections } from "./sections.js";

describe("pages without a sidebar entry", () => {
  const sidebar = sections.flatMap((s) => s.items.map((i) => i.resource));

  test("ReplicaSets and EndpointSlices have detail pages but no sidebar entry", () => {
    expect(hiddenPages.map((p) => p.resource)).toEqual(["replicasets", "endpointslices"]);
    for (const page of hiddenPages) {
      expect(sidebar).not.toContain(page.resource);
      expect(allPages).toContainEqual(page);
    }
    expect(sidebar).toContain("serviceaccounts");
  });

  test("their objects link to their pages", () => {
    expect(detailPath("lab", "replicasets", "shop", "web-5d8c")).toBe("/c/lab/workloads/replicasets/shop/web-5d8c");
    expect(detailPath("lab", "endpointslices", "shop", "web-x1")).toBe("/c/lab/network/endpointslices/shop/web-x1");
    expect(pageFor("network/endpointslices/shop/web-x1")?.resource).toBe("endpointslices");
  });

  test("they aren't offered as pages in the palette, but are found by name", () => {
    const ctx = { cluster: { id: "lab" }, namespace: "", nsSearch: "" };
    const titles = pages.commands(ctx).map((c) => c.title);
    expect(titles).toContain("ServiceAccounts");
    expect(titles).not.toContain("ReplicaSets");
    expect(titles).not.toContain("EndpointSlices");

    const found = objects.commands(ctx, {
      objects: [
        { resource: "replicasets", namespace: "shop", name: "web-5d8c" },
        { resource: "serviceaccounts", namespace: "shop", name: "ci" },
      ],
      skipped: [],
    });
    expect(found.map((c) => [c.group, c.to])).toEqual([
      ["ReplicaSets", "/c/lab/workloads/replicasets/shop/web-5d8c"],
      ["ServiceAccounts", "/c/lab/config/serviceaccounts/shop/ci"],
    ]);
    expect(found[0].scope).toContain("rs");
  });
});
