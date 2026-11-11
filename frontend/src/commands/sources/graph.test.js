import { describe, expect, it } from "vitest";
import { buildList, collect, collectFromQuery } from "../list.js";
import { graph, graphPhrase } from "./graph.js";

const ctx = { cluster: { id: "lab" }, namespace: "shop", nsSearch: "?ns=shop" };

describe("graph search in the palette", () => {
  it("notices a graph search phrase, and nothing else", () => {
    expect(graphPhrase("  pods   using db-creds ")).toBe("pods using db-creds");
    expect(graphPhrase("what runs on node-3")).toBe("what runs on node-3");
    expect(graphPhrase("what uses tls")).toBe("what uses tls");
    expect(graphPhrase("using")).toBeNull();
    expect(graphPhrase("pods api")).toBeNull();
    expect(graphPhrase("on call rota")).toBeNull();
  });

  it("offers the search on the Topology page, in the namespace picked", () => {
    const [command] = graph.fromQuery(ctx, "pods using db-creds");
    expect(command.to).toBe("/c/lab/topology?ns=shop&q=pods+using+db-creds");
    expect(graph.fromQuery({ ...ctx, cluster: null }, "pods using db-creds")).toEqual([]);
  });

  it("ranks first, even when its first word narrows the palette to a kind", () => {
    // An object source whose pods are scoped by "pods", as objects.js does.
    const objects = {
      id: "objects",
      commands: [{ id: "object:db-creds", title: "db-creds", group: "Pods", scope: ["pods", "pod", "po"], to: "/x" }],
    };
    const query = "pods using db-creds";
    const entries = [...collectFromQuery([objects, graph], ctx, query), ...collect([objects, graph], ctx)];
    const list = buildList({ entries, query });
    expect(list.items[0].command.id).toBe("graph.search:pods using db-creds");
  });
});
