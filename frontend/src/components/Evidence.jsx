import { Link } from "react-router";
import { age } from "../format.js";
import { detailPath } from "../sections.js";
import "./Evidence.css";

// What a signal was read from (contract 4): fields of the object, events,
// a few counts from its log, volume usage. Never a log line: logs give
// pattern names and counts only.
//
// With `object` (the signal's resource) and `clusterId`, each source links
// to where it can be seen: a field to the object's YAML, an event to its
// events, a log finding to a pod's live logs. `search` is the query string
// links keep (the namespace).

const SOURCE = { field: "Field", event: "Event", log: "Log", metric: "Measured" };

export default function Evidence({ evidence, object, clusterId, search = "" }) {
  if (!evidence?.length) return null;
  return (
    <ul className="evidence">
      {evidence.map((e, i) => {
        const to = clusterId && object && whereToSee(e, object, clusterId, search);
        return (
          <li key={i} className="evidence-item">
            <span className="evidence-source">
              {to ? (
                <Link to={to} title={`See it on ${object.name}'s page`}>
                  {SOURCE[e.source] ?? e.source}
                </Link>
              ) : (
                (SOURCE[e.source] ?? e.source)
              )}
            </span>
            <span className="evidence-what">{what(e)}</span>
          </li>
        );
      })}
    </ul>
  );
}

// TABS: the detail page tab each source can be seen on. Logs only for pods,
// the one kind with a Logs tab; measurements have no tab.
const TABS = { field: "yaml", event: "events", log: "logs" };

// whereToSee is the link to an evidence's source on its object's page, or
// null when there's nothing to show it on.
export function whereToSee(e, object, clusterId, search) {
  const tab = TABS[e.source];
  const path = detailPath(clusterId, object.resource, object.namespace, object.name);
  if (!tab || !path || (tab === "logs" && object.resource !== "pods")) return null;
  const q = new URLSearchParams(search);
  q.set("tab", tab);
  return `${path}?${q}`;
}

function what(e) {
  if (e.event) {
    return (
      <>
        <span className="evidence-reason">{e.event.reason}</span>
        {e.event.count > 1 && ` ×${e.event.count.toLocaleString()}`}
        {e.event.lastSeen && <span className="evidence-when">, last {age(e.event.lastSeen)} ago</span>}
      </>
    );
  }
  if (e.pattern) {
    return (
      <>
        <span className="evidence-field">{e.field}</span>: {e.pattern.replaceAll("_", " ")}
        {e.matches > 0 && ` (${e.matches.toLocaleString()} ${e.matches === 1 ? "line" : "lines"})`}
      </>
    );
  }
  return (
    <>
      <span className="evidence-field">{e.field}</span>
      {/* A space, not a margin, so a narrow screen wraps between the two
          (in the value's font, so it's as wide as the field's). */}
      {e.value && <span className="evidence-value"> {e.value}</span>}
    </>
  );
}
