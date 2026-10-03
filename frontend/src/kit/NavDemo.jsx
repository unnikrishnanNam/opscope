import { useState } from "react";
import Button from "../components/Button.jsx";
import ClusterSwitcher from "../components/ClusterSwitcher.jsx";
import Combobox from "../components/Combobox.jsx";
import { ConfirmDialog, Dialog, Drawer } from "../components/Dialog.jsx";
import { Breadcrumbs, PageHeader } from "../components/PageHeader.jsx";
import { Menu } from "../components/Popover.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import { Tag } from "../components/Tag.jsx";
import ThemeSwitch from "../components/ThemeSwitch.jsx";
import { Logo } from "../components/Logo.jsx";
import {
  CopyIcon,
  ExternalLinkIcon,
  MenuIcon,
  NodesIcon,
  OverviewIcon,
  PodsIcon,
  RefreshIcon,
  TrashIcon,
} from "../components/icons.jsx";

// Navigation and overlay components on /kit (phase R3).

const NAMESPACES = [
  "argocd",
  "calico-apiserver",
  "calico-system",
  "data",
  "default",
  "ingress-nginx",
  "kube-node-lease",
  "kube-public",
  "kube-system",
  "media",
  "monitoring",
  "nginx-gateway",
  "opscope",
  "shop",
  "tigera-operator",
  "topology-test",
  "a-namespace-with-a-name-long-enough-to-need-cutting-off",
];

const CLUSTERS = [
  { id: "multipass", name: "multipass", status: { reachable: true, version: "v1.31.14" } },
  { id: "opscope-test", name: "opscope-test", status: { reachable: true, version: "v1.35.0" } },
  { id: "staging", name: "staging (old VPN)", status: { reachable: false } },
  { id: "new", name: "just-added" }, // still being checked
];

/* --------------------------------------------------------------------------
   Pickers and menus
   -------------------------------------------------------------------------- */

function Pickers() {
  const [ns, setNs] = useState("");
  const [cluster, setCluster] = useState("multipass");
  const [lines, setLines] = useState("500");
  const [said, setSaid] = useState("");

  return (
    <div className="kit-stack">
      <Row label="Namespace">
        <Combobox
          label="Namespace"
          showLabel
          size="sm"
          noun="namespaces"
          value={ns}
          onChange={setNs}
          options={[{ value: "", label: "All namespaces" }, ...NAMESPACES.map((n) => ({ value: n, label: n }))]}
        />
        <Combobox
          label="Namespace"
          showLabel
          size="sm"
          value="cluster-wide"
          options={[{ value: "cluster-wide", label: "Cluster-wide" }]}
          onChange={() => {}}
          disabled
          title="This page isn't limited to a namespace"
        />
      </Row>
      <Row label="Cluster">
        <ClusterSwitcher clusters={CLUSTERS} value={cluster} onChange={setCluster} manageTo="/kit#pickers" />
      </Row>
      <Row label="Short list">
        <Combobox
          label="Log lines"
          value={lines}
          onChange={setLines}
          options={["100", "500", "2000"].map((n) => ({ value: n, label: `Last ${n} lines` }))}
        />
      </Row>
      <Row label="Menu">
        <Menu
          label="Cluster actions"
          icon={MenuIcon}
          items={[
            { label: "Copy server address", icon: CopyIcon, onSelect: () => setSaid("Copied the address") },
            { label: "Open in a new tab", icon: ExternalLinkIcon, onSelect: () => setSaid("Opened") },
            { label: "Remove cluster", icon: TrashIcon, danger: true, onSelect: () => setSaid("Removed (not really)") },
          ]}
        />
        <Menu
          label="More"
          variant="secondary"
          align="end"
          items={[{ label: "Refresh", icon: RefreshIcon, onSelect: () => setSaid("Refreshed") }]}
        >
          More
        </Menu>
        <span className="kit-muted">{said}</span>
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Breadcrumbs and page headers
   -------------------------------------------------------------------------- */

function Headers() {
  return (
    <div className="kit-stack">
      <Breadcrumbs
        items={[
          { label: "Workloads" },
          { label: "Pods", to: "/kit#headers" },
          { label: "checkout-api-6b7c9d8f4-mm2lz" },
        ]}
      />
      <Breadcrumbs
        items={[
          { label: "Workloads" },
          { label: "Pods", to: "/kit#headers" },
          { label: "thumbnail-worker-with-a-rather-long-generated-name-6f9d8c7b5-zz9q1" },
        ]}
      />
      <div className="kit-boxed kit-pad">
        <PageHeader
          title="Pods"
          description="Running containers, their status, restarts and the node they run on."
          meta="Updated 10:42:07"
          actions={
            <Button variant="quiet" size="sm" icon={RefreshIcon}>
              Refresh
            </Button>
          }
        />
      </div>
      <div className="kit-boxed kit-pad">
        <PageHeader
          before={<Tag>Pod</Tag>}
          title="checkout-api-6b7c9d8f4-mm2lz"
          after={<StatusBadge status="CrashLoopBackOff" />}
          description={
            <>
              Namespace <strong>shop</strong> · created 5h ago
            </>
          }
        />
      </div>
      <div className="kit-boxed kit-pad">
        <PageHeader
          title="multipass"
          description={
            <>
              Kubernetes v1.31.14 · 17 namespaces · <span className="mono">https://192.168.252.2:6443</span>
            </>
          }
          meta="Updated 10:42:07"
        />
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Theme, drawer and dialogs
   -------------------------------------------------------------------------- */

function Overlays() {
  const [drawer, setDrawer] = useState(false);
  const [info, setInfo] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);

  function remove() {
    setBusy(true);
    setError(null);
    // Pretend the first try fails, so the error state can be seen.
    setTimeout(() => {
      setBusy(false);
      setError(
        Object.assign(new Error("The cluster couldn't be removed: the data folder is read-only."), {
          detail: "remove /data/clusters/staging.yaml: read-only file system",
        }),
      );
    }, 1200);
  }

  return (
    <div className="kit-stack">
      <Row label="Theme switch">
        <ThemeSwitch />
        <ThemeSwitch showLabels />
      </Row>
      <Row label="Drawer">
        <Button icon={MenuIcon} label="Open navigation" onClick={() => setDrawer(true)} />
      </Row>
      <Row label="Dialogs">
        <Button onClick={() => setInfo(true)}>Open a dialog</Button>
        <Button variant="danger" icon={TrashIcon} onClick={() => setConfirm(true)}>
          Remove cluster
        </Button>
      </Row>

      <Drawer open={drawer} onClose={() => setDrawer(false)} label="Navigation">
        <div className="kit-drawer">
          <Logo height={26} />
          {[
            ["Overview", OverviewIcon],
            ["Nodes", NodesIcon],
            ["Pods", PodsIcon],
          ].map(([label, Icon]) => (
            <a key={label} href="#overlays" className="kit-drawer-link" onClick={() => setDrawer(false)}>
              <Icon size={16} />
              {label}
            </a>
          ))}
          <p className="kit-muted">Escape, a click outside, or picking a link closes it.</p>
        </div>
      </Drawer>

      <Dialog
        open={info}
        onClose={() => setInfo(false)}
        title="Reading Secrets"
        actions={
          <Button variant="primary" onClick={() => setInfo(false)}>
            Got it
          </Button>
        }
      >
        <p>
          Anyone who can open Opscope can read every Secret its service account can read. Keep it behind a port-forward,
          or put something that adds a login in front of it.
        </p>
      </Dialog>

      <ConfirmDialog
        open={confirm}
        onCancel={() => {
          setConfirm(false);
          setError(null);
        }}
        onConfirm={remove}
        busy={busy}
        error={error}
        title="Remove staging?"
        confirmLabel="Remove cluster"
      >
        Opscope will delete its saved kubeconfig. The cluster itself isn't touched, and you can add it again later.
      </ConfirmDialog>
    </div>
  );
}

function Row({ label, children }) {
  return (
    <div className="kit-row">
      <span className="kit-row-label">{label}</span>
      <div className="kit-row-items">{children}</div>
    </div>
  );
}

export const navSections = [
  {
    id: "pickers",
    title: "Pickers and menus",
    about: "Type to filter long lists; arrow keys and Enter pick; Escape closes and returns to the button.",
    render: () => <Pickers />,
  },
  {
    id: "headers",
    title: "Breadcrumbs and page headers",
    about: "Long names get “…” in the breadcrumb and wrap in the page title.",
    render: () => <Headers />,
  },
  {
    id: "overlays",
    title: "Theme, drawer and dialogs",
    about: "Built on the browser's <dialog>: focus stays inside, Escape closes, focus returns to the button.",
    render: () => <Overlays />,
  },
];
