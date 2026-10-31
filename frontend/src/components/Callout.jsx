import { Fragment } from "react";
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
// A plain string as children (an error from the server) may mark commands
// with backticks; see messageParts.
export function Callout({ tone = "info", title, detail, action, compact = false, children }) {
  const Icon = ICONS[tone];
  return (
    <div className={`callout callout-${tone} ${compact ? "callout-compact" : ""}`} role={tone === "error" ? "alert" : undefined}>
      <Icon size={16} className="callout-icon" />
      <div className="callout-body">
        {title && <div className="callout-title">{title}</div>}
        {children && (
          <div className="callout-text">{typeof children === "string" ? <Message text={children} /> : children}</div>
        )}
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

// messageParts splits a message into text and code, where code is marked with
// backticks as in Markdown: "run `kind get clusters`". A message with an odd
// number of backticks is left as it is.
export function messageParts(text) {
  const parts = text.split("`");
  if (parts.length % 2 === 0) return [{ text }];
  return parts.map((part, i) => ({ text: part, code: i % 2 === 1 })).filter((part) => part.text !== "");
}

// Code wraps between its words but never inside one, so "--network" doesn't
// break after its dashes and a long command still fits a narrow screen.
function Message({ text }) {
  return messageParts(text).map((part, i) =>
    part.code ? (
      <code key={i}>
        {part.text.split(" ").map((word, j) => (
          <Fragment key={j}>
            {j > 0 && " "}
            <span className="callout-word">{word}</span>
          </Fragment>
        ))}
      </code>
    ) : (
      <Fragment key={i}>{part.text}</Fragment>
    ),
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
