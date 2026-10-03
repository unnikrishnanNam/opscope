import { Link, useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { bytes, clock, cpu, percent } from "../format.js";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import { Card, StatTile } from "../components/Card.jsx";
import EventList from "../components/EventList.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import PodStatusBar from "../components/PodStatusBar.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import { MetricsUnavailable, Sparkline, UsageBar } from "../components/Usage.jsx";
import { allPages, detailPath, nsQuery } from "../sections.js";
import "./Overview.css";

const REFRESH_MS = 10_000;
const WARNINGS_SHOWN = 8;
const METRICS_REFRESH_MS = 15_000; // metrics-server's own refresh interval

// What each count's "needs attention" number is called. Pages (label, link,
// icon) come from sections.js.
const PROBLEMS = {
  deployments: "not ready",
  statefulsets: "not ready",
  daemonsets: "not ready",
  jobs: "failed",
  cronjobs: "",
  gateways: "not programmed",
  httproutes: "not accepted",
};

// The cluster's front page: counts, usage, pod health, nodes and recent warnings.
export default function Overview({ page }) {
  const { cluster, reachable, status } = useOutletContext();
  const [searchParams] = useSearchParams();
  const namespace = searchParams.get("ns") ?? "";
  const search = nsQuery(useLocation().search);

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
  // Links to list pages keep the selected namespace.
  const to = (path) => `/c/${cluster.id}/${path}${search}`;

  return (
    <section className="overview">
      <PageHeader
        title={cluster.name}
        description={
          <>
            {status?.version && `Kubernetes ${status.version} · `}
            {o && `${o.namespaces} namespaces · `}
            <span className="mono">{cluster.server}</span>
          </>
        }
        meta={overview.updatedAt && `Updated ${clock(overview.updatedAt)}`}
      />

      {namespace && (
        <Callout compact>
          Showing namespace <strong>{namespace}</strong>. Nodes, usage and the namespace count cover the whole cluster.
        </Callout>
      )}

      {error && (
        <Callout tone="error" title={`Couldn't load the ${page.label.toLowerCase()}`} detail={error.detail}>
          {error.message}
        </Callout>
      )}

      {o ? <Tiles o={o} to={to} /> : !error && reachable && <TileSkeletons />}

      {o && (
        <div className="overview-grid">
          <div className="overview-main">
            <Card title="Cluster usage" aside="of what nodes can give to pods">
              <ClusterUsage usage={usage} />
            </Card>
            <Card
              title="Pods by status"
              aside={`${o.pods.total} pods`}
              action={
                <Button variant="quiet" size="sm" to={to("workloads/pods")}>
                  View pods
                </Button>
              }
            >
              <PodStatusBar byStatus={o.pods.byStatus} total={o.pods.total} />
            </Card>
          </div>
          <Card
            title="Nodes"
            aside={`${o.nodes.ready} of ${o.nodes.total} ready`}
            action={
              <Button variant="quiet" size="sm" to={to("nodes")}>
                View nodes
              </Button>
            }
          >
            <NodeList nodes={nodes.data} usage={usage.data} clusterId={cluster.id} />
          </Card>
        </div>
      )}

      {warnings.data && (
        <Card
          title="Recent warnings"
          aside={
            warnings.data.length > WARNINGS_SHOWN
              ? `newest ${WARNINGS_SHOWN} of ${warnings.data.length}`
              : warnings.data.length > 0 && `${warnings.data.length}`
          }
        >
          <EventList
            clusterId={cluster.id}
            events={warnings.data.slice(0, WARNINGS_SHOWN)}
            showNamespace={!namespace}
            emptyText="No warnings. Nothing has complained recently."
          />
        </Card>
      )}
    </section>
  );
}

// The counts: nodes and pods first, then workloads, then Gateway API
// (only when the cluster has it: gatewayAPI is null otherwise).
function Tiles({ o, to }) {
  const pageFor = (resource) => allPages.find((p) => p.resource === resource);
  const nodesPage = pageFor("nodes");
  const podsPage = pageFor("pods");

  return (
    <div className="stat-grid">
      <StatTile label="Nodes" icon={nodesPage.icon} value={o.nodes.total} to={to(nodesPage.path)}>
        <Health bad={o.nodes.total - o.nodes.ready} problem="not ready" okText="All ready" />
      </StatTile>
      <StatTile label="Pods" icon={podsPage.icon} value={o.pods.total} to={to(podsPage.path)}>
        <Health
          bad={unhealthyPods(o.pods.byStatus)}
          problem="unhealthy"
          okText={o.pods.total ? "All healthy" : "None"}
        />
      </StatTile>
      {[...o.workloads, ...(o.gatewayAPI ?? [])].map((w) => {
        const p = pageFor(w.resource);
        const problem = PROBLEMS[w.resource];
        return (
          <StatTile key={w.resource} label={p.label} icon={p.icon} value={w.total} to={to(p.path)}>
            {problem ? (
              <Health bad={w.unhealthy} problem={problem} okText={w.total ? "All good" : "None"} />
            ) : w.total ? (
              "Scheduled"
            ) : (
              "None"
            )}
          </StatTile>
        );
      })}
    </div>
  );
}

function TileSkeletons() {
  return (
    <div className="stat-grid" aria-hidden="true">
      {Array.from({ length: 7 }, (_, i) => (
        <div key={i} className="stat-tile">
          <Skeleton width="60%" />
          <Skeleton width={40} height={28} />
          <Skeleton width="50%" />
        </div>
      ))}
    </div>
  );
}

// Pods whose status is not plainly fine (running or completed).
function unhealthyPods(byStatus) {
  return Object.entries(byStatus)
    .filter(([status]) => !["Running", "Completed", "Succeeded"].includes(status))
    .reduce((sum, [, count]) => sum + count, 0);
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
  return okText;
}

// CPU and memory for the whole cluster: all nodes added up, with the last
// 15 minutes as a line.
function ClusterUsage({ usage }) {
  if (usage.error?.code === "metrics_unavailable") return <MetricsUnavailable compact />;
  if (usage.error)
    return (
      <Callout compact tone="warning">
        Usage isn't available right now: {usage.error.message}
      </Callout>
    );
  if (!usage.data) return <Skeleton height={96} />;

  const { total, history } = usage.data;
  const rows = [
    { what: "CPU", field: "cpu", used: total.cpu, of: total.cpuAllocatable, format: cpu },
    { what: "Memory", field: "memory", used: total.memory, of: total.memoryAllocatable, format: bytes },
  ];
  return (
    <div className="cluster-usage">
      {rows.map((r) => (
        <div key={r.field} className="cluster-usage-item">
          <div className="cluster-usage-head">
            <span className="cluster-usage-label">{r.what}</span>
            <span className="cluster-usage-pct">{percent(r.used, r.of)}%</span>
          </div>
          <UsageBar
            used={r.used}
            total={r.of}
            width="100%"
            showPercent={false}
            label={`${r.format(r.used)} of ${r.format(r.of)} used`}
          />
          <div className="cluster-usage-amount">
            {r.format(r.used)} of {r.format(r.of)}
          </div>
          <Sparkline points={history} field={r.field} format={r.format} what={r.what} width={300} height={36} fluid />
        </div>
      ))}
    </div>
  );
}

// Each node: name, roles and status, and its usage when metrics-server is there.
function NodeList({ nodes, usage, clusterId }) {
  if (!nodes) return <Skeleton height={80} />;
  const byName = Object.fromEntries((usage?.nodes ?? []).map((n) => [n.name, n]));
  return (
    <ul className="node-rows">
      {nodes.map((n) => {
        const u = byName[n.name];
        return (
          <li key={n.name} className="node-row">
            <div className="node-row-head">
              <Link className="node-row-name" title={n.name} to={detailPath(clusterId, "nodes", "", n.name)}>
                {n.name}
              </Link>
              <StatusBadge status={n.status} />
            </div>
            <div className="node-row-meta">
              {n.roles.join(", ") || "no role"} · <span className="mono">{n.version}</span>
            </div>
            {u && (
              <div className="node-row-usage">
                <span>CPU</span>
                <UsageBar
                  used={u.cpu}
                  total={u.cpuAllocatable}
                  width="100%"
                  label={`CPU: ${cpu(u.cpu)} of ${cpu(u.cpuAllocatable)}`}
                />
                <span>Memory</span>
                <UsageBar
                  used={u.memory}
                  total={u.memoryAllocatable}
                  width="100%"
                  label={`Memory: ${bytes(u.memory)} of ${bytes(u.memoryAllocatable)}`}
                />
              </div>
            )}
          </li>
        );
      })}
    </ul>
  );
}
