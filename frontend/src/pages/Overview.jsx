import { Link, useLocation, useOutletContext, useParams, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { age, clock } from "../format.js";
import ErrorBox from "../components/ErrorBox.jsx";
import PodStatusBar from "../components/PodStatusBar.jsx";
import StatusBadge from "../components/StatusBadge.jsx";

const REFRESH_MS = 10_000;
const WARNINGS_SHOWN = 8;

// What each workload count links to and how its "unhealthy" number reads.
const WORKLOADS = {
  deployments: { label: "Deployments", path: "workloads/deployments", problem: "not ready" },
  statefulsets: { label: "StatefulSets", path: "workloads/statefulsets", problem: "not ready" },
  daemonsets: { label: "DaemonSets", path: "workloads/daemonsets", problem: "not ready" },
  jobs: { label: "Jobs", path: "workloads/jobs", problem: "failed" },
  cronjobs: { label: "CronJobs", path: "workloads/cronjobs", problem: "" },
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
            {o.workloads.map((w) => {
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
            <Tile label="Namespaces" value={o.namespaces}>
              <span className="muted">Cluster-wide</span>
            </Tile>
          </div>

          <div className="cards">
            <div className="card">
              <h2 className="card-title">Pods by status</h2>
              <PodStatusBar byStatus={o.pods.byStatus} total={o.pods.total} />
            </div>

            <div className="card">
              <h2 className="card-title">Nodes</h2>
              <NodeList nodes={nodes.data} />
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
          <WarningList events={warnings.data.slice(0, WARNINGS_SHOWN)} showNamespace={!namespace} />
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
  const { search } = useLocation(); // keep ?ns=... when following the link
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

function NodeList({ nodes }) {
  if (!nodes) return <p className="muted">Loading…</p>;
  return (
    <ul className="node-list">
      {nodes.map((n) => (
        <li key={n.name}>
          <span className="node-name">{n.name}</span>
          <span className="muted">{n.roles.join(", ") || "–"}</span>
          <span className="mono muted">{n.version}</span>
          <StatusBadge status={n.status} />
        </li>
      ))}
    </ul>
  );
}

function WarningList({ events, showNamespace }) {
  if (events.length === 0) return <p className="muted">No warnings. Nothing has complained recently.</p>;
  return (
    <table className="table events-table">
      <thead>
        <tr>
          <th className="num">Last seen</th>
          <th>Object</th>
          <th>Reason</th>
          <th>Message</th>
          <th className="num">Count</th>
        </tr>
      </thead>
      <tbody>
        {events.map((e) => (
          <tr key={`${e.namespace}/${e.name}`}>
            <td className="num">{age(e.lastSeen)} ago</td>
            <td className="mono">
              {showNamespace && <span className="muted">{e.namespace}/</span>}
              {e.object}
            </td>
            <td>
              <span className="status status-warn">{e.reason}</span>
            </td>
            <td className="message" title={e.message}>
              <div className="clamp-2">{e.message}</div>
            </td>
            <td className="num">×{e.count}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
