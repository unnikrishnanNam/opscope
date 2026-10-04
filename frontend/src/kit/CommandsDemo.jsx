import { useMemo, useState } from "react";
import { buildList, collect } from "../commands/list.js";
import { allPages } from "../sections.js";
import Button from "../components/Button.jsx";
import { SearchInput } from "../components/Field.jsx";
import Highlight from "../components/Highlight.jsx";
import { MonitorIcon, MoonIcon, PodsIcon, SunIcon } from "../components/icons.jsx";
import * as sample from "./samples.js";

// The command registry's matching on /kit (phase P0), with sample sources
// shaped like the real ones that arrive in P2 and P3. It calls the same
// functions the palette will (collect and buildList), without React state
// or a cluster.

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
      keywords: ["pods", "pod", "po"],
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

export const commandSections = [
  {
    id: "matching",
    title: "Command matching",
    about:
      "How the palette will find and rank commands: pages, theme commands and sample pods (which only show once something is typed). Matched letters are bold; the number is the score. With nothing typed, two commands are shown as recently used.",
    render: () => <Matching />,
  },
];
