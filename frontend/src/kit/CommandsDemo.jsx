import { useMemo, useRef, useState } from "react";
import { buildList, collect } from "../commands/list.js";
import { CommandsProvider, useCommands } from "../commands/registry.jsx";
import { allPages } from "../sections.js";
import Button from "../components/Button.jsx";
import CommandPalette from "../components/CommandPalette.jsx";
import { SearchInput } from "../components/Field.jsx";
import Highlight from "../components/Highlight.jsx";
import { ToastView } from "../components/Toast.jsx";
import { Checkbox } from "../components/Toggle.jsx";
import { toast } from "../toast.js";
import {
  FileTextIcon,
  LogsIcon,
  MonitorIcon,
  MoonIcon,
  PodsIcon,
  SearchIcon,
  SunIcon,
} from "../components/icons.jsx";
import * as sample from "./samples.js";

// The command registry and palette on /kit, with sample sources shaped like
// the real ones in commands/sources/ (pages, namespaces, theme, objects).

const sources = [
  {
    id: "pages",
    commands: allPages.map((page) => ({
      id: `go.${page.path}`,
      title: page.label,
      detail: page.group,
      group: "Go to",
      icon: page.icon,
      to: `/c/demo/${page.path}`,
    })),
  },
  {
    id: "theme",
    commands: [
      { id: "theme.system", title: "Follow the system theme", group: "Theme", icon: MonitorIcon, keywords: ["theme"] },
      { id: "theme.light", title: "Light theme", group: "Theme", icon: SunIcon },
      { id: "theme.dark", title: "Dark theme", group: "Theme", icon: MoonIcon },
    ],
  },
  {
    id: "objects",
    searchOnly: true,
    commands: sample.pods.map((pod) => ({
      id: `pods/${pod.namespace}/${pod.name}`,
      title: pod.name,
      detail: pod.namespace,
      group: "Pods",
      icon: PodsIcon,
      scope: ["pods", "pod", "po"],
    })),
  },
];

// As if these had been used last, so the empty box shows a "Recent" group.
const RECENT = ["pods/shop/checkout-api-6b7c9d8f4-mm2lz", "go.workloads/deployments"];

const EXAMPLES = ["", "dply", "sts", "po check", "api", "web 2", "kube", "theme", "zzz"];

function Matching() {
  const [query, setQuery] = useState("");
  const entries = useMemo(() => collect(sources, {}), []);
  const list = buildList({ entries, query, recent: RECENT });

  return (
    <div className="kit-stack">
      <SearchInput value={query} onChange={setQuery} label="Try a query" placeholder="Try a query…" />
      <div className="kit-row-items">
        {EXAMPLES.map((example) => (
          <Button key={example} size="sm" variant={example === query ? "secondary" : "quiet"} onClick={() => setQuery(example)}>
            {example ? <span className="mono">{example}</span> : "(empty)"}
          </Button>
        ))}
      </div>

      <p className="kit-muted">
        {list.total === list.items.length
          ? `${list.total} ${list.total === 1 ? "row" : "rows"}`
          : `${list.items.length} of ${list.total} rows`}
      </p>

      {list.groups.map((group) => (
        <div key={group.title}>
          <div className="kit-group-title">{group.title}</div>
          <ul className="kit-matches">
            {group.items.map(({ command, match, score }) => (
              <li key={command.id} className="kit-match">
                {command.icon && <command.icon size={16} />}
                <span className="kit-match-title">
                  <Highlight text={command.title} positions={match?.title} />
                </span>
                {command.detail && (
                  <span className="kit-match-detail">
                    <Highlight text={command.detail} positions={match?.detail} />
                  </span>
                )}
                {score !== undefined && <span className="kit-match-score">{score.toFixed(1)}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  );
}

/* --------------------------------------------------------------------------
   The palette
   -------------------------------------------------------------------------- */

const NAMESPACES = ["data", "default", "kube-system", "media", "monitoring", "shop", "topology-test"];

// Enough pods that a short query matches more than the 50 rows drawn.
const MANY_PODS = [
  ...sample.pods,
  ...Array.from({ length: 240 }, (_, i) => ({ name: `load-test-${String(i).padStart(3, "0")}`, namespace: "perf" })),
];

// Sources for one demo palette. Objects load slowly (and fail when asked
// to), so the loading and error lines can be seen.
function paletteSources(failRef) {
  return [
    {
      id: "pages",
      commands: allPages.map((page) => ({
        id: `go.${page.path}`,
        title: page.label,
        detail: page.group,
        group: "Go to",
        icon: page.icon,
        to: `/c/demo/${page.path}`,
        shortcut: { overview: "g o", "workloads/pods": "g p" }[page.path],
      })),
    },
    {
      id: "namespaces",
      commands: [
        {
          id: "namespace.switch",
          title: "Switch namespace…",
          group: "Namespace",
          items: (ctx) =>
            ["All namespaces", ...NAMESPACES].map((ns) => ({
              id: `namespace.${ns}`,
              title: ns,
              group: "Namespaces",
              run: () => ctx.say(`Namespace: ${ns}`),
            })),
        },
      ],
    },
    {
      id: "theme",
      commands: [
        { id: "theme.system", title: "Follow the system theme", icon: MonitorIcon },
        { id: "theme.light", title: "Light theme", icon: SunIcon },
        { id: "theme.dark", title: "Dark theme", icon: MoonIcon },
      ].map((c) => ({ ...c, group: "Theme", keywords: ["theme"], run: (ctx) => ctx.say(c.title) })),
    },
    {
      id: "objects",
      label: "objects",
      searchOnly: true,
      maxAge: 0, // load on every opening, so the checkbox takes effect at once
      load: () =>
        new Promise((resolve, reject) =>
          setTimeout(() => (failRef.current ? reject(new Error("the cluster didn't answer")) : resolve(MANY_PODS)), 900),
        ),
      commands: (ctx, pods) =>
        pods.map((pod) => ({
          id: `pods/${pod.namespace}/${pod.name}`,
          title: pod.name,
          detail: pod.namespace,
          group: "Pods",
          icon: PodsIcon,
          scope: ["pods", "pod", "po"],
          to: `/c/demo/workloads/pods/${pod.namespace}/${pod.name}`,
        })),
    },
  ];
}

function PaletteDemo() {
  const failRef = useRef(false);
  const [sources] = useState(() => paletteSources(failRef));
  return (
    <CommandsProvider sources={sources}>
      <PaletteDemoBody failRef={failRef} />
    </CommandsProvider>
  );
}

function PaletteDemoBody({ failRef }) {
  const [open, setOpen] = useState(false);
  const [said, setSaid] = useState("");
  const [fail, setFail] = useState(false);
  const [following, setFollowing] = useState(false);
  // Links aren't followed here; the line below says where they'd go.
  const ctx = useMemo(() => ({ navigate: (to) => setSaid(`Would open ${to}`), say: setSaid }), []);

  // What a page would register: these show first, and change with the page.
  useCommands(
    "kit-page",
    [
      {
        id: "logs.follow",
        title: following ? "Stop following logs" : "Follow logs",
        group: "This page",
        icon: LogsIcon,
        run: () => {
          setFollowing(!following);
          setSaid(following ? "Stopped following" : "Following logs");
        },
      },
      { id: "tab.yaml", title: "Open the YAML tab", group: "This page", icon: FileTextIcon, shortcut: "2", run: () => setSaid("Opened the YAML tab") },
    ],
    [following],
  );

  return (
    <div className="kit-stack">
      <div className="kit-row-items">
        <Button variant="primary" icon={SearchIcon} onClick={() => setOpen(true)}>
          Open the palette
        </Button>
        <Checkbox
          checked={fail}
          onChange={(value) => {
            failRef.current = value;
            setFail(value);
          }}
        >
          Loading objects fails
        </Checkbox>
      </div>
      <p className="kit-muted">{said || "Nothing run yet."}</p>
      <CommandPalette open={open} onClose={() => setOpen(false)} ctx={ctx} />
    </div>
  );
}

/* --------------------------------------------------------------------------
   Toasts
   -------------------------------------------------------------------------- */

function Toasts() {
  return (
    <div className="kit-stack">
      <div className="kit-row-items">
        <ToastView message="Copied the kubectl command" />
        <ToastView tone="error" message="Couldn't copy the name: the browser didn't allow it" />
      </div>
      <div className="kit-row-items">
        <Button size="sm" onClick={() => toast("Copied the kubectl command")}>
          Show a toast
        </Button>
        <Button size="sm" onClick={() => toast("Couldn't copy the name: the browser didn't allow it", { tone: "error" })}>
          Show a failure
        </Button>
      </div>
    </div>
  );
}

export const commandSections = [
  {
    id: "matching",
    title: "Command matching",
    about:
      "How the palette will find and rank commands: pages, theme commands and sample pods (which only show once something is typed). Matched letters are bold; the number is the score. With nothing typed, two commands are shown as recently used.",
    render: () => <Matching />,
  },
  {
    id: "palette",
    title: "Command palette",
    about:
      "Try it with the keyboard: type to search (pods appear after a moment, as they load), arrows to move, Enter to run, Cmd/Ctrl+Enter for a new tab, Escape to clear, go back and close. “Switch namespace…” opens a sub-list; Backspace in the empty box leaves it. The “This page” commands are registered with useCommands.",
    render: () => <PaletteDemo />,
  },
  {
    id: "toasts",
    title: "Toasts",
    about:
      "Short confirmations for things that happen out of sight, like a copy from the palette. One at a time at the bottom of the window, for 3 seconds; a new one replaces the last. The buttons show the real one, in the page's theme.",
    render: () => <Toasts />,
  },
];
