import { useId } from "react";
import { Link, NavLink, useLocation } from "react-router";
import { useApi } from "../api.js";
import { nsQuery, sections } from "../sections.js";
import { Logo } from "./Logo.jsx";
import ThemeSwitch from "./ThemeSwitch.jsx";
import { ClustersIcon } from "./icons.jsx";
import "./Sidebar.css";

const HEALTH_CHECK_MS = 15_000;

// The navigation: logo, a link per page, and at the bottom the Clusters
// page, whether the Opscope server answers, and the theme switch.
// `clusterId` is the cluster the links point to; without one there is
// nothing to browse yet. Used as the left column on wide screens, and
// inside the drawer on narrow ones.
export default function Sidebar({ clusterId }) {
  const { pathname, search: fullSearch } = useLocation();
  // Keep ?ns=... when moving between pages, so the namespace stays selected.
  const search = nsQuery(fullSearch);

  return (
    <div className="sidebar">
      <Link to="/" className="sidebar-brand" aria-label="Opscope home">
        <Logo height={26} title="" />
      </Link>

      <nav className="sidebar-nav" aria-label="Pages">
        {clusterId ? (
          sections.map((section) => (
            <NavGroup key={section.group} title={section.group}>
              {section.items.map((item) => {
                const to = `/c/${clusterId}/${item.path}`;
                // The current page's link (or the list a detail page belongs to).
                const current = pathname === to || pathname.startsWith(to + "/");
                return (
                  <li key={item.path}>
                    <NavLink
                      to={{ pathname: to, search }}
                      className="nav-link"
                      // In the drawer, focus starts on the current page.
                      data-autofocus={current || undefined}
                    >
                      <item.icon size={16} />
                      {item.label}
                    </NavLink>
                  </li>
                );
              })}
            </NavGroup>
          ))
        ) : (
          <p className="sidebar-empty">Add a cluster to start browsing.</p>
        )}
      </nav>

      <div className="sidebar-footer">
        <NavLink to="/clusters" className="nav-link">
          <ClustersIcon size={16} />
          Clusters
        </NavLink>
        <div className="sidebar-meta">
          <ServerStatus />
          <ThemeSwitch />
        </div>
      </div>
    </div>
  );
}

function NavGroup({ title, children }) {
  const id = useId();
  return (
    <div className="nav-group">
      <div className="nav-group-title" id={id}>
        {title}
      </div>
      <ul aria-labelledby={id}>{children}</ul>
    </div>
  );
}

// Whether the Opscope server answers, and its version. (This is about
// Opscope itself, not about a cluster.)
function ServerStatus() {
  const { data, error } = useApi("/health", { refreshMs: HEALTH_CHECK_MS });
  const [tone, label] = error
    ? ["bad", "Server unreachable"]
    : data
      ? ["ok", `Opscope ${data.version}`]
      : ["checking", "Checking server…"];
  return (
    <span className={`server-status server-status-${tone}`} role="status">
      <span className="server-status-dot" aria-hidden="true" />
      {label}
    </span>
  );
}
