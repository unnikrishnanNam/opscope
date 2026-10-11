// Sample data for /kit, shaped like the real API responses (see the README's
// API table). Names and numbers are made up; there are no real secrets here.

const now = Date.now();
const ago = (minutes) => new Date(now - minutes * 60_000).toISOString();
const GiB = 1024 ** 3;

// 15 minutes of usage, one point every 15 seconds, like the backend keeps.
// A gentle wave, so sparklines have something to show.
function history(cpuBase, memoryBase, seed = 1) {
  return Array.from({ length: 60 }, (_, i) => ({
    t: new Date(now - (59 - i) * 15_000).toISOString(),
    cpu: Math.round(cpuBase + cpuBase * 0.35 * Math.sin((i + seed * 7) / 6) + ((i * seed * 13) % 9)),
    memory: Math.round(memoryBase + memoryBase * 0.03 * Math.sin((i + seed) / 9)),
  }));
}

export const pods = [
  { name: "web-7d9f8c6b5d-x2k4p", namespace: "shop", created: ago(60 * 26), status: "Running", ready: 1, containers: 1, restarts: 0, node: "worker-1", usage: { cpu: 12, memory: 84 * 1024 ** 2 } },
  { name: "web-7d9f8c6b5d-q8n2w", namespace: "shop", created: ago(60 * 26), status: "Running", ready: 1, containers: 1, restarts: 0, node: "worker-2", usage: { cpu: 9, memory: 81 * 1024 ** 2 } },
  { name: "checkout-api-6b7c9d8f4-mm2lz", namespace: "shop", created: ago(60 * 5), status: "CrashLoopBackOff", ready: 0, containers: 2, restarts: 616, node: "worker-1", usage: { cpu: 1, memory: 22 * 1024 ** 2 } },
  { name: "cart-redis-0", namespace: "shop", created: ago(60 * 24 * 12), status: "Running", ready: 1, containers: 1, restarts: 2, node: "worker-2", usage: { cpu: 4, memory: 12 * 1024 ** 2 } },
  { name: "postgres-0", namespace: "data", created: ago(60 * 24 * 40), status: "Running", ready: 2, containers: 2, restarts: 0, node: "worker-2", usage: { cpu: 31, memory: 412 * 1024 ** 2 } },
  { name: "postgres-1", namespace: "data", created: ago(3), status: "ContainerCreating", ready: 0, containers: 2, restarts: 0, node: "worker-1" },
  { name: "nightly-backup-29311420-7xkq2", namespace: "data", created: ago(60 * 9), status: "Completed", ready: 0, containers: 1, restarts: 0, node: "worker-1" },
  { name: "image-resizer-5c8d7b9f6-b4t7r", namespace: "media", created: ago(45), status: "ImagePullBackOff", ready: 0, containers: 1, restarts: 0, node: "worker-2" },
  { name: "thumbnail-worker-with-a-rather-long-generated-name-6f9d8c7b5-zz9q1", namespace: "media", created: ago(60 * 2), status: "Running", ready: 1, containers: 1, restarts: 0, node: "worker-1", usage: { cpu: 220, memory: 1.2 * GiB } },
  { name: "prometheus-0", namespace: "monitoring", created: ago(60 * 24 * 90), status: "Running", ready: 2, containers: 2, restarts: 1, node: "worker-2", usage: { cpu: 87, memory: 1.6 * GiB } },
  { name: "node-exporter-4kq9m", namespace: "monitoring", created: ago(60 * 24 * 90), status: "Running", ready: 1, containers: 1, restarts: 0, node: "control-1", usage: { cpu: 3, memory: 18 * 1024 ** 2 } },
  { name: "coredns-7db6d8ff4d-9hl2c", namespace: "kube-system", created: ago(60 * 24 * 120), status: "Running", ready: 1, containers: 1, restarts: 0, node: "control-1", usage: { cpu: 5, memory: 21 * 1024 ** 2 } },
  { name: "metrics-server-6f8c7d9b5-lr8kp", namespace: "kube-system", created: ago(60 * 24 * 120), status: "Pending", ready: 0, containers: 1, restarts: 0, node: "" },
];

export const nodes = [
  { name: "control-1", status: "Ready", schedulable: true, roles: ["control-plane"], version: "v1.31.4", internalIP: "10.0.0.10", os: "linux", arch: "arm64", osImage: "Ubuntu 24.04.1 LTS", cpu: 2000, memory: 2.9 * GiB, created: ago(60 * 24 * 120), usage: { cpu: 61, memory: 2.1 * GiB, cpuAllocatable: 2000, memoryAllocatable: 2.8 * GiB, history: history(60, 2.1 * GiB, 1) } },
  { name: "worker-1", status: "Ready", schedulable: true, roles: [], version: "v1.31.4", internalIP: "10.0.0.11", os: "linux", arch: "arm64", osImage: "Ubuntu 24.04.1 LTS", cpu: 2000, memory: 2.9 * GiB, created: ago(60 * 24 * 120), usage: { cpu: 1540, memory: 2.2 * GiB, cpuAllocatable: 2000, memoryAllocatable: 2.8 * GiB, history: history(1400, 2.2 * GiB, 2) } },
  { name: "worker-2", status: "NotReady", schedulable: false, roles: [], version: "v1.31.4", internalIP: "10.0.0.12", os: "linux", arch: "arm64", osImage: "Ubuntu 24.04.1 LTS", cpu: 2000, memory: 2.9 * GiB, created: ago(60 * 24 * 120), usage: { cpu: 340, memory: 2.6 * GiB, cpuAllocatable: 2000, memoryAllocatable: 2.8 * GiB, history: history(300, 2.6 * GiB, 3) } },
];

export const clusterUsage = {
  total: { cpu: 1941, memory: 6.9 * GiB, cpuAllocatable: 6000, memoryAllocatable: 8.4 * GiB },
  history: history(1800, 6.9 * GiB, 4),
  shortHistory: history(1800, 6.9 * GiB, 4).slice(-3), // under a minute: "collecting…"
};

export const podsByStatus = { Running: 46, Completed: 1, ContainerCreating: 1, Pending: 1, CrashLoopBackOff: 2, ImagePullBackOff: 1 };

export const events = [
  { name: "checkout-api.1", namespace: "shop", type: "Warning", reason: "BackOff", message: "Back-off restarting failed container api in pod checkout-api-6b7c9d8f4-mm2lz_shop(3fda411d-b93d-4702-bc94-b373e090db84)", object: "Pod/checkout-api-6b7c9d8f4-mm2lz", objectName: "checkout-api-6b7c9d8f4-mm2lz", objectResource: "pods", count: 2379, lastSeen: ago(1) },
  { name: "image-resizer.1", namespace: "media", type: "Warning", reason: "Failed", message: 'Failed to pull image "registry.example.com/media/image-resizer:2.4.1": rpc error: code = NotFound desc = failed to resolve reference: not found', object: "Pod/image-resizer-5c8d7b9f6-b4t7r", objectName: "image-resizer-5c8d7b9f6-b4t7r", objectResource: "pods", count: 14, lastSeen: ago(4) },
  { name: "postgres-1.1", namespace: "data", type: "Warning", reason: "FailedMount", message: 'MountVolume.SetUp failed for volume "config" : configmap "postgres-extra-config" not found', object: "Pod/postgres-1", objectName: "postgres-1", objectResource: "pods", count: 3, lastSeen: ago(6) },
  { name: "metrics-server.1", namespace: "kube-system", type: "Warning", reason: "FailedScheduling", message: "0/3 nodes are available: 1 node(s) had untolerated taint {node.kubernetes.io/not-ready: }, 2 Insufficient memory. preemption: 0/3 nodes are available.", object: "Pod/metrics-server-6f8c7d9b5-lr8kp", objectName: "metrics-server-6f8c7d9b5-lr8kp", objectResource: "pods", count: 1, lastSeen: ago(12) },
  { name: "web.1", namespace: "shop", type: "Normal", reason: "ScalingReplicaSet", message: "Scaled up replica set web-7d9f8c6b5d to 2 from 1", object: "Deployment/web", objectName: "web", objectResource: "deployments", count: 1, lastSeen: ago(60 * 3) },
  { name: "web-rs.1", namespace: "shop", type: "Normal", reason: "SuccessfulCreate", message: "Created pod: web-7d9f8c6b5d-q8n2w", object: "ReplicaSet/web-7d9f8c6b5d", objectName: "web-7d9f8c6b5d", count: 1, lastSeen: ago(60 * 3) },
];

export const labels = {
  app: "checkout-api",
  "app.kubernetes.io/part-of": "shop",
  "pod-template-hash": "6b7c9d8f4",
  tier: "backend",
};

export const annotations = {
  "kubectl.kubernetes.io/restartedAt": "2026-10-01T09:12:44Z",
  "prometheus.io/scrape": "true",
  "prometheus.io/port": "9102",
  "kubectl.kubernetes.io/last-applied-configuration":
    '{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"annotations":{},"labels":{"app":"checkout-api","tier":"backend"},"name":"checkout-api","namespace":"shop"},"spec":{"replicas":1,"selector":{"matchLabels":{"app":"checkout-api"}},"template":{"metadata":{"labels":{"app":"checkout-api","tier":"backend"}},"spec":{"containers":[{"env":[{"name":"DATABASE_URL","valueFrom":{"secretKeyRef":{"key":"url","name":"checkout-db"}}}],"image":"registry.example.com/shop/checkout-api:1.8.0","name":"api","ports":[{"containerPort":8080}]}]}}}}',
  "config.example.com/notes": "",
};

export const deploymentYaml = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout-api
  namespace: shop
  labels:
    app: checkout-api
    tier: backend
  annotations:
    deployment.kubernetes.io/revision: "7"
    prometheus.io/scrape: "true"
  generation: 7
  uid: 0b6c3a52-6c1e-4f43-9d6f-0d3a7e6f1c11
spec:
  replicas: 2
  revisionHistoryLimit: 10
  selector:
    matchLabels:
      app: checkout-api
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 25%
      maxUnavailable: 25%
  template:
    metadata:
      labels:
        app: checkout-api
        tier: backend
    spec:
      containers:
        - name: api
          image: registry.example.com/shop/checkout-api:1.8.0 # pinned until the next release
          ports:
            - containerPort: 8080
              protocol: TCP
          env:
            - name: LOG_LEVEL
              value: debug
            - name: FEATURE_URL
              value: http://flags.shop.svc/#checkout
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: checkout-db
                  key: url
          readinessProbe:
            httpGet:
              path: /healthz
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              memory: 256Mi
      restartPolicy: Always
      terminationGracePeriodSeconds: 30
status:
  availableReplicas: 1
  readyReplicas: 1
  replicas: 2
  unavailableReplicas: 1
  conditions:
    - type: Available
      status: "False"
      reason: MinimumReplicasUnavailable
      message: Deployment does not have minimum availability.
`;

const LOG_LINES = [
  'time="{t}" level=info msg="starting checkout-api" version=1.8.0 commit=4f2a9c1',
  'time="{t}" level=info msg="connecting to database" host=postgres.data.svc port=5432',
  'time="{t}" level=warning msg="slow query" duration=1.82s query="SELECT * FROM orders WHERE customer_id = $1 AND created_at > now() - interval \'30 days\' ORDER BY created_at DESC"',
  'time="{t}" level=info msg="GET /healthz" status=200 duration=1.2ms',
  'time="{t}" level=error msg="payment provider timed out" provider=example-pay attempt=3 err="context deadline exceeded"',
  'time="{t}" level=info msg="POST /api/checkout" status=201 duration=184ms order=ord_8f2k1',
];

// logLine(i) gives a plausible log line, so the live demo can keep adding them.
export function logLine(i) {
  const t = new Date(now - (200 - i) * 2000).toISOString().replace(/\.\d+Z$/, "Z");
  return LOG_LINES[i % LOG_LINES.length].replace("{t}", t);
}

export const logText = Array.from({ length: 120 }, (_, i) => logLine(i)).join("\n") + "\n";

// Diagnoses, shaped like GET /api/clusters/{id}/diagnosis/...
const node = (id, kind, state, status) => {
  const parts = id.split("/");
  const [resource, namespace, name] = parts.length === 2 ? [parts[0], "", parts[1]] : parts;
  return { id, resource, kind, namespace, name, state, status };
};

const missingSecret = {
  id: "missing|secrets/shop/stripe-key|",
  rule: "missing",
  object: "secrets/shop/stripe-key",
  severity: "failing",
  confidence: "certain",
  title: "Secret shop/stripe-key doesn't exist",
  detail: "It's needed by Deployment checkout (container api: env STRIPE_KEY).",
  evidence: [{ source: "reference", text: "Opscope read every Secret in the cluster and found no shop/stripe-key." }],
  blames: [],
  related: [],
  next: [{ label: "kubectl get", command: "kubectl get secrets -n shop" }],
  objects: ["secrets/shop/stripe-key"],
  root: true,
  causes: [],
};

export const diagnoses = {
  failing: {
    object: node("deployments/shop/checkout", "Deployment", "failing", "0/3"),
    steps: [
      {
        id: "workload.unavailable|deployments/shop/checkout|",
        rule: "workload.unavailable",
        object: "deployments/shop/checkout",
        severity: "failing",
        confidence: "certain",
        title: "None of its 3 replicas are ready",
        evidence: [],
        blames: ["pods/shop/checkout-7d9f-a", "pods/shop/checkout-7d9f-b", "pods/shop/checkout-7d9f-c"],
        related: [],
        next: [{ label: "Events", object: "deployments/shop/checkout", tab: "events" }],
        objects: ["deployments/shop/checkout"],
        root: false,
        causes: [
          {
            id: "pod.config|pods/shop/checkout-7d9f-a|secrets/shop/stripe-key",
            rule: "pod.config",
            object: "pods/shop/checkout-7d9f-a",
            severity: "failing",
            confidence: "certain",
            title: "Container api can't start: it needs Secret stripe-key",
            detail: "Kubernetes can't find it, so the container isn't created.",
            evidence: [
              { source: "container", text: 'api: CreateContainerConfigError: secret "stripe-key" not found' },
              { source: "event", text: 'Failed: Error: secret "stripe-key" not found', time: ago(1), count: 42 },
            ],
            blames: ["secrets/shop/stripe-key"],
            related: [],
            next: [{ label: "kubectl describe", command: "kubectl describe pods checkout-7d9f-a -n shop" }],
            objects: ["pods/shop/checkout-7d9f-a", "pods/shop/checkout-7d9f-b", "pods/shop/checkout-7d9f-c"],
            root: false,
            causes: [missingSecret],
          },
        ],
      },
    ],
    impact: [node("httproutes/shop/storefront", "HTTPRoute", "ok", "Accepted"), node("services/shop/checkout", "Service", "failing", "0/3 ready")],
    nodes: {
      "deployments/shop/checkout": node("deployments/shop/checkout", "Deployment", "failing", "0/3"),
      "pods/shop/checkout-7d9f-a": node("pods/shop/checkout-7d9f-a", "Pod", "failing", "CreateContainerConfigError"),
      "pods/shop/checkout-7d9f-b": node("pods/shop/checkout-7d9f-b", "Pod", "failing", "CreateContainerConfigError"),
      "pods/shop/checkout-7d9f-c": node("pods/shop/checkout-7d9f-c", "Pod", "failing", "CreateContainerConfigError"),
      "secrets/shop/stripe-key": node("secrets/shop/stripe-key", "Secret", "missing", "Missing"),
    },
    snapshot: { taken: ago(0.2), skipped: [] },
  },
  degraded: {
    object: node("pods/shop/api-5c8d-x", "Pod", "degraded", "Running"),
    steps: [
      {
        id: "pod.crash|pods/shop/api-5c8d-x|",
        rule: "pod.crash",
        object: "pods/shop/api-5c8d-x",
        severity: "degraded",
        confidence: "likely",
        title: "Container api fails its liveness probe, so it's restarted",
        detail:
          "Kubernetes restarts a container whose liveness probe fails. This one is an HTTP GET of /healthz on port 8080, every 10s after an initial 5s, giving up after 3 failures. Check that the probe matches what the program offers, and that its delay leaves enough time to start.",
        evidence: [
          { source: "container", text: "api: restarted 6 times; last stopped: Error (exit 137)", time: ago(3) },
          { source: "event", text: "Unhealthy: Liveness probe failed: HTTP probe failed with statuscode: 503", time: ago(2), count: 18 },
        ],
        blames: [],
        related: [],
        next: [
          { label: "Events", object: "pods/shop/api-5c8d-x", tab: "events" },
          { label: "kubectl describe", command: "kubectl describe pods api-5c8d-x -n shop" },
        ],
        objects: ["pods/shop/api-5c8d-x"],
        root: true,
        causes: [],
      },
    ],
    impact: [],
    nodes: { "pods/shop/api-5c8d-x": node("pods/shop/api-5c8d-x", "Pod", "degraded", "Running") },
    snapshot: { taken: ago(0.1), skipped: [] },
  },
  clear: {
    object: node("deployments/shop/web", "Deployment", "ok", "3/3"),
    steps: [],
    impact: [],
    nodes: {},
    snapshot: { taken: ago(0.15), skipped: [] },
  },
  partOfAProblem: {
    object: node("resourcequotas/shop/memory", "ResourceQuota", "ok", ""),
    steps: [],
    impact: [node("deployments/shop/reports", "Deployment", "failing", "0/1")],
    nodes: {},
    snapshot: { taken: ago(0.15), skipped: [] },
  },
};

// Root causes, shaped like GET /api/clusters/{id}/problems
const problem = (rule, severity, confidence, title, objects, affects) => ({
  id: `${rule}|${objects[0]}|`, rule, object: objects[0], severity, confidence, title,
  evidence: [], blames: [], related: [], next: [], objects, affects,
});
export const problems = {
  problems: [
    problem("missing", "failing", "certain", "Secret shop/stripe-key doesn't exist", ["secrets/shop/stripe-key"], [
      node("deployments/shop/checkout", "Deployment", "failing", "0/3"),
      node("httproutes/shop/storefront", "HTTPRoute", "ok", "Accepted"),
      node("services/shop/checkout", "Service", "failing", "0/3 ready"),
      node("ingresses/shop/web", "Ingress", "ok", ""),
    ]),
    problem("pod.crash", "failing", "certain", "Container app exits with code 1", ["pods/shop/worker-5c8d-a", "pods/shop/worker-5c8d-b"], [
      node("deployments/shop/worker", "Deployment", "failing", "0/2"),
    ]),
    problem("pod.image", "failing", "likely", "Image registry.example.com/billing:2.1 doesn't exist", ["pods/billing/api-7f6d-x"], [
      node("deployments/billing/api", "Deployment", "degraded", "1/2"),
    ]),
    problem("hpa.scaling", "degraded", "certain", "It can't read the metrics it scales by", ["horizontalpodautoscalers/shop/web"], []),
  ],
  nodes: {
    "secrets/shop/stripe-key": node("secrets/shop/stripe-key", "Secret", "missing", "Missing"),
    "pods/shop/worker-5c8d-a": node("pods/shop/worker-5c8d-a", "Pod", "failing", "CrashLoopBackOff"),
    "pods/shop/worker-5c8d-b": node("pods/shop/worker-5c8d-b", "Pod", "failing", "CrashLoopBackOff"),
    "pods/billing/api-7f6d-x": node("pods/billing/api-7f6d-x", "Pod", "failing", "ImagePullBackOff"),
    "horizontalpodautoscalers/shop/web": node("horizontalpodautoscalers/shop/web", "HorizontalPodAutoscaler", "degraded", "1/0"),
  },
};

diagnoses.unexplained = {
  object: node("pods/shop/legacy", "Pod", "failing", "Shutdown"),
  steps: [
    {
      id: "unexplained|pods/shop/legacy|",
      rule: "unexplained",
      object: "pods/shop/legacy",
      severity: "failing",
      confidence: "possible",
      title: "Opscope can't tell why it's failing",
      detail: "No rule explains this yet. Its status and events may say more.",
      evidence: [{ source: "status", text: "Shutdown Pod was terminated in response to imminent node shutdown." }],
      blames: [],
      related: ["services/shop/legacy"],
      next: [{ label: "Events", object: "pods/shop/legacy", tab: "events" }],
      objects: ["pods/shop/legacy"],
      root: true,
      causes: [],
    },
  ],
  impact: [],
  nodes: {
    "pods/shop/legacy": node("pods/shop/legacy", "Pod", "failing", "Shutdown"),
    "services/shop/legacy": node("services/shop/legacy", "Service", "failing", "0/1 ready"),
  },
  snapshot: { taken: ago(0.1), skipped: [] },
};

// ---- Topology: views as GET /topology returns them ----

const KINDS = {
  gatewayclasses: "GatewayClass",
  gateways: "Gateway",
  httproutes: "HTTPRoute",
  ingresses: "Ingress",
  ingressclasses: "IngressClass",
  services: "Service",
  horizontalpodautoscalers: "HorizontalPodAutoscaler",
  deployments: "Deployment",
  statefulsets: "StatefulSet",
  replicasets: "ReplicaSet",
  pods: "Pod",
  configmaps: "ConfigMap",
  secrets: "Secret",
  serviceaccounts: "ServiceAccount",
  persistentvolumeclaims: "PersistentVolumeClaim",
  persistentvolumes: "PersistentVolume",
  storageclasses: "StorageClass",
  resourcequotas: "ResourceQuota",
  nodes: "Node",
};

// card("deployments/shop/web", "ok", "3/3") is a node of a view.
function card(id, state = "ok", status = "", extra = {}) {
  const parts = id.split("/");
  return {
    id,
    resource: parts[0],
    kind: KINDS[parts[0]],
    namespace: parts.length === 3 ? parts[1] : undefined,
    name: parts.at(-1),
    state,
    status,
    ...extra,
  };
}

// podGroup("deployments/shop/web", 3, 3, "Running") is a workload's pods, as one card.
function podGroup(owner, ready, total, worst = "Running", state = ready === total ? "ok" : "failing") {
  const [, namespace, name] = owner.split("/");
  const members = Array.from({ length: total }, (_, i) => `pods/${namespace}/${name}-7d9f-${"abcdef"[i]}`);
  return {
    id: `pods:${owner}`,
    resource: "pods",
    kind: "Pod",
    namespace,
    name,
    state,
    status: `${ready}/${total} ready`,
    message: state === "ok" ? undefined : worst,
    pods: { owner, ready, total, members },
  };
}

const link = (from, to, type = "uses", via) => ({ from, to, type, ...(via ? { via } : {}) });

const finding = (object, rule, severity, title, blames = [], extra = {}) => ({
  id: `${rule}|${object}|${blames.join(",")}`,
  rule,
  object,
  severity,
  confidence: "certain",
  title,
  evidence: [],
  blames,
  related: [],
  next: [],
  ...extra,
});

const snapshot = { taken: ago(0.2), skipped: [] };

// A shop whose checkout can't start: its pods need a Secret that doesn't
// exist. The Gateway and the storefront are fine.
const checkoutPods = podGroup("deployments/shop/checkout", 0, 3, "CreateContainerConfigError");
export const topologyBroken = {
  snapshot,
  nodes: [
    card("gatewayclasses/nginx", "ok", "Accepted"),
    card("gateways/infra/main", "ok", "Programmed"),
    card("httproutes/shop/storefront", "ok", "Accepted"),
    card("services/shop/web", "ok", "3 endpoints"),
    card("services/shop/checkout", "failing", "0 ready", { message: "None of the pods it selects are ready" }),
    card("deployments/shop/web", "ok", "3/3"),
    card("deployments/shop/checkout", "failing", "0/3"),
    podGroup("deployments/shop/web", 3, 3),
    checkoutPods,
    card("configmaps/shop/settings", "ok", "4 keys"),
    card("secrets/shop/stripe-key", "missing", ""),
    card("serviceaccounts/shop/checkout", "ok", ""),
  ],
  edges: [
    link("gateways/infra/main", "gatewayclasses/nginx", "attaches"),
    link("httproutes/shop/storefront", "gateways/infra/main", "attaches"),
    link("httproutes/shop/storefront", "services/shop/web", "routes-to", ["rule 1"]),
    link("httproutes/shop/storefront", "services/shop/checkout", "routes-to", ["rule 2"]),
    link("services/shop/web", "pods:deployments/shop/web", "selects"),
    link("services/shop/checkout", "pods:deployments/shop/checkout", "selects"),
    link("deployments/shop/web", "pods:deployments/shop/web", "owns"),
    link("deployments/shop/checkout", "pods:deployments/shop/checkout", "owns"),
    link("pods:deployments/shop/web", "configmaps/shop/settings", "uses", ["container web: envFrom"]),
    link("pods:deployments/shop/checkout", "configmaps/shop/settings", "uses", ["container api: envFrom"]),
    link("pods:deployments/shop/checkout", "secrets/shop/stripe-key", "uses", ["container api: env STRIPE_KEY"]),
    link("pods:deployments/shop/checkout", "serviceaccounts/shop/checkout", "uses", ["serviceAccountName"]),
  ],
  findings: [
    finding("deployments/shop/checkout", "workload.unavailable", "failing", "None of its 3 replicas are ready", checkoutPods.pods.members),
    finding("services/shop/checkout", "service.endpoints", "failing", "None of the 3 pods it selects are ready", checkoutPods.pods.members),
    ...checkoutPods.pods.members.map((pod) =>
      finding(pod, "pod.config", "failing", "Container api can't start: it needs Secret stripe-key", ["secrets/shop/stripe-key"], {
        detail: "Kubernetes can't find it, so the container isn't created.",
        evidence: [{ source: "container", text: 'api: CreateContainerConfigError: secret "stripe-key" not found' }],
        next: [{ label: "kubectl describe", command: `kubectl describe pods ${pod.split("/").at(-1)} -n shop` }],
      }),
    ),
    finding("secrets/shop/stripe-key", "missing", "failing", "Secret shop/stripe-key doesn't exist", [], {
      detail: "It's needed by Deployment checkout (container api: env STRIPE_KEY).",
      evidence: [{ source: "reference", text: "Opscope read every Secret in the cluster and found no shop/stripe-key." }],
      next: [{ label: "kubectl get", command: "kubectl get secrets -n shop" }],
    }),
  ],
};

// A healthy app with storage and an autoscaler.
export const topologyHealthy = {
  snapshot,
  nodes: [
    card("ingressclasses/nginx", "ok", ""),
    card("ingresses/blog/blog", "ok", "blog.example.com"),
    card("services/blog/blog", "ok", "2 endpoints"),
    card("services/blog/db", "ok", "1 endpoint"),
    card("horizontalpodautoscalers/blog/blog", "ok", "2/2–6"),
    card("deployments/blog/blog", "progressing", "2/3", { message: "Rolling out revision 4" }),
    card("statefulsets/blog/db", "ok", "1/1"),
    podGroup("deployments/blog/blog", 2, 3, "ContainerCreating", "progressing"),
    podGroup("statefulsets/blog/db", 1, 1),
    card("configmaps/blog/blog-config", "ok", "6 keys"),
    card("secrets/blog/db-password", "ok", "1 key"),
    card("persistentvolumeclaims/blog/data-db-0", "ok", "Bound"),
    card("persistentvolumes/pvc-3f2a", "ok", "Bound"),
    card("storageclasses/standard", "ok", "default"),
  ],
  edges: [
    link("ingresses/blog/blog", "ingressclasses/nginx"),
    link("ingresses/blog/blog", "services/blog/blog", "routes-to"),
    link("services/blog/blog", "pods:deployments/blog/blog", "selects"),
    link("services/blog/db", "pods:statefulsets/blog/db", "selects"),
    link("horizontalpodautoscalers/blog/blog", "deployments/blog/blog", "scales"),
    link("deployments/blog/blog", "pods:deployments/blog/blog", "owns"),
    link("statefulsets/blog/db", "pods:statefulsets/blog/db", "owns"),
    link("pods:deployments/blog/blog", "configmaps/blog/blog-config"),
    link("pods:deployments/blog/blog", "secrets/blog/db-password"),
    link("pods:statefulsets/blog/db", "secrets/blog/db-password"),
    link("pods:statefulsets/blog/db", "persistentvolumeclaims/blog/data-db-0"),
    link("persistentvolumeclaims/blog/data-db-0", "persistentvolumes/pvc-3f2a"),
    link("persistentvolumes/pvc-3f2a", "storageclasses/standard", "uses", undefined),
  ],
  findings: [finding("deployments/blog/blog", "deployment.rollout", "info", "It's rolling out revision 4")],
};
topologyHealthy.edges.at(-1).optional = true;

// Missing and unknown objects, and a quota no edge leads to.
export const topologyMissing = {
  snapshot,
  nodes: [
    card("ingressclasses/no-such-class", "missing"),
    card("ingresses/lab/secure", "failing", "no address"),
    card("services/lab/api", "missing"),
    card("deployments/lab/worker", "failing", "0/2"),
    card("replicasets/lab/worker-5c8d", "failing", "0/2", { message: "exceeded quota: memory" }),
    card("resourcequotas/lab/memory", "ok", "1Gi of 1Gi"),
    card("pods/lab/reader", "failing", "CreateContainerConfigError"),
    card("secrets/lab/secure-tls", "missing"),
    card("secrets/lab/token", "unknown", "", { message: "Opscope isn't allowed to read Secrets" }),
  ],
  edges: [
    link("ingresses/lab/secure", "ingressclasses/no-such-class"),
    link("ingresses/lab/secure", "services/lab/api", "routes-to"),
    link("ingresses/lab/secure", "secrets/lab/secure-tls", "uses", ["tls"]),
    link("deployments/lab/worker", "replicasets/lab/worker-5c8d", "owns"),
    link("replicasets/lab/worker-5c8d", "resourcequotas/lab/memory", "blames"),
    link("pods/lab/reader", "secrets/lab/token"),
  ],
  findings: [
    finding("ingressclasses/no-such-class", "missing", "failing", "IngressClass no-such-class doesn't exist"),
    finding("services/lab/api", "missing", "failing", "Service lab/api doesn't exist"),
    finding("secrets/lab/secure-tls", "missing", "failing", "Secret lab/secure-tls doesn't exist"),
    finding("ingresses/lab/secure", "ingress.class", "failing", "Its IngressClass no-such-class doesn't exist", ["ingressclasses/no-such-class"]),
    finding("deployments/lab/worker", "workload.unavailable", "failing", "None of its 2 replicas are ready", ["replicasets/lab/worker-5c8d"]),
    finding("replicasets/lab/worker-5c8d", "create-failed", "failing", "ResourceQuota memory doesn't leave room for its pods", ["resourcequotas/lab/memory"], {
      detail: "Each pod asks for 512Mi of memory; 1Gi of the quota's 1Gi is in use.",
    }),
    finding("pods/lab/reader", "pod.config", "failing", "Container app can't start: it needs Secret token", ["secrets/lab/token"]),
    finding("secrets/lab/token", "unknown", "info", "Opscope can't check Secret lab/token", [], { confidence: "possible" }),
  ],
};

// A busy namespace: eight apps behind two routes, a few of them broken.
export const topologyLarge = (() => {
  const apps = ["cart", "catalog", "checkout", "search", "users", "orders", "payments", "reviews"];
  const broken = { payments: "CrashLoopBackOff", reviews: "ImagePullBackOff" };
  const nodes = [card("gateways/infra/main", "ok", "Programmed")];
  const edges = [];
  const findings = [];
  for (const route of ["store", "api"]) {
    nodes.push(card(`httproutes/shop/${route}`, "ok", "Accepted"));
    edges.push(link(`httproutes/shop/${route}`, "gateways/infra/main", "attaches"));
  }
  apps.forEach((app, i) => {
    const bad = broken[app];
    const group = podGroup(`deployments/shop/${app}`, bad ? 0 : 2, 2, bad ?? "Running");
    nodes.push(
      card(`services/shop/${app}`, bad ? "failing" : "ok", bad ? "0 ready" : "2 endpoints"),
      card(`deployments/shop/${app}`, bad ? "failing" : "ok", bad ? "0/2" : "2/2"),
      group,
      card(`configmaps/shop/${app}-config`, "ok", "3 keys"),
    );
    edges.push(
      link(`httproutes/shop/${i % 2 ? "api" : "store"}`, `services/shop/${app}`, "routes-to"),
      link(`services/shop/${app}`, group.id, "selects"),
      link(`deployments/shop/${app}`, group.id, "owns"),
      link(group.id, `configmaps/shop/${app}-config`),
      link(group.id, `secrets/shop/${i < 4 ? "db" : "queue"}-credentials`),
    );
    if (bad) {
      findings.push(finding(`deployments/shop/${app}`, "workload.unavailable", "failing", "None of its 2 replicas are ready", group.pods.members));
      for (const pod of group.pods.members) {
        findings.push(
          finding(pod, bad === "CrashLoopBackOff" ? "pod.crash" : "pod.image", "failing",
            bad === "CrashLoopBackOff" ? "Container app keeps crashing: it exits with code 1" : `Image registry.example.com/${app}:2.4 doesn't exist`),
        );
      }
    }
  });
  nodes.push(card("secrets/shop/db-credentials", "ok", "2 keys"), card("secrets/shop/queue-credentials", "ok", "2 keys"));
  return { snapshot, nodes, edges, findings };
})();
