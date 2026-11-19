import { describe, expect, test } from "vitest";
import { whereToSee } from "./Evidence.jsx";

describe("whereToSee", () => {
  const pod = { resource: "pods", namespace: "shop", name: "api-1" };
  const svc = { resource: "services", namespace: "shop", name: "web" };

  test("a field is in the YAML, an event in the events, a pod's log in its logs", () => {
    expect(whereToSee({ source: "field" }, pod, "lab", "?ns=shop")).toBe("/c/lab/workloads/pods/shop/api-1?ns=shop&tab=yaml");
    expect(whereToSee({ source: "event" }, svc, "lab", "")).toBe("/c/lab/network/services/shop/web?tab=events");
    expect(whereToSee({ source: "log" }, pod, "lab", "")).toBe("/c/lab/workloads/pods/shop/api-1?tab=logs");
  });

  test("nothing to link to: a measurement, a log on a kind without logs, a kind without a page", () => {
    expect(whereToSee({ source: "metric" }, pod, "lab", "")).toBe(null);
    expect(whereToSee({ source: "log" }, svc, "lab", "")).toBe(null);
    expect(whereToSee({ source: "field" }, { resource: "widgets", name: "x" }, "lab", "")).toBe(null);
  });
});
