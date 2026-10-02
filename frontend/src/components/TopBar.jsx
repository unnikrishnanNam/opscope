import { useLocation } from "react-router";
import { allPages } from "../sections.js";
import HealthBadge from "./HealthBadge.jsx";

export default function TopBar() {
  const { pathname } = useLocation();
  const page = allPages.find((p) => p.path === pathname);

  return (
    <header className="topbar">
      <div className="crumbs">
        {page ? (
          <>
            <span className="crumb-group">{page.group}</span>
            <span className="crumb-sep">/</span>
            <span className="crumb-page">{page.label}</span>
          </>
        ) : (
          <span className="crumb-page">Not found</span>
        )}
      </div>

      <div className="topbar-right">
        {/* Replaced by the real cluster name and namespace picker in Phase 1. */}
        <span className="muted">No cluster connected</span>
        <HealthBadge />
      </div>
    </header>
  );
}
