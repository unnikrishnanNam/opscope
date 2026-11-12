import { expect, test } from "vitest";
import { typedNamespace } from "./namespaces.js";

test("a name as typed, trimmed and in lowercase", () => {
  expect(typedNamespace("team-a")).toBe("team-a");
  expect(typedNamespace("  Team-A ")).toBe("team-a");
  expect(typedNamespace("a")).toBe("a");
  expect(typedNamespace("x".repeat(63))).toBe("x".repeat(63));
});

test("nothing that can't be a namespace", () => {
  for (const text of ["", "   ", "a b", "-a", "a-", "a_b", "a.b", "x".repeat(64), null, undefined]) {
    expect(typedNamespace(text)).toBe(null);
  }
});
