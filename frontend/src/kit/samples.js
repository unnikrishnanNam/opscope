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
