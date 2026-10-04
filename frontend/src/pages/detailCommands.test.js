import { describe, expect, test, vi } from "vitest";
import { allPages } from "../sections.js";
import { detailCommands } from "./detailCommands.js";

const pods = allPages.find((p) => p.resource === "pods");
const nodes = allPages.find((p) => p.resource === "nodes");
const tabs = [
  { value: "summary", label: "Summary" },
  { value: "yaml", label: "YAML" },
  { value: "events", label: "Events" },
  { value: "logs", label: "Logs" },
];
const pod = {
  namespace: "web",
  name: "api-1",
  yaml: "kind: Pod",
  fields: [{ label: "Node", value: "worker-1" }],
  owners: [
    { kind: "ReplicaSet", name: "api-7d9f" }, // no page for ReplicaSets
    { kind: "StatefulSet", name: "api", resource: "statefulsets" },
  ],
};
const base = { d: pod, page: pods, clusterId: "lab", tab: "summary", tabs, openTab: () => {}, search: "?ns=web" };
const byId = (commands) => Object.fromEntries(commands.map((c) => [c.id, c]));

describe("detail page commands", () => {
  test("the other tabs, not the open one", () => {
    const openTab = vi.fn();
    const c = byId(detailCommands({ ...base, tab: "yaml", openTab }));
    expect(c["detail.tab.yaml"]).toBeUndefined();
    expect(c["detail.tab.summary"].title).toBe("Show summary");
    c["detail.tab.logs"].run();
    expect(openTab).toHaveBeenCalledWith("logs");
  });

  test("read-only kubectl lines", () => {
    const c = byId(detailCommands(base));
    expect(c["detail.kubectl.get"].detail).toBe("kubectl -n web get pods api-1 -o yaml");
    expect(c["detail.kubectl.describe"].detail).toBe("kubectl -n web describe pods api-1");
    expect(c["detail.kubectl.logs"].detail).toBe("kubectl -n web logs api-1");
    // On the Logs tab the log viewer has its own, for the container shown.
    expect(byId(detailCommands({ ...base, tab: "logs" }))["detail.kubectl.logs"]).toBeUndefined();
  });

  test("links to the namespace's list, owners with a page, and the node", () => {
    const c = byId(detailCommands(base));
    expect(c["detail.namespace-list"]).toMatchObject({ title: "Pods in web", to: "/c/lab/workloads/pods?ns=web" });
    expect(c["detail.owner.statefulsets.api"]).toMatchObject({
      title: "Go to StatefulSet api",
      to: "/c/lab/workloads/statefulsets/web/api?ns=web",
    });
    expect(Object.keys(c).some((id) => id.includes("ReplicaSet") || id.includes("api-7d9f"))).toBe(false);
    expect(c["detail.node"]).toMatchObject({ title: "Go to node worker-1", to: "/c/lab/nodes/worker-1?ns=web" });
  });

  test("a cluster-wide object has no namespace list and no logs", () => {
    const node = { namespace: undefined, name: "worker-1", yaml: "", fields: [], owners: [] };
    const c = byId(detailCommands({ ...base, d: node, page: nodes, tabs: tabs.slice(0, 3) }));
    expect(c["detail.namespace-list"]).toBeUndefined();
    expect(c["detail.kubectl.logs"]).toBeUndefined();
    expect(c["detail.kubectl.get"].detail).toBe("kubectl get nodes worker-1 -o yaml");
  });
});
