import { useEffect, useId, useRef } from "react";
import { Link, useLocation } from "react-router";
import { RELATIONS, kindIcon, ownerKind } from "../graph/kinds.js";
import { shownState } from "../graph/trace.js";
import { detailPath, nsQuery } from "../sections.js";
import Button from "./Button.jsx";
import { Evidence, NextSteps } from "./Diagnosis.jsx";
import { Tag } from "./Tag.jsx";
import { StateMark, cardName, kindText, statusText } from "./TopologyGraph.jsx";
import { CloseIcon, ErrorIcon, InfoIcon, MapIcon, WarningIcon } from "./icons.jsx";
import "./TopologyPanel.css";

const MARKS = { failing: ErrorIcon, degraded: WarningIcon, info: InfoIcon };
const CONFIDENCE = {
  likely: "Read from the text of an event or message, which can change between Kubernetes versions.",
  possible: "A guess: Opscope can't see enough to be sure.",
};

// TopologyPanel shows the card selected on a map: what it is, what's wrong
// with it, and what it's connected to. It sits over the map's right side
// and leaves the map usable, so picking another card just shows that one.
//
//   view, index  the map's view and its findings (see graph/trace.js)
//   id           the selected card; focus, the map's own object
//   onSelect(id) picks another card; onClose() closes the panel
//   expanded, onExpand(id, on)  workloads whose pods are shown one by one,
//                and how to change that (optional); with expands="namespaces",
//                the cluster view's namespaces shown open
export default function TopologyPanel({
  clusterId,
  view,
  index,
  id,
  focus,
  namespace,
  onSelect,
  onClose,
  expanded,
  onExpand,
  expands = "pods",
}) {
  const titleId = useId();
  const titleRef = useRef(null);
  const search = nsQuery(useLocation().search);
  const nodes = Object.fromEntries(view.nodes.map((n) => [n.id, n]));
  const n = nodes[id];

  // Reading starts at the top when another card is picked.
  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
    titleRef.current?.closest(".topo-panel")?.scrollTo(0, 0);
  }, [id]);

  if (!n) return null;
  const Icon = kindIcon(n.resource);
  const ctx = { clusterId, search, nodes, namespace };
  const here = Boolean(focus) && (id === focus || n.pods?.owner === focus); // its page is this one
  const page = here ? null : pageOf(clusterId, n, nodes);
  const map = mapOf(clusterId, n);
  const pods = onExpand && expands === "pods";
  const namespaces = onExpand && expands === "namespaces";
  const findings = merge(index.byNode.get(id) ?? [], index);

  return (
    <aside
      className="topo-panel"
      aria-labelledby={titleId}
      onKeyDown={(event) => {
        if (event.key === "Escape") {
          event.stopPropagation();
          onClose();
        }
      }}
    >
      <header className="topo-panel-header">
        <div className="topo-panel-kind">
          <Icon size={14} />
          {n.pods ? `${kindText(n)} of ${ownerKind(n, nodes)}` : n.kind}
        </div>
        <Button variant="quiet" size="sm" icon={CloseIcon} label="Close details" onClick={onClose} />
      </header>
      <h3 id={titleId} ref={titleRef} tabIndex={-1} className="topo-panel-title">
        {cardName(n, namespace)}
      </h3>
      <p className="topo-panel-status">
        <StateMark state={shownState(n, index)} />
        <span>{statusText(n, shownState(n, index))}</span>
      </p>
      {n.message && n.state !== "missing" && <p className="topo-panel-message">{n.message}</p>}

      {/* Links to its page and its own map (there are none for the page's
          own object), and showing a group's pods or a namespace's apps. */}
      <div className="topo-panel-actions">
        {page && (
          <Button size="sm" to={page.path + search}>
            Open {page.label}
          </Button>
        )}
        {page && (
          <Button size="sm" variant="quiet" icon={MapIcon} to={`${page.path}?${mapQuery(search)}`}>
            Its map
          </Button>
        )}
        {map && (
          <Button size="sm" icon={MapIcon} to={map}>
            Open its map
          </Button>
        )}
        {pods && n.pods && n.pods.total > 1 && (
          <Button size="sm" variant="quiet" onClick={() => onExpand(n.pods.owner, true)}>
            Show each pod
          </Button>
        )}
        {pods && expanded?.includes(id) && (
          <Button size="sm" variant="quiet" onClick={() => onExpand(id, false)}>
            Group its pods
          </Button>
        )}
        {namespaces && n.resource === "namespaces" && (
          <Button size="sm" variant="quiet" onClick={() => onExpand(id, true)}>
            Show its apps
          </Button>
        )}
        {namespaces && n.group && expanded?.includes(n.group) && (
          <Button size="sm" variant="quiet" onClick={() => onExpand(n.group, false)}>
            Fold {n.namespace} up
          </Button>
        )}
      </div>

      <section className="topo-panel-section" aria-label="Findings">
        {findings.length === 0 ? (
          <p className="topo-panel-quiet">{n.state === "ok" ? "Opscope found nothing wrong with it." : "No finding about it."}</p>
        ) : (
          <ul className="topo-panel-findings">
            {findings.map(({ finding: f, objects, root }) => {
              const Mark = MARKS[f.severity];
              return (
                <li key={f.id} className={`topo-finding topo-finding-${f.severity}`}>
                  <Mark size={16} className="topo-finding-mark" />
                  <div className="topo-finding-main">
                    {(root || f.confidence !== "certain" || objects.length > 1 || n.pods) && (
                      <div className="topo-finding-head">
                        {n.pods && <span className="topo-finding-objects">{objectsText(objects)}</span>}
                        {root && <span className={`topo-tag topo-tag-${f.severity === "failing" ? "bad" : f.severity === "degraded" ? "warn" : "quiet"}`}>root cause</span>}
                        {f.confidence !== "certain" && <Tag title={CONFIDENCE[f.confidence]}>{f.confidence}</Tag>}
                      </div>
                    )}
                    <p className="topo-finding-title">{f.title}</p>
                    {f.detail && <p className="topo-finding-detail">{f.detail}</p>}
                    <Evidence items={f.evidence} open={false} />
                    <NextSteps steps={f.next} ctx={ctx} />
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      {n.pods && <Members clusterId={clusterId} n={n} index={index} search={search} />}

      <Connections id={id} view={view} index={index} nodes={nodes} namespace={namespace} onSelect={onSelect} />
    </aside>
  );
}

// Members lists the pods a group card stands for.
function Members({ clusterId, n, index, search }) {
  return (
    <section className="topo-panel-section">
      <h4 className="topo-panel-heading">Pods</h4>
      <ul className="topo-panel-list">
        {n.pods.members.map((member) => {
          const name = member.split("/").at(-1);
          const worst = (index.byObject.get(member) ?? [])[0];
          return (
            <li key={member}>
              <Link to={detailPath(clusterId, "pods", n.namespace, name) + search} className="topo-panel-link">
                {name}
              </Link>
              {worst && <span className="topo-panel-quiet"> · {worst.title}</span>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// Connections lists the card's edges by how they read from here ("Uses",
// "Selected by"), each a button that picks the card at the other end.
function Connections({ id, view, index, nodes, namespace, onSelect }) {
  const groups = new Map();
  for (const e of view.edges) {
    const out = e.from === id;
    if (!out && e.to !== id) continue;
    const relation = RELATIONS[e.type]?.[out ? "out" : "in"] ?? e.type;
    if (!groups.has(relation)) groups.set(relation, []);
    groups.get(relation).push({ other: nodes[out ? e.to : e.from], via: e.via ?? [] });
  }
  if (groups.size === 0) return null;
  return (
    <section className="topo-panel-section">
      <h4 className="topo-panel-heading">Connections</h4>
      <dl className="topo-panel-connections">
        {[...groups].map(([relation, list]) => (
          <div key={relation}>
            <dt>{relation}</dt>
            {list.map(({ other, via }) => (
              <dd key={other.id}>
                <button type="button" className="topo-panel-pick" onClick={() => onSelect(other.id)}>
                  <StateMark state={shownState(other, index)} />
                  <span>
                    {kindText(other)} {cardName(other, namespace)}
                  </span>
                </button>
                {via.length > 0 && <span className="topo-panel-via">{via.join(", ")}</span>}
              </dd>
            ))}
          </div>
        ))}
      </dl>
    </section>
  );
}

// merge folds alike findings (the same rule, title and culprits about
// several pods of a group) into one, and marks root causes: findings whose
// culprits have no findings of their own.
function merge(findings, index) {
  const byKey = new Map();
  for (const f of findings) {
    const key = `${f.rule}|${f.title}|${f.blames.join(",")}`;
    if (!byKey.has(key)) {
      const root = !f.blames.some((b) => (index.byObject.get(b) ?? []).length > 0);
      byKey.set(key, { finding: f, objects: [], root });
    }
    byKey.get(key).objects.push(f.object);
  }
  return [...byKey.values()];
}

function objectsText(objects) {
  return objects.length === 1 ? objects[0].split("/").at(-1) : `${objects.length} pods`;
}

// pageOf is the page to open for a card: the object's own, or for a group
// of pods, their workload's. Missing and unknown objects have none.
function pageOf(clusterId, n, nodes) {
  if (n.state === "missing" || n.state === "unknown") return null;
  if (n.pods) {
    const [resource, namespace, name] = n.pods.owner.split("/");
    const path = detailPath(clusterId, resource, namespace, name);
    return path && { path, label: ownerKind(n, nodes) };
  }
  const path = detailPath(clusterId, n.resource, n.namespace, n.name);
  return path && { path, label: n.kind };
}

// mapOf is the Topology page's map of a cluster view's app or namespace
// (or of a Namespace on any map).
function mapOf(clusterId, n) {
  const params = new URLSearchParams();
  if (n.resource === "apps") {
    params.set("ns", n.namespace);
    params.set("app", n.name);
  } else if (n.resource === "namespaces" && n.state !== "missing") {
    params.set("ns", n.name);
  } else {
    return null;
  }
  return `/c/${clusterId}/topology?${params}`;
}

function mapQuery(search) {
  const params = new URLSearchParams(search);
  params.set("tab", "map");
  return params.toString();
}
