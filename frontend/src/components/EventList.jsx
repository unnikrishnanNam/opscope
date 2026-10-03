import { Link, useLocation } from "react-router";
import { age } from "../format.js";
import { detailPath, nsQuery } from "../sections.js";
import { WarningIcon } from "./icons.jsx";
import "./EventList.css";

// EventList: Kubernetes events, newest first (the backend sorts them).
// Each one shows what happened (reason), to what (object), the message,
// when it was last seen and how many times.
//   showObject     include the object (off on an object's own page)
//   showNamespace  prefix objects with their namespace (across namespaces)
export default function EventList({ clusterId, events, showObject = true, showNamespace = false, emptyText }) {
  const search = nsQuery(useLocation().search);
  if (events.length === 0) return <p className="event-list-empty">{emptyText}</p>;

  return (
    <ul className="event-list">
      {events.map((e) => {
        const warning = e.type === "Warning";
        const link = showObject && detailPath(clusterId, e.objectResource, e.namespace, e.objectName);
        return (
          <li key={`${e.namespace}/${e.name}`} className="event">
            <span className="event-mark">
              {warning ? <WarningIcon size={14} className="event-warning" /> : <span className="event-dot" />}
              <span className="visually-hidden">{warning ? "Warning:" : "Normal:"}</span>
            </span>
            <div className="event-main">
              <div className="event-head">
                <span className={`event-reason ${warning ? "event-reason-warning" : ""}`}>{e.reason}</span>
                {showObject && (
                  <span className="event-object">
                    {showNamespace && <span className="event-namespace">{e.namespace}/</span>}
                    {link ? <Link to={link + search}>{e.object}</Link> : e.object}
                  </span>
                )}
              </div>
              <p className="event-message">{e.message}</p>
            </div>
            <div className="event-meta">
              <time dateTime={e.lastSeen} title={new Date(e.lastSeen).toLocaleString()}>
                {age(e.lastSeen)} ago
              </time>
              {e.count > 1 && <span className="event-count">{e.count.toLocaleString()} times</span>}
            </div>
          </li>
        );
      })}
    </ul>
  );
}
