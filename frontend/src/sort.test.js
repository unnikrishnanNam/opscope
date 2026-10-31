import { describe, expect, test } from "vitest";
import { sortRows } from "./sort.js";

describe("sortRows", () => {
  const rows = [{ name: "a", used: 41 }, { name: "b" }, { name: "c", used: 92 }, { name: "d", used: 66 }];
  const columns = [{ key: "used", sortValue: (row) => row.used }];
  const names = (sorted) => sorted.map((row) => row.name).join("");

  test("sorts numbers both ways", () => {
    expect(names(sortRows(rows, columns, { key: "used", ascending: true }))).toBe("adcb");
    expect(names(sortRows(rows, columns, { key: "used", ascending: false }))).toBe("cdab");
  });

  test("empty values stay last either way", () => {
    // Fullest first shouldn't start with the rows that have no number.
    expect(names(sortRows(rows, columns, { key: "used", ascending: false })).at(-1)).toBe("b");
    expect(names(sortRows(rows, columns, { key: "used", ascending: true })).at(-1)).toBe("b");
  });

  test("text sorts naturally", () => {
    const pods = [{ name: "pod-10" }, { name: "pod-2" }];
    expect(names(sortRows(pods, [{ key: "name" }], { key: "name", ascending: true }))).toBe("pod-2pod-10");
  });
});
