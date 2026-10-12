import { describe, expect, it } from "vitest";
import { objectParts, problemSummary, rootCauseText, rootSteps } from "./diagnosis.js";

const step = (title, objects, root, causes = []) => ({ title, objects, root, causes });

// A Deployment whose pods can't start because a Secret is missing, and an
// old Service that selects nothing.
const steps = [
  step("None of its 3 replicas are ready", ["deployments/shop/web"], false, [
    step("Container api can't start", ["pods/shop/web-a", "pods/shop/web-b"], false, [
      step("Secret shop/stripe doesn't exist", ["secrets/shop/stripe"], true),
    ]),
  ]),
  step("Its selector matches no pods", ["services/shop/old"], true),
];

describe("rootSteps", () => {
  it("finds root causes at any depth, in order", () => {
    expect(rootSteps(steps).map((s) => s.title)).toEqual(["Secret shop/stripe doesn't exist", "Its selector matches no pods"]);
  });

  it("is empty for a healthy object", () => {
    expect(rootSteps([])).toEqual([]);
  });
});

describe("rootCauseText", () => {
  it("writes one line per root cause", () => {
    expect(rootCauseText(steps)).toBe(
      "secrets/shop/stripe: Secret shop/stripe doesn't exist\nservices/shop/old: Its selector matches no pods",
    );
  });
});

describe("objectParts", () => {
  it("reads namespaced and cluster-wide IDs", () => {
    expect(objectParts("pods/shop/web-1")).toEqual({ resource: "pods", namespace: "shop", name: "web-1" });
    expect(objectParts("nodes/worker-1")).toEqual({ resource: "nodes", namespace: "", name: "worker-1" });
  });
});

describe("problemSummary", () => {
  it("counts each object once", () => {
    const problems = [
      { objects: ["secrets/shop/stripe"], affects: [{ id: "deployments/shop/web" }, { id: "services/shop/web" }] },
      { objects: ["pods/shop/web-a", "pods/shop/web-b"], affects: [{ id: "deployments/shop/web" }] },
    ];
    expect(problemSummary(problems)).toBe("2 problems, affecting 5 objects");
    expect(problemSummary([{ objects: ["nodes/worker-1"], affects: [] }])).toBe("1 problem, affecting 1 object");
  });
});
