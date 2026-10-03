import { useState } from "react";
import { Link } from "react-router";
import { api } from "../api.js";
import { useClusters } from "../clusters.jsx";
import Button from "../components/Button.jsx";
import { Callout, EmptyState } from "../components/Callout.jsx";
import { ConfirmDialog } from "../components/Dialog.jsx";
import { PageHeader } from "../components/PageHeader.jsx";
import { Tag } from "../components/Tag.jsx";
import { ClustersIcon, PlusIcon, TrashIcon } from "../components/icons.jsx";
import "./Clusters.css";

// Every cluster Opscope knows, whether it answers, and where it came from.
// Clusters added in the UI can be removed (after a confirmation); ones from
// the environment are set when Opscope starts.
export default function ManageClusters() {
  const { data: clusters, error, reload, statuses } = useClusters();
  const [removing, setRemoving] = useState(null); // the cluster being confirmed
  const [busy, setBusy] = useState(false);
  const [removeError, setRemoveError] = useState(null);

  function close() {
    setRemoving(null);
    setRemoveError(null);
  }

  async function remove() {
    setBusy(true);
    setRemoveError(null);
    try {
      await api(`/clusters/${encodeURIComponent(removing.id)}`, { method: "DELETE" });
      reload();
      close();
    } catch (err) {
      setRemoveError(err);
    }
    setBusy(false);
  }

  return (
    <section className="cluster-page">
      <PageHeader
        title="Clusters"
        description="The clusters Opscope can read from. Ones added here can be removed; ones from the environment are set when Opscope starts."
        actions={
          <Button to="/clusters/add" variant="primary" icon={PlusIcon}>
            Add a cluster
          </Button>
        }
      />

      {error && (
        <Callout tone="error" title="Couldn't load clusters">
          {error.message}
        </Callout>
      )}

      {clusters?.length === 0 && (
        <div className="cluster-list-empty">
          <EmptyState
            icon={ClustersIcon}
            title="No clusters yet"
            action={
              <Button to="/clusters/add" variant="primary" icon={PlusIcon}>
                Add a cluster
              </Button>
            }
          >
            Add one with a kubeconfig, or start Opscope with <code>OPSCOPE_KUBECONFIG</code>.
          </EmptyState>
        </div>
      )}

      {clusters?.length > 0 && (
        <ul className="cluster-list">
          {clusters.map((c) => (
            <ClusterRow key={c.id} cluster={c} status={statuses[c.id]} onRemove={() => setRemoving(c)} />
          ))}
        </ul>
      )}

      <ConfirmDialog
        open={Boolean(removing)}
        onCancel={close}
        onConfirm={remove}
        busy={busy}
        error={removeError}
        title={`Remove ${removing?.name}?`}
        confirmLabel="Remove cluster"
      >
        Opscope will delete its saved kubeconfig for this cluster. The cluster itself isn't touched, and you can add it
        again later.
      </ConfirmDialog>
    </section>
  );
}

function ClusterRow({ cluster, status, onRemove }) {
  const tone = !status ? "checking" : status.reachable ? "ok" : "bad";
  return (
    <li className="cluster-row">
      <span className={`cluster-dot cluster-dot-${tone}`} aria-hidden="true" />
      <div className="cluster-row-main">
        <div className="cluster-row-head">
          <Link to={`/c/${cluster.id}/overview`} className="cluster-row-name">
            {cluster.name}
          </Link>
          {cluster.source === "env" ? (
            <Tag title="Set with OPSCOPE_KUBECONFIG or OPSCOPE_IN_CLUSTER when Opscope starts">Environment</Tag>
          ) : (
            <Tag>Added here</Tag>
          )}
        </div>
        <div className="cluster-row-meta">
          <span className="mono">{cluster.context}</span>
          <span className="mono">{cluster.server}</span>
        </div>
      </div>
      <span className={`cluster-row-status cluster-row-status-${tone}`}>
        {!status ? "Checking…" : status.reachable ? `Kubernetes ${status.version}` : "Unreachable"}
      </span>
      <div className="cluster-row-actions">
        {cluster.source === "ui" && (
          <Button variant="quiet" size="sm" icon={TrashIcon} label={`Remove ${cluster.name}`} onClick={onRemove} />
        )}
      </div>
    </li>
  );
}
