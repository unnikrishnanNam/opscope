import { createContext, useContext, useEffect, useState } from "react";
import { api, useApi } from "./api.js";

// How often every cluster is checked (GET /api/clusters/{id}).
const STATUS_REFRESH_MS = 30_000;

// The list of clusters, and whether each one answers, are needed in many
// places (sidebar, top bar, cluster switcher, pages), so we load them once
// and share them through React context instead of passing them down
// through every component.
const ClustersContext = createContext(null);

export function ClustersProvider({ children }) {
  const clusters = useApi("/clusters");
  const { statuses, recheck } = useStatuses(clusters.data);
  return <ClustersContext.Provider value={{ ...clusters, statuses, recheck }}>{children}</ClustersContext.Provider>;
}

// useClusters returns:
//   data      [...clusters] (null while loading)
//   error, loading, reload   for the list itself
//   statuses  { [id]: status } from GET /api/clusters/{id}: reachable,
//             version, or error and detail. A cluster that hasn't answered
//             yet has no entry.
//   recheck   checks every cluster again now (a "Try again" button)
export function useClusters() {
  return useContext(ClustersContext);
}

// useStatuses checks every cluster now and every 30 seconds. Each answer is
// stored as it arrives, so one slow cluster doesn't hold up the others.
function useStatuses(clusters) {
  const [statuses, setStatuses] = useState({});
  const [round, setRound] = useState(0); // bumped by recheck()
  // The effect only needs to restart when the set of clusters changes.
  const ids = clusters?.map((c) => c.id).join("\n") ?? "";

  useEffect(() => {
    if (!ids) return;
    let cancelled = false;
    function checkAll() {
      for (const id of ids.split("\n")) {
        api(`/clusters/${encodeURIComponent(id)}`).then(
          (status) => !cancelled && setStatuses((s) => ({ ...s, [id]: status })),
          () => {}, // the Opscope server itself didn't answer; the sidebar says so
        );
      }
    }
    checkAll();
    const timer = setInterval(checkAll, STATUS_REFRESH_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [ids, round]);

  return { statuses, recheck: () => setRound((n) => n + 1) };
}
