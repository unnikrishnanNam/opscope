import { Link, useLocation, useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { bytes, clock, cpu, percent } from "../format.js";
import Button from "../components/Button.jsx";
import { Callout } from "../components/Callout.jsx";
import { Card } from "../components/Card.jsx";
import EventList from "../components/EventList.jsx";
import { Skeleton } from "../components/Loading.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import PodStatusBar from "../components/PodStatusBar.jsx";
import StatusBadge, { statusTone } from "../components/StatusBadge.jsx";
import Tooltip from "../components/Tooltip.jsx";
import { Sparkline, UsageBar } from "../components/Usage.jsx";
import { CpuIcon, MemoryIcon, NodesIcon, PodsIcon, SuccessIcon, WarningIcon } from "../components/icons.jsx";
import { allPages, detailPath, nsQuery } from "../sections.js";
import "./Overview.css";

const REFRESH_MS = 10_000;
const WARNINGS_SHOWN = 8;
const METRICS_REFRESH_MS = 15_000; // metrics-server's own refresh interval

// How each kind's "needs attention" count reads, and what to say when it's
// zero. Labels, links and icons come from sections.js.
const KINDS = {
  deployments: { problem: "not ready", ok: "All ready" },
  statefulsets: { problem: "not ready", ok: "All ready" },
  daemonsets: { problem: "not ready", ok: "All ready" },
  jobs: { problem: "failed", ok: "None failed", tone: "bad" },
  cronjobs: { problem: null, ok: "Scheduled" }, // nothing to be unhealthy about
  gateways: { problem: "not programmed", ok: "All programmed" },
  httproutes: { problem: "not accepted", ok: "All accepted" },
};

const pageFor = (resource) => allPages.find((p) => p.resource === resource);
// "Deployments" -> "Deployment", for "1 Deployment not ready".
const singular = (label) => label.replace(/s$/, "");

// The cluster's front page: a health summary and the four key numbers, then
// every resource kind with how many need attention, pod health, nodes and
// recent warnings.
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
        after={o && <HealthChip issues={issuesOf(o)} />}
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

      {o ? (
        <SummaryStrip o={o} nodes={nodes.data} usage={usage} to={to} />
      ) : (
        !error && reachable && <Skeleton height={148} radius="var(--radius-lg)" />
      )}

      {o && (
        <div className="overview-grid">
          <div className="overview-main">
            <Card title="Resources" aside={namespace ? `in ${namespace}` : "all namespaces"}>
              <ResourceGroup title="Workloads" items={o.workloads} to={to} />
              {/* gatewayAPI is null when the cluster doesn't have Gateway API. */}
              {o.gatewayAPI && <ResourceGroup title="Gateway API" items={o.gatewayAPI} to={to} />}
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

// Everything that needs a look, as short phrases: ["3 Pods unhealthy", "2 Deployments not ready"].
function issuesOf(o) {
  const issues = [];
  const add = (count, label, problem) => {
    if (count > 0) issues.push({ count, text: `${count} ${count === 1 ? singular(label) : label} ${problem}` });
  };
  add(o.nodes.total - o.nodes.ready, "Nodes", "not ready");
  add(unhealthyPods(o.pods.byStatus), "Pods", "unhealthy");
  for (const w of [...o.workloads, ...(o.gatewayAPI ?? [])]) {
    const problem = KINDS[w.resource]?.problem;
    if (problem) add(w.unhealthy, pageFor(w.resource).label, problem);
  }
  return issues;
}

// Next to the cluster's name: "All healthy", or how many things need a look
// (the list is in its hover text, and read out by screen readers).
function HealthChip({ issues }) {
  const total = issues.reduce((sum, i) => sum + i.count, 0);
  if (total === 0) {
    return (
      <span className="health-chip health-chip-ok">
        <SuccessIcon size={14} />
        All healthy
      </span>
    );
  }
  const list = issues.map((i) => i.text).join(" · ");
  return (
    <Tooltip label={list}>
      <span className="health-chip health-chip-warn" tabIndex={0} aria-label={`${total} need attention: ${list}`}>
        <WarningIcon size={14} />
        {total} need attention
      </span>
    </Tooltip>
  );
}

// One card, four cells: nodes, pods, CPU and memory. On narrow screens it
// becomes two by two.
function SummaryStrip({ o, nodes, usage, to }) {
  const badPods = unhealthyPods(o.pods.byStatus);
  const notReady = o.nodes.total - o.nodes.ready;
  return (
    <div className="summary-strip">
      <Link className="summary-cell summary-cell-link" to={to("nodes")}>
        <span className="summary-label">
          <NodesIcon size={14} /> Nodes
        </span>
        <span className="summary-value">
          {o.nodes.ready}
          <span className="summary-of">/ {o.nodes.total} ready</span>
        </span>
        <span className="summary-note">
          {notReady > 0 ? <span className="status status-warn">{notReady} not ready</span> : "All ready"}
        </span>
        {nodes && (
          // One square per node, coloured by its status.
          <span className="node-squares" aria-hidden="true">
            {nodes.map((n) => (
              <span
                key={n.name}
                className={`node-square tone-${statusTone(n.status)}`}
                title={`${n.name}: ${n.status}`}
              />
            ))}
          </span>
        )}
      </Link>

      <Link className="summary-cell summary-cell-link" to={to("workloads/pods")}>
        <span className="summary-label">
          <PodsIcon size={14} /> Pods
        </span>
        <span className="summary-value">{o.pods.total}</span>
        <span className="summary-note">
          {badPods > 0 ? (
            <span className="status status-warn">{badPods} unhealthy</span>
          ) : o.pods.total ? (
            "All healthy"
          ) : (
            "None"
          )}
        </span>
        <MiniStack byStatus={o.pods.byStatus} />
      </Link>

      <UsageCell usage={usage} field="cpu" what="CPU" format={cpu} />
      <UsageCell usage={usage} field="memory" what="Memory" format={bytes} />
    </div>
  );
}

// A thin bar of pods by status (the card below has the legend).
function MiniStack({ byStatus }) {
  const order = ["ok", "warn", "bad", "neutral"];
  const parts = Object.entries(byStatus)
    .map(([s, count]) => ({ count, tone: statusTone(s) }))
    .sort((a, b) => order.indexOf(a.tone) - order.indexOf(b.tone));
  if (parts.length === 0) return null;
  return (
    <span className="mini-stack" aria-hidden="true">
      {parts.map((p, i) => (
        <span key={i} className={`tone-${p.tone}`} style={{ flexGrow: p.count }} />
      ))}
    </span>
  );
}

// Cluster-wide CPU or memory: percentage, bar, amount and the last 15 minutes.
function UsageCell({ usage, field, what, format }) {
  const Icon = field === "cpu" ? CpuIcon : MemoryIcon;
  const head = (
    <span className="summary-label">
      <Icon size={14} /> {what}
    </span>
  );
  if (usage.error) {
    return (
      <div className="summary-cell">
        {head}
        <span className="summary-value summary-value-empty">–</span>
        <span className="summary-note">
          {usage.error.code === "metrics_unavailable" ? "Needs metrics-server" : "Not available right now"}
        </span>
      </div>
    );
  }
  if (!usage.data) {
    return (
      <div className="summary-cell">
        {head}
        <Skeleton width={64} height={28} />
        <Skeleton width="70%" />
      </div>
    );
  }
  const { total, history } = usage.data;
  const used = total[field];
  const of = total[`${field}Allocatable`];
  return (
    <div className="summary-cell">
      {head}
      <span className="summary-value">
        {percent(used, of)}
        <span className="summary-unit">%</span>
      </span>
      <span className="summary-note">
        {format(used)} of {format(of)}
      </span>
      <UsageBar
        used={used}
        total={of}
        width="100%"
        showPercent={false}
        label={`${format(used)} of ${format(of)} used`}
      />
      <Sparkline points={history} field={field} format={format} what={what} width={240} height={28} fluid />
    </div>
  );
}

// One group of resource kinds ("Workloads", "Gateway API"), a row each:
// name, count, a bar of healthy against not, and what needs attention.
function ResourceGroup({ title, items, to }) {
  return (
    <div className="resource-group">
      <div className="resource-group-title">{title}</div>
      <ul className="resource-rows">
        {items.map((w) => {
          const p = pageFor(w.resource);
          const kind = KINDS[w.resource];
          const bad = kind.problem ? w.unhealthy : 0;
          const tone = kind.tone ?? "warn";
          return (
            <li key={w.resource}>
              <Link className="resource-row" to={to(p.path)}>
                <span className="resource-name">
                  <p.icon size={16} />
                  {p.label}
                </span>
                <span className="resource-count">{w.total}</span>
                <span className="resource-bar" aria-hidden="true">
                  {w.total > 0 && (
                    <>
                      <span className={kind.problem ? "tone-ok" : "tone-neutral"} style={{ flexGrow: w.total - bad }} />
                      {bad > 0 && <span className={`tone-${tone}`} style={{ flexGrow: bad }} />}
                    </>
                  )}
                </span>
                <span className="resource-state">
                  {bad > 0 ? (
                    <span className={`status status-${tone}`}>
                      {bad} {kind.problem}
                    </span>
                  ) : w.total > 0 ? (
                    kind.ok
                  ) : (
                    "None"
                  )}
                </span>
              </Link>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

// Pods whose status is not plainly fine (running or completed).
function unhealthyPods(byStatus) {
  return Object.entries(byStatus)
    .filter(([status]) => !["Running", "Completed", "Succeeded"].includes(status))
    .reduce((sum, [, count]) => sum + count, 0);
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
