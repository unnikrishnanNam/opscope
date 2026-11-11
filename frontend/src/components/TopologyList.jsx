import { useMemo } from "react";
import { Link, useLocation } from "react-router";
import { kindIcon, relation } from "../graph/kinds.js";
import { layout } from "../graph/layout.js";
import { cardKind } from "../graph/view.js";
import { detailPath, nsQuery } from "../sections.js";
import { gapTag, statusText } from "./TopologyGraph.jsx";
import "./TopologyList.css";

// TopologyList is a map as text: the same columns in the same order, each
// object with its status, any gap, and what it needs. It reads the same
// with a screen reader or on a phone, so nothing a map shows exists only in
// the picture.
export default function TopologyList({ view, focus, clusterId, empty = "Nothing to show." }) {
  const search = nsQuery(useLocation().search);
  const placed = useMemo(() => layout(view), [view]);
  const nodes = useMemo(() => new Map(view.nodes.map((n) => [n.id, n])), [view]);

  if (view.nodes.length === 0) return <p className="topo-list-empty">{empty}</p>;

  const columns = placed.columns.map((c, rank) => ({
    ...c,
    ids: Object.values(placed.nodes)
      .filter((p) => p.rank === rank)
      .sort((a, b) => a.y - b.y)
      .map((p) => p.id),
  }));

  return (
    <div className="topo-list">
      {columns.map((c) => (
        // Columns are plain sections under their heading, not landmarks: a
        // list of eleven landmarks is noise to a screen reader.
        <section key={c.key} className="topo-list-column">
          <h3 className="topo-list-heading">
            {c.label} <span className="topo-list-count">{c.ids.length}</span>
          </h3>
          <ul className="topo-list-items">
            {c.ids.map((id) => (
              <Item key={id} n={nodes.get(id)} view={view} nodes={nodes} focus={focus} clusterId={clusterId} search={search} />
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function Item({ n, view, nodes, focus, clusterId, search }) {
  const Icon = kindIcon(n.resource);
  const tag = gapTag(n);
  const status = statusText(n);
  // What it needs, by how: "Uses ConfigMap app-config, Secret db-creds".
  // Each edge is listed once, at the end that needs the other.
  const needs = new Map();
  for (const e of view.edges) {
    if (e.from !== n.id) continue;
    const words = relation(e.kind, true);
    if (!needs.has(words)) needs.set(words, []);
    needs.get(words).push(nodes.get(e.to));
  }
  return (
    <li className={`topo-list-item topo-list-${n.presence} ${n.id === focus ? "topo-list-focus" : ""}`}>
      <div className="topo-list-head">
        <Icon size={16} className="topo-list-icon" />
        <span className="topo-list-kind">{n.pods ? `${cardKind(n)} of ${n.pods.ownerKind}` : n.kind}</span>
        <span className="topo-list-name">
          <Name n={n} clusterId={clusterId} search={search} />
        </span>
        {n.namespace && <span className="topo-list-ns">{n.namespace}</span>}
        {tag && (
          <span className="topo-list-tag">
            <tag.Icon size={12} />
            {tag.label}
          </span>
        )}
        {n.id === focus && <span className="topo-list-tag topo-list-tag-here">This object</span>}
      </div>
      {status && <p className="topo-list-status">{status}</p>}
      {n.pods && <p className="topo-list-members">{n.pods.members.map((p) => `${p.name} (${p.status || "Unknown"})`).join(", ")}</p>}
      {needs.size > 0 && (
        <ul className="topo-list-needs">
          {[...needs].map(([words, others]) => (
            <li key={words}>
              <span className="topo-list-words">{words}</span>{" "}
              {others.map((o, i) => (
                <span key={o.id}>
                  {i > 0 && ", "}
                  <span className={`topo-list-other topo-list-other-${o.presence}`}>
                    {cardKind(o)} {o.name}
                    {o.presence !== "present" && ` (${gapTag(o).label.toLowerCase()})`}
                  </span>
                </span>
              ))}
            </li>
          ))}
        </ul>
      )}
    </li>
  );
}

// Name links to the object's page, or for a group of pods to their
// workload's, when there is one.
function Name({ n, clusterId, search }) {
  let path = null;
  if (clusterId && n.presence === "present") {
    if (n.pods) {
      const resource = { Deployment: "deployments", StatefulSet: "statefulsets", DaemonSet: "daemonsets", Job: "jobs", CronJob: "cronjobs" }[
        n.pods.ownerKind
      ];
      path = resource && detailPath(clusterId, resource, n.namespace, n.name);
    } else {
      path = detailPath(clusterId, n.resource, n.namespace, n.name);
    }
  }
  return path ? <Link to={path + search}>{n.name}</Link> : n.name;
}
