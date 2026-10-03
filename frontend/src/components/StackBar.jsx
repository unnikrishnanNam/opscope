import "./StackBar.css";

// StackBar: one bar split into coloured parts, with a legend that names
// every part and its count, so colour is never the only way to tell them apart.
//   segments  [{ label, value, tone }], drawn in this order; tone is
//             "ok", "warn", "bad" or "neutral" (the status colours)
//   noun      what's being counted, for the hover text: "pods"
export default function StackBar({ segments, noun }) {
  const total = segments.reduce((sum, s) => sum + s.value, 0);
  if (total === 0) return null;

  return (
    <div className="stack">
      <div className="stack-bar" role="img" aria-label={segments.map((s) => `${s.label}: ${s.value}`).join(", ")}>
        {segments.map((s) => (
          <div
            key={s.label}
            className={`stack-segment tone-${s.tone}`}
            style={{ flexGrow: s.value }}
            title={`${s.label}: ${s.value} of ${total} ${noun} (${Math.round((s.value / total) * 100)}%)`}
          />
        ))}
      </div>
      <ul className="stack-legend">
        {segments.map((s) => (
          <li key={s.label}>
            <span className={`stack-swatch tone-${s.tone}`} />
            <span className="stack-label">{s.label}</span>
            <span className="stack-value">{s.value}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
