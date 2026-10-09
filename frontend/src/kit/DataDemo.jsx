import { useEffect, useState } from "react";
import { columns } from "../columns.jsx";
import { bytes, cpu } from "../format.js";
import Button from "../components/Button.jsx";
import { Card, Section } from "../components/Card.jsx";
import { Callout, EmptyState } from "../components/Callout.jsx";
import CodeBlock from "../components/CodeBlock.jsx";
import DataTable from "../components/DataTable.jsx";
import EventList from "../components/EventList.jsx";
import { Fact, FactGrid, KeyValueList } from "../components/Facts.jsx";
import LogView from "../components/LogView.jsx";
import PodStatusBar from "../components/PodStatusBar.jsx";
import StackBar from "../components/StackBar.jsx";
import StatusBadge from "../components/StatusBadge.jsx";
import Tabs from "../components/Tabs.jsx";
import { SegmentedControl, Switch } from "../components/Toggle.jsx";
import { MetricsUnavailable, Sparkline, UsageBar } from "../components/Usage.jsx";
import { ClustersIcon, PlusIcon, PodsIcon, RefreshIcon } from "../components/icons.jsx";
import * as sample from "./samples.js";

// Data display components on /kit, all fed with sample data.

/* --------------------------------------------------------------------------
   Cards and tiles
   -------------------------------------------------------------------------- */

function CardsAndTiles() {
  return (
    <div className="kit-stack">
      <Card
        title="Pods by status"
        aside="52 pods"
        action={
          <Button variant="quiet" size="sm" to="/kit#cards">
            View all
          </Button>
        }
      >
        <PodStatusBar byStatus={sample.podsByStatus} total={52} />
      </Card>
      <Card title="Recent warnings" aside="newest 2 of 4" flush>
        <div className="kit-flush-body">
          <EventList clusterId="lab" events={sample.events.slice(0, 2)} showNamespace />
        </div>
      </Card>
      <Section title="Containers" aside="2">
        <p className="kit-muted">A Section is a titled block without a box, for detail pages.</p>
      </Section>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Table
   -------------------------------------------------------------------------- */

// The real pod columns from columns.jsx, so the table is tested on what the
// app will show.
function Table() {
  const [density, setDensity] = useState("comfortable");
  const [state, setState] = useState("data");
  const rows = { data: sample.pods, loading: null, empty: [] }[state];

  return (
    <div className="kit-stack">
      <div className="kit-row-items">
        <SegmentedControl
          label="State"
          size="sm"
          value={state}
          onChange={setState}
          options={[
            { value: "data", label: "Data" },
            { value: "loading", label: "Loading" },
            { value: "empty", label: "Empty" },
          ]}
        />
        <SegmentedControl
          label="Density"
          size="sm"
          value={density}
          onChange={setDensity}
          options={[
            { value: "comfortable", label: "Comfortable" },
            { value: "compact", label: "Compact" },
          ]}
        />
      </div>
      <DataTable
        columns={columns.pods}
        rows={rows}
        noun="pods"
        density={density}
        maxHeight="420px"
        linkTo={(row) => `/kit?pod=${encodeURIComponent(row.name)}#table`}
        emptyIcon={PodsIcon}
        emptyText="No pods in this namespace."
        toolbar={
          <>
            <span>Updated 10:42:07</span>
            <Button variant="quiet" size="sm" icon={RefreshIcon}>
              Refresh
            </Button>
          </>
        }
      />
    </div>
  );
}

/* --------------------------------------------------------------------------
   Tabs
   -------------------------------------------------------------------------- */

function TabsDemo() {
  const [tab, setTab] = useState("summary");
  return (
    <Tabs
      label="Pod details"
      value={tab}
      onChange={setTab}
      tabs={[
        { value: "summary", label: "Summary" },
        { value: "yaml", label: "YAML" },
        { value: "events", label: "Events", count: sample.events.length },
        { value: "logs", label: "Logs" },
      ]}
    >
      <p className="kit-muted">The “{tab}” panel. Click a tab, or focus one and use the arrow keys, Home and End.</p>
    </Tabs>
  );
}

/* --------------------------------------------------------------------------
   Callouts and empty states
   -------------------------------------------------------------------------- */

function Callouts() {
  return (
    <div className="kit-stack">
      <Callout title="Gateway API isn't installed">
        Install the Gateway API CRDs and a controller to see Gateways and HTTPRoutes.
      </Callout>
      <Callout tone="warning" title="Usage is a few minutes old">
        metrics-server hasn't answered since 10:38. The numbers shown are from then.
      </Callout>
      <Callout
        tone="error"
        title="Can't reach multipass"
        detail='Get "https://192.168.252.2:6443/version?timeout=10s": dial tcp 192.168.252.2:6443: connect: no route to host'
        action={
          <Button size="sm" icon={RefreshIcon}>
            Try again
          </Button>
        }
      >
        The API server didn’t answer. Check that the cluster is running and that this machine can reach its address.
      </Callout>
      <Callout compact>Usage needs metrics-server, which isn't installed on this cluster.</Callout>
      <MetricsUnavailable />
      <div className="kit-boxed">
        <EmptyState
          icon={ClustersIcon}
          title="No clusters yet"
          action={
            <Button variant="primary" icon={PlusIcon}>
              Add a cluster
            </Button>
          }
        >
          Add one with a kubeconfig, or start Opscope with <code>OPSCOPE_KUBECONFIG</code>.
        </EmptyState>
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Code and logs
   -------------------------------------------------------------------------- */

function Code() {
  return (
    <div className="kit-stack">
      <CodeBlock
        code={sample.deploymentYaml}
        language="yaml"
        title="Read-only. managedFields are left out."
        maxHeight="360px"
      />
      <CodeBlock
        code={"listen 8080;\nserver_name shop.example.com;\n\nlocation / {\n  proxy_pass http://web;\n}"}
        lineNumbers={false}
        title="nginx.conf"
      />
    </div>
  );
}

function Logs() {
  const [live, setLive] = useState(false);
  const [wrap, setWrap] = useState(true);
  const [text, setText] = useState(sample.logText);

  // While "live", add a line every second, like a followed log stream.
  useEffect(() => {
    if (!live) return;
    let i = 120;
    const timer = setInterval(() => setText((t) => t + sample.logLine(i++) + "\n"), 1000);
    return () => clearInterval(timer);
  }, [live]);

  return (
    <div className="kit-stack">
      <div className="kit-row-items">
        <Switch checked={live} onChange={setLive}>
          Follow
        </Switch>
        <Switch checked={wrap} onChange={setWrap}>
          Wrap lines
        </Switch>
        <span className="kit-muted">Scroll up while following to see “Jump to newest”.</span>
      </div>
      <LogView text={text} live={live} wrap={wrap} height="320px" />
      <div className="kit-two">
        <LogView text="" loading height="120px" />
        <LogView text="" height="120px" />
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Usage and charts
   -------------------------------------------------------------------------- */

function Charts() {
  const { total, history, shortHistory } = sample.clusterUsage;
  return (
    <div className="kit-stack">
      <Row label="Usage bar">
        {[3, 61, 82, 95].map((pct) => (
          <UsageBar key={pct} used={pct} total={100} label={`${pct}% used`} />
        ))}
      </Row>
      <Row label="Sparkline">
        <Sparkline points={history} field="cpu" format={cpu} what="CPU" width={220} height={28} />
        <Sparkline points={history} field="memory" format={bytes} what="Memory" />
        <Sparkline points={shortHistory} field="cpu" format={cpu} what="CPU" />
      </Row>
      <Row label="Node usage">
        {sample.nodes.map((n) => (
          <span key={n.name} className="usage-cell">
            <UsageBar used={n.usage.cpu} total={n.usage.cpuAllocatable} label={`${cpu(n.usage.cpu)} of 2 cores`} />
            <Sparkline points={n.usage.history} field="cpu" format={cpu} what="CPU" />
          </span>
        ))}
      </Row>
      <Row label="Cluster">
        <span className="kit-muted">
          CPU {cpu(total.cpu)} of {cpu(total.cpuAllocatable)}, memory {bytes(total.memory)} of{" "}
          {bytes(total.memoryAllocatable)}
        </span>
      </Row>
      <StackBar
        noun="nodes"
        segments={[
          { label: "Ready", value: 2, tone: "ok" },
          { label: "NotReady", value: 1, tone: "bad" },
        ]}
      />
      <StackBar noun="jobs" segments={[{ label: "Complete", value: 4, tone: "ok" }]} />
    </div>
  );
}

/* --------------------------------------------------------------------------
   Facts, labels and events
   -------------------------------------------------------------------------- */

function Facts() {
  return (
    <div className="kit-stack">
      <FactGrid>
        <Fact label="Status">
          <StatusBadge status="CrashLoopBackOff" />
        </Fact>
        <Fact label="Node">worker-1</Fact>
        <Fact label="Pod IP" mono>
          10.244.1.17
        </Fact>
        <Fact label="QoS class">Burstable</Fact>
        <Fact label="Service account">checkout-api</Fact>
        <Fact label="Owned by">
          <a href="#facts">ReplicaSet/checkout-api-6b7c9d8f4</a>
        </Fact>
      </FactGrid>
      <Section title="Labels">
        <KeyValueList values={sample.labels} empty="No labels." />
      </Section>
      <Section title="Annotations">
        <KeyValueList values={sample.annotations} empty="No annotations." />
      </Section>
      <Section title="Empty">
        <KeyValueList values={{}} empty="No annotations." />
      </Section>
    </div>
  );
}

function Events() {
  return (
    <div className="kit-stack">
      <EventList clusterId="lab" events={sample.events} showNamespace />
      <EventList clusterId="lab" events={[]} emptyText="No warnings. Nothing has complained recently." />
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

export const dataSections = [
  {
    id: "cards",
    title: "Cards",
    about: "Boxes for the overview, and section titles for detail pages.",
    render: () => <CardsAndTiles />,
  },
  {
    id: "table",
    title: "Table",
    about: "The real pod columns. Click anywhere on a row to open it; the header stays put while the table scrolls.",
    render: () => <Table />,
  },
  {
    id: "tabs",
    title: "Tabs",
    about: "The open tab gets the orange line: it's where you are.",
    render: () => <TabsDemo />,
  },
  {
    id: "callouts",
    title: "Callouts and empty states",
    about:
      "Info for missing optional features, warning for something stale, error for failures (with the raw error folded away).",
    render: () => <Callouts />,
  },
  {
    id: "code",
    title: "Code",
    about: "Line numbers aren't copied. YAML gets quiet colours for keys, quoted strings, literals and comments.",
    render: () => <Code />,
  },
  {
    id: "logs",
    title: "Logs",
    about: "Opens at the newest line and stays there while following, unless you scroll up to read.",
    render: () => <Logs />,
  },
  {
    id: "charts",
    title: "Usage and charts",
    about:
      "Neutral bars and lines; amber from 75%, red from 90%. Hover a sparkline for its low, high and current values.",
    render: () => <Charts />,
  },
  {
    id: "facts",
    title: "Facts and labels",
    about: "Long values start folded to three lines.",
    render: () => <Facts />,
  },
  {
    id: "events",
    title: "Events",
    about: "Newest first. Warnings get the icon; the count shows how often it happened.",
    render: () => <Events />,
  },
];
