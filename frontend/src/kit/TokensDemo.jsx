import { useLayoutEffect, useRef, useState } from "react";

// Design tokens on /kit: colours with their contrast ratios, type and shape.

/* --------------------------------------------------------------------------
   Colours
   -------------------------------------------------------------------------- */

// Each token, and the backgrounds it must stand out against with a minimum
// contrast ratio: 4.5 for text, 3 for marks.
const TEXT = 4.5;
const MARK = 3;
const COLOUR_GROUPS = [
  {
    title: "Surfaces",
    tokens: [
      { name: "bg", note: "page background" },
      { name: "surface", note: "cards, tables, inputs" },
      { name: "surface-2", note: "table headers, code" },
      { name: "surface-hover" },
      { name: "border-subtle", note: "between table rows" },
      { name: "border" },
      { name: "border-strong", note: "inputs" },
    ],
  },
  {
    title: "Text",
    tokens: [
      { name: "text", checks: [["bg", TEXT], ["surface", TEXT], ["surface-2", TEXT]] },
      { name: "text-muted", checks: [["bg", TEXT], ["surface", TEXT], ["surface-2", TEXT]] },
      { name: "text-subtle", checks: [["bg", TEXT], ["surface", TEXT], ["surface-2", TEXT]] },
    ],
  },
  {
    title: "Actions and focus",
    tokens: [
      { name: "primary", note: "button fill" },
      { name: "primary-text", checks: [["primary", TEXT]] },
      { name: "signal", note: "you are here", checks: [["bg", MARK], ["surface", MARK]] },
      { name: "focus", checks: [["bg", MARK], ["surface", MARK]] },
      { name: "danger", note: "button fill" },
      { name: "danger-text", checks: [["danger", TEXT]] },
    ],
  },
  {
    title: "Status",
    tokens: [
      { name: "ok", checks: [["surface", TEXT], ["ok-soft", TEXT]] },
      { name: "ok-mark", checks: [["surface", MARK]] },
      { name: "ok-soft" },
      { name: "warn", checks: [["surface", TEXT], ["warn-soft", TEXT]] },
      { name: "warn-mark", checks: [["surface", MARK]] },
      { name: "warn-soft" },
      { name: "bad", checks: [["surface", TEXT], ["bad-soft", TEXT]] },
      { name: "bad-mark", checks: [["surface", MARK]] },
      { name: "bad-soft" },
      { name: "neutral-mark", checks: [["surface", MARK]] },
    ],
  },
  {
    title: "Data",
    tokens: [
      { name: "data", note: "usage bars, sparklines", checks: [["surface", MARK], ["data-track", MARK]] },
      { name: "data-track" },
    ],
  },
  {
    title: "Code",
    tokens: ["code-key", "code-string", "code-literal", "code-comment"].map((name) => ({
      name,
      checks: [["surface-2", TEXT]],
    })),
  },
];

function Colours({ theme }) {
  return (
    <div className="kit-colour-groups">
      {COLOUR_GROUPS.map((group) => (
        <div key={group.title}>
          <h3 className="kit-group-title">{group.title}</h3>
          <div className="kit-swatches">
            {group.tokens.map((token) => (
              <Swatch key={token.name} token={token} theme={theme} />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

// One colour token: a sample, its value as resolved in this panel, and its
// contrast against each background it's used on.
function Swatch({ token, theme }) {
  const ref = useRef(null);
  const [measured, setMeasured] = useState(null);

  // Read the real values from the browser, so the numbers always match the CSS.
  useLayoutEffect(() => {
    const resolve = (name) => resolveColour(ref.current, name);
    const value = resolve(token.name);
    setMeasured({
      hex: toHex(value),
      checks: (token.checks ?? []).map(([against, min]) => ({
        against,
        min,
        ratio: contrast(value, resolve(against)),
      })),
    });
  }, [token, theme]);

  return (
    <div className="kit-swatch" ref={ref}>
      <span className="kit-swatch-sample" style={{ background: `var(--${token.name})` }} />
      <div className="kit-swatch-text">
        <code className="kit-swatch-name">--{token.name}</code>
        <span className="kit-swatch-value">
          {measured?.hex}
          {token.note && ` · ${token.note}`}
        </span>
        {measured?.checks.map((c) => (
          <span key={c.against} className={`kit-ratio ${c.ratio >= c.min ? "kit-pass" : "kit-fail"}`}>
            {c.ratio.toFixed(2)} on {c.against} {c.ratio >= c.min ? "✓" : `✗ needs ${c.min}`}
          </span>
        ))}
      </div>
    </div>
  );
}

// resolveColour turns a token into [r, g, b, alpha] as computed inside `el`.
function resolveColour(el, name) {
  const probe = document.createElement("span");
  probe.style.color = `var(--${name})`;
  el.appendChild(probe);
  const rgb = getComputedStyle(probe).color; // "rgb(1, 2, 3)" or "rgba(1, 2, 3, 0.5)"
  probe.remove();
  const [r, g, b, a = 1] = rgb.match(/[\d.]+/g).map(Number);
  return [r, g, b, a];
}

function toHex([r, g, b, a]) {
  const hex = "#" + [r, g, b].map((v) => v.toString(16).padStart(2, "0")).join("");
  return a < 1 ? `${hex} at ${Math.round(a * 100)}%` : hex;
}

// WCAG contrast ratio between two opaque colours.
function contrast(a, b) {
  const luminance = ([r, g, b]) => {
    const [R, G, B] = [r, g, b].map((v) => {
      v /= 255;
      return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
    });
    return 0.2126 * R + 0.7152 * G + 0.0722 * B;
  };
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/* --------------------------------------------------------------------------
   Type
   -------------------------------------------------------------------------- */

const SIZES = [
  { token: "text-2xl", px: 28, weight: "display", sample: "44 pods" },
  { token: "text-xl", px: 20, weight: "title", sample: "multipass" },
  { token: "text-lg", px: 16, weight: "title", sample: "Pods by status" },
  { token: "text-md", px: 14, weight: "body", sample: "Running containers, their status, restarts and the node they run on." },
  { token: "text-sm", px: 13, weight: "body", sample: "Updated 10:42 · 3 of 44 pods match “crash”" },
  { token: "text-xs", px: 12, weight: "strong", sample: "Last exit: Error (exit code 1), 4 minutes ago" },
];

const WEIGHTS = [
  { token: "weight-body", value: 500, use: "body text" },
  { token: "weight-strong", value: 600, use: "labels, headers, buttons" },
  { token: "weight-title", value: 700, use: "titles" },
  { token: "weight-display", value: 800, use: "big numbers" },
];

function Type() {
  return (
    <div className="kit-type">
      <div className="kit-type-scale">
        {SIZES.map((s) => (
          <div key={s.token} className="kit-type-row">
            <code className="kit-type-label">
              --{s.token} · {s.px}px
            </code>
            <span style={{ fontSize: `var(--${s.token})`, fontWeight: `var(--weight-${s.weight})`, lineHeight: 1.3 }}>
              {s.sample}
            </span>
          </div>
        ))}
      </div>

      <h3 className="kit-group-title">Weights</h3>
      <div className="kit-type-scale">
        {WEIGHTS.map((w) => (
          <div key={w.token} className="kit-type-row">
            <code className="kit-type-label">
              --{w.token} · {w.value}
            </code>
            <span style={{ fontWeight: w.value, fontSize: "var(--text-lg)" }}>
              Kubernetes v1.31.4 · 3 nodes ready · {w.use}
            </span>
          </div>
        ))}
      </div>

      <h3 className="kit-group-title">Mono</h3>
      <div className="kit-type-scale">
        <div className="kit-type-row">
          <code className="kit-type-label">inline · 500</code>
          <span className="kit-mono" style={{ fontWeight: 500 }}>
            registry.k8s.io/metrics-server/metrics-server:v0.7.2 · 10.96.0.1 · 80:32049/TCP
          </span>
        </div>
        <div className="kit-type-row">
          <code className="kit-type-label">blocks · 400</code>
          <pre className="kit-code">{`apiVersion: apps/v1
kind: Deployment
metadata:
  name: web          # 0O 1lI {}[]
  namespace: topology-test
2026-10-03T10:42:07Z INFO  listening on :8080`}</pre>
        </div>
      </div>

      <h3 className="kit-group-title">Tabular figures</h3>
      <div className="kit-numbers">
        {["1,024 Mi", "512 Mi", "11.8 Gi", "0.25 cores", "616"].map((n) => (
          <span key={n}>{n}</span>
        ))}
      </div>
    </div>
  );
}

/* --------------------------------------------------------------------------
   Shape
   -------------------------------------------------------------------------- */

function Shape() {
  return (
    <div className="kit-shapes">
      {["radius-sm", "radius", "radius-lg"].map((r) => (
        <div key={r} className="kit-shape" style={{ borderRadius: `var(--${r})` }}>
          <code>--{r}</code>
        </div>
      ))}
      {["shadow-sm", "shadow-lg"].map((s) => (
        <div key={s} className="kit-shape kit-shape-raised" style={{ boxShadow: `var(--${s})` }}>
          <code>--{s}</code>
        </div>
      ))}
    </div>
  );
}

export const tokenSections = [
  {
    id: "colours",
    title: "Colours",
    about: "Each text colour needs 4.5:1 against the surfaces it sits on; marks (dots, bars, focus) need 3:1.",
    render: (theme) => <Colours theme={theme} />,
  },
  {
    id: "type",
    title: "Type",
    about: "Manrope from 500 up, JetBrains Mono for code. Nothing below 12 px.",
    render: () => <Type />,
  },
  {
    id: "shape",
    title: "Shape",
    about: "Radius, borders and the two shadows (only for things that float).",
    render: () => <Shape />,
  },
];
