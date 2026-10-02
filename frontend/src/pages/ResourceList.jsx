import { useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { columns as allColumns } from "../columns.jsx";
import { clock } from "../format.js";
import ResourceTable from "../components/ResourceTable.jsx";
import ErrorBox from "../components/ErrorBox.jsx";
import { detailPath, nsQuery } from "../sections.js";
import { MetricsUnavailable } from "../components/Usage.jsx";

const REFRESH_MS = 10_000;
// metrics-server refreshes its numbers every 15 seconds; asking more often
// would only return the same values.
const METRICS_REFRESH_MS = 15_000;

// A page that lists one resource type for the selected cluster and namespace.
// `page` comes from sections.js; page.resource is the API name, e.g. "pods".
export default function ResourceList({ page }) {
  // Layout passes the selected cluster down through the router's <Outlet>.
  const { cluster, reachable } = useOutletContext();
  const [searchParams] = useSearchParams();
  const search = nsQuery(useLocation().search); // detail links keep ?ns
  // Cluster-scoped resources (like nodes) ignore the namespace picker.
  const namespace = page.clusterScoped ? "" : (searchParams.get("ns") ?? "");

  // Don't ask an unreachable cluster; Layout already shows why.
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  const path = reachable ? `/clusters/${cluster.id}/${page.resource}${query}` : null;
  const { data, error, loading, updatedAt, reload } = useApi(path, { refreshMs: REFRESH_MS });

  // Live usage, for pages that show it (nodes and pods).
  const metricsPath = page.metrics && reachable ? `/clusters/${cluster.id}/metrics/${page.metrics}${query}` : null;
  const usage = useApi(metricsPath, { refreshMs: METRICS_REFRESH_MS });
  const rows = data && withUsage(data, usage.data, page.metrics);

  // With one namespace selected, a namespace column would say the same thing on every row.
  const columns = allColumns[page.resource].filter((col) => !(namespace && col.key === "namespace"));
  const noun = page.label.toLowerCase();

  return (
    <section>
      <h1 className="page-title">{page.label}</h1>
      <p className="page-about">{page.about}</p>

      {error?.code === "not_installed" ? (
        // A missing optional feature, not a failure: say so calmly instead of showing an error.
        <div className="empty">
          {error.message}. Install the Gateway API CRDs and a controller to use {page.label}.
        </div>
      ) : (
        error && <ErrorBox title={`Couldn't load ${noun}`} message={error.message} detail={error.detail} />
      )}

      {usage.error?.code === "metrics_unavailable" && <MetricsUnavailable compact={page.metrics === "pods"} />}
      {usage.error && usage.error.code !== "metrics_unavailable" && (
        <p className="muted">Usage isn't available right now: {usage.error.message}</p>
      )}

      {reachable && error?.code !== "not_installed" && (
        <ResourceTable
          columns={columns}
          rows={rows}
          noun={noun}
          linkTo={(row) => detailPath(cluster.id, page.resource, row.namespace, row.name) + search}
          emptyText={namespace ? `No ${noun} in ${namespace}.` : `No ${noun} in this cluster.`}
          toolbar={
            <>
              {updatedAt && <span className="muted">Updated {clock(updatedAt)}</span>}
              <button type="button" className="button button-quiet" onClick={reload} disabled={loading}>
                {loading ? "Refreshing…" : "Refresh"}
              </button>
            </>
          }
        />
      )}
    </section>
  );
}

// withUsage attaches live usage to each row as `row.usage` (undefined when
// there's no number for it, e.g. a pod that just started).
//   nodes: metrics is { nodes: [...], nodeHistory: { name: [...] } }
//   pods:  metrics is [{ namespace, name, cpu, memory }, ...]
function withUsage(rows, metrics, kind) {
  if (!metrics) return rows;
  const byKey = {};
  if (kind === "nodes") {
    for (const n of metrics.nodes) byKey[n.name] = { ...n, history: metrics.nodeHistory[n.name] };
  } else {
    for (const p of metrics) byKey[`${p.namespace}/${p.name}`] = p;
  }
  return rows.map((row) => ({ ...row, usage: byKey[kind === "nodes" ? row.name : `${row.namespace}/${row.name}`] }));
}
