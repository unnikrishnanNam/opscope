import { Link, Outlet, useMatch } from "react-router";
import { useApi } from "../api.js";
import { useClusters } from "../clusters.jsx";
import Sidebar from "./Sidebar.jsx";
import TopBar from "./TopBar.jsx";
import ErrorBox from "./ErrorBox.jsx";

const STATUS_REFRESH_MS = 30_000;

// The frame around every page. It works out which cluster is selected
// (from the URL) and checks that the cluster is reachable.
export default function Layout() {
  // On /c/<id>/<rest>, match.params is { clusterId, "*": rest }.
  const match = useMatch("/c/:clusterId/*");
  const clusterId = match?.params.clusterId;
  const pagePath = match?.params["*"];

  const { data: clusters, loading } = useClusters();
  const cluster = clusters?.find((c) => c.id === clusterId);
  // Wait for a reload to finish: a cluster that was just added isn't in the old list.
  const unknownCluster = clusterId && clusters && !cluster && !loading;

  // GET /api/clusters/<id> tells us the version and whether it answers.
  const status = useApi(cluster ? `/clusters/${cluster.id}` : null, { refreshMs: STATUS_REFRESH_MS });

  return (
    <div className="shell">
      <Sidebar clusterId={cluster?.id ?? clusters?.[0]?.id} />
      <div className="main">
        <TopBar cluster={cluster} pagePath={pagePath} status={cluster ? status : null} />
        <main className="content">
          {cluster && status.data && !status.data.reachable && (
            <ErrorBox title={`Can't reach ${cluster.name}`} message={status.data.error} detail={status.data.detail} />
          )}

          {clusterId && !cluster && !unknownCluster ? (
            // Still loading the cluster list. Waiting here means every page
            // under /c/<id>/ can rely on `cluster` being set.
            <p className="muted">Loading…</p>
          ) : unknownCluster ? (
            <section>
              <h1 className="page-title">Cluster not found</h1>
              <p className="page-about">
                There is no cluster called “{clusterId}”. It may have been removed.{" "}
                <Link to="/clusters">See all clusters</Link>.
              </p>
            </section>
          ) : (
            // Pages read these with useOutletContext().
            <Outlet context={{ cluster, reachable: status.data?.reachable, status: status.data }} />
          )}
        </main>
      </div>
    </div>
  );
}
