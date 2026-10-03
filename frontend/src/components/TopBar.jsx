import { Link, useLocation, useNavigate, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useClusters } from "../clusters.jsx";
import { allPages, nsQuery } from "../sections.js";
import { Select } from "./Field.jsx";

// Crumbs for pages that don't belong to a cluster.
const otherPages = {
  "/clusters": ["Settings", "Clusters"],
  "/clusters/add": ["Clusters", "Add a cluster"],
};

export default function TopBar({ cluster, pagePath, status }) {
  const { pathname, search: fullSearch } = useLocation();
  const search = nsQuery(fullSearch);
  // The page this URL belongs to: "workloads/pods" itself, or a detail page
  // under it like "workloads/pods/web/api-1".
  const page = allPages.find((p) => pagePath === p.path || pagePath?.startsWith(p.path + "/"));
  const objectName = page && pagePath !== page.path ? decodeURIComponent(pagePath.split("/").pop()) : null;
  const [group, label] = page ? [page.group, page.label] : (otherPages[pathname] ?? [null, null]);

  return (
    <header className="topbar">
      <div className="crumbs">
        {group && (
          <>
            <span className="crumb-group">{group}</span>
            <span className="crumb-sep">/</span>
          </>
        )}
        {/* `cluster` is still undefined for a moment while the cluster list loads. */}
        {objectName && cluster ? (
          <>
            <Link className="crumb-link" to={{ pathname: `/c/${cluster.id}/${page.path}`, search }}>
              {label}
            </Link>
            <span className="crumb-sep">/</span>
            <span className="crumb-page">{objectName}</span>
          </>
        ) : (
          <span className="crumb-page">{label}</span>
        )}
      </div>

      <div className="topbar-right">
        {/* Switching cluster from a detail page goes to the list: the object is in the old cluster. */}
        <ClusterSwitcher cluster={cluster} pagePath={page?.path ?? pagePath} />
        {cluster && (
          <NamespacePicker cluster={cluster} reachable={status?.data?.reachable} clusterScoped={page?.clusterScoped} />
        )}
        {cluster && <ConnectionBadge status={status} />}
      </div>
    </header>
  );
}

// Dropdown to jump to another cluster, staying on the same kind of page.
function ClusterSwitcher({ cluster, pagePath }) {
  const { data: clusters } = useClusters();
  const navigate = useNavigate();

  if (!clusters?.length) return null;

  return (
    <label className="picker">
      <span className="picker-label">Cluster</span>
      <Select
        size="sm"
        value={cluster?.id ?? ""}
        onChange={(e) => navigate(`/c/${e.target.value}/${pagePath || "overview"}`)}
      >
        {!cluster && <option value="">Choose…</option>}
        {clusters.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </Select>
    </label>
  );
}

// Namespace dropdown. The choice lives in the URL as ?ns=<name>;
// no ?ns means "all namespaces".
function NamespacePicker({ cluster, reachable, clusterScoped }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selected = searchParams.get("ns") ?? "";
  const { data: namespaces } = useApi(reachable ? `/clusters/${cluster.id}/namespaces` : null);

  // On pages like Nodes the namespace doesn't apply. We keep ?ns in the URL
  // so it's still selected when you go back to a namespaced page.
  if (clusterScoped) {
    return (
      <label className="picker" title="This page isn't limited to a namespace">
        <span className="picker-label">Namespace</span>
        <Select size="sm" disabled>
          <option>Cluster-wide</option>
        </Select>
      </label>
    );
  }

  function choose(name) {
    const next = new URLSearchParams(searchParams);
    if (name) next.set("ns", name);
    else next.delete("ns");
    setSearchParams(next);
  }

  return (
    <label className="picker">
      <span className="picker-label">Namespace</span>
      <Select size="sm" value={selected} onChange={(e) => choose(e.target.value)} disabled={!namespaces}>
        <option value="">All namespaces</option>
        {/* Keep a namespace from the URL selectable even before the list arrives. */}
        {selected && !namespaces?.some((ns) => ns.name === selected) && <option value={selected}>{selected}</option>}
        {namespaces?.map((ns) => (
          <option key={ns.name} value={ns.name}>
            {ns.name}
          </option>
        ))}
      </Select>
    </label>
  );
}

// Small pill: Kubernetes version when connected, "Unreachable" when not.
function ConnectionBadge({ status }) {
  if (!status?.data) {
    return (
      <span className="badge badge-checking">
        <span className="dot" />
        Connecting
      </span>
    );
  }
  if (!status.data.reachable) {
    return (
      <span className="badge badge-down">
        <span className="dot" />
        Unreachable
      </span>
    );
  }
  return (
    <span className="badge badge-ok" title={status.data.server}>
      <span className="dot" />
      {status.data.version}
    </span>
  );
}
