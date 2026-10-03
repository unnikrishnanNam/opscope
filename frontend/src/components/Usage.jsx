// Small pieces for showing resource usage: a bar against a limit, and a
// sparkline of the last 15 minutes. Both are plain HTML/SVG; no chart library.

import { percent } from "../format.js";
import { Callout } from "./Callout.jsx";
import CodeBlock from "./CodeBlock.jsx";
import "./Usage.css";

// How much of the 15-minute window the sparkline covers, in milliseconds.
// It matches the history the backend keeps.
const WINDOW_MS = 15 * 60 * 1000;
// Wait for this much history before drawing a line; a few seconds of data
// squeezed into a 15-minute timeline just looks like a glitch.
const MIN_HISTORY_MS = 60 * 1000;

// UsageBar: "34%" with a thin bar, e.g. CPU in use against what's allocatable.
// `label` (like "0.7 of 2 cores") is shown on hover and read by screen readers.
export function UsageBar({ used, total, label }) {
  const pct = percent(used, total);
  // Busy is worth a glance, nearly full is worth acting on. The number is
  // always shown, so colour is never the only signal.
  const tone = pct >= 90 ? "bad" : pct >= 75 ? "warn" : "ok";
  return (
    <span className="usage" title={label} aria-label={label}>
      <span className="usage-track">
        <span className={`usage-fill usage-${tone}`} style={{ width: `${Math.min(pct, 100)}%` }} />
      </span>
      <span className="usage-pct">{pct}%</span>
    </span>
  );
}

// Sparkline: one line showing how a value changed over the last 15 minutes.
//   points  [{t, cpu, memory}, ...] oldest first, from the backend
//   field   which value to draw ("cpu" or "memory")
//   format  turns a value into text for the hover label
// The y-axis starts at 0 and stretches to a bit above the highest value, so
// changes are visible even when usage is low.
export function Sparkline({ points, field, format, width = 96, height = 24, what }) {
  const span = points?.length ? Date.parse(points[points.length - 1].t) - Date.parse(points[0].t) : 0;
  if (!points || points.length < 2 || span < MIN_HISTORY_MS) {
    return (
      <span className="sparkline-empty" style={{ width }} title="History builds up over the next minutes">
        collecting…
      </span>
    );
  }

  const values = points.map((p) => p[field]);
  const max = Math.max(...values) * 1.2 || 1; // "|| 1" avoids dividing by zero when everything is 0
  const end = Date.parse(points[points.length - 1].t);
  const start = end - WINDOW_MS;

  // Position each point by its time, so a half-filled history fills half the width.
  const coords = points.map((p) => {
    const x = Math.max(0, ((Date.parse(p.t) - start) / WINDOW_MS) * width);
    const y = height - 1 - (p[field] / max) * (height - 2);
    return [x, y];
  });
  const line = coords.map(([x, y], i) => `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `${line} L${coords[coords.length - 1][0].toFixed(1)},${height} L${coords[0][0].toFixed(1)},${height} Z`;

  const label = `${what}, last 15 minutes: low ${format(Math.min(...values))}, high ${format(
    Math.max(...values),
  )}, now ${format(values[values.length - 1])}`;

  return (
    <svg className="sparkline" width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label}>
      <title>{label}</title>
      {/* A faint baseline across the whole 15 minutes, so a short history reads
          as "the line starts here" rather than as a glitch. */}
      <line x1="0" y1={height - 0.5} x2={width} y2={height - 0.5} className="sparkline-base" />
      <path d={area} className="sparkline-area" />
      <path d={line} className="sparkline-line" />
    </svg>
  );
}

// MetricsUnavailable: what to do when a cluster has no metrics-server.
// `compact` is a one-line note, for places where usage is a side detail.
export function MetricsUnavailable({ compact = false }) {
  if (compact) {
    return <Callout compact>Usage needs metrics-server, which isn't installed on this cluster.</Callout>;
  }
  return (
    <Callout title="Live usage needs metrics-server">
      <p>
        metrics-server isn't installed (or isn't answering) on this cluster, so CPU and memory show capacity only. To
        install it:
      </p>
      <CodeBlock
        lineNumbers={false}
        code="kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml"
      />
      <p>
        On kind and other local clusters, the kubelets use self-signed certificates, so metrics-server also needs the{" "}
        <code>--kubelet-insecure-tls</code> flag.
      </p>
    </Callout>
  );
}
