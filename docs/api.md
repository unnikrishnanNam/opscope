# HTTP API

Opscope's web UI is built on a small JSON API served by the same process, under `/api/`. This page
describes it for anyone who wants to script against a running Opscope or understand what the UI
asks for.

> **Not a stable API.** It exists to serve the web UI and may change between minor versions. Pin
> an Opscope version if you depend on it.

The API has no authentication of its own, like the UI. Anyone who can reach it can read everything
Opscope can read; see [Exposing Opscope](deployment.md#exposing-opscope).

## Conventions

- All endpoints are `GET` unless noted, and return JSON, except pod logs, which are plain text.
- Requests that read a cluster never change it. The only endpoints that change anything are
  `POST` and `DELETE` on `/api/clusters`, and they change Opscope's own list of clusters, not a
  cluster.
- `{id}` is a cluster's id from `GET /api/clusters`.
- CPU is in millicores and memory in bytes. Times are RFC 3339.
- Request bodies are limited to 1 MiB.

### Errors

Errors have a readable message and, when a cluster's API server returned the error, the original
error as `detail`:

```json
{ "error": "The cluster rejected the credentials in the kubeconfig. They may have expired.", "detail": "Unauthorized" }
```

| Status | When                                                                              |
| ------ | --------------------------------------------------------------------------------- |
| `400`  | The request is invalid (a bad body, a kubeconfig that can't be used, a bad log request) |
| `403`  | The cluster's RBAC doesn't allow the read (often Secrets)                         |
| `404`  | No such cluster, resource type, object or endpoint                                |
| `502`  | The cluster couldn't be reached or failed to answer                               |

Two optional features answer `404` with a `code` rather than failing, so a client can tell "not
installed" from an error:

| `code`                | Meaning                                                              |
| --------------------- | -------------------------------------------------------------------- |
| `not_installed`       | The cluster has no Gateway API (`gateway.networking.k8s.io/v1`)      |
| `metrics_unavailable` | The cluster has no working metrics-server                            |

## Health

### `GET /api/health`

```json
{ "status": "ok", "version": "v1.2.0" }
```

Used by the Kubernetes liveness and readiness probes. It doesn't contact any cluster.

## Clusters

### `GET /api/clusters`

Every cluster Opscope knows, environment clusters first:

```json
[
  { "id": "multipass", "name": "multipass", "source": "env", "context": "kubernetes-admin@kubernetes", "server": "https://10.0.0.10:6443" }
]
```

`source` is `env` for a cluster from `OPSCOPE_KUBECONFIG` or `OPSCOPE_IN_CLUSTER`, and `ui` for one
added in the UI.

### `GET /api/clusters/{id}`

The cluster, plus whether it answers. It returns `200` whenever the cluster exists; a cluster that
can't be reached has `reachable: false` and an explanation:

| Field       | Value                                                        |
| ----------- | ------------------------------------------------------------ |
| `reachable` | Whether the API server answered                              |
| `version`   | The Kubernetes version, when reachable (`v1.31.4`)           |
| `via`       | Where it was reached, when not at `server` (a kind node)     |
| `error`     | A readable explanation, when not                             |
| `detail`    | The original error, when not                                 |

### `POST /api/clusters/inspect`

Lists the contexts in a kubeconfig, without saving or connecting.

- Request: `{ "kubeconfig": "<kubeconfig YAML>" }`
- Response: `{ "contexts": [{ "name": "...", "server": "..." }], "current": "<current context>" }`

### `POST /api/clusters`

Adds a cluster. Opscope checks that the kubeconfig is safe to accept from a browser and that the
cluster answers, then saves only the chosen context.

- Request: `{ "name": "lab", "kubeconfig": "<kubeconfig YAML>", "context": "<context name>" }`
- `201` with the new cluster.
- `400` when the kubeconfig can't be used. Kubeconfigs sent this way must have their credentials
  embedded: `exec` and `auth-provider` (which run a program) and file paths (which read files on the
  Opscope server) are rejected.
- `502` when the cluster can't be reached, with `error` and `detail`.

### `DELETE /api/clusters/{id}`

Removes a cluster added in the UI and deletes its saved file. `204` when done, `404` for an unknown
id, and `400` for an environment cluster, which can only be removed by changing how Opscope is
started.

## Resources

### `GET /api/clusters/{id}/{resource}`

One row per object of a resource type, with the columns its table shows. Every row has `name`,
`namespace` (namespaced kinds only) and `created`.

| Parameter    | Effect                                                         |
| ------------ | -------------------------------------------------------------- |
| `namespace`  | Only objects in this namespace (namespaced kinds)              |
| `type`       | Events only: `Warning` or `Normal`                             |

Events are returned newest first, at most 100.

### `GET /api/clusters/{id}/overview`

Counts for the overview page. `namespace` limits the namespaced counts.

```json
{
  "namespaces": 12,
  "nodes": { "total": 3, "ready": 3 },
  "pods": { "total": 54, "byStatus": { "Running": 52, "CrashLoopBackOff": 2 } },
  "workloads": [{ "resource": "deployments", "total": 20, "unhealthy": 1 }],
  "gatewayAPI": [{ "resource": "gateways", "total": 2, "unhealthy": 0 }]
}
```

`unhealthy` counts what needs a look: workloads with fewer replicas ready than wanted, failed Jobs,
Gateways that aren't `Programmed` and HTTPRoutes that aren't `Accepted`. CronJobs are never counted as
unhealthy.
`gatewayAPI` is `null` when the cluster has no Gateway API.

### `GET /api/clusters/{id}/names`

Every object's resource type, namespace and name, for search. Events are not included.

```json
{
  "objects": [{ "resource": "pods", "namespace": "web", "name": "api-7d9f" }],
  "skipped": [{ "resource": "secrets", "code": "forbidden" }]
}
```

A resource type that can't be listed is named in `skipped` instead of failing the request, with
`code` `not_installed`, `forbidden` or `failed` (with `error`). The request fails only when nothing
at all can be read.

### `GET /api/clusters/{id}/{resource}/{namespace}/{name}`
### `GET /api/clusters/{id}/{resource}/{name}`

One object, for its detail page. The second form is for cluster-wide kinds (`nodes`, `namespaces`,
`gatewayclasses`).

| Field                      | Contents                                                          |
| -------------------------- | ----------------------------------------------------------------- |
| `name`, `namespace`, `created`, `kind`, `uid` | Identity                                       |
| `labels`, `annotations`    | As on the object                                                  |
| `owners`                   | Owner references (`kind`, `name`, and `resource` when Opscope has a page for it) |
| `fields`                   | Short facts (`label`, `value`), such as a pod's node              |
| `containers`               | For pods and anything with a pod template                         |
| `conditions`               | From `status.conditions`                                          |
| `tables`                   | Kind-specific lists, such as a Service's ports                    |
| `data`                     | ConfigMap entries                                                 |
| `secretKeys`               | Secret key names only                                             |
| `events`                   | Events about the object                                           |
| `yaml`                     | The object as YAML                                                |

Sections that don't apply to a kind are empty. For Secrets, the YAML has every value replaced with
`(hidden, about N bytes)` and leaves out kubectl's `last-applied-configuration` annotation, which
would otherwise contain the values.

### `GET /api/clusters/{id}/secrets/{namespace}/{name}/{key}`

One value of one Secret. This is the only endpoint that returns a Secret value; the UI calls it
only when you choose "Reveal" on a key.

```json
{ "value": "s3cr3t", "base64": false }
```

`base64` is `true` when the value isn't readable text, and `value` is then base64-encoded. The
response is sent with `Cache-Control: no-store`.

### `GET /api/clusters/{id}/pods/{namespace}/{name}/logs`

A pod's logs as plain text, sent with `Cache-Control: no-store`.

| Parameter   | Effect                                                                          |
| ----------- | ------------------------------------------------------------------------------- |
| `container` | Which container; may be left out when the pod has only one                      |
| `tail`      | How many recent lines: default 500, at most 10,000                              |
| `previous`  | `true`: the previous run of the container, after a restart                      |
| `follow`    | `true`: keep the response open and send new lines as they're written            |

Without `follow`, a request is cut off after 30 seconds. A request the API server rejects (no
previous run, a container that doesn't exist) returns `400` with its message.

## Topology and diagnosis

### `GET /api/clusters/{id}/topology`

A graph of objects and what they need, for the whole cluster, one namespace, one app or one
object's neighbourhood. Opscope reads the whole cluster at once for it and uses that reading for up
to 10 seconds, so several requests in a row read the cluster only once. It also reads each cluster
in the background (see [Background snapshots](deployment.md#background-snapshots)).

| Parameter   | Effect                                                                                  |
| ----------- | --------------------------------------------------------------------------------------- |
| `namespace` | A namespace: everything in it, what points at it from elsewhere, and what all of that needs |
| `app`       | With `namespace`, one app of that namespace                                             |
| `focus`     | One object's ID (`pods/shop/web-1`, `nodes/worker-1`) and everything within `depth` edges of it, not going on through a node, a namespace or a class that many objects share |
| `depth`     | With `focus`, from 1 to 5; default 2                                                    |
| `expand`    | Workloads whose pods are shown one by one, as comma-separated IDs, or `all`; for the whole cluster, namespaces (`namespaces/blog`) shown open although nothing is wrong in them |
| `problems`  | `true`: only objects that aren't `ok`, and what points at them                          |

Without `namespace` or `focus`, the whole cluster (below). An unknown `focus` answers `404`, and
`app` without `namespace` answers `400`.

```json
{
  "nodes": [
    { "id": "deployments/shop/api", "resource": "deployments", "kind": "Deployment", "namespace": "shop",
      "name": "api", "app": "api", "state": "failing", "status": "0/2", "message": "None of its pods are ready." },
    { "id": "pods:replicasets/shop/api-7d9f", "resource": "pods", "kind": "Pod", "namespace": "shop",
      "name": "api", "app": "api", "state": "failing", "status": "0/2 ready",
      "message": "secret \"stripe\" not found",
      "pods": { "owner": "deployments/shop/api", "ready": 0, "total": 2,
                "members": ["pods/shop/api-7d9f-a", "pods/shop/api-7d9f-b"] } },
    { "id": "secrets/shop/stripe", "resource": "secrets", "kind": "Secret", "namespace": "shop",
      "name": "stripe", "app": "api", "state": "missing", "status": "Missing",
      "message": "It's referred to, but doesn't exist." }
  ],
  "edges": [
    { "from": "deployments/shop/api", "to": "pods:replicasets/shop/api-7d9f", "type": "owns" },
    { "from": "pods:replicasets/shop/api-7d9f", "to": "secrets/shop/stripe", "type": "uses",
      "via": ["container api: env STRIPE_KEY"] }
  ],
  "apps": [{ "name": "api", "state": "failing", "workloads": 1 }],
  "findings": [
    { "id": "pod.config|pods/shop/api-7d9f-a|secrets/shop/stripe", "rule": "pod.config",
      "object": "pods/shop/api-7d9f-a", "severity": "failing", "confidence": "certain",
      "title": "Container api can't start: it needs Secret stripe", "blames": ["secrets/shop/stripe"],
      "related": [], "evidence": [], "next": [] }
  ],
  "snapshot": { "taken": "2026-10-11T10:00:00Z", "skipped": [{ "resource": "secrets", "code": "forbidden" }] }
}
```

A node's `id` is its resource type, namespace and name, as in the detail page's URL. Pods of one
workload are grouped into one node whose `id` starts with `pods:` and which has `pods` set, unless
`expand` asks for them. `apps`, the namespace's apps, is returned for a namespace or an app (apps
of nothing but unused configuration are left out). `snapshot` says when the cluster
was read and which resource types couldn't be (as in `/names`); objects of those types are
`unknown` rather than `missing`. `findings` are the diagnosis's findings about every object shown,
the pods in a group included, as in the [diagnosis](#get-apiclustersiddiagnosisresourcenamespacename)
steps but without nesting; following `blames` from one leads to its root causes. What a finding
blames is always in the view: when nothing else would put it there (a ResourceQuota that stops a
ReplicaSet), it's added with a `blames` edge.

**The whole cluster** is drawn by namespace. Each app is one node (`resource` `apps`, `kind`
`App`, `id` `apps/<namespace>/<app>`) with a summary as its `status` ("3 workloads, 1 failing")
and its namespace as `group`; `groups` lists those namespaces with their worst state. A namespace
with nothing wrong in it is one node instead (`resource` `namespaces`, `status` "4 apps") unless
`expand` names it. Gateways, GatewayClasses and IngressClasses are nodes of their own, shared by
apps, and every node (machine) is there. The edges join these: an app's routes attach to a
Gateway, its Ingress uses a class, a Gateway owns the app that runs it, an app routes to another;
`via` names the objects inside the app. Only root causes come as `findings`, and an app's or a
folded namespace's `members` lists the objects whose findings it shows, so each finding belongs to
a node (a missing StorageClass belongs to the apps whose claims need it).

```json
{
  "nodes": [
    { "id": "apps/shop/checkout", "resource": "apps", "kind": "App", "namespace": "shop", "name": "checkout",
      "app": "checkout", "state": "failing", "status": "1 workload, 1 failing", "group": "namespaces/shop",
      "members": ["secrets/shop/stripe-key"] },
    { "id": "namespaces/blog", "resource": "namespaces", "kind": "Namespace", "name": "blog", "state": "ok",
      "status": "2 apps" },
    { "id": "gateways/infra/main", "resource": "gateways", "kind": "Gateway", "namespace": "infra",
      "name": "main", "state": "ok", "status": "Programmed" }
  ],
  "edges": [
    { "from": "namespaces/blog", "to": "gateways/infra/main", "type": "attaches", "via": ["HTTPRoute blog"] }
  ],
  "groups": [{ "id": "namespaces/shop", "name": "shop", "state": "failing", "apps": 1 }],
  "findings": [{ "id": "missing|secrets/shop/stripe-key|", "rule": "missing", "object": "secrets/shop/stripe-key", "...": "..." }],
  "snapshot": { "taken": "2026-10-12T10:00:00Z", "skipped": [] }
}
```

| `state`       | Meaning                                                                           |
| ------------- | --------------------------------------------------------------------------------- |
| `ok`          | Working                                                                           |
| `progressing` | Starting, rolling out or scaling, for less than 5 minutes                         |
| `degraded`    | Working, but not fully: some replicas not ready, a node under pressure            |
| `failing`     | Not working                                                                       |
| `missing`     | Referred to by another object, but doesn't exist                                  |
| `unknown`     | Referred to, but Opscope can't read that resource type                            |

An edge points from an object to something it needs. `via` says where the references are (at most
five, then "and N more"), and `optional` is `true` when every reference is optional, so a missing
target breaks nothing.

| `type`      | From → to                                                                   |
| ----------- | --------------------------------------------------------------------------- |
| `owns`      | Deployment → ReplicaSet → pods, CronJob → Job → pods, and other owners      |
| `runs-on`   | Pod → node                                                                  |
| `uses`      | Pod or workload → ConfigMap, Secret, claim, service account; claim → volume → StorageClass; Ingress → IngressClass, TLS Secret; Gateway → certificate Secret |
| `selects`   | Service or PodDisruptionBudget → pods                                       |
| `routes-to` | Ingress or HTTPRoute → Service                                              |
| `attaches`  | HTTPRoute → Gateway → GatewayClass                                          |
| `permits`   | HTTPRoute → the ReferenceGrant that lets it reach a Service in another namespace; a missing one is named `(none)` |
| `scales`    | HorizontalPodAutoscaler → what it scales                                    |
| `blames`    | Only in views: a finding about the first object blames the second, and no other edge joins them |

### `GET /api/clusters/{id}/diagnosis/{resource}/{namespace}/{name}`
### `GET /api/clusters/{id}/diagnosis/{resource}/{name}`

Why one object is failing, from the same reading of the cluster as `/topology`. The second form is
for cluster-wide kinds (`nodes`, `gatewayclasses`). An object Opscope doesn't know answers `404`.

```json
{
  "object": { "id": "deployments/shop/api", "kind": "Deployment", "state": "failing", "status": "0/1" },
  "steps": [
    {
      "id": "workload.unavailable|deployments/shop/api|pods/shop/api-7d9f-a",
      "rule": "workload.unavailable", "object": "deployments/shop/api",
      "severity": "failing", "confidence": "certain",
      "title": "Its only replica isn't ready",
      "evidence": [], "blames": ["pods/shop/api-7d9f-a"],
      "next": [{ "label": "Events", "object": "deployments/shop/api", "tab": "events" }],
      "objects": ["deployments/shop/api"], "root": false,
      "causes": [
        {
          "rule": "pod.config", "title": "Container api can't start: it needs Secret stripe",
          "detail": "Kubernetes can't find it, so the container isn't created.",
          "evidence": [{ "source": "container", "text": "api: CreateContainerConfigError: secret \"stripe\" not found" }],
          "blames": ["secrets/shop/stripe"], "objects": ["pods/shop/api-7d9f-a"], "root": false,
          "causes": [
            {
              "rule": "missing", "title": "Secret shop/stripe doesn't exist", "root": true,
              "next": [{ "label": "kubectl get", "command": "kubectl get secrets -n shop" }]
            }
          ]
        }
      ]
    }
  ],
  "impact": [],
  "nodes": { "pods/shop/api-7d9f-a": { "id": "pods/shop/api-7d9f-a", "kind": "Pod", "status": "CreateContainerConfigError" } },
  "snapshot": { "taken": "2026-10-11T10:00:00Z", "skipped": [] }
}
```

(Some fields are left out above for length; every step has all of them.)

| Field      | Contents                                                                                     |
| ---------- | -------------------------------------------------------------------------------------------- |
| `steps`    | The object's findings, worst first. Each has its `causes`: the findings of the objects it `blames`, and so on |
| `root`     | `true` when nothing a step blames has a finding of its own: a root cause. One reached along two paths (a node that's down, through its pods and through their replacements) is under both, with the same `id` |
| `objects`  | What a step is about. Alike findings are merged: pods failing the same way are one step      |
| `severity` | `failing`, `degraded`, or `info` for something worth knowing that isn't a fault              |
| `confidence` | `certain` (from structured status), `likely` (read from event or message text), `possible` |
| `next`     | Links to an Opscope page (`object`, `tab`, `query`) or a read-only kubectl `command`. Only `get`, `describe` and `logs` are ever suggested |
| `related`  | Only on `unexplained` findings, which no rule could explain: unhealthy neighbours that may be involved, a guess |
| `impact`   | Objects whose problems lead to this one; pods are left out when their workload is listed     |
| `nodes`    | Every object the steps name, as in `/topology`, to show and link them                        |

### `GET /api/clusters/{id}/problems`

The root causes of what's wrong, in one namespace (`namespace`) or the whole cluster, for the
overview. Each is a finding, as in the diagnosis, that nothing else explains, with what it affects.
Alike root causes are one problem: two pods crashing the same way are one, with both in `objects`.

```json
{
  "problems": [
    {
      "rule": "missing", "severity": "failing", "confidence": "certain",
      "title": "Secret shop/stripe doesn't exist",
      "objects": ["secrets/shop/stripe"],
      "affects": [{ "id": "deployments/shop/api", "kind": "Deployment", "state": "failing", "status": "0/1" }]
    }
  ],
  "nodes": { "secrets/shop/stripe": { "id": "secrets/shop/stripe", "kind": "Secret", "state": "missing" } },
  "snapshot": { "taken": "2026-10-11T10:00:00Z", "skipped": [] }
}
```

(Each problem also has every field of a diagnosis step's finding.) The worst come first, then those
that affect the most. A problem belongs to a namespace when it, or anything it affects, is in it: a
node that's down is a problem in every namespace with pods on it. Notes (`info`) aren't problems.

## Usage

Usage comes from metrics-server. Without it, these endpoints answer `404` with
`metrics_unavailable`.

### `GET /api/clusters/{id}/metrics/nodes`

```json
{
  "nodes": [{ "name": "worker-1", "cpu": 420, "memory": 2147483648, "cpuAllocatable": 4000, "memoryAllocatable": 8254390272 }],
  "total": { "name": "cluster", "cpu": 900, "memory": 5368709120, "cpuAllocatable": 12000, "memoryAllocatable": 24763170816 },
  "history": [{ "t": "2026-10-09T10:00:00Z", "cpu": 880, "memory": 5300000000 }],
  "nodeHistory": { "worker-1": [{ "t": "2026-10-09T10:00:00Z", "cpu": 410, "memory": 2100000000 }] }
}
```

Opscope samples each cluster's usage every 15 seconds in the background and keeps the last
15 minutes in memory for `history` and `nodeHistory` (oldest first). The history starts empty when
Opscope restarts.

### `GET /api/clusters/{id}/metrics/pods`

Usage per pod, with its containers added together. `namespace` limits it to one namespace.

```json
[{ "namespace": "web", "name": "api-7d9f", "cpu": 12, "memory": 73400320 }]
```

## Resource types

The values of `{resource}`:

| `{resource}`     | Kind         | Scope     | API group                   | Detail page |
| ---------------- | ------------ | --------- | --------------------------- | ----------- |
| `namespaces`     | Namespace    | cluster   | core                        | yes         |
| `nodes`          | Node         | cluster   | core                        | yes         |
| `events`         | Event        | namespace | core                        | no          |
| `pods`           | Pod          | namespace | core                        | yes         |
| `deployments`    | Deployment   | namespace | `apps`                      | yes         |
| `statefulsets`   | StatefulSet  | namespace | `apps`                      | yes         |
| `daemonsets`     | DaemonSet    | namespace | `apps`                      | yes         |
| `jobs`           | Job          | namespace | `batch`                     | yes         |
| `cronjobs`       | CronJob      | namespace | `batch`                     | yes         |
| `configmaps`     | ConfigMap    | namespace | core                        | yes         |
| `secrets`        | Secret       | namespace | core                        | yes         |
| `services`       | Service      | namespace | core                        | yes         |
| `ingresses`      | Ingress      | namespace | `networking.k8s.io`         | yes         |
| `gateways`       | Gateway      | namespace | `gateway.networking.k8s.io` | yes         |
| `httproutes`     | HTTPRoute    | namespace | `gateway.networking.k8s.io` | yes         |
| `gatewayclasses` | GatewayClass | cluster   | `gateway.networking.k8s.io` | yes         |

The list is defined in `backend/internal/resources/resources.go` (`Listers`) and, for detail
pages, `detail.go`.

## Examples

```bash
curl -s http://localhost:8080/api/clusters
```

```bash
curl -s "http://localhost:8080/api/clusters/multipass/pods?namespace=kube-system"
```

```bash
curl -sN "http://localhost:8080/api/clusters/multipass/pods/web/api-7d9f/logs?tail=100&follow=true"
```

```bash
curl -s "http://localhost:8080/api/clusters/multipass/topology?namespace=web&problems=true"
```
