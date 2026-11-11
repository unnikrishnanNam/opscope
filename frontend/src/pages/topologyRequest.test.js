import { describe, expect, it } from "vitest";
import { objectParam, parseObject, topologyRequest } from "./topologyRequest.js";

const req = (query) => topologyRequest(new URLSearchParams(query));

describe("topologyRequest", () => {
  it("lists the namespaces with none picked", () => {
    expect(req("")).toMatchObject({ mode: "pick", path: null });
  });

  it("maps a namespace", () => {
    expect(req("ns=shop")).toMatchObject({ mode: "namespace", path: "/graph/subgraph?namespace=shop", namespace: "shop" });
  });

  it("maps around an object, in its own namespace, two steps out unless asked", () => {
    expect(req("ns=other&object=deployments/shop/api")).toMatchObject({
      mode: "object",
      path: "/graph/find?resource=deployments&namespace=shop&name=api&depth=2",
      namespace: "shop",
      object: { resource: "deployments", namespace: "shop", name: "api" },
    });
    expect(req("object=nodes/worker-1&depth=3").path).toBe("/graph/find?resource=nodes&name=worker-1&depth=3");
    expect(req("object=nodes/worker-1&depth=9").depth).toBe(3);
    expect(req("object=nodes/worker-1&depth=x").depth).toBe(2);
  });

  it("searches, in the picked namespace", () => {
    expect(req("ns=shop&q=pods using db-creds")).toMatchObject({
      mode: "search",
      path: "/graph/search?q=pods%20using%20db-creds&namespace=shop",
      q: "pods using db-creds",
    });
  });

  it("keeps the same view across depths, a new one for another object", () => {
    expect(req("object=pods/a/b&depth=1").key).toBe(req("object=pods/a/b&depth=3").key);
    expect(req("object=pods/a/b").key).not.toBe(req("object=pods/a/c").key);
  });

  it("reads and writes ?object= both ways", () => {
    expect(parseObject(objectParam("pods", "shop", "web-1"))).toEqual({ resource: "pods", namespace: "shop", name: "web-1" });
    expect(parseObject(objectParam("nodes", "", "n1"))).toEqual({ resource: "nodes", name: "n1" });
    expect(parseObject("nonsense")).toBeNull();
  });
});
