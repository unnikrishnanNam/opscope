import { useLocation, useNavigate, useSearchParams } from "react-router";
import { useApi } from "../api.js";
import { useClusters } from "../clusters.jsx";
import { allPages, nsQuery } from "../sections.js";
import Button from "./Button.jsx";
import ClusterSwitcher from "./ClusterSwitcher.jsx";
import Combobox from "./Combobox.jsx";
import { Breadcrumbs } from "./PageHeader.jsx";
import { MenuIcon } from "./icons.jsx";
import "./TopBar.css";

// Breadcrumbs for pages that don't belong to a cluster.
const OTHER_PAGES = {
  "/clusters": [{ label: "Clusters" }],
  "/clusters/add": [{ label: "Clusters", to: "/clusters" }, { label: "Add a cluster" }],
};

// The bar above every page: where you are on the left; which cluster and
// namespace on the right. On narrow screens a menu button opens the
// sidebar (`onMenu`).
//   cluster   the cluster in the URL (undefined on pages outside a cluster,
//             and for a moment while the cluster list loads)
//   status    that cluster's status: { reachable, version, ... }
//   pagePath  the rest of the URL after /c/<id>/, e.g. "workloads/pods/web/api-1"
export default function TopBar({ cluster, status, pagePath, onMenu }) {
  const { pathname, search: fullSearch } = useLocation();
  const search = nsQuery(fullSearch);
  // The page this URL belongs to: "workloads/pods" itself, or a detail page
  // under it like "workloads/pods/web/api-1".
  const page = allPages.find((p) => pagePath === p.path || pagePath?.startsWith(p.path + "/"));
  const objectName = page && pagePath !== page.path ? decodeURIComponent(pagePath.split("/").pop()) : null;

  let crumbs = OTHER_PAGES[pathname] ?? [];
  if (page) {
    crumbs = [{ label: page.group }, { label: page.label }];
    if (objectName && cluster) {
      crumbs = [
        { label: page.group },
        { label: page.label, to: `/c/${cluster.id}/${page.path}${search}` },
        { label: objectName },
      ];
    }
  }

  return (
    <header className="topbar">
      <div className="topbar-start">
        <span className="topbar-menu">
          <Button variant="quiet" icon={MenuIcon} label="Open navigation" onClick={onMenu} />
        </span>
        {crumbs.length > 0 && <Breadcrumbs items={crumbs} />}
      </div>
      <div className="topbar-end">
        {/* Switching cluster from a detail page goes to the list: the object is in the old cluster. */}
        <ClusterPicker cluster={cluster} pagePath={page?.path ?? pagePath} />
        {cluster && (
          <NamespacePicker cluster={cluster} reachable={status?.reachable} clusterScoped={page?.clusterScoped} />
        )}
      </div>
    </header>
  );
}

// Jump to another cluster, staying on the same kind of page.
function ClusterPicker({ cluster, pagePath }) {
  const { data: clusters, statuses } = useClusters();
  const navigate = useNavigate();
  if (!clusters?.length) return null;

  return (
    <ClusterSwitcher
      clusters={clusters.map((c) => ({ ...c, status: statuses[c.id] }))}
      value={cluster?.id}
      onChange={(id) => navigate(`/c/${id}/${pagePath || "overview"}`)}
    />
  );
}

// The namespace lives in the URL as ?ns=<name>; no ?ns means "all namespaces".
function NamespacePicker({ cluster, reachable, clusterScoped }) {
  const [searchParams, setSearchParams] = useSearchParams();
  const selected = searchParams.get("ns") ?? "";
  const { data: namespaces } = useApi(reachable ? `/clusters/${cluster.id}/namespaces` : null);

  // On pages like Nodes the namespace doesn't apply. ?ns stays in the URL, so
  // it's still selected when you go back to a namespaced page.
  if (clusterScoped) {
    return (
      <Combobox
        label="Namespace"
        showLabel
        size="sm"
        value="cluster-wide"
        options={[{ value: "cluster-wide", label: "Cluster-wide" }]}
        onChange={() => {}}
        disabled
        title="This page isn't limited to a namespace"
      />
    );
  }

  const names = namespaces?.map((ns) => ns.name) ?? [];
  // Keep a namespace from the URL selectable even before the list arrives.
  if (selected && !names.includes(selected)) names.unshift(selected);

  function choose(name) {
    const next = new URLSearchParams(searchParams);
    if (name) next.set("ns", name);
    else next.delete("ns");
    setSearchParams(next);
  }

  return (
    <Combobox
      label="Namespace"
      showLabel
      size="sm"
      align="end"
      noun="namespaces"
      value={selected}
      onChange={choose}
      options={[{ value: "", label: "All namespaces" }, ...names.map((n) => ({ value: n, label: n }))]}
      disabled={!namespaces}
    />
  );
}
