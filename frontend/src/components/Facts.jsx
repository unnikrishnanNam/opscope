import { useState } from "react";
import "./Facts.css";

// FactGrid: short label/value pairs in a grid, e.g. a pod's node, IP and QoS class.
//   <FactGrid>
//     <Fact label="Node">kubeworker01</Fact>
//     <Fact label="Pod IP" mono>10.244.1.17</Fact>
//   </FactGrid>
export function FactGrid({ children }) {
  return <dl className="fact-grid">{children}</dl>;
}

export function Fact({ label, mono = false, children }) {
  return (
    <div className="fact">
      <dt>{label}</dt>
      <dd className={mono ? "mono" : undefined}>{children}</dd>
    </div>
  );
}

// Values longer than this (or with line breaks) start folded to three lines.
const LONG = 160;

// KeyValueList: labels or annotations, sorted by key. Long values (like
// kubectl's last-applied-configuration) start folded, with "Show all".
export function KeyValueList({ values, empty }) {
  const entries = Object.entries(values ?? {}).sort(([a], [b]) => a.localeCompare(b));
  if (entries.length === 0) return <p className="kv-empty">{empty}</p>;
  return (
    <dl className="kv-list">
      {entries.map(([key, value]) => (
        <div key={key} className="kv-row">
          <dt>{key}</dt>
          <dd>
            <KeyValue value={value} />
          </dd>
        </div>
      ))}
    </dl>
  );
}

function KeyValue({ value }) {
  const long = value.length > LONG || value.includes("\n");
  const [open, setOpen] = useState(false);
  if (!value) return <span className="kv-blank">(empty)</span>;
  if (!long) return value;
  return (
    <>
      <span className={open ? "kv-value-open" : "kv-value-folded"}>{value}</span>
      <button type="button" className="kv-toggle" onClick={() => setOpen(!open)} aria-expanded={open}>
        {open ? "Show less" : `Show all (${value.length.toLocaleString()} characters)`}
      </button>
    </>
  );
}
