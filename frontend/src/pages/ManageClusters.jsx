import { useState } from "react";
import { Link } from "react-router";
import { api } from "../api.js";
import { useClusters } from "../clusters.jsx";
import ErrorBox from "../components/ErrorBox.jsx";

export default function ManageClusters() {
  const { data: clusters, error, reload } = useClusters();
  const [removeError, setRemoveError] = useState(null);

  async function remove(cluster) {
    if (!window.confirm(`Remove “${cluster.name}”? Opscope will delete its saved kubeconfig.`)) return;
    try {
      await api(`/clusters/${cluster.id}`, { method: "DELETE" });
      setRemoveError(null);
      reload();
    } catch (err) {
      setRemoveError(err);
    }
  }

  return (
    <section>
      <div className="page-header">
        <div>
          <h1 className="page-title">Clusters</h1>
          <p className="page-about">Clusters Opscope can read from.</p>
        </div>
        <Link to="/clusters/add" className="button button-primary">
          Add a cluster
        </Link>
      </div>

      {error && <ErrorBox title="Couldn't load clusters" message={error.message} />}
      {removeError && <ErrorBox title="Couldn't remove the cluster" message={removeError.message} />}

      {clusters?.length === 0 && (
        <div className="empty">
          No clusters yet. <Link to="/clusters/add">Add one</Link> or start Opscope with <code>OPSCOPE_KUBECONFIG</code>.
        </div>
      )}

      {clusters?.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Name</th>
              <th>Source</th>
              <th>Context</th>
              <th>API server</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {clusters.map((cluster) => (
              <tr key={cluster.id}>
                <td>
                  <Link to={`/c/${cluster.id}/overview`}>{cluster.name}</Link>
                </td>
                <td>
                  {cluster.source === "env" ? (
                    <span className="tag" title="Set with OPSCOPE_KUBECONFIG">
                      Environment
                    </span>
                  ) : (
                    <span className="tag">Added in UI</span>
                  )}
                </td>
                <td className="mono">{cluster.context}</td>
                <td className="mono">{cluster.server}</td>
                <td className="actions">
                  {cluster.source === "ui" && (
                    <button className="button button-quiet" onClick={() => remove(cluster)}>
                      Remove
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
