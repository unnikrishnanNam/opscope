import { Link, NavLink, useLocation } from "react-router";
import { nsQuery, sections } from "../sections.js";
import HealthBadge from "./HealthBadge.jsx";

// Left-hand navigation. `clusterId` is the cluster the links point to;
// without one there is nothing to browse yet.
export default function Sidebar({ clusterId }) {
  // Keep ?ns=... when moving between pages, so the namespace stays selected.
  const search = nsQuery(useLocation().search);

  return (
    <aside className="sidebar">
      <Link to="/" className="brand">
        <img src="/favicon.svg" alt="" width="22" height="22" />
        <span>OpScope</span>
      </Link>

      <nav className="sidebar-nav">
        {clusterId ? (
          sections.map((section) => (
            <div key={section.group} className="nav-group">
              <div className="nav-group-title">{section.group}</div>
              {section.items.map((item) => (
                // NavLink adds the "active" class when its path matches the URL.
                <NavLink key={item.path} to={{ pathname: `/c/${clusterId}/${item.path}`, search }} className="nav-link">
                  {item.label}
                </NavLink>
              ))}
            </div>
          ))
        ) : (
          <p className="sidebar-empty">Add a cluster to start browsing.</p>
        )}
      </nav>

      <div className="sidebar-footer">
        <NavLink to="/clusters" end className="nav-link">
          Clusters
        </NavLink>
        <HealthBadge />
      </div>
    </aside>
  );
}
