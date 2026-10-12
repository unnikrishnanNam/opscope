import { Link, useLocation } from "react-router";
import { copyText } from "../clipboard.js";
import { objectParts, pluralKind, rootCauseText, rootSteps } from "../diagnosis.js";
import { focusSectionSoon } from "../focus.js";
import { age } from "../format.js";
import { detailPath, nsQuery } from "../sections.js";
import { Tag } from "./Tag.jsx";
import { CopyIcon, ErrorIcon, InfoIcon, SuccessIcon, WarningIcon } from "./icons.jsx";
import "./Diagnosis.css";

const MARKS = { failing: ErrorIcon, degraded: WarningIcon, info: InfoIcon };
const SEVERITY_WORDS = { failing: "Failing", degraded: "Needs a look", info: "Note" };
const HEADINGS = { failing: "Why it's failing", degraded: "What needs a look", info: "What's going on" };
const CONFIDENCE = {
  likely: "Read from the text of an event or message, which can change between Kubernetes versions.",
  possible: "A guess: Opscope can't see enough to be sure.",
};

// Diagnosis: why an object is failing, as a trace from what's wrong with it
// down to the root causes, with the evidence for each step and what to look
// at next. `trace` is the diagnosis endpoint's answer.
//
// It's text first: the trace is nested lists, so it reads the same with a
// screen reader or on a phone as it looks on a wide screen.
export default function Diagnosis({ clusterId, trace }) {
  const search = nsQuery(useLocation().search);
  const nodes = { ...trace.nodes, [trace.object.id]: trace.object };
  const ctx = { clusterId, search, nodes, self: trace.object.id, repeats: repeatedSteps(trace.steps) };
  const taken = trace.snapshot?.taken;
  const asOf = taken && (
    <time dateTime={taken} title={new Date(taken).toLocaleString()}>
      as of {age(taken)} ago
    </time>
  );

  if (trace.steps.length === 0) {
    return (
      <section id="diagnosis" tabIndex={-1} className="diagnosis-clear" aria-label="Diagnosis">
        <SuccessIcon size={16} className="diagnosis-clear-icon" />
        <p>
          {trace.impact.length > 0
            ? "Opscope found nothing wrong with this object itself."
            : "Opscope found nothing wrong with this object or what it needs."}{" "}
          <span className="diagnosis-asof">{asOf}</span>
        </p>
        <Impact trace={trace} ctx={ctx} label="It's part of a problem in" />
      </section>
    );
  }

  const worst = trace.steps[0].severity; // the backend puts the worst first
  const roots = rootSteps(trace.steps).length;
  return (
    <section id="diagnosis" tabIndex={-1} className={`diagnosis diagnosis-${worst}`} aria-labelledby="diagnosis-title">
      <header className="diagnosis-header">
        <h2 id="diagnosis-title" className="diagnosis-title">
          {HEADINGS[worst]}
        </h2>
        <span className="diagnosis-asof">
          {roots === 1 ? "1 root cause" : `${roots} root causes`} · {asOf}
        </span>
      </header>
      <ol className="trace">
        {trace.steps.map((step) => (
          <TraceStep key={step.id} step={step} ctx={ctx} />
        ))}
      </ol>
      <Impact trace={trace} ctx={ctx} label="Also affected" />
    </section>
  );
}

function TraceStep({ step, ctx }) {
  const Mark = MARKS[step.severity];
  // Shown in full higher up: one line, so a node that's down isn't
  // explained again under each of its pods.
  if (ctx.repeats?.has(step)) {
    return (
      <li className={`trace-step trace-${step.severity}`}>
        <span className="trace-mark" aria-hidden="true">
          <Mark size={16} />
        </span>
        <div className="trace-main">
          <div className="trace-head">
            <Objects ids={step.objects} ctx={ctx} />
            {step.root && <span className="trace-root">Root cause</span>}
          </div>
          <p className="trace-title">
            <span className="visually-hidden">{SEVERITY_WORDS[step.severity]}: </span>
            {step.title} <span className="trace-again">(as above)</span>
          </p>
        </div>
      </li>
    );
  }
  // Culprits that have no problem of their own aren't in the causes below
  // (a ResourceQuota, a ConfigMap missing a key); link to them here.
  const shown = new Set([...step.objects, ...step.causes.flatMap((c) => c.objects)]);
  const see = step.blames.filter((id) => !shown.has(id));

  return (
    <li className={`trace-step trace-${step.severity}`}>
      <span className="trace-mark" aria-hidden="true">
        <Mark size={16} />
      </span>
      <div className="trace-main">
        <div className="trace-head">
          <Objects ids={step.objects} ctx={ctx} />
          {step.root && <span className="trace-root">Root cause</span>}
          {step.confidence !== "certain" && <Tag title={CONFIDENCE[step.confidence]}>{step.confidence}</Tag>}
        </div>
        <p className="trace-title">
          <span className="visually-hidden">{SEVERITY_WORDS[step.severity]}: </span>
          {step.title}
        </p>
        {step.detail && <p className="trace-detail">{step.detail}</p>}
        {see.length > 0 && (
          <p className="trace-see">
            See{" "}
            {see.map((id, i) => (
              <span key={id}>
                {i > 0 && ", "}
                <ObjectLink id={id} ctx={ctx} />
              </span>
            ))}
          </p>
        )}
        {step.related.length > 0 && (
          <p className="trace-see">
            Possibly related:{" "}
            {step.related.map((id, i) => (
              <span key={id}>
                {i > 0 && ", "}
                <ObjectLink id={id} ctx={ctx} showStatus />
              </span>
            ))}
          </p>
        )}
        <Evidence items={step.evidence} open={step.root} />
        <NextSteps steps={step.next} ctx={ctx} />
      </div>
      {step.causes.length > 0 && (
        <ol className="trace trace-causes">
          {step.causes.map((cause) => (
            <TraceStep key={cause.id} step={cause} ctx={ctx} />
          ))}
        </ol>
      )}
    </li>
  );
}

// Objects names what a step is about: one object, or a group of alike
// ones ("3 Pods") that unfolds into the list.
function Objects({ ids, ctx }) {
  if (ids.length === 1) return <ObjectLink id={ids[0]} ctx={ctx} showStatus />;
  const kind = ctx.nodes[ids[0]]?.kind ?? "object";
  return (
    <details className="trace-objects">
      <summary>
        {ids.length} {pluralKind(kind)}
      </summary>
      <ul>
        {ids.map((id) => (
          <li key={id}>
            <ObjectLink id={id} ctx={ctx} showStatus />
          </li>
        ))}
      </ul>
    </details>
  );
}

// ObjectLink: "Pod web-1", linked to its page when Opscope has one and it
// exists. The page's own object is "This Deployment".
//   ctx  { clusterId, search, nodes (by ID, from the API), self (the page's
//         object, if any), namespace (the one being looked at, if any) }
// The namespace is written only when it isn't the one being looked at.
export function ObjectLink({ id, ctx, showStatus = false }) {
  const n = ctx.nodes[id];
  if (!n) return <span className="trace-object">{id}</span>;
  if (id === ctx.self) return <span className="trace-object">This {n.kind}</span>;

  const here = ctx.namespace ?? ctx.nodes[ctx.self]?.namespace;
  const name = n.namespace && n.namespace !== here ? `${n.namespace}/${n.name}` : n.name;
  const exists = n.state !== "missing" && n.state !== "unknown";
  const link = exists && detailPath(ctx.clusterId, n.resource, n.namespace, n.name);
  const label = `${n.kind} ${name}`;
  return (
    <span className="trace-object">
      {link ? <Link to={link + ctx.search}>{label}</Link> : label}
      {showStatus && n.status && <span className="trace-status">{n.status}</span>}
    </span>
  );
}

export function Evidence({ items, open }) {
  if (items.length === 0) return null;
  return (
    <details className="trace-evidence" open={open}>
      <summary>Evidence</summary>
      <ul>
        {items.map((e, i) => (
          <li key={i}>
            <Tag>{e.source}</Tag>
            <span className="trace-evidence-text">{e.text}</span>
            {(e.time || e.count > 1) && (
              <span className="trace-evidence-meta">
                {e.time && (
                  <time dateTime={e.time} title={new Date(e.time).toLocaleString()}>
                    {age(e.time)} ago
                  </time>
                )}
                {e.count > 1 && <span>{e.count.toLocaleString()} times</span>}
              </span>
            )}
          </li>
        ))}
      </ul>
    </details>
  );
}

// NextSteps: links to a page of Opscope, and read-only kubectl commands to
// copy. The backend only ever suggests get, describe and logs.
export function NextSteps({ steps, ctx }) {
  if (steps.length === 0) return null;
  return (
    <div className="trace-next">
      {steps.map((step) =>
        step.command ? (
          <span key={step.command} className="trace-command">
            <code>{step.command}</code>
            <button
              type="button"
              className="trace-copy"
              aria-label={`Copy: ${step.command}`}
              title="Copy"
              onClick={() => copyText(step.command, "the kubectl command")}
            >
              <CopyIcon size={14} />
            </button>
          </span>
        ) : (
          <StepLink key={step.label + step.object} step={step} ctx={ctx} />
        ),
      )}
    </div>
  );
}

function StepLink({ step, ctx }) {
  const path = step.object && detailPathFor(ctx, step.object);
  if (!path) return null;
  const params = new URLSearchParams(ctx.search);
  if (step.tab) params.set("tab", step.tab);
  for (const [key, value] of new URLSearchParams(step.query ?? "")) params.set(key, value);
  return (
    <Link className="trace-link" to={`${path}?${params}`}>
      {step.label}
    </Link>
  );
}

function Impact({ trace, ctx, label }) {
  if (trace.impact.length === 0) return null;
  const nodes = { ...ctx.nodes, ...Object.fromEntries(trace.impact.map((n) => [n.id, n])) };
  return (
    <p className="diagnosis-impact">
      {label}:{" "}
      {trace.impact.map((n, i) => (
        <span key={n.id}>
          {i > 0 && ", "}
          <ObjectLink id={n.id} ctx={{ ...ctx, nodes }} />
        </span>
      ))}
    </p>
  );
}

// repeatedSteps finds the steps that are the same finding as one before
// them in reading order: a root reached along two paths.
function repeatedSteps(steps) {
  const seen = new Set();
  const repeats = new Set();
  const walk = (list) => {
    for (const step of list) {
      if (seen.has(step.id)) {
        repeats.add(step);
        continue;
      }
      seen.add(step.id);
      walk(step.causes);
    }
  };
  walk(steps);
  return repeats;
}

// detailPathFor builds an object's page from its ID.
function detailPathFor(ctx, id) {
  const { resource, namespace, name } = objectParts(id);
  return detailPath(ctx.clusterId, resource, namespace, name);
}

// diagnosisCommands: what the palette offers on a page with a diagnosis.
export function diagnosisCommands({ trace, openTab }) {
  if (!trace || trace.steps.length === 0) return [];
  const roots = rootSteps(trace.steps);
  return [
    {
      id: "diagnosis.show",
      title: "Show diagnosis",
      detail: roots.length === 1 ? roots[0].title : `${roots.length} root causes`,
      group: "This page",
      icon: WarningIcon,
      keywords: ["why", "failing", "root cause", "problem"],
      run: () => {
        openTab("summary");
        focusSectionSoon("diagnosis");
      },
    },
    {
      id: "diagnosis.copy",
      title: roots.length === 1 ? "Copy root cause" : "Copy root causes",
      group: "This page",
      icon: CopyIcon,
      keywords: ["why", "diagnosis"],
      run: () => copyText(rootCauseText(trace.steps), roots.length === 1 ? "the root cause" : "the root causes"),
    },
  ];
}
