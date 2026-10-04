import { useEffect, useState } from "react";
import { useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useCommands } from "../commands/registry.jsx";
import { columns as allColumns } from "../columns.jsx";
import { clock } from "../format.js";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import DataTable from "../components/DataTable.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { MetricsUnavailable } from "../components/Usage.jsx";
import { RefreshIcon } from "../components/icons.jsx";
import { detailPath, nsQuery } from "../sections.js";
import "./ResourceList.css";

const REFRESH_MS = 10_000;
// metrics-server refreshes its numbers every 15 seconds; asking more often
// would only return the same values.
const METRICS_REFRESH_MS = 15_000;

// A page that lists one resource type for the selected cluster and namespace.
// `page` comes from sections.js; page.resource is the API name, e.g. "pods".
// The table fills the rest of the window and scrolls inside, so its column
// headers stay in view.
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

  // The refresh button only spins for a refresh someone asked for, not for
  // the automatic one every 10 seconds.
  const [refreshing, setRefreshing] = useState(false);
  useEffect(() => {
    if (!loading) setRefreshing(false);
  }, [loading]);
  function refresh() {
    setRefreshing(true);
    reload();
    usage.reload();
  }

  // With one namespace selected, a namespace column would say the same thing on every row.
  const columns = allColumns[page.resource].filter((col) => !(namespace && col.key === "namespace"));
  const noun = page.label.toLowerCase();
  // Two answers aren't failures: an optional feature that isn't installed,
  // and RBAC that doesn't allow this kind (often on purpose, for Secrets).
  const notInstalled = error?.code === "not_installed";
  const forbidden = error?.status === 403;

  useCommands(
    "list",
    path ? [{ id: "list.refresh", title: `Refresh ${noun}`, group: "This page", icon: RefreshIcon, run: refresh }] : [],
    [path, noun],
  );

  return (
    <section className="list-page">
      <PageHeader title={page.label} description={page.about} />

      {notInstalled && (
        <Callout title="Gateway API isn't installed on this cluster">
          It has no <code>gateway.networking.k8s.io/v1</code> resources. Install the Gateway API CRDs and a controller
          to see {page.label} here.
        </Callout>
      )}
      {forbidden && (
        <Callout tone="warning" title={`Opscope isn't allowed to read ${noun}`} detail={error.detail}>
          The Kubernetes user or service account it connects with has no permission for this. See “Permissions” in the
          README.
        </Callout>
      )}
      {error && !notInstalled && !forbidden && (
        <Callout tone="error" title={`Couldn't load ${noun}`} detail={error.detail}>
          {error.message}
        </Callout>
      )}

      {usage.error?.code === "metrics_unavailable" && <MetricsUnavailable compact={page.metrics === "pods"} />}
      {usage.error && usage.error.code !== "metrics_unavailable" && (
        <Callout compact tone="warning">
          Usage isn't available right now: {usage.error.message}
        </Callout>
      )}

      {reachable && !notInstalled && !forbidden && (
        <DataTable
          fill
          shortcut="/"
          columns={columns}
          rows={rows}
          noun={noun}
          linkTo={(row) => detailPath(cluster.id, page.resource, row.namespace, row.name) + search}
          emptyIcon={page.icon}
          emptyText={namespace ? `No ${noun} in ${namespace}.` : `No ${noun} in this cluster.`}
          toolbar={
            <>
              {updatedAt && <span className="list-updated">Updated {clock(updatedAt)}</span>}
              <Button
                variant="quiet"
                size="sm"
                icon={RefreshIcon}
                loading={refreshing}
                onClick={refresh}
              >
                Refresh
              </Button>
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
