import { describe, expect, test } from "vitest";
import { bytes, size } from "./format.js";

describe("size", () => {
  test("round sizes read like the request they came from", () => {
    expect(size(1024 ** 3)).toBe("1 GiB");
    expect(size(256 * 1024 ** 2)).toBe("256 MiB");
    expect(size(0)).toBe("0 B");
  });

  test("anything else keeps one decimal, like bytes()", () => {
    expect(size(1.5 * 1024 ** 3)).toBe("1.5 GiB");
    expect(bytes(1024 ** 3)).toBe("1.0 GiB");
  });
});
