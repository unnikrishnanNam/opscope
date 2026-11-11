# HTTP API

Opscope's web UI is built on a small JSON API served by the same process, under `/api/`. This page
describes it for anyone who wants to script against a running Opscope or understand what the UI
asks for.

> **Not a stable API.** It exists to serve the web UI and may change between minor versions. Pin
> an Opscope version if you depend on it.

The API has no authentication of its own, like the UI. Anyone who can reach it can read everything
Opscope can read; see [Giving people access](deployment.md#giving-people-access).

## Conventions

- All endpoints are `GET` unless noted, and return JSON, except pod logs, which are plain text,
  and the live stream, which is server-sent events.
- Requests that read a cluster never change it. The only endpoints that change anything are
  `POST` and `DELETE` on `/api/clusters`, and they change Opscope's own list of clusters, not a
  cluster.
- `{id}` is a cluster's id from `GET /api/clusters`.
- CPU is in millicores, and memory and storage in bytes. Times are RFC 3339.
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

Optional features answer with a `code` rather than failing, so a client can tell "not installed"
or "turned off" from an error:

| Status | `code`                   | Meaning                                                          |
| ------ | ------------------------ | ---------------------------------------------------------------- |
| `404`  | `not_installed`          | The cluster has no Gateway API (`gateway.networking.k8s.io/v1`)  |
| `404`  | `metrics_unavailable`    | The cluster has no working metrics-server                        |
| `404`  | `volume_usage_off`       | Volume usage is off (`OPSCOPE_VOLUME_USAGE` isn't `true`)        |
| `403`  | `volume_usage_forbidden` | Volume usage is on, but Opscope may not `get` `nodes/proxy`      |

## Health

### `GET /api/health`

```json
{ "status": "ok", "version": "v1.4.0" }
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

One row per object of a resource type, with the columns its table shows. Every row has `uid`,
`name`, `namespace` (namespaced kinds only) and `created`. The `uid` is the object's
`metadata.uid`: unlike the name, it isn't reused when an object is deleted and made again.

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
  "storage": [{ "resource": "persistentvolumeclaims", "total": 8, "unhealthy": 1 }],
  "gatewayAPI": [{ "resource": "gateways", "total": 2, "unhealthy": 0 }]
}
```

`unhealthy` counts what needs a look: workloads with fewer replicas ready than wanted, failed Jobs,
claims that are `Pending` or `Lost`, volumes that are `Released` or `Failed`, Gateways that aren't
`Programmed` and HTTPRoutes that aren't `Accepted`. CronJobs are never counted as unhealthy.
PersistentVolumes, like nodes, are counted across the whole cluster whatever the `namespace`.
`storage` is `null` when the cluster has no claims and no volumes, or Opscope isn't allowed to list
them. `gatewayAPI` is `null` when the cluster has no Gateway API.

### `GET /api/clusters/{id}/names`

Every object's resource type, UID, namespace and name, for search. Events are not included.

```json
{
  "objects": [{ "resource": "pods", "uid": "6f1c0d2e-…", "namespace": "web", "name": "api-7d9f" }],
  "skipped": [{ "resource": "secrets", "code": "forbidden" }]
}
```

A resource type that can't be listed is named in `skipped` instead of failing the request, with
`code` `not_installed`, `forbidden` or `failed` (with `error`). The request fails only when nothing
at all can be read.

### `GET /api/clusters/{id}/{resource}/{namespace}/{name}`
### `GET /api/clusters/{id}/{resource}/{name}`

One object, for its detail page. The second form is for cluster-wide kinds (`nodes`, `namespaces`,
`persistentvolumes`, `storageclasses`, `gatewayclasses`).

| Field                      | Contents                                                          |
| -------------------------- | ----------------------------------------------------------------- |
| `name`, `namespace`, `created`, `kind`, `uid` | Identity                                       |
| `labels`, `annotations`    | As on the object                                                  |
| `owners`                   | Owner references (`kind`, `name`, and `resource` when Opscope has a page for it) |
| `fields`                   | Short facts (`label`, `value`), such as a pod's node, and `link` when the value names another object |
| `containers`               | For pods and anything with a pod template                         |
| `conditions`               | From `status.conditions`                                          |
| `tables`                   | Kind-specific lists, such as a Service's ports or a pod's volumes; `links` matches `rows` cell by cell, where a cell names another object |
| `data`                     | ConfigMap entries                                                 |
| `secretKeys`               | Secret key names only                                             |
| `events`                   | Events about the object                                           |
| `yaml`                     | The object as YAML                                                |

A `link` is `{ "resource": "persistentvolumes", "name": "pvc-1a2b" }`, with `namespace` for
namespaced kinds. Sections that don't apply to a kind are empty. For Secrets, the YAML has every value replaced with
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

## Live updates

Opscope keeps a copy of each cluster in memory, watched for changes, and lists are read from it
(see [Permissions](deployment.md#permissions)). These endpoints say how that copy is kept and
when it changes. With live data turned off (`OPSCOPE_LIVE=false`), every
list is read from the cluster on each request.

### `GET /api/clusters/{id}/live`

```json
{
  "enabled": true,
  "state": "live",
  "kinds": [
    { "resource": "pods", "mode": "watch", "synced": true, "objects": 412, "lastChange": "2026-11-02T10:04:11Z" },
    { "resource": "secrets", "mode": "forbidden", "reason": "listing isn't allowed", "synced": false, "objects": 0, "lastChange": null },
    { "resource": "gateways", "mode": "not_installed", "reason": "not installed on this cluster", "synced": false, "objects": 0, "lastChange": null }
  ]
}
```

`state` is the cluster's as a whole:

| `state`       | Meaning                                                                        |
| ------------- | ------------------------------------------------------------------------------ |
| `live`        | Every kind is watched, or read from the cluster on each request                 |
| `partial`     | Some kinds may only be listed, not watched, so they're listed every 30 seconds |
| `connecting`  | Some kinds haven't been read for the first time yet                             |
| `unreachable` | The cluster doesn't answer; lists are read from it, and fail, until it does    |
| `off`         | Live data is turned off; `enabled` is `false` and `kinds` is empty             |

Each kind's `mode` says how it's read:

| `mode`          | How                                                                                 |
| --------------- | ----------------------------------------------------------------------------------- |
| `watch`         | Listed once, then watched                                                           |
| `poll`          | Listed every 30 seconds: watching isn't allowed                                     |
| `direct`        | Read from the cluster on each request, one namespace at a time: listing across all namespaces isn't allowed |
| `forbidden`     | Listing isn't allowed; its list answers `403`                                       |
| `not_installed` | The cluster doesn't have it (Gateway API)                                           |
| `pending`       | Not known yet, or the cluster didn't answer; read from the cluster meanwhile        |

`synced` is whether a watched or polled kind has its first list, `objects` how many objects it
holds, and `lastChange` when it last saw a change after that first list. `error` is the last
failed list, watch or probe while the kind isn't synced. Opscope probes the kinds that aren't
watched again every 5 minutes, so a permission granted or Gateway API installed is picked up
without a restart.

### `GET /api/clusters/{id}/live/stream`

[Server-sent events](https://html.spec.whatwg.org/multipage/server-sent-events.html) that say
when to fetch again. They never carry objects: a page that's told its kind changed asks for its
list again.

```
event: status
data: {"state":"live","kinds":[{"resource":"pods","mode":"watch"},{"resource":"secrets","mode":"forbidden","reason":"listing isn't allowed"}]}

event: changed
data: {"resource":"pods","namespaces":["default","shop"]}

event: changed
data: {"resource":"nodes"}

event: heartbeat
data: {}
```

| Event       | When                                                                                       |
| ----------- | ------------------------------------------------------------------------------------------ |
| `status`    | First, and whenever `state` or a kind's mode changes; `/live` without counts or errors      |
| `changed`   | Objects of `resource` were added, changed or deleted, in `namespaces` (left out for cluster-wide kinds); at most once a second per resource |
| `resync`    | Changes may have been missed (the stream fell behind): fetch everything again              |
| `heartbeat` | Every 15 seconds, so proxies keep the connection and a client can tell it's alive          |

The stream sets `Cache-Control: no-cache` and `X-Accel-Buffering: no`, so an nginx in front of
Opscope passes messages on at once. With live data off it answers `204 No Content`, which tells a
browser's `EventSource` not to reconnect. A cluster whose collector hasn't started yet (for a
moment after it's added) answers `503`.

After reconnecting, fetch everything again: changes made while disconnected aren't replayed.

## Graph

Opscope keeps a graph of how each cluster's objects connect: what a pod uses, what selects it, what
it runs on, what routes traffic to a Service. Every edge says which field it was read from. These
endpoints query it; the Topology page and detail pages' links are built on them.

Every answer has the same shape:

```json
{
  "built": "live",
  "partial": [{ "resource": "secrets", "code": "forbidden", "reason": "listing across all namespaces isn't allowed" }],
  "focus": "3f6c1e2a-…",
  "nodes": [
    {
      "id": "3f6c1e2a-…",
      "resource": "pods",
      "kind": "Pod",
      "namespace": "shop",
      "name": "web-7d9f-x1",
      "presence": "present",
      "status": "Running",
      "health": null,
      "resourceVersion": "48211"
    },
    {
      "id": "missing:secrets/shop/stripe-key",
      "resource": "secrets",
      "kind": "Secret",
      "namespace": "shop",
      "name": "stripe-key",
      "presence": "missing",
      "health": null
    }
  ],
  "edges": [
    {
      "id": "9b2e…",
      "from": "3f6c1e2a-…",
      "to": "missing:secrets/shop/stripe-key",
      "kind": "references",
      "evidence": [
        {
          "builder": "pods.spec",
          "field": "spec.containers[0].env[1].valueFrom.secretKeyRef.name",
          "note": "env STRIPE_KEY",
          "resourceVersion": "48211",
          "seen": "2026-11-10T12:00:00Z"
        }
      ]
    }
  ]
}
```

- `built` is `live` when the graph is kept up to date from the copy in memory, or `on_request`
  when it was built for this request from the cluster (live data off, the cluster unreachable, or
  pods readable only per namespace); a graph built on request is reused for 10 seconds.
- `partial` lists the kinds the graph couldn't read, and why: `code` is `not_installed` (the
  cluster doesn't have the kind, as with Gateway API) or `forbidden` (Opscope isn't allowed to
  list it), and `reason` says it in words. References to their objects are `unknown` nodes.
- `focus` is the node the query was about; `matches` (search only) the nodes that answer it.
- A node's `id` is its object's UID. An object that's referred to but doesn't exist is a
  `missing` node, `missing:<resource>/<namespace>/<name>`; one whose kind can't be read is
  `unknown`. A Service whose selector matches nothing has a `dangling` entry
  (`{"kind":"selects","field":"spec.selector","reason":"matches no pods"}`).
- `status` is what the node's list page shows, for display. `health` is always `null` for now.
- An edge points from the object that needs to the one it needs: Pod → Secret, Service → Pod,
  HTTPRoute → Service. Its `id` is derived from its two ends and kind, so it's the same in every
  answer. Each piece of `evidence` is one field that makes it; `optional` is set when every
  reference behind it is marked optional.

| Edge `kind`  | From → To                                                                 |
| ------------ | ------------------------------------------------------------------------- |
| `owns`       | Deployment → ReplicaSet → Pod; StatefulSet, DaemonSet, Job → Pod; CronJob → Job; Service → EndpointSlice; any owner → what it owns |
| `references` | Pod or workload → ConfigMap, Secret, PersistentVolumeClaim, ServiceAccount; StatefulSet → its claims; Ingress, Gateway → TLS Secret |
| `binds`      | PersistentVolumeClaim → PersistentVolume → StorageClass; claim → StorageClass |
| `runs-on`    | Pod → Node                                                                |
| `selects`    | Service → Pod                                                             |
| `endpoints`  | EndpointSlice → Pod (`note`: `ready` or `not ready`)                      |
| `routes`     | Ingress, HTTPRoute → Service                                              |
| `attaches`   | HTTPRoute → Gateway → GatewayClass                                        |

Every endpoint takes `?namespace=`: a graph built on request reads that namespace (and the
cluster-wide kinds). Walks take `?depth=` (steps, 2 by default, at most 10) and `?kinds=` (edge
kinds to follow, comma-separated). A walk reaches a Node, StorageClass or GatewayClass but doesn't
go on through it, unless it starts there. Node IDs are passed in the query string, since a
placeholder's has slashes. An unknown node is `404`, a bad parameter `400`.

### `GET /api/clusters/{id}/graph/find?resource=&namespace=&name=&depth=`

One object, named the way pages name it, and everything one edge away, or `depth` edges either
way. A missing or unknown object is found as its placeholder.

### `GET /api/clusters/{id}/graph/node?id=`

One node by ID, and everything one edge away.

### `GET /api/clusters/{id}/graph/upstream?id=&depth=&kinds=`

What a node depends on: its edges followed forward.

### `GET /api/clusters/{id}/graph/downstream?id=&depth=&kinds=`

What depends on a node: its edges followed backward. For a Secret, every pod and workload that uses
it, and what sends traffic to those.

### `GET /api/clusters/{id}/graph/path?from=&to=&kinds=`

The shortest chain of edges between two nodes, in either direction, in order from `from`. `404`
when nothing joins them.

### `GET /api/clusters/{id}/graph/subgraph?id=&depth=&direction=&kinds=`
### `GET /api/clusters/{id}/graph/subgraph?namespace=`

With `id`, a node's neighbourhood: `direction` is `up`, `down` or `both` (the default). Without, a
whole namespace, with what it's joined to outside it one edge away.

### `GET /api/clusters/{id}/graph/search?q=&namespace=`

Graph search, as the command palette asks it. `q` is `using <name>` (what has an edge to any object
of that name) or `on <node>` (what runs on that node), with an optional kind first: `pods using
db-creds`, `what runs on node-3`. `<name>` can be `namespace/name`. With `namespace`, only answers
in that namespace or cluster-wide count. A phrase it can't read is `400`, with what it can.

## Usage

CPU and memory usage comes from metrics-server. Without it, these endpoints answer `404` with
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

### `GET /api/clusters/{id}/metrics/volumes`

How full each claim's volume is, from the kubelets. Off unless Opscope runs with
`OPSCOPE_VOLUME_USAGE=true`; see [Volume usage](deployment.md#volume-usage) for what that needs.
`namespace` limits it to one namespace.

```json
{
  "volumes": [{ "namespace": "data", "name": "postgres-data", "used": 2254857830, "capacity": 10726932480, "available": 8455118848, "inodesUsed": 1204, "inodes": 655360 }],
  "unread": [{ "node": "worker-2", "error": "context deadline exceeded", "claims": ["data/logs"] }],
  "unmeasured": ["web/uploads"]
}
```

| Field        | Contents                                                                            |
| ------------ | ----------------------------------------------------------------------------------- |
| `volumes`    | Claims with numbers. `capacity` is the filesystem's size, which can differ a little from the claim's |
| `unread`     | Nodes that couldn't be read, with the claims mounted there                          |
| `unmeasured` | Claims that are mounted, but the kubelet gave no numbers (hostPath volumes, or just mounted) |

A claim in none of the lists isn't mounted by a running pod; the kubelet only measures mounted
volumes. Each node's answer is kept for 30 seconds.
## Resource types

The values of `{resource}`:

| `{resource}`             | Kind                  | Scope     | API group                   | Detail page |
| ------------------------ | --------------------- | --------- | --------------------------- | ----------- |
| `namespaces`             | Namespace             | cluster   | core                        | yes         |
| `nodes`                  | Node                  | cluster   | core                        | yes         |
| `events`                 | Event                 | namespace | core                        | no          |
| `pods`                   | Pod                   | namespace | core                        | yes         |
| `deployments`            | Deployment            | namespace | `apps`                      | yes         |
| `statefulsets`           | StatefulSet           | namespace | `apps`                      | yes         |
| `daemonsets`             | DaemonSet             | namespace | `apps`                      | yes         |
| `jobs`                   | Job                   | namespace | `batch`                     | yes         |
| `cronjobs`               | CronJob               | namespace | `batch`                     | yes         |
| `configmaps`             | ConfigMap             | namespace | core                        | yes         |
| `secrets`                | Secret                | namespace | core                        | yes         |
| `services`               | Service               | namespace | core                        | yes         |
| `ingresses`              | Ingress               | namespace | `networking.k8s.io`         | yes         |
| `gateways`               | Gateway               | namespace | `gateway.networking.k8s.io` | yes         |
| `httproutes`             | HTTPRoute             | namespace | `gateway.networking.k8s.io` | yes         |
| `gatewayclasses`         | GatewayClass          | cluster   | `gateway.networking.k8s.io` | yes         |
| `persistentvolumeclaims` | PersistentVolumeClaim | namespace | core                        | yes         |
| `persistentvolumes`      | PersistentVolume      | cluster   | core                        | yes         |
| `storageclasses`         | StorageClass          | cluster   | `storage.k8s.io`            | yes         |

The list is defined in `backend/internal/resources/resources.go` (`Listers`) and, for detail
pages, `detail.go`. The graph also reads ReplicaSets (`apps`), EndpointSlices (`discovery.k8s.io`)
and ServiceAccounts (core), which have no endpoints of their own (`graphOnly`).

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
