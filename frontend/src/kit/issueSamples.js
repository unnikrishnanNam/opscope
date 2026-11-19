// Sample Issues-feed answers for /kit, shaped like GET /issues.

const minutes = (n) => new Date(Date.now() - n * 60_000).toISOString();

const ref = (resource, kind, namespace, name) => ({ uid: `uid-${name}`, resource, kind, namespace, name });

function signal(type, severity, r, message, evidence, extra = {}) {
  return {
    id: `id-${type}-${r.name}`,
    type,
    severity,
    cluster: "kit",
    resource: r,
    message,
    firstSeen: minutes(42),
    lastSeen: minutes(0),
    count: 1,
    state: "open",
    evidence,
    ...extra,
  };
}

const crashPod = (name, restarts) =>
  signal(
    "crash_loop",
    "critical",
    ref("pods", "Pod", "shop", name),
    `Container order-service restarted ${restarts} times; last exit Error (exit 1); its log shows refused credentials`,
    [
      { source: "field", detector: "pods.containers", field: "status.containerStatuses[0].state.waiting.reason", value: "CrashLoopBackOff" },
      { source: "field", detector: "pods.containers", field: "status.containerStatuses[0].lastState.terminated", value: "Error (exit 1)" },
      { source: "log", detector: "pods.containers", field: "log of container order-service, previous run", pattern: "auth_failed", matches: 1 },
      { source: "log", detector: "pods.containers", field: "log of container order-service, previous run", value: "3 other kinds of line, not kept" },
    ],
    { count: restarts, owner: ref("deployments", "Deployment", "shop", "order-service") },
  );

export function openIssues() {
  return [
    {
      id: "group:uid-order-service:crash_loop",
      type: "crash_loop",
      title: "Crash loop",
      severity: "critical",
      state: "open",
      owner: ref("deployments", "Deployment", "shop", "order-service"),
      firstSeen: minutes(180),
      lastSeen: minutes(0),
      signals: [crashPod("order-service-79689895d4-9zcpx", 645), crashPod("order-service-fb47c5848-9hpsf", 656)],
    },
    one(
      signal("rollout_stuck", "critical", ref("deployments", "Deployment", "shop", "order-service"),
        'The rollout stopped: ReplicaSet "order-service-fb47c5848" has timed out progressing.',
        [{ source: "field", detector: "workloads.conditions", field: "status.conditions[1]", value: "Progressing=False (ProgressDeadlineExceeded)" }]),
      "Rollout stuck",
    ),
    one(
      signal("image_pull_failed", "critical", ref("pods", "Pod", "shop", "bad-image"), "Image registry.invalid/shop:1 can't be pulled (ImagePullBackOff)", [
        { source: "field", detector: "pods.containers", field: "status.containerStatuses[0].state.waiting.reason", value: "ImagePullBackOff" },
      ]),
      "Image can't be pulled",
    ),
    one(
      signal("probe_failing", "warning", ref("pods", "Pod", "web", "web-7d9f-x1"), "Readiness probe failed: HTTP probe failed with statuscode: 503", [
        { source: "event", detector: "pods.events", event: { uid: "e1", reason: "Unhealthy", count: 37, lastSeen: minutes(1) } },
      ], { state: "resolving" }),
      "Probe failing",
    ),
    one(
      signal("volume_nearly_full", "warning", ref("persistentvolumeclaims", "PersistentVolumeClaim", "data", "postgres-data"), "93% of its space used", [
        { source: "metric", detector: "claims.usage", field: "kubelet stats: volume used/capacity", value: "9986703360/10737418240 bytes" },
      ], { stale: true }),
      "Volume nearly full",
    ),
    one(
      signal("selector_matches_nothing", "warning", ref("services", "Service", "shop", "typo"), "Its selector app=web-frontend matches no pods", [
        { source: "field", detector: "services.endpoints", field: "spec.selector", value: "app=web-frontend" },
      ]),
      "Selector matches nothing",
    ),
    one(
      signal("node_unschedulable", "info", ref("nodes", "Node", "", "kubeworker2"), "Cordoned: no new pods are scheduled on it", [
        { source: "field", detector: "nodes.conditions", field: "spec.unschedulable", value: "true" },
      ]),
      "Node cordoned",
    ),
  ];
}

export function resolvedIssues() {
  return openIssues()
    .slice(2, 4)
    .map((g) => ({ ...g, state: "resolved", signals: g.signals.map((s) => ({ ...s, state: "resolved", resolved: minutes(12) })) }));
}

function one(s, title) {
  return { id: s.id, type: s.type, title, severity: s.severity, state: s.state === "resolved" ? "resolved" : "open",
    firstSeen: s.firstSeen, lastSeen: s.lastSeen, signals: [s] };
}
