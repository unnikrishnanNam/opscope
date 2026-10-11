import { useState } from "react";
import { Link } from "react-router";
import { objectParts } from "../diagnosis.js";

export { problemSummary } from "../diagnosis.js";
import { detailPath } from "../sections.js";
import Button from "./Button.jsx";
import { ObjectLink, pluralKind } from "./Diagnosis.jsx";
import { Tag } from "./Tag.jsx";
import { ErrorIcon, WarningIcon } from "./icons.jsx";
import "./ProblemList.css";

const SHOWN = 6;

// PROBLEMS_ID is the list's id, for jumping to it. It differs from the
// "#problems" in the overview's URL on purpose: a matching id would make the
// browser scroll to it by itself on page load, moving boxes that hide their
// overflow (the top bar) out of view. The overview moves there itself.
export const PROBLEMS_ID = "problem-list";
const CONFIDENCE = {
  likely: "Read from the text of an event or message, which can change between Kubernetes versions.",
  possible: "A guess: Opscope can't see enough to be sure.",
};

// ProblemList: the root causes of what's wrong, from the problems endpoint,
// worst and widest first. Each names what it's about and what it affects,
// and links to the page whose diagnosis explains it. `namespace` is the one
// selected, if any: objects in it are named without it.
export default function ProblemList({ clusterId, search, namespace = "", data }) {
  const [all, setAll] = useState(false);
  const ctx = { clusterId, search, nodes: data.nodes, self: null, namespace };
  const problems = all ? data.problems : data.problems.slice(0, SHOWN);

  return (
    <div id={PROBLEMS_ID} tabIndex={-1} className="problems">
      <ul className="problem-list">
        {problems.map((p) => (
          <Problem key={p.id} problem={p} ctx={ctx} />
        ))}
      </ul>
      {data.problems.length > SHOWN && (
        <Button variant="quiet" size="sm" onClick={() => setAll(!all)}>
          {all ? "Show fewer" : `Show all ${data.problems.length} problems`}
        </Button>
      )}
    </div>
  );
}

function Problem({ problem: p, ctx }) {
  const Mark = p.severity === "failing" ? ErrorIcon : WarningIcon;
  const link = pageFor(p, ctx);
  const shownAffects = p.affects.slice(0, 3);
  const kind = ctx.nodes[p.objects[0]]?.kind ?? "object";

  return (
    <li className={`problem problem-${p.severity}`}>
      <span className="problem-mark" aria-hidden="true">
        <Mark size={14} />
      </span>
      <div className="problem-main">
        <p className="problem-subject">
          {p.objects.length === 1 ? (
            <ObjectLink id={p.objects[0]} ctx={ctx} />
          ) : (
            <span className="trace-object">
              {p.objects.length} {pluralKind(kind)}
            </span>
          )}
        </p>
        <p className="problem-title">
          {link ? <Link to={link}>{p.title}</Link> : p.title}
          {p.confidence !== "certain" && (
            <Tag className="tag-gap" title={CONFIDENCE[p.confidence]}>
              {p.confidence}
            </Tag>
          )}
        </p>
        {shownAffects.length > 0 && (
          <p className="problem-affects">
            Affects{" "}
            {shownAffects.map((n, i) => (
              <span key={n.id}>
                {i > 0 && ", "}
                <ObjectLink id={n.id} ctx={{ ...ctx, nodes: { ...ctx.nodes, [n.id]: n } }} />
              </span>
            ))}
            {p.affects.length > shownAffects.length && ` and ${p.affects.length - shownAffects.length} more`}
          </p>
        )}
      </div>
    </li>
  );
}

// pageFor picks the page whose diagnosis explains a problem best: the
// object it's about, when Opscope has a page for it and it exists, or else
// the first thing it affects that has one (a missing Secret has no page;
// the Deployment that needs it does).
function pageFor(p, ctx) {
  for (const id of [...p.objects, ...p.affects.map((n) => n.id)]) {
    const n = ctx.nodes[id] ?? p.affects.find((a) => a.id === id);
    if (!n || n.state === "missing" || n.state === "unknown") continue;
    const { resource, namespace, name } = objectParts(id);
    const path = detailPath(ctx.clusterId, resource, namespace, name);
    if (path) return path + ctx.search;
  }
  return null;
}
