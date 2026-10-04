import { describe, expect, test } from "vitest";
import { kubectlDescribe, kubectlGet, kubectlLogs } from "./kubectl.js";

const pods = { resource: "pods" };
const nodes = { resource: "nodes" };
const gateways = { resource: "gateways", kubectl: "gateways.gateway.networking.k8s.io" };

describe("kubectl lines", () => {
  test("get and describe, with and without a namespace", () => {
    expect(kubectlGet(pods, "web", "api-1")).toBe("kubectl -n web get pods api-1 -o yaml");
    expect(kubectlDescribe(nodes, "", "worker-1")).toBe("kubectl describe nodes worker-1");
  });

  test("Gateway API kinds use their full name", () => {
    expect(kubectlGet(gateways, "web", "main")).toBe("kubectl -n web get gateways.gateway.networking.k8s.io main -o yaml");
  });

  test("logs, for a container and its previous run", () => {
    expect(kubectlLogs("web", "api-1")).toBe("kubectl -n web logs api-1");
    expect(kubectlLogs("web", "api-1", { container: "api", previous: true })).toBe(
      "kubectl -n web logs api-1 -c api --previous",
    );
  });

  test("anything unusual is quoted for the shell", () => {
    expect(kubectlDescribe(pods, "web", "a b; rm -rf ~")).toBe("kubectl -n web describe pods 'a b; rm -rf ~'");
    expect(kubectlDescribe(pods, "web", "it's")).toBe("kubectl -n web describe pods 'it'\\''s'");
  });
});
