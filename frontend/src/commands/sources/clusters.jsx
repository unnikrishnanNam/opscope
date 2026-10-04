import { StatusDot, statusText } from "../../components/ClusterSwitcher.jsx";
import { ClustersIcon, PlusIcon } from "../../components/icons.jsx";

// Switching to another cluster (like the cluster switcher: on the same kind
// of page, without the namespace, which may not exist there), and the
// clusters pages.
export const clusters = {
  id: "clusters",
  commands: (ctx) => [
    ...ctx.clusters
      .filter((c) => c.id !== ctx.cluster?.id)
      .map((c) => {
        const status = ctx.statuses[c.id];
        return {
          id: `cluster.${c.id}`,
          title: `Switch to ${c.name}`,
          detail: statusText(status),
          group: "Clusters",
          icon: () => <StatusDot status={status} />,
          keywords: ["cluster"],
          // From a detail page, its list: the object is in the old cluster.
          to: `/c/${encodeURIComponent(c.id)}/${ctx.page?.path ?? "overview"}`,
        };
      }),
    { id: "clusters.manage", title: "Manage clusters", group: "Clusters", icon: ClustersIcon, shortcut: "g c", to: "/clusters" },
    { id: "clusters.add", title: "Add a cluster", group: "Clusters", icon: PlusIcon, keywords: ["kubeconfig"], to: "/clusters/add" },
  ],
};
