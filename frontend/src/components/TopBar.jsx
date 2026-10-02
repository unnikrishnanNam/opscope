import { useLocation, useNavigate, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useClusters } from "../clusters.jsx";
import { allPages } from "../sections.js";

// Crumbs for pages that don't belong to a cluster.
const otherPages = {
  "/clusters": ["Settings", "Clusters"],
  "/clusters/add": ["Clusters", "Add a cluster"],
};

export default function TopBar({ cluster, pagePath, status }) {
  const { pathname } = useLocation();
  const page = allPages.find((p) => p.path === pagePath);
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
        <span className="crumb-page">{label}</span>
      </div>

      <div className="topbar-right">
        <ClusterSwitcher cluster={cluster} pagePath={pagePath} />
        {cluster && <NamespacePicker cluster={cluster} reachable={status?.data?.reachable} />}
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
      <select
        className="select"
        value={cluster?.id ?? ""}
        onChange={(e) => navigate(`/c/${e.target.value}/${pagePath || "overview"}`)}
      >
        {!cluster && <option value="">Choose…</option>}
        {clusters.map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </select>
    </label>
  );
}

// Namespace dropdown. The choice lives in the URL as ?ns=<name>;
// no ?ns means "all namespaces".
function NamespacePicker({ cluster, reachable }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selected = searchParams.get("ns") ?? "";
  const { data: namespaces } = useApi(reachable ? `/clusters/${cluster.id}/namespaces` : null);

  function choose(name) {
    const next = new URLSearchParams(searchParams);
    if (name) next.set("ns", name);
    else next.delete("ns");
    setSearchParams(next);
  }

  return (
    <label className="picker">
      <span className="picker-label">Namespace</span>
      <select className="select" value={selected} onChange={(e) => choose(e.target.value)} disabled={!namespaces}>
        <option value="">All namespaces</option>
        {/* Keep a namespace from the URL selectable even before the list arrives. */}
        {selected && !namespaces?.some((ns) => ns.name === selected) && <option value={selected}>{selected}</option>}
        {namespaces?.map((ns) => (
          <option key={ns.name} value={ns.name}>
            {ns.name}
          </option>
        ))}
      </select>
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
