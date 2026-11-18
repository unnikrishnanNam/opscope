import { age } from "../format.js";
import "./Evidence.css";

// What a signal was read from (contract 4): fields of the object, events,
// a few counts from its log, volume usage. Never a log line: logs give
// pattern names and counts only.

const SOURCE = { field: "Field", event: "Event", log: "Log", metric: "Measured" };

export default function Evidence({ evidence }) {
  if (!evidence?.length) return null;
  return (
    <ul className="evidence">
      {evidence.map((e, i) => (
        <li key={i} className="evidence-item">
          <span className="evidence-source">{SOURCE[e.source] ?? e.source}</span>
          <span className="evidence-what">{what(e)}</span>
        </li>
      ))}
    </ul>
  );
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
      {e.value && <span className="evidence-value">{e.value}</span>}
    </>
  );
}
