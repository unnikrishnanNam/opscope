import { api } from "../../api.js";
import { allPages, detailPath } from "../../sections.js";

// Every object in the cluster, found by name: GET /api/clusters/{id}/names
// lists them all (see backend/internal/resources/names.go), and picking one
// opens its page. Only once something is typed: there are too many to list.
//
// Kinds come from sections.js: an object is offered when its kind has a
// page there, grouped and labelled by that page. So a kind the backend
// lists becomes searchable as soon as it gets a page.
//
// A kind's name and short names narrow the search without finding anything
// alone ("po api": pods called api), and objects in the selected namespace
// rank a little higher.
export const objects = {
  id: "objects",
  label: "objects",
  searchOnly: true,
  key: (ctx) => (ctx.cluster && ctx.reachable ? ctx.cluster.id : null),
  load: (ctx) => api(`/clusters/${encodeURIComponent(ctx.cluster.id)}/names`),
  commands: (ctx, index) =>
    index.objects.flatMap(({ resource, namespace, name }) => {
      const page = pageOf.get(resource);
      if (!page) return [];
      return [
        {
          id: `object:${ctx.cluster.id}/${resource}/${namespace ?? ""}/${name}`,
          title: name,
          detail: namespace,
          group: page.label,
          icon: page.icon,
          scope: [page.label.toLowerCase(), ...(page.aliases ?? [])],
          boost: namespace && namespace === ctx.namespace ? 3 : 0,
          to: detailPath(ctx.cluster.id, resource, namespace, name) + ctx.nsSearch,
        },
      ];
    }),
  // Kinds that couldn't be searched, so a missing result isn't a mystery.
  // A kind the cluster doesn't have (Gateway API) needs no explaining.
  notes: (ctx, index) =>
    index.skipped.flatMap(({ resource, code, error }) => {
      const page = pageOf.get(resource);
      if (!page || code === "not_installed") return [];
      return code === "forbidden"
        ? [`${page.label} aren't searched: this user isn't allowed to list them.`]
        : [`Couldn't search ${page.label}: ${error}`];
    }),
};

// The page for each kind, e.g. "pods" -> Pods.
const pageOf = new Map(allPages.filter((p) => p.resource).map((p) => [p.resource, p]));
