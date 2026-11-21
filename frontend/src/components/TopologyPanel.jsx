import { useEffect, useId, useRef } from "react";
import { Link, useLocation } from "react-router";
import { useApi } from "../api.js";
import { issuesLink } from "../pages/issuesRequest.js";
import { countOf, kindIcon, pluralKind, relation } from "../graph/kinds.js";
import { cardKind, pageOf } from "../graph/view.js";
import { detailPath, nsQuery } from "../sections.js";
import Button from "./Button.jsx";
import HealthBadge from "./HealthBadge.jsx";
import { SEVERITY } from "./IssueList.jsx";
import { gapTag, statusText, troubleOf } from "./TopologyGraph.jsx";
import { ArrowLeftIcon, ArrowRightIcon, ChevronRightIcon, CloseIcon, EndpointSlicesIcon, NamespaceIcon } from "./icons.jsx";
import "./TopologyPanel.css";

// TopologyPanel shows the card selected on a map: what it is, any gap it
// is, and what it's connected to, both ways. It sits over the map's right
// side and leaves the map usable, so picking another card shows that one.
//
//   view         the shaped view (graph/view.js); id, the selected card
//   clusterId    for links to objects' pages: the selected one's, and each
//                connection's beside it
//   onSelect(id) picks another card; onClose() closes the panel
//   expanded, onExpand(owner, on)  workloads whose pods are shown one by one
// Name keeps an object's name in one piece in a sentence: "team-a" never
// breaks at its hyphen.
function Name({ children }) {
  return <span className="topo-note-name">{children}</span>;
}

export default function TopologyPanel({ view, id, clusterId, level = 3, onSelect, onClose, expanded, onExpand }) {
  const Title = `h${level}`;
  const titleId = useId();
  const titleRef = useRef(null);
  const search = nsQuery(useLocation().search);
  const nodes = new Map(view.nodes.map((n) => [n.id, n]));
  const n = nodes.get(id);

  // Reading starts at the top when another card is picked.
  useEffect(() => {
    titleRef.current?.focus({ preventScroll: true });
    titleRef.current?.closest(".topo-panel")?.scrollTo(0, 0);
  }, [id]);

  if (!n) return null;
  const Icon = kindIcon(n.resource);
  const page = pageOf(clusterId, n);
  const canExpand = onExpand && n.pods;
  const canGroup = onExpand && expanded?.has(n.id);

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
        <span className="topo-panel-kind">
          <Icon size={14} />
          {n.pods ? `${cardKind(n)} of ${n.pods.ownerKind}` : n.kind}
        </span>
        <Button variant="quiet" size="sm" icon={CloseIcon} label="Close details" onClick={onClose} />
      </header>

      <Title id={titleId} ref={titleRef} tabIndex={-1} className="topo-panel-title">
        {n.name}
      </Title>
      {n.namespace && (
        <p className="topo-panel-where">
          <NamespaceIcon size={13} />
          <span className="visually-hidden">Namespace </span>
          {n.namespace}
        </p>
      )}

      <Notes n={n} view={view} />

      {n.presence === "present" && (n.status || n.endpoints || n.health) && (
        <dl className="topo-panel-facts">
          {n.health && (
            <div>
              <dt>Health</dt>
              <dd>
                <HealthBadge health={n.health} />
                {n.pods && n.health.state !== "healthy" && " (the worst of its pods)"}
              </dd>
            </div>
          )}
          {n.status && (
            <div>
              <dt>Status</dt>
              <dd>{n.status}</dd>
            </div>
          )}
          {n.endpoints && (
            <div>
              <dt>Endpoints</dt>
              <dd>{statusText(n)}</dd>
            </div>
          )}
        </dl>
      )}

      {(page || canExpand || canGroup) && (
        <div className="topo-panel-actions">
          {page && (
            <Button size="sm" to={page.path + search}>
              Open {page.kind}
            </Button>
          )}
          {canExpand && (
            <Button size="sm" variant="quiet" onClick={() => onExpand(n.pods.owner, true)}>
              Show each pod
            </Button>
          )}
          {canGroup && (
            <Button size="sm" variant="quiet" onClick={() => onExpand(n.id, false)}>
              Group its pods
            </Button>
          )}
        </div>
      )}

      {n.health && !n.pods && clusterId && n.health.state !== "healthy" && (
        <PanelIssues clusterId={clusterId} n={n} search={search} level={level + 1} />
      )}

      {n.pods && <Members clusterId={clusterId} n={n} search={search} level={level + 1} />}

      <Connections id={id} view={view} nodes={nodes} clusterId={clusterId} search={search} onSelect={onSelect} level={level + 1} />
    </aside>
  );
}

// Notes say what a gap means, each with its icon, in plain words.
function Notes({ n, view }) {
  const tag = gapTag(n);
  const notes = [];
  const referrers = view.edges.filter((e) => e.to === n.id).length;
  if (n.presence === "missing") {
    notes.push({
      title: "Doesn't exist",
      text: (
        <>
          Nothing named <Name>{n.name}</Name> exists{n.namespace && <> in <Name>{n.namespace}</Name></>}, but {countOf(referrers, "object")} on
          this map {referrers === 1 ? "refers" : "refer"} to it.
        </>
      ),
    });
  }
  if (n.presence === "unknown") {
    notes.push({
      title: "Can't be read",
      text: n.resource
        ? `Opscope isn't allowed to read ${pluralKind(n.kind)} here, so it can't tell whether this exists.`
        : `Opscope doesn't read ${pluralKind(n.kind)}: it knows this one only because something names it as its owner.`,
    });
  }
  for (const d of n.dangling ?? []) {
    notes.push({ title: "Selects nothing", text: `Its selector matches no pods, so it has nowhere to send traffic.`, field: d.field });
  }
  if (n.endpoints?.notReady) {
    notes.push({
      title: "Not every pod is ready",
      text: `${countOf(n.endpoints.notReady, "pod")} behind it ${n.endpoints.notReady === 1 ? "isn't" : "aren't"} ready, so ${
        n.endpoints.notReady === 1 ? "it gets" : "they get"
      } no traffic.`,
      Icon: EndpointSlicesIcon,
    });
  }
  if (notes.length === 0) return null;
  return (
    <ul className="topo-panel-notes">
      {notes.map((note) => {
        const NoteIcon = note.Icon ?? tag?.Icon;
        return (
          <li key={note.title} className="topo-note">
            {NoteIcon && <NoteIcon size={16} className="topo-note-icon" />}
            <div>
              <p className="topo-note-title">{note.title}</p>
              <p className="topo-note-text">{note.text}</p>
              {note.field && <code className="topo-note-field">{note.field}</code>}
            </div>
          </li>
        );
      })}
    </ul>
  );
}

// Members lists the pods a group card stands for, with their status.
// PanelIssues lists a card's open issues, each linking to its row on the
// Issues page. Read live, as the Issues page is.
function PanelIssues({ clusterId, n, search, level }) {
  const Heading = `h${level}`;
  const q = new URLSearchParams({ resource: n.resource, name: n.name });
  if (n.namespace) q.set("namespace", n.namespace);
  const { data } = useApi(`/clusters/${encodeURIComponent(clusterId)}/health?${q}`, { refreshMs: 10_000, live: "issues" });
  const open = (data?.signals ?? []).filter((s) => s.state !== "resolved");
  if (!open.length) return null;
  return (
    <section className="topo-panel-section">
      <Heading className="topo-panel-heading">
        Issues <span className="topo-panel-count">{open.length}</span>
      </Heading>
      <ul className="topo-panel-issues">
        {open.map((s) => {
          const sev = SEVERITY[s.severity] ?? SEVERITY.info;
          return (
            <li key={s.id}>
              <span className={`topo-panel-issue-mark issue-mark-${sev.tone}`}>
                <sev.icon size={14} />
                <span className="visually-hidden">{sev.label}:</span>
              </span>
              <span>
                <Link to={issuesLink(clusterId, { signal: s.id }, search)} className="topo-panel-link">
                  {s.title}
                </Link>
                <span className="topo-panel-issue-message">{s.message}</span>
              </span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function Members({ clusterId, n, search, level }) {
  const Heading = `h${level}`;
  return (
    <section className="topo-panel-section">
      <Heading className="topo-panel-heading">
        Pods <span className="topo-panel-count">{n.pods.members.length}</span>
      </Heading>
      <ul className="topo-panel-members">
        {n.pods.members.map((pod) => {
          const path = clusterId && detailPath(clusterId, "pods", pod.namespace, pod.name);
          return (
            <li key={pod.id}>
              {path ? (
                <Link to={path + search} className="topo-panel-link">
                  {pod.name}
                </Link>
              ) : (
                <span>{pod.name}</span>
              )}
              <span className="topo-panel-quiet">{pod.status}</span>
              {troubleOf(pod) && <HealthBadge health={pod.health} />}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

// Connections lists the card's edges in two parts: what it needs (edges
// from it) and what needs it (edges to it), each by how it reads ("Uses",
// "Selected by"). Each is a button that picks the card at the other end,
// with what the edge was read from, and a link to that object's page.
function Connections({ id, view, nodes, clusterId, search, onSelect, level }) {
  const Heading = `h${level}`;
  const sides = [
    { out: true, title: "Needs", Icon: ArrowRightIcon, about: "What this object depends on." },
    { out: false, title: "Needed by", Icon: ArrowLeftIcon, about: "What depends on this object." },
  ].map((side) => {
    const groups = new Map();
    for (const e of view.edges) {
      if ((side.out ? e.from : e.to) !== id) continue;
      const words = relation(e.kind, side.out);
      if (!groups.has(words)) groups.set(words, []);
      groups.get(words).push({ e, other: nodes.get(side.out ? e.to : e.from) });
    }
    for (const list of groups.values()) list.sort((a, b) => (a.other.name < b.other.name ? -1 : 1));
    return { ...side, groups: [...groups], total: [...groups.values()].reduce((sum, l) => sum + l.length, 0) };
  });
  if (sides.every((s) => s.total === 0)) {
    return <p className="topo-panel-quiet topo-panel-alone">Nothing on this map is connected to it.</p>;
  }
  return sides
    .filter((s) => s.total > 0)
    .map((side) => (
      <section key={side.title} className="topo-panel-section">
        <Heading className="topo-panel-heading" title={side.about}>
          <side.Icon size={14} />
          {side.title} <span className="topo-panel-count">{side.total}</span>
        </Heading>
        {side.groups.map(([words, list]) => (
          <div key={words} className="topo-rel">
            <p className="topo-rel-words">{words}</p>
            <ul className="topo-rel-list">
              {list.map(({ e, other }) => (
                <Connection
                  key={e.id}
                  e={e}
                  other={other}
                  here={nodes.get(id)}
                  page={pageOf(clusterId, other)}
                  search={search}
                  onSelect={onSelect}
                />
              ))}
            </ul>
          </div>
        ))}
      </section>
    ));
}

function Connection({ e, other, here, page, search, onSelect }) {
  const OtherIcon = kindIcon(other.resource);
  const tag = gapTag(other);
  const notes = unique((e.evidence ?? []).map((ev) => ev.note).filter(Boolean));
  const fields = unique((e.evidence ?? []).map((ev) => ev.field));
  // On a group's panel, how many of its pods this edge stands for. (On the
  // other end's, the card already says "3 pods".)
  const total = here?.pods?.members.length;
  const inPods = total && e.count ? (e.count === total ? `in all ${total} pods` : `in ${e.count} of ${total} pods`) : null;
  return (
    <li className="topo-rel-item">
      <div className="topo-rel-row">
        <button type="button" className={`topo-pick topo-pick-${other.presence}`} onClick={() => onSelect(other.id)}>
          <OtherIcon size={14} className="topo-pick-icon" />
          <span className="topo-pick-kind">{cardKind(other)}</span>
          <span className="topo-pick-name">{other.name}</span>
          {tag && (
            <span className="topo-pick-tag">
              <tag.Icon size={12} />
              <span>{tag.label}</span>
            </span>
          )}
        </button>
        {page && (
          <Button size="sm" variant="quiet" icon={ChevronRightIcon} label={`Open ${page.kind} ${other.name}`} to={page.path + search} />
        )}
      </div>
      {(notes.length > 0 || inPods || e.optional) && (
        <p className="topo-rel-notes">
          {[notes.slice(0, 3).join(", ") + (notes.length > 3 ? `, +${notes.length - 3} more` : ""), inPods, e.optional && "optional"]
            .filter(Boolean)
            .join(" · ")}
        </p>
      )}
      {fields.length > 0 && (
        <details className="topo-rel-fields">
          <summary>Read from {fields.length === 1 ? "1 field" : `${fields.length} fields`}</summary>
          <ul>
            {fields.map((f) => (
              <li key={f}>
                <code>{f}</code>
              </li>
            ))}
          </ul>
        </details>
      )}
    </li>
  );
}

function unique(list) {
  return [...new Set(list)];
}
