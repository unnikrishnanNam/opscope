// Graph answers for /kit, shaped as GET /api/clusters/{id}/graph/… sends
// them: nodes keyed by UID (made up here), edges with their evidence.

const KINDS = {
  gatewayclasses: "GatewayClass",
  gateways: "Gateway",
  httproutes: "HTTPRoute",
  ingresses: "Ingress",
  services: "Service",
  endpointslices: "EndpointSlice",
  deployments: "Deployment",
  replicasets: "ReplicaSet",
  statefulsets: "StatefulSet",
  daemonsets: "DaemonSet",
  cronjobs: "CronJob",
  jobs: "Job",
  pods: "Pod",
  configmaps: "ConfigMap",
  secrets: "Secret",
  serviceaccounts: "ServiceAccount",
  persistentvolumeclaims: "PersistentVolumeClaim",
  persistentvolumes: "PersistentVolume",
  storageclasses: "StorageClass",
  nodes: "Node",
};

const CLUSTER_WIDE = new Set(["gatewayclasses", "persistentvolumes", "storageclasses", "nodes"]);

// A tiny builder: add("pods/shop/web-1", { status }) makes a node; a key
// with "missing:" or "unknown:" in front makes a placeholder, as the API's.
function builder() {
  const nodes = new Map();
  const edges = [];
  const add = (key, extra = {}) => {
    const presence = key.startsWith("missing:") ? "missing" : key.startsWith("unknown:") ? "unknown" : "present";
    const bare = key.replace(/^(missing|unknown):/, "");
    const parts = bare.split("/");
    const resource = parts[0];
    const id = presence === "present" ? `uid-${bare.replaceAll("/", "-")}` : key;
    nodes.set(key, {
      id,
      resource,
      kind: KINDS[resource] ?? resource,
      ...(CLUSTER_WIDE.has(resource) ? {} : { namespace: parts[1] }),
      name: parts.at(-1),
      presence,
      status: "",
      health: null,
      ...extra,
    });
    return id;
  };
  const edge = (from, to, kind, evidence = [], extra = {}) => {
    const f = nodes.get(from).id;
    const t = nodes.get(to).id;
    edges.push({
      id: `${f}>${t}>${kind}`,
      from: f,
      to: t,
      kind,
      evidence: evidence.map(([field, note = ""]) => ({ builder: "kit", field, note, resourceVersion: "1", seen: "2026-11-10T12:00:00Z" })),
      ...extra,
    });
  };
  const answer = (extra = {}) => ({ built: "live", partial: [], nodes: [...nodes.values()], edges, ...extra });
  return { add, edge, answer, id: (key) => nodes.get(key).id };
}

// shop: a namespace with something for every edge kind: a Gateway and an
// Ingress in front of a Service, a Deployment and a StatefulSet with their
// config and storage, a CronJob, and a few things that aren't there.
export function shopAnswer({ focus } = {}) {
  const b = builder();
  b.add("gatewayclasses/nginx", { status: "Accepted" });
  b.add("gateways/shop/edge", { status: "Programmed" });
  b.add("httproutes/shop/store", { status: "Accepted" });
  b.add("ingresses/shop/web");
  b.add("services/shop/api");
  b.add("services/shop/db");
  b.add("services/shop/ghost", { dangling: [{ kind: "selects", field: "spec.selector", reason: "matches no pods" }] });
  b.add("missing:services/shop/old-api");
  b.add("endpointslices/shop/api-x7k2p");
  b.add("deployments/shop/api", { status: "2/3" });
  b.add("replicasets/shop/api-5d8c", { status: "2/3" });
  b.add("replicasets/shop/api-77f1", { status: "0/0" });
  b.add("statefulsets/shop/db", { status: "1/1" });
  b.add("cronjobs/shop/nightly");
  b.add("jobs/shop/nightly-29384", { status: "Complete" });
  const apiPods = ["api-5d8c-2kxl9", "api-5d8c-9fh2q", "api-5d8c-vm7tn"];
  apiPods.forEach((p, i) => b.add(`pods/shop/${p}`, { status: i === 2 ? "ContainerCreating" : "Running" }));
  b.add("pods/shop/db-0", { status: "Running" });
  b.add("pods/shop/nightly-29384-pq8x", { status: "Completed" });
  b.add("pods/shop/broken", { status: "CreateContainerConfigError" });
  b.add("configmaps/shop/app-config");
  b.add("secrets/shop/db-creds");
  b.add("secrets/shop/tls-cert");
  b.add("missing:secrets/shop/stripe-key");
  b.add("missing:configmaps/shop/missing-config");
  b.add("serviceaccounts/shop/api");
  b.add("persistentvolumeclaims/shop/data", { status: "Bound" });
  b.add("persistentvolumeclaims/shop/data-db-0", { status: "Bound" });
  b.add("persistentvolumeclaims/shop/pending", { status: "Pending" });
  b.add("persistentvolumes/pvc-1f3a9c", { status: "Bound" });
  b.add("persistentvolumes/pvc-8b2e44", { status: "Bound" });
  b.add("storageclasses/standard");
  b.add("missing:storageclasses/slow");
  b.add("nodes/kind-worker", { status: "Ready" });

  b.edge("gateways/shop/edge", "gatewayclasses/nginx", "attaches", [["spec.gatewayClassName"]]);
  b.edge("gateways/shop/edge", "secrets/shop/tls-cert", "references", [["spec.listeners[0].tls.certificateRefs[0]", "listener https"]]);
  b.edge("httproutes/shop/store", "gateways/shop/edge", "attaches", [["spec.parentRefs[0]", "listener https"]]);
  b.edge("httproutes/shop/store", "services/shop/api", "routes", [["spec.rules[0].backendRefs[0]", "port 80"]]);
  b.edge("ingresses/shop/web", "services/shop/api", "routes", [["spec.rules[0].http.paths[0].backend.service.name", "shop.example/api"]]);
  b.edge("ingresses/shop/web", "missing:services/shop/old-api", "routes", [["spec.rules[0].http.paths[1].backend.service.name", "shop.example/old"]]);
  b.edge("ingresses/shop/web", "secrets/shop/tls-cert", "references", [["spec.tls[0].secretName", "TLS"]]);
  b.edge("services/shop/api", "endpointslices/shop/api-x7k2p", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  b.edge("services/shop/db", "pods/shop/db-0", "selects", [["spec.selector"]]);
  b.edge("deployments/shop/api", "replicasets/shop/api-5d8c", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  b.edge("deployments/shop/api", "replicasets/shop/api-77f1", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  for (const ref of ["configmaps/shop/app-config", "secrets/shop/db-creds", "serviceaccounts/shop/api"]) {
    const evidence = {
      "configmaps/shop/app-config": [["spec.template.spec.volumes[0].configMap.name", "volume config"]],
      "secrets/shop/db-creds": [["spec.template.spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env DB_PASSWORD"]],
      "serviceaccounts/shop/api": [["spec.template.spec.serviceAccountName", "service account"]],
    }[ref];
    b.edge("deployments/shop/api", ref, "references", evidence);
  }
  b.edge("deployments/shop/api", "persistentvolumeclaims/shop/data", "references", [
    ["spec.template.spec.volumes[1].persistentVolumeClaim.claimName", "volume data"],
  ]);
  apiPods.forEach((p, i) => {
    const pod = `pods/shop/${p}`;
    b.edge("replicasets/shop/api-5d8c", pod, "owns", [["metadata.ownerReferences[0]", "controller"]]);
    b.edge("services/shop/api", pod, "selects", [["spec.selector"]]);
    b.edge("endpointslices/shop/api-x7k2p", pod, "endpoints", [[`endpoints[${i}].targetRef`, i === 2 ? "not ready" : "ready"]]);
    b.edge(pod, "configmaps/shop/app-config", "references", [["spec.volumes[0].configMap.name", "volume config"]]);
    b.edge(pod, "secrets/shop/db-creds", "references", [["spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env DB_PASSWORD"]]);
    b.edge(pod, "serviceaccounts/shop/api", "references", [["spec.serviceAccountName", "service account"]]);
    b.edge(pod, "persistentvolumeclaims/shop/data", "references", [["spec.volumes[1].persistentVolumeClaim.claimName", "volume data"]]);
    b.edge(pod, "nodes/kind-worker", "runs-on", [["spec.nodeName"]]);
  });
  b.edge("statefulsets/shop/db", "pods/shop/db-0", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  b.edge("statefulsets/shop/db", "persistentvolumeclaims/shop/data-db-0", "references", [["spec.volumeClaimTemplates[0]", "claim template data"]]);
  b.edge("pods/shop/db-0", "persistentvolumeclaims/shop/data-db-0", "references", [["spec.volumes[0].persistentVolumeClaim.claimName", "volume data"]]);
  b.edge("pods/shop/db-0", "secrets/shop/db-creds", "references", [["spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env POSTGRES_PASSWORD"]]);
  b.edge("pods/shop/db-0", "nodes/kind-worker", "runs-on", [["spec.nodeName"]]);
  b.edge("cronjobs/shop/nightly", "jobs/shop/nightly-29384", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  b.edge("jobs/shop/nightly-29384", "pods/shop/nightly-29384-pq8x", "owns", [["metadata.ownerReferences[0]", "controller"]]);
  b.edge("cronjobs/shop/nightly", "configmaps/shop/app-config", "references", [
    ["spec.jobTemplate.spec.template.spec.containers[0].envFrom[0].configMapRef.name", "envFrom"],
  ]);
  b.edge("jobs/shop/nightly-29384", "configmaps/shop/app-config", "references", [["spec.template.spec.containers[0].envFrom[0].configMapRef.name", "envFrom"]]);
  b.edge("pods/shop/nightly-29384-pq8x", "configmaps/shop/app-config", "references", [["spec.containers[0].envFrom[0].configMapRef.name", "envFrom"]]);
  b.edge("pods/shop/nightly-29384-pq8x", "nodes/kind-worker", "runs-on", [["spec.nodeName"]]);
  b.edge("pods/shop/broken", "missing:secrets/shop/stripe-key", "references", [["spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env STRIPE_KEY"]]);
  b.edge("pods/shop/broken", "missing:configmaps/shop/missing-config", "references", [["spec.volumes[0].configMap.name", "volume config"]]);
  b.edge("pods/shop/broken", "configmaps/shop/app-config", "references", [["spec.containers[0].envFrom[0].configMapRef.name", "envFrom"]], {
    optional: true,
  });
  b.edge("pods/shop/broken", "nodes/kind-worker", "runs-on", [["spec.nodeName"]]);
  b.edge("persistentvolumeclaims/shop/data", "persistentvolumes/pvc-1f3a9c", "binds", [["spec.volumeName"]]);
  b.edge("persistentvolumeclaims/shop/data", "storageclasses/standard", "binds", [["spec.storageClassName"]]);
  b.edge("persistentvolumeclaims/shop/data-db-0", "persistentvolumes/pvc-8b2e44", "binds", [["spec.volumeName"]]);
  b.edge("persistentvolumeclaims/shop/data-db-0", "storageclasses/standard", "binds", [["spec.storageClassName"]]);
  b.edge("persistentvolumeclaims/shop/pending", "missing:storageclasses/slow", "binds", [["spec.storageClassName"]]);
  b.edge("persistentvolumes/pvc-1f3a9c", "storageclasses/standard", "binds", [["spec.storageClassName"]]);
  b.edge("persistentvolumes/pvc-8b2e44", "storageclasses/standard", "binds", [["spec.storageClassName"]]);
  return b.answer({ focus: focus && b.id(focus) });
}

// gaps: every kind of gap. Secrets can't be read (as for a default install
// without secrets-access.yaml), a Rollout (a kind Opscope doesn't know)
// owns pods, a Service selects nothing, and things referred to are missing.
export function gapsAnswer() {
  const b = builder();
  b.add("unknown:Rollout.argoproj.io/web@4f2c", { resource: "", kind: "Rollout", namespace: undefined, name: "web" });
  b.add("pods/team-a/web-6c9f-a", { status: "Running" });
  b.add("pods/team-a/web-6c9f-b", { status: "Running" });
  b.add("services/team-a/web");
  b.add("services/team-a/web-preview", { dangling: [{ kind: "selects", field: "spec.selector", reason: "matches no pods" }] });
  b.add("unknown:secrets/team-a/web-tls");
  b.add("unknown:secrets/team-a/registry");
  b.add("missing:configmaps/team-a/feature-flags");
  b.add("configmaps/team-a/web-config");
  for (const p of ["web-6c9f-a", "web-6c9f-b"]) {
    const pod = `pods/team-a/${p}`;
    b.edge("unknown:Rollout.argoproj.io/web@4f2c", pod, "owns", [["metadata.ownerReferences[0]", "controller"]]);
    b.edge("services/team-a/web", pod, "selects", [["spec.selector"]]);
    b.edge(pod, "configmaps/team-a/web-config", "references", [["spec.volumes[0].configMap.name", "volume config"]]);
    b.edge(pod, "missing:configmaps/team-a/feature-flags", "references", [["spec.containers[0].envFrom[0].configMapRef.name", "envFrom"]], {
      optional: true,
    });
    b.edge(pod, "unknown:secrets/team-a/web-tls", "references", [["spec.volumes[1].secret.secretName", "volume tls"]]);
    b.edge(pod, "unknown:secrets/team-a/registry", "references", [["spec.imagePullSecrets[0].name", "image pull secret"]]);
  }
  return b.answer({ partial: [{ resource: "secrets", code: "forbidden", reason: "listing across all namespaces isn't allowed" }] });
}

// large: a namespace of about 1,000 objects: 110 apps, each a Deployment
// with its ReplicaSet, three pods, a Service and its EndpointSlice, a
// ConfigMap and a Secret, on ten nodes.
export function largeAnswer(apps = 110) {
  const b = builder();
  for (let n = 0; n < 10; n++) b.add(`nodes/worker-${n}`, { status: "Ready" });
  for (let i = 0; i < apps; i++) {
    const app = `app-${String(i).padStart(3, "0")}`;
    b.add(`deployments/big/${app}`, { status: "3/3" });
    b.add(`replicasets/big/${app}-7d9f`, { status: "3/3" });
    b.add(`services/big/${app}`);
    b.add(`endpointslices/big/${app}-x`);
    b.add(`configmaps/big/${app}-config`);
    b.add(`secrets/big/${app}-secret`);
    b.edge(`deployments/big/${app}`, `replicasets/big/${app}-7d9f`, "owns", [["metadata.ownerReferences[0]", "controller"]]);
    b.edge(`deployments/big/${app}`, `configmaps/big/${app}-config`, "references", [["spec.template.spec.volumes[0].configMap.name", "volume config"]]);
    b.edge(`deployments/big/${app}`, `secrets/big/${app}-secret`, "references", [["spec.template.spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env TOKEN"]]);
    b.edge(`services/big/${app}`, `endpointslices/big/${app}-x`, "owns", [["metadata.ownerReferences[0]", "controller"]]);
    for (let p = 0; p < 3; p++) {
      const pod = `pods/big/${app}-7d9f-${p}`;
      b.add(pod, { status: "Running" });
      b.edge(`replicasets/big/${app}-7d9f`, pod, "owns", [["metadata.ownerReferences[0]", "controller"]]);
      b.edge(`services/big/${app}`, pod, "selects", [["spec.selector"]]);
      b.edge(`endpointslices/big/${app}-x`, pod, "endpoints", [[`endpoints[${p}].targetRef`, "ready"]]);
      b.edge(pod, `configmaps/big/${app}-config`, "references", [["spec.volumes[0].configMap.name", "volume config"]]);
      b.edge(pod, `secrets/big/${app}-secret`, "references", [["spec.containers[0].env[0].valueFrom.secretKeyRef.name", "env TOKEN"]]);
      b.edge(pod, `nodes/worker-${(i * 3 + p) % 10}`, "runs-on", [["spec.nodeName"]]);
    }
  }
  return b.answer();
}
