import { describe, expect, test } from "vitest";
import { inodesText, isNearlyFull, spaceFull, spaceText, usageByClaim } from "./volumeUsage.js";

const GiB = 1024 ** 3;
const volume = (over) => ({
  namespace: "data",
  name: "db",
  used: 2 * GiB,
  capacity: 10 * GiB,
  available: 8 * GiB,
  inodesUsed: 1204,
  inodes: 655360,
  ...over,
});

describe("usageByClaim", () => {
  test("claims with numbers, and claims on nodes that couldn't be read", () => {
    const byKey = usageByClaim({
      volumes: [volume()],
      unread: [{ node: "b", error: "timeout", claims: ["data/logs", "data/db"] }],
      unmeasured: ["data/files"],
    });
    expect(byKey["data/files"]).toEqual({ unmeasured: true });
    expect(byKey["data/db"].used).toBe(2 * GiB); // numbers win over an unread second node
    expect(byKey["data/logs"]).toEqual({ unread: { node: "b", error: "timeout", claims: ["data/logs", "data/db"] } });
    expect(byKey["data/other"]).toBeUndefined();
  });

  test("no report yet", () => {
    expect(usageByClaim(undefined)).toEqual({});
  });
});

describe("fullness", () => {
  test("counts from what's still available, not from used", () => {
    // ext4 keeps space back: 8 GiB used of 10, but only 1 GiB left for the pod.
    const u = volume({ used: 8 * GiB, available: 1 * GiB });
    expect(spaceFull(u)).toBe(90);
    expect(isNearlyFull(u)).toBe(true);
  });

  test("running out of inodes counts too", () => {
    expect(isNearlyFull(volume({ inodesUsed: 95, inodes: 100 }))).toBe(true);
    expect(isNearlyFull(volume())).toBe(false);
  });

  test("a volume with no inode numbers isn't full", () => {
    expect(isNearlyFull(volume({ inodesUsed: 0, inodes: 0 }))).toBe(false);
  });
});

test("text", () => {
  expect(spaceText(volume())).toBe("2 GiB used, 8 GiB free of 10 GiB");
  expect(inodesText(volume())).toBe(`${(1204).toLocaleString()} of ${(655360).toLocaleString()} inodes (0%)`);
});
