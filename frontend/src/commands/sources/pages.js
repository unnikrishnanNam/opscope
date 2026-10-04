import { allPages } from "../../sections.js";

// "Go to" every page of the current cluster, straight from sections.js, so
// a new page there is in the palette too. Links keep the selected
// namespace, as the sidebar's do. `aliases` ("po", "svc") find them as well.
export const pages = {
  id: "pages",
  commands: (ctx) =>
    ctx.cluster
      ? allPages.map((page) => ({
          id: `go.${page.path}`,
          title: page.label,
          detail: page.group,
          group: "Go to",
          icon: page.icon,
          keywords: page.aliases,
          shortcut: page.shortcut,
          to: `/c/${encodeURIComponent(ctx.cluster.id)}/${page.path}${ctx.nsSearch}`,
        }))
      : [],
};
