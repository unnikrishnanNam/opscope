import { Link } from "react-router";
import Combobox from "./Combobox.jsx";
import { ClustersIcon } from "./icons.jsx";
import "./ClusterSwitcher.css";

// ClusterSwitcher: the cluster picker in the top bar. Each cluster shows
// whether it answers and its Kubernetes version; "Manage clusters" sits
// at the bottom.
//   clusters  [{ id, name, status }], where status is the answer from
//             GET /api/clusters/{id} ({ reachable, version }), or undefined
//             while it's still being checked
//   value     the current cluster's id (undefined: none chosen yet)
//   onChange  receives the picked cluster's id
export default function ClusterSwitcher({ clusters, value, onChange, manageTo = "/clusters" }) {
  return (
    <Combobox
      label="Cluster"
      size="sm"
      value={value}
      options={clusters.map((c) => ({ value: c.id, label: c.name, status: c.status }))}
      onChange={onChange}
      noun="clusters"
      // The button also says whether the current cluster answers, and its version.
      renderValue={(o) => (
        <>
          <StatusDot status={o.status} />
          {o.label}
          <span className={`cluster-value-meta ${o.status && !o.status.reachable ? "is-bad" : ""}`}>
            {statusText(o.status)}
          </span>
        </>
      )}
      renderOption={(o) => (
        <>
          <StatusDot status={o.status} />
          <span className="cluster-option-name">{o.label}</span>
          <span className="cluster-option-meta">{statusText(o.status)}</span>
        </>
      )}
      footer={
        <Link to={manageTo} className="cluster-manage">
          <ClustersIcon size={16} />
          Manage clusters
        </Link>
      }
    />
  );
}

function StatusDot({ status }) {
  const tone = !status ? "checking" : status.reachable ? "ok" : "bad";
  return <span className={`cluster-dot cluster-dot-${tone}`} aria-hidden="true" />;
}

function statusText(status) {
  if (!status) return "checking…";
  return status.reachable ? status.version : "unreachable";
}
