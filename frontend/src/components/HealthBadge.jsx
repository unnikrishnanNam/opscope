import { ErrorIcon, WarningIcon } from "./icons.jsx";
import "./StatusBadge.css";

// An object's health from the Signal engine, as a badge in the same style
// as StatusBadge: a dot when healthy, an icon and coloured text when not,
// so it reads without colour too.
//
//   health  { state: "healthy" | "degraded" | "failing" | "unknown", since, signals }
//           or null, which shows nothing: the engine has no word on it.

export const HEALTH = {
  healthy: { tone: "ok", label: "Healthy" },
  degraded: { tone: "warn", label: "Degraded", icon: WarningIcon },
  failing: { tone: "bad", label: "Failing", icon: ErrorIcon },
  unknown: { tone: "neutral", label: "Unknown" },
};

export default function HealthBadge({ health, title }) {
  const h = health && HEALTH[health.state];
  if (!h) return null;
  const Icon = h.icon;
  return (
    <span className={`status status-${h.tone} ${Icon ? "status-has-icon" : ""}`} title={title || undefined}>
      {Icon && <Icon size={14} className="status-icon" />}
      {h.label}
    </span>
  );
}
