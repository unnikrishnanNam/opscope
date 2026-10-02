// Table columns for each resource type, keyed by the name used in the API
// URL (/api/clusters/{id}/pods). See ResourceTable.jsx for what each field means.
import StatusBadge, { Fraction } from "./components/StatusBadge.jsx";
import { age, duration } from "./format.js";

// Columns that most tables share.
const name = { key: "name", label: "Name", className: "name" };
const namespace = { key: "namespace", label: "Namespace", className: "muted-cell" };
const created = {
  key: "created",
  label: "Age",
  className: "num",
  render: (row) => age(row.created),
  // Newer first when sorted ascending, so "Age ↑" reads as "youngest first".
  sortValue: (row) => -Date.parse(row.created),
};
const status = { key: "status", label: "Status", render: (row) => <StatusBadge status={row.status} /> };

export const columns = {
  pods: [
    name,
    namespace,
    status,
    {
      key: "ready",
      label: "Ready",
      className: "num",
      render: (row) => <Fraction have={row.ready} want={row.containers} />,
    },
    { key: "restarts", label: "Restarts", className: "num" },
    { key: "node", label: "Node", className: "mono" },
    created,
  ],

  deployments: [
    name,
    namespace,
    {
      key: "ready",
      label: "Ready",
      className: "num",
      render: (row) => <Fraction have={row.ready} want={row.desired} />,
    },
    { key: "upToDate", label: "Up to date", className: "num" },
    { key: "available", label: "Available", className: "num" },
    created,
  ],

  statefulsets: [
    name,
    namespace,
    {
      key: "ready",
      label: "Ready",
      className: "num",
      render: (row) => <Fraction have={row.ready} want={row.desired} />,
    },
    created,
  ],

  daemonsets: [
    name,
    namespace,
    { key: "desired", label: "Desired", className: "num" },
    { key: "current", label: "Current", className: "num" },
    {
      key: "ready",
      label: "Ready",
      className: "num",
      render: (row) => <Fraction have={row.ready} want={row.desired} />,
    },
    {
      key: "nodeSelector",
      label: "Node selector",
      render: (row) => <Labels labels={row.nodeSelector} />,
      sortValue: (row) => Object.keys(row.nodeSelector ?? {}).join(","),
    },
    created,
  ],

  jobs: [
    name,
    namespace,
    status,
    {
      key: "succeeded",
      label: "Completions",
      className: "num",
      // Without a fixed number of completions, just show how many succeeded.
      render: (row) => (row.completions == null ? row.succeeded : <Fraction have={row.succeeded} want={row.completions} />),
    },
    {
      key: "started",
      label: "Duration",
      className: "num",
      render: (row) => duration(row.started, row.finished),
      sortValue: (row) => (row.started ? Date.parse(row.finished ?? new Date()) - Date.parse(row.started) : null),
    },
    created,
  ],

  cronjobs: [
    name,
    namespace,
    {
      key: "schedule",
      label: "Schedule",
      className: "mono",
      render: (row) => (row.timeZone ? `${row.schedule} (${row.timeZone})` : row.schedule),
    },
    {
      key: "suspended",
      label: "Suspended",
      render: (row) => (row.suspended ? <StatusBadge status="Suspended" /> : <span className="muted">No</span>),
    },
    { key: "active", label: "Active", className: "num" },
    {
      key: "lastSchedule",
      label: "Last run",
      className: "num",
      render: (row) => (row.lastSchedule ? `${age(row.lastSchedule)} ago` : <span className="muted">Never</span>),
      sortValue: (row) => (row.lastSchedule ? -Date.parse(row.lastSchedule) : null),
    },
    created,
  ],
};

// key=value pairs as small tags.
function Labels({ labels }) {
  const entries = Object.entries(labels ?? {});
  if (entries.length === 0) return <span className="muted">–</span>;
  return entries.map(([k, v]) => (
    <span key={k} className="tag mono">
      {k}={v}
    </span>
  ));
}
