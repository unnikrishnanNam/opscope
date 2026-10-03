// Table columns for each resource type, keyed by the name used in the API
// URL (/api/clusters/{id}/pods). See ResourceTable.jsx for what each field means.
import StatusBadge, { Fraction } from "./components/StatusBadge.jsx";
import { Tag } from "./components/Tag.jsx";
import { age, bytes, cores, cpu, duration } from "./format.js";
import { Sparkline, UsageBar } from "./components/Usage.jsx";

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

// Gateway API objects carry a `message` explaining a bad status; show it on hover.
const gatewayStatus = {
  key: "status",
  label: "Status",
  render: (row) => <StatusBadge status={row.status} title={row.message} />,
};

export const columns = {
  configmaps: [
    name,
    namespace,
    {
      key: "keys",
      label: "Keys",
      className: "num",
      render: (row) => <span title={row.keys.join("\n")}>{row.keys.length}</span>,
      sortValue: (row) => row.keys.length,
    },
    created,
  ],

  secrets: [
    name,
    namespace,
    { key: "type", label: "Type", className: "mono" },
    {
      key: "keys",
      label: "Keys",
      className: "num",
      render: (row) => row.keys.length,
      sortValue: (row) => row.keys.length,
    },
    created,
  ],

  services: [
    name,
    namespace,
    { key: "type", label: "Type" },
    { key: "clusterIP", label: "Cluster IP", className: "mono" },
    {
      key: "externalIPs",
      label: "External IP",
      className: "mono",
      render: (row) => <List items={row.externalIPs} />,
      sortValue: (row) => row.externalIPs.join(","),
    },
    {
      key: "ports",
      label: "Ports",
      className: "mono",
      render: (row) => <List items={row.ports} />,
      sortValue: (row) => row.ports.join(","),
    },
    created,
  ],

  gateways: [
    name,
    namespace,
    gatewayStatus,
    { key: "class", label: "Class" },
    {
      key: "listeners",
      label: "Listeners",
      className: "mono",
      // "80/HTTP, 443/HTTPS app.local", one per port the gateway accepts traffic on.
      render: (row) => <List items={row.listeners.map((l) => `${l.port}/${l.protocol}${l.hostname ? ` ${l.hostname}` : ""}`)} />,
      sortValue: (row) => row.listeners.length,
    },
    {
      key: "addresses",
      label: "Address",
      className: "mono",
      render: (row) => <List items={row.addresses} />,
      sortValue: (row) => row.addresses.join(","),
    },
    { key: "attachedRoutes", label: "Routes", className: "num" },
    created,
  ],

  httproutes: [
    name,
    namespace,
    gatewayStatus,
    {
      key: "hostnames",
      label: "Hostnames",
      className: "mono",
      render: (row) => <List items={row.hostnames} />,
      sortValue: (row) => row.hostnames.join(","),
    },
    {
      key: "parents",
      label: "Gateways",
      render: (row) => <List items={row.parents} />,
      sortValue: (row) => row.parents.join(","),
    },
    {
      key: "backends",
      label: "Backends",
      className: "mono",
      render: (row) => <List items={row.backends} />,
      sortValue: (row) => row.backends.join(","),
    },
    { key: "rules", label: "Rules", className: "num" },
    created,
  ],

  gatewayclasses: [
    name,
    gatewayStatus,
    { key: "controller", label: "Controller", className: "mono" },
    { key: "description", label: "Description", render: (row) => row.description || <span className="muted">–</span> },
    created,
  ],

  ingresses: [
    name,
    namespace,
    { key: "class", label: "Class", render: (row) => row.class || <span className="muted">–</span> },
    {
      key: "hosts",
      label: "Hosts",
      className: "mono",
      render: (row) => <List items={row.hosts} />,
      sortValue: (row) => row.hosts.join(","),
    },
    {
      key: "addresses",
      label: "Address",
      className: "mono",
      render: (row) => <List items={row.addresses} />,
      sortValue: (row) => row.addresses.join(","),
    },
    { key: "tls", label: "TLS", render: (row) => (row.tls ? "Yes" : <span className="muted">No</span>) },
    created,
  ],

  nodes: [
    name,
    {
      key: "status",
      label: "Status",
      render: (row) => (
        <>
          <StatusBadge status={row.status} />
          {!row.schedulable && (
            <Tag className="tag-gap" title="New pods won't be scheduled on this node">
              Cordoned
            </Tag>
          )}
        </>
      ),
    },
    {
      key: "roles",
      label: "Roles",
      render: (row) => (row.roles.length ? row.roles.join(", ") : <span className="muted">–</span>),
      sortValue: (row) => row.roles.join(","),
    },
    { key: "version", label: "Version", className: "mono" },
    { key: "internalIP", label: "Internal IP", className: "mono" },
    {
      key: "os",
      label: "OS",
      render: (row) => <span title={row.osImage}>{`${row.os}/${row.arch}`}</span>,
    },
    // With metrics-server: usage against allocatable, plus the last 15 minutes.
    // Without: just the node's capacity.
    {
      key: "cpu",
      label: "CPU",
      render: (row) =>
        row.usage ? (
          <NodeUsage usage={row.usage} field="cpu" total="cpuAllocatable" format={cpu} what="CPU" />
        ) : (
          cores(row.cpu)
        ),
      sortValue: (row) => (row.usage ? row.usage.cpu / row.usage.cpuAllocatable : row.cpu),
    },
    {
      key: "memory",
      label: "Memory",
      render: (row) =>
        row.usage ? (
          <NodeUsage usage={row.usage} field="memory" total="memoryAllocatable" format={bytes} what="Memory" />
        ) : (
          bytes(row.memory)
        ),
      sortValue: (row) => (row.usage ? row.usage.memory / row.usage.memoryAllocatable : row.memory),
    },
    created,
  ],

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
    {
      key: "cpuUsage",
      label: "CPU",
      className: "num",
      render: (row) => (row.usage ? cpu(row.usage.cpu) : <span className="muted">–</span>),
      sortValue: (row) => row.usage?.cpu,
    },
    {
      key: "memoryUsage",
      label: "Memory",
      className: "num",
      render: (row) => (row.usage ? bytes(row.usage.memory) : <span className="muted">–</span>),
      sortValue: (row) => row.usage?.memory,
    },
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

// A node's usage of one resource: bar with percentage, then a sparkline.
function NodeUsage({ usage, field, total, format, what }) {
  return (
    <span className="usage-cell">
      <UsageBar used={usage[field]} total={usage[total]} label={`${format(usage[field])} of ${format(usage[total])} used`} />
      <Sparkline points={usage.history} field={field} format={format} what={what} />
    </span>
  );
}

// A list of short values (IPs, ports, hosts) on one line, or a dash.
function List({ items }) {
  if (!items?.length) return <span className="muted">–</span>;
  return items.join(", ");
}

// key=value pairs as small tags.
function Labels({ labels }) {
  const entries = Object.entries(labels ?? {});
  if (entries.length === 0) return <span className="muted">–</span>;
  return entries.map(([k, v]) => (
    <Tag key={k} mono>
      {k}={v}
    </Tag>
  ));
}
