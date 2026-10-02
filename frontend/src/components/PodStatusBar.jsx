import { statusTone } from "./StatusBadge.jsx";

// Order of tones along the bar and in the legend: healthy first, then
// in-progress, then broken, then anything else.
const TONE_ORDER = ["ok", "warn", "bad", "neutral"];

// A single horizontal bar split by pod status, with a legend that lists
// every status and its count. Colours come from the same tones as the
// status badges; the legend gives each segment a name, so colour is never
// the only way to tell them apart.
export default function PodStatusBar({ byStatus, total }) {
  const entries = Object.entries(byStatus)
    .map(([status, count]) => ({ status, count, tone: statusTone(status) }))
    .sort((a, b) => TONE_ORDER.indexOf(a.tone) - TONE_ORDER.indexOf(b.tone) || b.count - a.count);

  if (total === 0) return <p className="muted">No pods.</p>;

  return (
    <div>
      <div className="stack-bar" role="img" aria-label={entries.map((e) => `${e.status}: ${e.count}`).join(", ")}>
        {entries.map((e) => (
          <div
            key={e.status}
            className={`stack-segment tone-${e.tone}`}
            style={{ flexGrow: e.count }}
            title={`${e.status}: ${e.count} of ${total} pods (${Math.round((e.count / total) * 100)}%)`}
          />
        ))}
      </div>

      <ul className="legend">
        {entries.map((e) => (
          <li key={e.status}>
            <span className={`legend-swatch tone-${e.tone}`} />
            <span className="legend-label">{e.status}</span>
            <span className="legend-value">{e.count}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
