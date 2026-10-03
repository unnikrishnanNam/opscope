import { statusTone } from "./StatusBadge.jsx";
import StackBar from "./StackBar.jsx";

// Order of tones along the bar and in the legend: healthy first, then
// in-progress, then broken, then anything else.
const TONE_ORDER = ["ok", "warn", "bad", "neutral"];

// Pods split by status, using the same colours as the status badges.
export default function PodStatusBar({ byStatus, total }) {
  if (total === 0) return <p className="muted">No pods.</p>;

  const segments = Object.entries(byStatus)
    .map(([status, count]) => ({ label: status, value: count, tone: statusTone(status) }))
    .sort((a, b) => TONE_ORDER.indexOf(a.tone) - TONE_ORDER.indexOf(b.tone) || b.value - a.value);

  return <StackBar segments={segments} noun="pods" />;
}
