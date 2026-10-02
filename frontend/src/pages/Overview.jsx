import { Link, useLocation, useOutletContext, useParams, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { bytes, clock, cpu } from "../format.js";
import ErrorBox from "../components/ErrorBox.jsx";
import PodStatusBar from "../components/PodStatusBar.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import EventsTable from "../components/EventsTable.jsx";
import { detailPath, nsQuery } from "../sections.js";
import { MetricsUnavailable, Sparkline, UsageBar } from "../components/Usage.jsx";

const REFRESH_MS = 10_000;
const WARNINGS_SHOWN = 8;
const METRICS_REFRESH_MS = 15_000; // metrics-server's own refresh interval

// What each workload count links to and how its "unhealthy" number reads.
const WORKLOADS = {
  deployments: { label: "Deployments", path: "workloads/deployments", problem: "not ready" },
  statefulsets: { label: "StatefulSets", path: "workloads/statefulsets", problem: "not ready" },
  daemonsets: { label: "DaemonSets", path: "workloads/daemonsets", problem: "not ready" },
  jobs: { label: "Jobs", path: "workloads/jobs", problem: "failed" },
  cronjobs: { label: "CronJobs", path: "workloads/cronjobs", problem: "" },
  gateways: { label: "Gateways", path: "network/gateways", problem: "not programmed" },
  httproutes: { label: "HTTPRoutes", path: "network/httproutes", problem: "not accepted" },
};

// The cluster's front page: counts, pod health, nodes and recent warnings.
export default function Overview({ page }) {
  const { cluster, reachable, status } = useOutletContext();
  const [searchParams] = useSearchParams();
  const namespace = searchParams.get("ns") ?? "";

  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  const base = reachable ? `/clusters/${cluster.id}` : null;
  const overview = useApi(base && `${base}/overview${query}`, { refreshMs: REFRESH_MS });
  const nodes = useApi(base && `${base}/nodes`, { refreshMs: REFRESH_MS });
  // Usage is cluster-wide (it comes from the nodes), whatever namespace is picked.
  const usage = useApi(base && `${base}/metrics/nodes`, { refreshMs: METRICS_REFRESH_MS });
  const warningsQuery = namespace ? `&namespace=${encodeURIComponent(namespace)}` : "";
  const warnings = useApi(base && `${base}/events?type=Warning${warningsQuery}`, { refreshMs: REFRESH_MS });

  const o = overview.data;
  const error = overview.error ?? nodes.error ?? warnings.error;

  return (
    <section>
      <div className="page-header">
        <div>
          <h1 className="page-title">{cluster.name}</h1>
          <p className="page-about">
            {status?.version ? `Kubernetes ${status.version} · ` : ""}
            {o ? `${o.namespaces} namespaces · ` : ""}
            <span className="mono">{cluster.server}</span>
            {namespace && (
              <>
                <br />
                Showing namespace <strong>{namespace}</strong>. Nodes and namespaces cover the whole cluster.
              </>
            )}
          </p>
        </div>
        {overview.updatedAt && <span className="muted">Updated {clock(overview.updatedAt)}</span>}
      </div>

      {error && <ErrorBox title={`Couldn't load the ${page.label.toLowerCase()}`} message={error.message} detail={error.detail} />}

      {o && (
        <>
          <div className="tiles">
            <Tile to="nodes" label="Nodes" value={o.nodes.total}>
              <Health bad={o.nodes.total - o.nodes.ready} problem="not ready" okText="All ready" />
            </Tile>
            <Tile to="workloads/pods" label="Pods" value={o.pods.total}>
              <Health bad={unhealthyPods(o.pods.byStatus)} problem="unhealthy" okText="All healthy" />
            </Tile>
            {/* gatewayAPI is null when the cluster doesn't have Gateway API, so no tiles appear. */}
            {[...o.workloads, ...(o.gatewayAPI ?? [])].map((w) => {
              const info = WORKLOADS[w.resource];
              return (
                <Tile key={w.resource} to={info.path} label={info.label} value={w.total}>
                  {info.problem ? (
                    <Health bad={w.unhealthy} problem={info.problem} okText={w.total ? "All good" : "None"} />
                  ) : (
                    <span className="muted">{w.total ? "Scheduled" : "None"}</span>
                  )}
                </Tile>
              );
            })}
          </div>

          <div className="card usage-card">
            <h2 className="card-title">Cluster usage</h2>
            <ClusterUsage usage={usage} />
          </div>

          <div className="cards">
            <div className="card">
              <h2 className="card-title">Pods by status</h2>
              <PodStatusBar byStatus={o.pods.byStatus} total={o.pods.total} />
            </div>

            <div className="card">
              <h2 className="card-title">Nodes</h2>
              <NodeList nodes={nodes.data} clusterId={cluster.id} />
            </div>
          </div>
        </>
      )}

      {warnings.data && (
        <div className="card">
          <h2 className="card-title">
            Recent warnings
            {warnings.data.length > WARNINGS_SHOWN && (
              <span className="muted"> · newest {WARNINGS_SHOWN} of {warnings.data.length}</span>
            )}
          </h2>
          <EventsTable
            clusterId={cluster.id}
            events={warnings.data.slice(0, WARNINGS_SHOWN)}
            showNamespace={!namespace}
            emptyText="No warnings. Nothing has complained recently."
          />
        </div>
      )}
    </section>
  );
}

// Pods whose status is not plainly fine (running or completed).
function unhealthyPods(byStatus) {
  return Object.entries(byStatus)
    .filter(([status]) => !["Running", "Completed", "Succeeded"].includes(status))
    .reduce((sum, [, count]) => sum + count, 0);
}

// A count with a label; links to its page when `to` is given.
function Tile({ to, label, value, children }) {
  const { clusterId } = useParams();
  const search = nsQuery(useLocation().search); // keep ?ns=... when following the link
  const body = (
    <>
      <div className="tile-label">{label}</div>
      <div className="tile-value">{value}</div>
      <div className="tile-note">{children}</div>
    </>
  );
  return to ? (
    <Link className="tile tile-link" to={{ pathname: `/c/${clusterId}/${to}`, search }}>
      {body}
    </Link>
  ) : (
    <div className="tile">{body}</div>
  );
}

// "2 not ready" in the warning colour, or a calm "All ready".
function Health({ bad, problem, okText }) {
  if (bad > 0) {
    return (
      <span className="status status-warn">
        {bad} {problem}
      </span>
    );
  }
  return <span className="muted">{okText}</span>;
}

function NodeList({ nodes, clusterId }) {
  if (!nodes) return <p className="muted">Loading…</p>;
  return (
    <ul className="node-list">
      {nodes.map((n) => (
        <li key={n.name}>
          <Link className="node-name" title={n.name} to={detailPath(clusterId, "nodes", "", n.name)}>
            {n.name}
          </Link>
          <span className="muted">{n.roles.join(", ") || "–"}</span>
          <span className="mono muted">{n.version}</span>
          <StatusBadge status={n.status} />
        </li>
      ))}
    </ul>
  );
}

// CPU and memory for the whole cluster: all nodes added up.
function ClusterUsage({ usage }) {
  if (usage.error?.code === "metrics_unavailable") return <MetricsUnavailable compact />;
  if (usage.error) return <p className="muted">Usage isn't available right now: {usage.error.message}</p>;
  if (!usage.data) return <p className="muted">Loading…</p>;

  const { total, history } = usage.data;
  const rows = [
    { what: "CPU", field: "cpu", used: total.cpu, of: total.cpuAllocatable, format: cpu },
    { what: "Memory", field: "memory", used: total.memory, of: total.memoryAllocatable, format: bytes },
  ];
  return (
    <div className="usage-rows">
      {rows.map((r) => (
        <div key={r.field} className="usage-row">
          <span className="usage-row-label">{r.what}</span>
          <UsageBar used={r.used} total={r.of} label={`${r.format(r.used)} of ${r.format(r.of)} used`} />
          <span className="muted">
            {r.format(r.used)} of {r.format(r.of)}
          </span>
          <Sparkline points={history} field={r.field} format={r.format} what={r.what} width={220} height={28} />
        </div>
      ))}
      <p className="muted small">
        Of what nodes can give to pods (allocatable). The line shows the last 15 minutes.
      </p>
    </div>
  );
}
