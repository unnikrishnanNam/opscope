import { api } from "../../api.js";
import { NamespaceIcon } from "../../components/icons.jsx";

// Choosing a namespace, like the picker in the top bar: "Switch namespace…"
// opens them all, and typing a namespace's name finds it directly. Left out
// where the picker is disabled (cluster-wide pages like Nodes) and while
// the cluster doesn't answer.
export const namespaces = {
  id: "namespaces",
  label: "namespaces",
  key: (ctx) => (ctx.cluster && ctx.reachable ? ctx.cluster.id : null),
  load: (ctx) => api(`/clusters/${encodeURIComponent(ctx.cluster.id)}/namespaces`),
  commands: (ctx, list) => {
    if (ctx.page?.clusterScoped) return [];
    const names = list.map((ns) => ns.name);

    // One command per choice; `searchOnly` keeps them out of the empty
    // palette (a cluster can have many), but typing finds them.
    const choice = (name, searchOnly) => ({
      // "namespace:" can't clash with "namespace.switch", whatever the name.
      id: `namespace:${name || "*"}`,
      title: name || "All namespaces",
      // Found by typing at the top, a row needs to say what it is.
      detail: name === ctx.namespace ? "selected" : searchOnly ? "Namespace" : undefined,
      group: "Namespaces",
      icon: NamespaceIcon,
      keywords: name ? [] : ["namespace"],
      searchOnly,
      run: (c) => c.setNamespace(name),
    });

    return [
      {
        id: "namespace.switch",
        title: "Switch namespace…",
        detail: ctx.namespace || "All namespaces",
        group: "Namespace",
        icon: NamespaceIcon,
        keywords: ["ns"],
        items: () => ["", ...names].map((name) => choice(name, false)),
      },
      ...names.map((name) => choice(name, true)),
      // "All namespaces" is offered without typing once one is selected.
      ...(ctx.namespace ? [{ ...choice("", false), group: "Namespace" }] : []),
    ];
  },
};
