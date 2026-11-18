import { useEffect, useId, useState } from "react";
import { Link } from "react-router";
import { age } from "../format.js";
import { detailPath } from "../sections.js";
import Evidence from "./Evidence.jsx";
import { ChevronDownIcon, ChevronRightIcon, ErrorIcon, InfoIcon, SuccessIcon, WarningIcon } from "./icons.jsx";
import "./IssueList.css";

// IssueList: the Issues feed's rows. Each row is one signal, or one type of
// signal on several pods of one workload ("Crash loop · 3 pods of
// Deployment web"). A row opens to show what it was read from, or each pod.
//
//   groups         from GET /issues (see docs/api.md)
//   clusterId      for links to the objects' pages
//   search         the query string links keep (the namespace)
//   showNamespace  name each object's namespace (across namespaces)
//   open           the ID of a row to open and scroll to (?signal=)

export const SEVERITY = {
  critical: { icon: ErrorIcon, label: "Critical", tone: "bad" },
  warning: { icon: WarningIcon, label: "Warning", tone: "warn" },
  info: { icon: InfoIcon, label: "Info", tone: "info" },
};

export default function IssueList({ groups, clusterId, search = "", showNamespace = false, open }) {
  const [opened, setOpened] = useState(() => new Set(open ? [open] : []));
  useEffect(() => {
    if (!open) return;
    setOpened((s) => new Set(s).add(open));
    document.getElementById(rowId(open))?.scrollIntoView({ block: "center" });
  }, [open]);
  const toggle = (id) =>
    setOpened((s) => {
      const next = new Set(s);
      next.has(id) ? next.delete(id) : next.add(id);
      return next;
    });

  return (
    <ul className="issue-list">
      {groups.map((g) => (
        <IssueRow
          key={g.id}
          group={g}
          clusterId={clusterId}
          search={search}
          showNamespace={showNamespace}
          isOpen={opened.has(g.id)}
          onToggle={() => toggle(g.id)}
          highlighted={g.id === open}
        />
      ))}
    </ul>
  );
}

const rowId = (id) => `issue-${id.replaceAll(":", "-")}`;

function IssueRow({ group: g, clusterId, search, showNamespace, isOpen, onToggle, highlighted }) {
  const panelId = useId();
  const sev = SEVERITY[g.severity] ?? SEVERITY.info;
  const one = g.signals.length === 1 ? g.signals[0] : null;
  const resolved = g.state === "resolved";
  const Icon = resolved ? SuccessIcon : sev.icon;
  const stale = g.signals.some((s) => s.stale);
  const resolving = !resolved && g.signals.every((s) => s.state === "resolving");
  const evidenceCount = one ? one.evidence.length : g.signals.length;

  return (
    <li id={rowId(g.id)} className={`issue ${resolved ? "issue-resolved" : ""} ${highlighted ? "issue-highlighted" : ""}`}>
      <span className={`issue-mark issue-mark-${sev.tone}`}>
        <Icon size={16} />
        <span className="visually-hidden">{resolved ? `Resolved, was ${sev.label.toLowerCase()}:` : `${sev.label}:`}</span>
      </span>
      <div className="issue-main">
        <div className="issue-head">
          <span className="issue-title">{g.title}</span>
          <span className="issue-object">
            {one ? (
              <ObjectLink r={one.resource} clusterId={clusterId} search={search} showNamespace={showNamespace} />
            ) : (
              <>
                {g.signals.length} pods of{" "}
                <ObjectLink r={g.owner} clusterId={clusterId} search={search} showNamespace={showNamespace} />
              </>
            )}
          </span>
          {resolving && <span className="issue-tag">going away</span>}
          {stale && (
            <span className="issue-tag" title="Opscope missed some changes to it and hasn't read it again yet">
              not checked since a gap
            </span>
          )}
        </div>
        <p className="issue-message">{one ? one.message : g.signals[0].message}</p>
        <button type="button" className="issue-toggle" aria-expanded={isOpen} aria-controls={panelId} onClick={onToggle}>
          {isOpen ? <ChevronDownIcon size={14} /> : <ChevronRightIcon size={14} />}
          {one ? `Read from (${evidenceCount})` : `Each pod (${evidenceCount})`}
        </button>
        {isOpen && (
          <div id={panelId} className="issue-panel">
            {one ? (
              <Evidence evidence={one.evidence} />
            ) : (
              <ul className="issue-pods">
                {g.signals.map((s) => (
                  <li key={s.id}>
                    <div className="issue-pod-head">
                      <ObjectLink r={s.resource} clusterId={clusterId} search={search} />
                      <span className="issue-pod-message">{s.message}</span>
                    </div>
                    <Evidence evidence={s.evidence} />
                  </li>
                ))}
              </ul>
            )}
          </div>
        )}
      </div>
      <div className="issue-meta">
        {resolved ? (
          <span title={new Date(latest(g.signals)).toLocaleString()}>resolved {age(latest(g.signals))} ago</span>
        ) : (
          <span title={`Since ${new Date(g.firstSeen).toLocaleString()}`}>for {age(g.firstSeen)}</span>
        )}
        {one && one.count > 1 && <span className="issue-count">{one.count.toLocaleString()}×</span>}
      </div>
    </li>
  );
}

// latest is when the last of a group's signals resolved.
function latest(signals) {
  return signals.map((s) => s.resolved).sort().at(-1);
}

// ObjectLink: "Deployment web", linked to its page when Opscope has one.
function ObjectLink({ r, clusterId, search, showNamespace }) {
  const link = detailPath(clusterId, r.resource, r.namespace, r.name);
  const name = (
    <>
      {showNamespace && r.namespace && <span className="issue-namespace">{r.namespace}/</span>}
      {r.name}
    </>
  );
  return (
    <span className="issue-ref">
      <span className="issue-kind">{r.kind}</span> {link ? <Link to={link + search}>{name}</Link> : name}
    </span>
  );
}
