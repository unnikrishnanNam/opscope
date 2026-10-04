import { describe, expect, test } from "vitest";
import { buildList, collect } from "../list.js";
import { objects } from "./objects.js";

const index = {
  objects: [
    { resource: "namespaces", name: "web" },
    { resource: "nodes", name: "worker-1" },
    { resource: "pods", namespace: "web", name: "api-1" },
    { resource: "pods", namespace: "data", name: "api-2" },
    { resource: "services", namespace: "web", name: "api" },
  ],
  skipped: [
    { resource: "gateways", code: "not_installed" },
    { resource: "secrets", code: "forbidden" },
    { resource: "configmaps", code: "failed", error: "timeout" },
  ],
};
const ctx = { cluster: { id: "lab" }, namespace: "", nsSearch: "" };
const ids = (list) => list.items.map((item) => item.command.id);

describe("objects", () => {
  test("only kinds with a page, linking to it", () => {
    const commands = objects.commands(ctx, index);
    expect(commands.map((c) => c.id)).toEqual([
      "object:lab/nodes//worker-1",
      "object:lab/pods/web/api-1",
      "object:lab/pods/data/api-2",
      "object:lab/services/web/api",
    ]);
    expect(commands[0]).toMatchObject({ to: "/c/lab/nodes/worker-1", group: "Nodes", detail: undefined });
    expect(commands[1]).toMatchObject({ to: "/c/lab/workloads/pods/web/api-1", group: "Pods", detail: "web" });
  });

  test("links keep the selected namespace", () => {
    const [, pod] = objects.commands({ ...ctx, namespace: "web", nsSearch: "?ns=web" }, index);
    expect(pod.to).toBe("/c/lab/workloads/pods/web/api-1?ns=web");
  });

  test("the kind narrows the search", () => {
    const entries = collect([objects], ctx, () => index);
    expect(ids(buildList({ entries, query: "api" }))).toHaveLength(3);
    expect(ids(buildList({ entries, query: "po api" }))).toEqual(["object:lab/pods/web/api-1", "object:lab/pods/data/api-2"]);
    expect(ids(buildList({ entries, query: "svc api" }))).toEqual(["object:lab/services/web/api"]);
    // A kind alone finds nothing here; that's the "Go to" page's job.
    expect(ids(buildList({ entries, query: "pods" }))).toEqual([]);
  });

  test("objects in the selected namespace rank higher", () => {
    const entries = collect([objects], { ...ctx, namespace: "data" }, () => index);
    expect(ids(buildList({ entries, query: "po api" }))[0]).toBe("object:lab/pods/data/api-2");
  });

  test("notes name kinds that couldn't be searched, but not missing ones", () => {
    expect(objects.notes(ctx, index)).toEqual([
      "Secrets aren't searched: this user isn't allowed to list them.",
      "Couldn't search ConfigMaps: timeout",
    ]);
  });

  test("nothing to load without a reachable cluster", () => {
    expect(objects.key({})).toBeNull();
    expect(objects.key({ cluster: { id: "lab" }, reachable: false })).toBeNull();
    expect(objects.key({ cluster: { id: "lab" }, reachable: true })).toBe("lab");
  });
});
