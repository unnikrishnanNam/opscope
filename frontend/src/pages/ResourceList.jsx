import { useOutletContext, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { columns as allColumns } from "../columns.jsx";
import { clock } from "../format.js";
import ResourceTable from "../components/ResourceTable.jsx";
import ErrorBox from "../components/ErrorBox.jsx";
import SecretKeys from "../components/SecretKeys.jsx";

// Resource types whose rows can be opened to show more. Each entry gets the
// row and the cluster and returns what to show under the row.
const expanders = {
  secrets: (row, cluster) => <SecretKeys cluster={cluster} secret={row} />,
};

const REFRESH_MS = 10_000;

// A page that lists one resource type for the selected cluster and namespace.
// `page` comes from sections.js; page.resource is the API name, e.g. "pods".
export default function ResourceList({ page }) {
  // Layout passes the selected cluster down through the router's <Outlet>.
  const { cluster, reachable } = useOutletContext();
  const [searchParams] = useSearchParams();
  // Cluster-scoped resources (like nodes) ignore the namespace picker.
  const namespace = page.clusterScoped ? "" : (searchParams.get("ns") ?? "");

  // Don't ask an unreachable cluster; Layout already shows why.
  const query = namespace ? `?namespace=${encodeURIComponent(namespace)}` : "";
  const path = reachable ? `/clusters/${cluster.id}/${page.resource}${query}` : null;
  const { data, error, loading, updatedAt, reload } = useApi(path, { refreshMs: REFRESH_MS });

  // With one namespace selected, a namespace column would say the same thing on every row.
  const columns = allColumns[page.resource].filter((col) => !(namespace && col.key === "namespace"));
  const noun = page.label.toLowerCase();

  return (
    <section>
      <h1 className="page-title">{page.label}</h1>
      <p className="page-about">{page.about}</p>

      {error?.code === "not_installed" ? (
        // A missing optional feature, not a failure: say so calmly instead of showing an error.
        <div className="empty">
          {error.message}. Install the Gateway API CRDs and a controller to use {page.label}.
        </div>
      ) : (
        error && <ErrorBox title={`Couldn't load ${noun}`} message={error.message} detail={error.detail} />
      )}

      {reachable && error?.code !== "not_installed" && (
        <ResourceTable
          columns={columns}
          rows={data}
          noun={noun}
          expand={expanders[page.resource] && ((row) => expanders[page.resource](row, cluster))}
          emptyText={namespace ? `No ${noun} in ${namespace}.` : `No ${noun} in this cluster.`}
          toolbar={
            <>
              {updatedAt && <span className="muted">Updated {clock(updatedAt)}</span>}
              <button type="button" className="button button-quiet" onClick={reload} disabled={loading}>
                {loading ? "Refreshing…" : "Refresh"}
              </button>
            </>
          }
        />
      )}
    </section>
  );
}
