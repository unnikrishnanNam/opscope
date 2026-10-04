import { useEffect, useState } from "react";
import { Outlet, useLocation, useMatch } from "react-router";
import { useClusters } from "../clusters.jsx";
import { useCommandContext } from "../commands/context.js";
import { hasModKey } from "../platform.js";
import Button from "./Button.jsx";
import { Callout, EmptyState } from "./Callout.jsx";
import CommandPalette from "./CommandPalette.jsx";
import { Drawer } from "./Dialog.jsx";
import { Spinner } from "./Loading.jsx";
import Sidebar from "./Sidebar.jsx";
import TopBar from "./TopBar.jsx";
import Toaster from "./Toast.jsx";
import { ClustersIcon, RefreshIcon } from "./icons.jsx";
import "./Layout.css";

// The frame around every page: sidebar, top bar, content and the command
// palette. It works out which cluster is selected (from the URL) and
// whether it answers.
export default function Layout() {
  // On /c/<id>/<rest>, match.params is { clusterId, "*": rest }.
  const match = useMatch("/c/:clusterId/*");
  const clusterId = match?.params.clusterId;
  const pagePath = match?.params["*"];
  const { pathname } = useLocation();
  const [menuOpen, setMenuOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);

  const { data: clusters, loading, statuses, recheck } = useClusters();
  const cluster = clusters?.find((c) => c.id === clusterId);
  // Wait for a reload to finish: a cluster that was just added isn't in the old list.
  const unknownCluster = clusterId && clusters && !cluster && !loading;
  // GET /api/clusters/<id>: the version, or why it doesn't answer. Undefined until it has.
  const status = cluster ? statuses[cluster.id] : undefined;

  // Picking a page in the drawer closes it.
  useEffect(() => setMenuOpen(false), [pathname]);

  // ⌘K (Ctrl+K elsewhere) opens the palette from anywhere, even while
  // typing in a field, and closes it again. Not on top of another dialog
  // (a confirm, the drawer): that one has to be dealt with first.
  useEffect(() => {
    function onKeyDown(event) {
      if (event.key.toLowerCase() !== "k" || !hasModKey(event) || event.altKey || event.shiftKey) return;
      if (document.querySelector("dialog[open]:not(.palette)")) return;
      event.preventDefault();
      setPaletteOpen((open) => !open);
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, []);

  // What the palette's commands know: cluster, page, namespace, ...
  const commandContext = useCommandContext({ cluster, status, pagePath });

  const sidebarCluster = cluster?.id ?? clusters?.[0]?.id;

  return (
    <div className="shell">
      {/* First thing the keyboard reaches: jump past the sidebar's links. */}
      <a className="skip-link" href="#content">
        Skip to content
      </a>
      <aside className="shell-sidebar">
        <Sidebar clusterId={sidebarCluster} />
      </aside>
      {/* Narrow screens: the same sidebar in a drawer, behind the top bar's menu button. */}
      <Drawer open={menuOpen} onClose={() => setMenuOpen(false)} label="Navigation">
        {menuOpen && <Sidebar clusterId={sidebarCluster} />}
      </Drawer>

      <div className="shell-main">
        <TopBar
          cluster={cluster}
          status={status}
          pagePath={pagePath}
          onMenu={() => setMenuOpen(true)}
          onSearch={() => setPaletteOpen(true)}
        />
        <main className="shell-content" id="content" tabIndex={-1}>
          {cluster && status && !status.reachable && (
            <div className="shell-notice">
              <Callout
                tone="error"
                title={`Can't reach ${cluster.name}`}
                detail={status.detail}
                action={
                  <Button size="sm" icon={RefreshIcon} onClick={recheck}>
                    Try again
                  </Button>
                }
              >
                {status.error}
              </Callout>
            </div>
          )}

          {clusterId && !cluster && !unknownCluster ? (
            // Still loading the cluster list. Waiting here means every page
            // under /c/<id>/ can rely on `cluster` being set.
            <div className="shell-loading">
              <Spinner size={20} label="Loading clusters" />
            </div>
          ) : unknownCluster ? (
            <EmptyState
              icon={ClustersIcon}
              title="Cluster not found"
              action={
                <Button to="/clusters" variant="primary">
                  See all clusters
                </Button>
              }
            >
              There's no cluster called “{clusterId}”. It may have been removed.
            </EmptyState>
          ) : (
            // Pages read these with useOutletContext().
            <Outlet context={{ cluster, reachable: status?.reachable, status }} />
          )}
        </main>
      </div>
      <CommandPalette open={paletteOpen} onClose={() => setPaletteOpen(false)} ctx={commandContext} />
      <Toaster />
    </div>
  );
}
