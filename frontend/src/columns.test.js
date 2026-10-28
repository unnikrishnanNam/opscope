import { describe, expect, test } from "vitest";
import { accessModeNames, columns } from "./columns.jsx";
import { statusTone } from "./components/StatusBadge.jsx";
import { allPages } from "./sections.js";

describe("tables", () => {
  test("every page with a table has columns", () => {
    for (const page of allPages.filter((p) => p.resource)) {
      expect(columns[page.resource], page.resource).toBeDefined();
    }
  });

  test("access modes are spelled out on hover", () => {
    expect(accessModeNames(["RWO", "RWOP"])).toBe(
      "ReadWriteOnce: read-write from one node\nReadWriteOncePod: read-write from one pod",
    );
    expect(accessModeNames(["SomethingNew"])).toBe("SomethingNew");
  });
});

describe("storage statuses", () => {
  test("claims and volumes", () => {
    expect(statusTone("Bound")).toBe("ok");
    expect(statusTone("Available")).toBe("neutral"); // ready, just unused
    expect(statusTone("Pending")).toBe("warn");
    expect(statusTone("Released")).toBe("warn");
    expect(statusTone("Terminating")).toBe("warn");
    expect(statusTone("Lost")).toBe("bad");
    expect(statusTone("Failed")).toBe("bad");
  });
});
