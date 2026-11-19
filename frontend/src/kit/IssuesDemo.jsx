import { EmptyState } from "../components/Callout.jsx";
import Evidence from "../components/Evidence.jsx";
import HealthBadge from "../components/HealthBadge.jsx";
import IssueList from "../components/IssueList.jsx";
import { SuccessIcon } from "../components/icons.jsx";
import { openIssues, resolvedIssues } from "./issueSamples.js";

// The Issues feed's parts on /kit, on sample answers, before the page uses them.

function Open() {
  const groups = openIssues();
  return <IssueList groups={groups} clusterId="kit" showNamespace open={groups[0].id} />;
}

function Resolved() {
  return <IssueList groups={resolvedIssues()} clusterId="kit" />;
}

function Health() {
  return (
    <div className="kit-row">
      {["healthy", "degraded", "failing", "unknown"].map((state) => (
        <HealthBadge key={state} health={{ state }} />
      ))}
    </div>
  );
}

function EvidenceDemo() {
  return <Evidence evidence={openIssues()[0].signals[0].evidence.concat(openIssues()[3].signals[0].evidence)} />;
}

function Empty() {
  return (
    <EmptyState icon={SuccessIcon} title="No open issues in shop">
      Every object Opscope checks looks healthy.
    </EmptyState>
  );
}

export const issueSections = [
  {
    id: "issues",
    title: "Issues",
    about:
      "The Issues feed's rows: one signal, or one type on several pods of a workload. The first is open, as a link to it (?signal=) opens it. Tags say when a signal is going away or wasn't checked since a gap.",
    render: () => <Open />,
  },
  {
    id: "issues-resolved",
    title: "Resolved issues",
    about: "The Resolved tab: what cleared in the last hour, with when.",
    render: () => <Resolved />,
  },
  {
    id: "health",
    title: "Health",
    about: "An object's health, as detail pages and the topology show it: an icon and a word, never colour alone.",
    render: () => <Health />,
  },
  {
    id: "evidence",
    title: "Evidence",
    about: "What a signal was read from: fields, events, counts from a log (never a line), measurements.",
    render: () => <EvidenceDemo />,
  },
  {
    id: "issues-empty",
    title: "No issues",
    about: "What the page says when nothing needs a look.",
    render: () => <Empty />,
  },
];
