import { Link, useLocation } from "react-router";
import { age } from "../format.js";
import { detailPath, nsQuery } from "../sections.js";

// A table of Kubernetes events, newest first (the backend sorts them).
//   showObject     include the "Object" column (off on an object's own page)
//   showNamespace  prefix objects with their namespace (when listing across namespaces)
export default function EventsTable({ clusterId, events, showObject = true, showNamespace = false, emptyText }) {
  const search = nsQuery(useLocation().search);
  if (events.length === 0) return <p className="muted">{emptyText}</p>;

  return (
    // On narrow screens the table scrolls sideways inside its box instead of spilling out.
    <div className="card-scroll">
      <table className="table events-table">
        <thead>
          <tr>
            <th className="num">Last seen</th>
            {showObject && <th>Object</th>}
            <th>Reason</th>
            <th>Message</th>
            <th className="num">Count</th>
          </tr>
        </thead>
        <tbody>
          {events.map((e) => {
            const link = detailPath(clusterId, e.objectResource, e.namespace, e.objectName);
            return (
              <tr key={`${e.namespace}/${e.name}`}>
                <td className="num">{age(e.lastSeen)} ago</td>
                {showObject && (
                  <td className="mono">
                    {showNamespace && <span className="muted">{e.namespace}/</span>}
                    {link ? <Link to={link + search}>{e.object}</Link> : e.object}
                  </td>
                )}
                <td>
                  <span className={`status status-${e.type === "Warning" ? "warn" : "neutral"}`}>{e.reason}</span>
                </td>
                <td className="message" title={e.message}>
                  <div className="clamp-2">{e.message}</div>
                </td>
                <td className="num">×{e.count}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
