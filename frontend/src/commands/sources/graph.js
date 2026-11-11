import { TopologyIcon } from "../../components/icons.jsx";

// Graph search from the palette: "pods using db-creds", "what runs on
// node-3". Typing a phrase like that offers one command, which opens the
// Topology page's map of the answer (GET /graph/search). The server reads
// the phrase; this only notices one: two words or more, with "using",
// "uses" or "on" just before the last.
export const graph = {
  id: "graph",
  commands: [],
  fromQuery: (ctx, query) => {
    const phrase = graphPhrase(query);
    if (!ctx.cluster || !phrase) return [];
    const params = new URLSearchParams();
    if (ctx.namespace) params.set("ns", ctx.namespace);
    params.set("q", phrase);
    return [
      {
        id: `graph.search:${phrase}`,
        title: phrase,
        detail: ctx.namespace ? `Search the topology in ${ctx.namespace}` : "Search the topology",
        group: "Topology",
        icon: TopologyIcon,
        // Its first word may name a kind ("pods using …"), which narrows the
        // palette to that kind's commands: as a scope word, this one stays in.
        scope: [phrase.split(" ")[0].toLowerCase()],
        boost: 20,
        to: `/c/${encodeURIComponent(ctx.cluster.id)}/topology?${params}`,
      },
    ];
  },
};

// graphPhrase returns the query, tidied, if it reads like a graph search.
export function graphPhrase(query) {
  const words = query.trim().split(/\s+/).filter(Boolean);
  if (words.length < 2) return null;
  const verb = words.at(-2).toLowerCase();
  return verb === "using" || verb === "uses" || verb === "on" ? words.join(" ") : null;
}
