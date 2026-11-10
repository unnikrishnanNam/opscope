import { useRef } from "react";
import { describe, useLive } from "../live.js";
import { allPages } from "../sections.js";
import { usePopover } from "./Popover.jsx";
import { ErrorIcon, WarningIcon } from "./icons.jsx";
import "./LiveStatus.css";

// "Pods", "PersistentVolumeClaims", ... for a resource's API name. These
// kinds have no list page of their own; the last three are read only for
// the topology.
const OTHER_LABELS = {
  events: "Events",
  namespaces: "Namespaces",
  replicasets: "ReplicaSets",
  endpointslices: "EndpointSlices",
  serviceaccounts: "ServiceAccounts",
};
const labelOf = (resource) =>
  allPages.find((p) => p.resource === resource)?.label ?? OTHER_LABELS[resource] ?? resource;

// LiveStatus says how fresh the pages are: "Live", "Every 30 s",
// "Reconnecting", ... Clicking it explains, and lists the kinds that aren't
// live and why. A click rather than a hover, so touch and keyboard reach it.
//
//   info  from describe() in live.js: { label, tone, summary, groups }
//
// Nothing moves: no pulse or spinner, so reduced motion needs no special
// case. Like StatusBadge, a problem gets an icon, not only a colour.
export default function LiveStatus({ info, align = "end" }) {
  const buttonRef = useRef(null);
  const popover = usePopover({
    onClose: ({ returnFocus }) => returnFocus && buttonRef.current?.focus(),
  });
  if (!info) return null;
  const Icon = info.tone === "bad" ? ErrorIcon : info.tone === "warn" ? WarningIcon : null;

  return (
    <div ref={popover.ref} className="popover-anchor live-status">
      <button
        ref={buttonRef}
        type="button"
        className={`live-status-button live-${info.tone}`}
        aria-label={`Updates: ${info.label}`}
        aria-expanded={popover.open}
        onClick={popover.toggle}
      >
        {Icon ? <Icon size={14} /> : <span className="live-dot" aria-hidden="true" />}
        <span className="live-status-label">{info.label}</span>
      </button>
      {popover.open && (
        <div className={`popover-panel popover-${align} live-status-panel`} role="group" aria-label="Updates">
          <p className="live-status-summary">{info.summary}</p>
          {info.groups.map((g) => (
            <div key={g.title + g.reason} className="live-status-group">
              <h3 className="live-status-group-title">{g.title}</h3>
              <p>{g.kinds.join(", ")}</p>
              {g.reason && <p className="live-status-reason">{sentence(g.reason)}</p>}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

// ClusterLiveStatus is LiveStatus for the cluster on screen. `kinds` are
// the page's (kindsOn in sections.js), so its label is about this page.
export function ClusterLiveStatus({ kinds }) {
  const { snapshot } = useLive();
  return <LiveStatus info={describe(snapshot, labelOf, kinds)} />;
}

// "watching isn't allowed" -> "Watching isn't allowed."
const sentence = (text) => text.charAt(0).toUpperCase() + text.slice(1) + ".";
