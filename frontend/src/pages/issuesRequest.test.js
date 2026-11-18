import { describe, expect, test } from "vitest";
import { choices, filtered, issuesLink, issuesPath, issuesRequest, withFilter } from "./issuesRequest.js";

const group = (type, title, severity, resource, kind) => ({
  id: `${type}-${resource}`,
  type,
  title,
  severity,
  signals: [{ resource: { resource, kind } }],
});
const groups = [
  group("crash_loop", "Crash loop", "critical", "pods", "Pod"),
  group("replicas_unavailable", "Replicas not ready", "warning", "deployments", "Deployment"),
  group("rollout_stuck", "Rollout stuck", "critical", "deployments", "Deployment"),
  group("crash_loop", "Crash loop", "critical", "pods", "Pod"),
];

describe("issuesRequest", () => {
  test("reads the URL", () => {
    const r = issuesRequest(new URLSearchParams("ns=shop&tab=resolved&severity=critical&type=a,b&kind=pods&signal=x"));
    expect(r).toEqual({ namespace: "shop", tab: "resolved", severity: "critical", types: ["a", "b"], kind: "pods", signal: "x" });
    expect(issuesRequest(new URLSearchParams("tab=nonsense"))).toMatchObject({ tab: "open", types: [], kind: "" });
  });

  test("asks the server only for the tab and namespace", () => {
    const r = issuesRequest(new URLSearchParams("ns=shop&tab=resolved&kind=pods"));
    expect(issuesPath("lab", r)).toBe("/clusters/lab/issues?namespace=shop&state=resolved");
    expect(issuesPath("lab", issuesRequest(new URLSearchParams()))).toBe("/clusters/lab/issues");
  });

  test("filters by severity, types and kind", () => {
    const f = (q) => filtered(groups, issuesRequest(new URLSearchParams(q))).map((g) => g.type);
    expect(f("severity=critical")).toEqual(["crash_loop", "rollout_stuck", "crash_loop"]);
    expect(f("type=replicas_unavailable,rollout_stuck")).toEqual(["replicas_unavailable", "rollout_stuck"]);
    expect(f("kind=deployments&severity=warning")).toEqual(["replicas_unavailable"]);
    expect(f("")).toHaveLength(4);
  });

  test("offers what's there, most first", () => {
    const c = choices(groups);
    expect(c.types).toEqual([
      { value: "crash_loop", label: "Crash loop", count: 2 },
      { value: "replicas_unavailable", label: "Replicas not ready", count: 1 },
      { value: "rollout_stuck", label: "Rollout stuck", count: 1 },
    ]);
    expect(c.kinds.map((k) => `${k.label} ${k.count}`)).toEqual(["Deployment 2", "Pod 2"]);
  });

  test("setting a filter keeps the rest and closes an opened row", () => {
    const next = withFilter(new URLSearchParams("ns=shop&kind=pods&signal=x"), "severity", "critical");
    expect(next.toString()).toBe("ns=shop&kind=pods&severity=critical");
    expect(withFilter(next, "kind", "").toString()).toBe("ns=shop&severity=critical");
  });

  test("links from elsewhere keep the namespace", () => {
    expect(issuesLink("lab", { kind: "deployments", types: ["replicas_unavailable", "rollout_stuck"] }, "?ns=shop")).toBe(
      "/c/lab/issues?ns=shop&kind=deployments&type=replicas_unavailable%2Crollout_stuck",
    );
    expect(issuesLink("lab", { signal: "abc" })).toBe("/c/lab/issues?signal=abc");
    expect(issuesLink("lab")).toBe("/c/lab/issues");
  });
});
