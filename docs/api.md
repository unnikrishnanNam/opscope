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
