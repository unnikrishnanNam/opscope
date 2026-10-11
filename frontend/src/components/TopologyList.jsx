import { useMemo } from "react";
import { Link, useLocation } from "react-router";
import { layout } from "../graph/layout.js";
import { RELATIONS, ownerKind } from "../graph/kinds.js";
import { indexFindings, rootCauses, shownState } from "../graph/trace.js";
import { detailPath, nsQuery } from "../sections.js";
import { StateMark, cardName, kindText, statusText } from "./TopologyGraph.jsx";
import "./TopologyList.css";

// TopologyList is a map as text: the same columns, in the same order, each
// object with its status, what's wrong with it and what it connects to.
// It reads the same with a screen reader or on a phone, so nothing a map
// shows exists only in the picture.
export default function TopologyList({ clusterId, view, focus, namespace }) {
  const search = nsQuery(useLocation().search);
  const placed = useMemo(() => layout(view), [view]);
  const index = useMemo(() => indexFindings(view), [view]);
  const roots = useMemo(() => rootCauses(index), [index]);
  const nodes = Object.fromEntries(view.nodes.map((n) => [n.id, n]));

  const columns = placed.columns.map((c, rank) => ({
    ...c,
    ids: Object.values(placed.nodes)
      .filter((p) => p.rank === rank)
      .sort((a, b) => a.y - b.y)
      .map((p) => p.id),
  }));

  if (view.nodes.length === 0) return <p className="topo-list-empty">Nothing to show.</p>;

  return (
    <div className="topo-list">
      {columns.map((c) => (
        <section key={c.key} className="topo-list-column">
          <h3 className="topo-list-heading">{c.label}</h3>
          <ul>
            {c.ids.map((id) => {
              const n = nodes[id];
              const findings = unique(index.byNode.get(id) ?? []);
              const relations = relationsOf(id, view.edges, nodes);
              return (
                <li key={id} className={`topo-list-item ${id === focus ? "topo-list-focus" : ""}`}>
                  <div className="topo-list-head">
                    <StateMark state={shownState(n, index)} />
                    <span className="topo-list-name">
                      <Name clusterId={clusterId} n={n} nodes={nodes} namespace={namespace} search={search} />
                    </span>
                    <span className="topo-list-status">{statusText(n, shownState(n, index))}</span>
                    {roots.get(id) && <span className={`topo-tag topo-tag-${roots.get(id) === "failing" ? "bad" : "warn"}`}>root cause</span>}
                    {id === focus && <span className="topo-tag">this object</span>}
                  </div>
                  {findings.length > 0 && (
                    <ul className="topo-list-findings">
                      {findings.map((f) => (
                        <li key={f.title} className={`topo-list-finding-${f.severity}`}>
                          {f.title}
                        </li>
                      ))}
                    </ul>
                  )}
                  {relations.length > 0 && (
                    <p className="topo-list-relations">
                      {relations.map(([relation, others], i) => (
                        <span key={relation}>
                          {i > 0 && " · "}
                          {relation}{" "}
                          {others.map((other, j) => (
                            <span key={other.id}>
                              {j > 0 && ", "}
                              <span className="topo-list-other">{`${kindText(other)} ${cardName(other, namespace)}`}</span>
                            </span>
                          ))}
                        </span>
                      ))}
                    </p>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}

// Name is "Deployment web" linked to its page, or for a group of pods,
// "3 Pods" linked to their workload's page.
function Name({ clusterId, n, nodes, namespace, search }) {
  const exists = n.state !== "missing" && n.state !== "unknown";
  let link = null;
  let label = `${n.kind} ${cardName(n, namespace)}`;
  if (n.pods) {
    const [resource, ns, name] = n.pods.owner.split("/");
    link = detailPath(clusterId, resource, ns, name);
    label = `${kindText(n)} of ${ownerKind(n, nodes)} ${n.name}`;
  } else if (exists) {
    link = detailPath(clusterId, n.resource, n.namespace, n.name);
  }
  return link ? <Link to={link + search}>{label}</Link> : label;
}

// relationsOf groups what a card needs by how it needs it: "Uses
// ConfigMap a, Secret b". Each edge is listed once, at the end that
// depends on the other.
function relationsOf(id, edges, nodes) {
  const groups = new Map();
  for (const e of edges) {
    if (e.from !== id) continue;
    const relation = RELATIONS[e.type]?.out ?? e.type;
    if (!groups.has(relation)) groups.set(relation, []);
    groups.get(relation).push(nodes[e.to]);
  }
  return [...groups];
}

// unique drops findings repeated by the pods of a group.
function unique(findings) {
  const seen = new Set();
  return findings.filter((f) => !seen.has(f.title) && seen.add(f.title));
}
