// Plain helpers for a diagnosis (the trace the diagnosis endpoint returns),
// kept apart from the component so they're easy to test.

// rootSteps lists every root cause in a trace, in the order they appear.
export function rootSteps(steps) {
  return steps.flatMap((step) => (step.root ? [step, ...rootSteps(step.causes)] : rootSteps(step.causes)));
}

// rootCauseText is what "Copy root cause" puts on the clipboard: one line
// per root cause, naming the objects it's about.
//   "secrets/shop/stripe-key: Secret shop/stripe-key doesn't exist"
export function rootCauseText(steps) {
  return rootSteps(steps)
    .map((r) => `${r.objects.join(", ")}: ${r.title}`)
    .join("\n");
}

// objectParts splits an object's ID into its parts: "pods/shop/web-1" or,
// for a cluster-wide kind, "nodes/worker-1".
export function objectParts(id) {
  const parts = id.split("/");
  return parts.length === 2 ? { resource: parts[0], namespace: "", name: parts[1] } : { resource: parts[0], namespace: parts[1], name: parts[2] };
}

// problemSummary is the overview card's aside: "3 problems, affecting 17
// objects". The objects are those the problems are about and those they
// affect, each counted once.
export function problemSummary(problems) {
  const ids = new Set(problems.flatMap((p) => [...p.objects, ...p.affects.map((n) => n.id)]));
  const what = problems.length === 1 ? "1 problem" : `${problems.length} problems`;
  return `${what}, affecting ${ids.size === 1 ? "1 object" : `${ids.size} objects`}`;
}

// pluralKind: "Pods", "Ingresses", "GatewayClasses".
export function pluralKind(kind) {
  return kind.endsWith("s") ? `${kind}es` : `${kind}s`;
}
