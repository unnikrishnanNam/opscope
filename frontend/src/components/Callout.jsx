import { ErrorIcon, InfoIcon, WarningIcon } from "./icons.jsx";
import "./Callout.css";

const ICONS = { info: InfoIcon, warning: WarningIcon, error: ErrorIcon };

// Callout: a message in a box, with an icon for its tone.
//   tone     "info" (default): something to know, like a missing optional feature
//            "warning": works, but something needs a look
//            "error": something failed
//   title    one short line; children add the detail
//   detail   the raw error, folded away under "Technical details"
//   action   e.g. a "Try again" button, shown underneath
//   compact  one line, no box padding to speak of (for notes inside cards)
export function Callout({ tone = "info", title, detail, action, compact = false, children }) {
  const Icon = ICONS[tone];
  return (
    <div className={`callout callout-${tone} ${compact ? "callout-compact" : ""}`} role={tone === "error" ? "alert" : undefined}>
      <Icon size={16} className="callout-icon" />
      <div className="callout-body">
        {title && <div className="callout-title">{title}</div>}
        {children && <div className="callout-text">{children}</div>}
        {detail && (
          <details className="callout-detail">
            <summary>Technical details</summary>
            <code>{detail}</code>
          </details>
        )}
        {action && <div className="callout-action">{action}</div>}
      </div>
    </div>
  );
}

// EmptyState: what an empty list or page says, with an optional next step.
//   icon    an icon component for the kind of thing that's missing
//   title   short, e.g. "No pods"
//   action  e.g. a button to add one, or to clear a filter
export function EmptyState({ icon: Icon, title, action, children }) {
  return (
    <div className="empty-state">
      {Icon && (
        <span className="empty-state-icon">
          <Icon size={20} />
        </span>
      )}
      {title && <div className="empty-state-title">{title}</div>}
      {children && <div className="empty-state-text">{children}</div>}
      {action && <div className="empty-state-action">{action}</div>}
    </div>
  );
}
