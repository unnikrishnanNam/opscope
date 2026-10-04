import { useMemo } from "react";
import { useLocation, useNavigate } from "react-router";
import { useClusters } from "../clusters.jsx";
import { nsQuery, pageFor } from "../sections.js";

// useCommandContext builds `ctx`, what every command may know and do (see
// registry.jsx). It's built once, by the layout, so every source sees the
// same thing. Adding a field here makes it available to every command.
//
//   cluster      the cluster in the URL; undefined on pages outside a cluster
//                (the welcome and clusters pages)
//   reachable    whether it answers (undefined while it's being checked)
//   clusters     every cluster, and `statuses` ({ [id]: status }) for each
//   page         the sections.js page on screen, or the list a detail page
//                belongs to; undefined elsewhere
//   pagePath     the URL after /c/<id>/, e.g. "workloads/pods/web/api-1"
//   namespace    the selected namespace ("" for all namespaces)
//   nsSearch     "?ns=web", for links that keep the namespace ("" for all)
//   location     the router's location
//   navigate     the router's navigate
//   setNamespace(name)  selects a namespace ("" for all), on the same page
//   openShortcuts()     shows the keyboard shortcuts help
//
// `showShortcuts` is the layout's setter for the help dialog.
export function useCommandContext({ cluster, status, pagePath, showShortcuts }) {
  const { data: clusters, statuses } = useClusters();
  const location = useLocation();
  const navigate = useNavigate();
  const reachable = status?.reachable;

  return useMemo(() => {
    const params = new URLSearchParams(location.search);
    return {
      cluster,
      reachable,
      clusters: clusters ?? [],
      statuses,
      page: pageFor(pagePath),
      pagePath,
      namespace: params.get("ns") ?? "",
      nsSearch: nsQuery(location.search),
      location,
      navigate,
      // The same as the namespace picker: change ?ns, keep everything else.
      setNamespace(name) {
        const next = new URLSearchParams(location.search);
        if (name) next.set("ns", name);
        else next.delete("ns");
        const search = next.toString();
        navigate({ pathname: location.pathname, search: search ? `?${search}` : "" });
      },
      openShortcuts: () => showShortcuts(true),
    };
  }, [cluster, reachable, clusters, statuses, pagePath, location, navigate, showShortcuts]);
}
