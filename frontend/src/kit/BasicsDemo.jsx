import { useState } from "react";
import * as icons from "../components/icons.jsx";
import { AppIcon, Logo, LogoMark } from "../components/Logo.jsx";
import Button from "../components/Button.jsx";
import { Field, SearchInput, Select, Textarea, TextInput } from "../components/Field.jsx";
import { Checkbox, SegmentedControl, Switch } from "../components/Toggle.jsx";
import StatusBadge, { Fraction } from "../components/StatusBadge.jsx";
import { Kbd, Tag } from "../components/Tag.jsx";
import { Skeleton, SkeletonText, Spinner } from "../components/Loading.jsx";
import Tooltip from "../components/Tooltip.jsx";

// Basic components on /kit (phase R1). Hover and keyboard focus are real:
// point at things and press Tab to see them.

const { CopyIcon, MonitorIcon, MoonIcon, PlusIcon, RefreshIcon, SunIcon, TrashIcon, UploadIcon } = icons;

/* --------------------------------------------------------------------------
   Logo and icons
   -------------------------------------------------------------------------- */

function Brand() {
  return (
    <div className="kit-stack">
      <Row label="Logo">
        <Logo height={26} />
        <Logo height={40} />
      </Row>
      <Row label="Mark">
        <LogoMark size={24} />
        <LogoMark size={40} />
      </Row>
      <Row label="App icon (below 24 px)">
        <AppIcon size={16} />
        <AppIcon size={20} />
        <AppIcon size={32} />
      </Row>
      <Row label="Icon sizes">
        {[14, 16, 20, 24].map((size) => (
          <icons.PodsIcon key={size} size={size} />
        ))}
      </Row>
      <div className="kit-icons">
        {Object.entries(icons).map(([name, Icon]) => (
          <div key={name} className="kit-icon">
            <Icon />
            <span>{name.replace(/Icon$/, "")}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Buttons
   -------------------------------------------------------------------------- */

const VARIANTS = ["primary", "secondary", "quiet", "danger"];

function Buttons() {
  const [busy, setBusy] = useState(false);

  function pretendToWork() {
    setBusy(true);
    setTimeout(() => setBusy(false), 1500);
  }

  return (
    <div className="kit-stack">
      {VARIANTS.map((variant) => (
        <Row key={variant} label={variant}>
          <Button variant={variant}>Save</Button>
          <Button variant={variant} icon={variant === "danger" ? TrashIcon : RefreshIcon}>
            {variant === "danger" ? "Remove" : "Refresh"}
          </Button>
          <Button variant={variant} size="sm" icon={CopyIcon}>
            Copy
          </Button>
          <Button variant={variant} icon={PlusIcon} label="Add a cluster" />
          <Button variant={variant} size="sm" icon={RefreshIcon} label="Refresh" />
          <Button variant={variant} disabled>
            Disabled
          </Button>
          <Button variant={variant} loading>
            Loading
          </Button>
        </Row>
      ))}
      <Row label="Try it">
        <Button variant="primary" loading={busy} onClick={pretendToWork}>
          Test connection and save
        </Button>
        <Button to="/kit#buttons">Router link</Button>
        <Button href="https://kubernetes.io" variant="quiet" icon={icons.ExternalLinkIcon}>
          Plain link
        </Button>
        <Button to="/kit" disabled>
          Disabled link
        </Button>
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Form controls
   -------------------------------------------------------------------------- */

function FormControls() {
  const [name, setName] = useState("");
  const [filter, setFilter] = useState("crash");
  const [lines, setLines] = useState("500");

  return (
    <div className="kit-form">
      <Field label="Display name" hint="Shown in the cluster switcher.">
        <TextInput placeholder="e.g. Home lab" value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <Field label="Context" error="This kubeconfig has no contexts.">
        <TextInput defaultValue="kubernetes-admin@kubernetes" />
      </Field>
      <Field label="API server">
        <TextInput defaultValue="https://192.168.252.2:6443" disabled />
      </Field>
      <Field
        label="Kubeconfig"
        action={
          <Button variant="quiet" size="sm" icon={UploadIcon}>
            Load from file
          </Button>
        }
      >
        <Textarea mono rows={4} defaultValue={"apiVersion: v1\nkind: Config\nclusters:\n  - name: multipass"} />
      </Field>
      <Row label="Search">
        <SearchInput value={filter} onChange={setFilter} placeholder="Filter…" />
        <SearchInput size="sm" value={filter} onChange={setFilter} placeholder="Filter…" />
      </Row>
      <Row label="Select">
        <Select value={lines} onChange={(e) => setLines(e.target.value)}>
          {["100", "500", "2000"].map((n) => (
            <option key={n} value={n}>
              {n} lines
            </option>
          ))}
        </Select>
        <Select size="sm" value={lines} onChange={(e) => setLines(e.target.value)}>
          {["100", "500", "2000"].map((n) => (
            <option key={n} value={n}>
              {n} lines
            </option>
          ))}
        </Select>
        <Select size="sm" disabled>
          <option>Cluster-wide</option>
        </Select>
      </Row>
      <Row label="In a row">
        <SearchInput value="" onChange={() => {}} placeholder="Filter…" />
        <Select defaultValue="all">
          <option value="all">All namespaces</option>
        </Select>
        <Button icon={RefreshIcon}>Refresh</Button>
        <Button variant="primary">Save</Button>
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Toggles
   -------------------------------------------------------------------------- */

function Toggles() {
  const [previous, setPrevious] = useState(false);
  const [follow, setFollow] = useState(true);
  const [tab, setTab] = useState("summary");
  const [theme, setTheme] = useState("system");

  return (
    <div className="kit-stack">
      <Row label="Checkbox">
        <Checkbox checked={previous} onChange={setPrevious}>
          Previous run
        </Checkbox>
        <Checkbox checked onChange={() => {}}>
          Checked
        </Checkbox>
        <Checkbox checked={false} onChange={() => {}} disabled>
          Disabled
        </Checkbox>
      </Row>
      <Row label="Switch">
        <Switch checked={follow} onChange={setFollow}>
          Follow
        </Switch>
        <Switch checked={false} onChange={() => {}}>
          Wrap lines
        </Switch>
        <Switch checked onChange={() => {}} disabled>
          Disabled
        </Switch>
      </Row>
      <Row label="Segmented">
        <SegmentedControl
          label="View"
          value={tab}
          onChange={setTab}
          options={[
            { value: "summary", label: "Summary" },
            { value: "yaml", label: "YAML" },
            { value: "events", label: "Events" },
          ]}
        />
      </Row>
      <Row label="Small, with icons">
        <SegmentedControl
          label="Theme"
          size="sm"
          value={theme}
          onChange={setTheme}
          options={[
            { value: "system", label: "System", icon: MonitorIcon },
            { value: "light", label: "Light", icon: SunIcon },
            { value: "dark", label: "Dark", icon: MoonIcon },
          ]}
        />
        <SegmentedControl
          label="Theme"
          iconOnly
          value={theme}
          onChange={setTheme}
          options={[
            { value: "system", label: "System", icon: MonitorIcon },
            { value: "light", label: "Light", icon: SunIcon },
            { value: "dark", label: "Dark", icon: MoonIcon },
          ]}
        />
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Status and tags
   -------------------------------------------------------------------------- */

const STATUSES = [
  "Running",
  "Completed",
  "Ready",
  "Pending",
  "ContainerCreating",
  "Init:1/2",
  "Terminating",
  "CrashLoopBackOff",
  "ImagePullBackOff",
  "Init:Error",
  "OOMKilled",
  "Released",
  "",
];

function StatusAndTags() {
  return (
    <div className="kit-stack">
      <div className="kit-statuses">
        {STATUSES.map((s) => (
          <StatusBadge key={s} status={s} />
        ))}
      </div>
      <Row label="Plain markup">
        <span className="status status-warn">BackOff</span>
        <span className="status status-neutral">Scheduled</span>
      </Row>
      <Row label="Fraction">
        <Fraction have={3} want={3} />
        <Fraction have={1} want={3} />
        <Fraction have={0} want={1} />
      </Row>
      <Row label="Tag">
        <Tag>Pod</Tag>
        <Tag>init</Tag>
        <Tag title="Set with OPSCOPE_KUBECONFIG">Environment</Tag>
        <Tag mono>app.kubernetes.io/name=web</Tag>
      </Row>
      <Row label="Kbd">
        <span className="kit-muted">
          Press <Kbd>/</Kbd> to filter, <Kbd>Esc</Kbd> to clear, <Kbd>⌘</Kbd> <Kbd>K</Kbd> to jump
        </span>
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Loading
   -------------------------------------------------------------------------- */

function LoadingStates() {
  return (
    <div className="kit-stack">
      <Row label="Spinner">
        <Spinner size={14} />
        <Spinner />
        <Spinner size={24} label="Loading pods" />
        <span className="kit-muted kit-inline">
          <Spinner size={14} /> Connecting to multipass…
        </span>
      </Row>
      <Row label="Skeleton">
        <Skeleton width={120} />
        <Skeleton width={64} height={20} />
        <Skeleton width={28} height={28} radius="50%" />
      </Row>
      <div className="kit-skeleton-card">
        <Skeleton width="40%" height={16} />
        <SkeletonText lines={3} />
      </div>
      <div className="kit-skeleton-rows">
        {[70, 55, 80, 45].map((w, i) => (
          <div key={i} className="kit-skeleton-row">
            <Skeleton width={`${w}%`} />
            <Skeleton width={64} />
            <Skeleton width={40} />
          </div>
        ))}
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Tooltip
   -------------------------------------------------------------------------- */

function Tooltips() {
  const longName = "argocd-applicationset-controller-7bcd6d4dcc-2qsdj";
  return (
    <div className="kit-stack">
      <Row label="Icon buttons">
        <Button icon={RefreshIcon} label="Refresh" />
        <Button variant="quiet" icon={CopyIcon} label="Copy YAML" />
        <Button variant="danger" icon={TrashIcon} label="Remove this cluster" />
      </Row>
      <Row label="Cut-off text">
        <Tooltip label={longName}>
          <span className="kit-truncate" tabIndex={0}>
            {longName}
          </span>
        </Tooltip>
      </Row>
      <Row label="Long label">
        <Tooltip label="New pods won't be scheduled on this node until it's uncordoned with kubectl uncordon.">
          <Tag>Cordoned</Tag>
        </Tooltip>
      </Row>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Layout helpers for this page
   -------------------------------------------------------------------------- */

function Row({ label, children }) {
  return (
    <div className="kit-row">
      <span className="kit-row-label">{label}</span>
      <div className="kit-row-items">{children}</div>
    </div>
  );
}

export const basicSections = [
  { id: "brand", title: "Logo and icons", about: "The logo follows the text colour; the pupil stays orange. Icons are Lucide at a 1.6 px line.", render: () => <Brand /> },
  { id: "buttons", title: "Buttons", about: "Primary for the one main action on a page, secondary for the rest, quiet inside toolbars and rows, danger only for removing things.", render: () => <Buttons /> },
  { id: "forms", title: "Form controls", about: "Inputs, selects and buttons share one height, so they line up in a toolbar.", render: () => <FormControls /> },
  { id: "toggles", title: "Toggles", about: "Real checkboxes and radio buttons underneath: Space toggles, arrow keys move within a segmented control.", render: () => <Toggles /> },
  { id: "status", title: "Status and tags", about: "Healthy is calm (a dot, normal text). Problems get an icon and a colour, so they stand out without relying on colour alone.", render: () => <StatusAndTags /> },
  { id: "loading", title: "Loading", about: "A spinner for short waits on a control; skeletons shaped like the content for anything bigger.", render: () => <LoadingStates /> },
  { id: "tooltip", title: "Tooltip", about: "On hover (after a moment) and on keyboard focus. Escape or scrolling closes it.", render: () => <Tooltips /> },
];
