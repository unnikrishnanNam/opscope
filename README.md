# OpScope

A small, read-only Kubernetes dashboard. Go backend, React frontend, one Docker image.

The project is built in phases; see [docs/PHASES.md](docs/PHASES.md) for the plan and progress.

## Layout

```
backend/                  Go server
  main.go                 entry point: reads env vars, loads clusters, starts the server
  internal/clusters/      known clusters: from env or added in the UI, one client each
  internal/resources/     one file per resource type: lists it and flattens it into table rows
                          (gatewayapi.go reads Gateway API custom resources with the dynamic client)
  internal/metrics/       live usage from metrics-server, and the in-memory history
  internal/server/        HTTP routes, middleware, static file serving
frontend/                 React app (Vite, plain JavaScript)
  src/api.js              fetch helper and the useApi hook
  src/clusters.jsx        shared list of clusters (React context)
  src/columns.jsx         table columns for each resource type
  src/format.js           ages and durations, kubectl style
  src/sections.js         list of pages; drives the sidebar and the routes
  src/components/         shared pieces (layout, sidebar, top bar, tables, status badges)
  src/pages/              one file per page
  src/styles.css          all styles; design tokens at the top
data/                     local data (git- and docker-ignored): kubeconfigs, saved clusters
Dockerfile                builds the single image
Makefile                  common commands
docs/PHASES.md            build plan and progress
```

## Requirements

- Go 1.26+
- Node.js 24+
- Docker (for the image)

## Connecting clusters

OpScope has no built-in cluster and never reads `~/.kube/config` on its own. There are two ways
to give it one:

1. **From the environment.** Set `OPSCOPE_KUBECONFIG` to a kubeconfig file. That cluster is
   loaded at startup and marked "Environment" in the UI. Any kubeconfig works here, including
   ones that log in through a command (cloud CLI plugins).
2. **From the UI.** Open *Clusters → Add a cluster*, paste or upload a kubeconfig and pick a
   context. OpScope tests the connection and saves only that context to `DATA_DIR/clusters/`
   (files readable only by OpScope). For safety, kubeconfigs added this way must have their
   credentials embedded and can't run login commands. `kubectl config view --minify --flatten`
   prints a suitable copy of your current context.

> **OpScope has no login.** Anyone who can open the page can read every cluster it knows.
> Keep it on `127.0.0.1` (the Makefile does) or behind something that adds authentication.

## Run in development

Two terminals:

```bash
make dev-backend
```

```bash
make dev-frontend
```

Open http://localhost:5173. Vite reloads the page when you edit frontend files and forwards
`/api/*` calls to the Go server on port 8080. Restart `make dev-backend` after Go changes.

To start the backend with a cluster from a file:

```bash
make dev-backend OPSCOPE_KUBECONFIG=$PWD/data/multipass.kubeconfig
```

## Run with Docker

```bash
make docker-build
```

Then either start it empty and add clusters in the UI:

```bash
make docker-run
```

or with a cluster from a kubeconfig file (mounted read-only):

```bash
make docker-run-env KUBECONFIG_FILE=data/multipass.kubeconfig
```

Open http://localhost:8080. Saved clusters are kept in the `opscope-data` Docker volume.

The container must be able to reach the cluster's API server. A `server:` address of
`127.0.0.1` or `localhost` in the kubeconfig points at the container itself, not your machine.

### Using a kind cluster with Docker

kind clusters run in Docker and publish their API server on your machine's `127.0.0.1`, which a
container can't reach. Put OpScope on kind's Docker network instead, and use the kubeconfig kind
writes for that network (it uses the node's container name, which kind's certificate includes):

```bash
kind get kubeconfig --internal --name <cluster> > data/kind.internal.kubeconfig
```

Then either start OpScope with `--network kind` added to `docker run`, or connect a running one:

```bash
docker network connect kind <opscope-container>
```

When OpScope runs directly on your machine (`make dev-backend`), the normal kubeconfig works.

## Configuration

| Variable                 | Default              | What it does                                                   |
| ------------------------ | -------------------- | -------------------------------------------------------------- |
| `PORT`                 | `8080`             | Port the server listens on                                     |
| `STATIC_DIR`           | `../frontend/dist` | Folder with the built React app                                |
| `DATA_DIR`             | `../data`          | Where clusters added in the UI are saved (`/data` in Docker) |
| `OPSCOPE_KUBECONFIG`   | (none)               | Kubeconfig file for a cluster loaded at startup                |
| `OPSCOPE_CONTEXT`      | current context      | Which context of that file to use                              |
| `OPSCOPE_CLUSTER_NAME` | context name         | Display name for that cluster                                  |

## API

| Method | Path                              | Returns                                                             |
| ------ | --------------------------------- | ------------------------------------------------------------------- |
| GET    | `/api/health`                   | `{"status":"ok","version":"..."}`                                 |
| GET    | `/api/clusters`                 | All clusters (id, name, source, context, server)                    |
| POST   | `/api/clusters/inspect`         | Contexts in a kubeconfig:`{"kubeconfig": "..."}`                  |
| POST   | `/api/clusters`                 | Add a cluster:`{"name", "kubeconfig", "context"}`                 |
| GET    | `/api/clusters/{id}`            | Cluster plus`reachable`, `version`, or `error` and `detail` |
| DELETE | `/api/clusters/{id}`            | Remove a cluster added in the UI                                    |
| GET    | `/api/clusters/{id}/{resource}` | Rows for one resource type; `?namespace=` to limit to one namespace |
| GET    | `/api/clusters/{id}/overview`   | Counts, node health and pods by status; `?namespace=` limits the namespaced counts |
| GET    | `/api/clusters/{id}/secrets/{namespace}/{name}/{key}` | One secret value: `{"value", "base64"}`; sent with `Cache-Control: no-store` |
| GET    | `/api/clusters/{id}/{resource}/{namespace}/{name}` | One object: summary fields, containers, conditions, tables, events, YAML |
| GET    | `/api/clusters/{id}/{resource}/{name}` | The same for cluster-wide kinds (nodes, namespaces, gatewayclasses) |
| GET    | `/api/clusters/{id}/pods/{namespace}/{name}/logs` | Plain-text logs; `?container=`, `?tail=` (default 500, max 10000), `?previous=true`, `?follow=true` streams |
| GET    | `/api/clusters/{id}/metrics/nodes` | Usage per node vs allocatable, cluster total, and 15 minutes of history |
| GET    | `/api/clusters/{id}/metrics/pods` | Usage per pod (containers summed); `?namespace=` |

`{resource}` is one of `namespaces`, `nodes`, `events`, `pods`, `deployments`, `statefulsets`,
`daemonsets`, `jobs`, `cronjobs`, `configmaps`, `secrets`, `services`, `ingresses`, `gateways`,
`httproutes` or `gatewayclasses` (see `backend/internal/resources/resources.go`). Events are
returned newest first (at most 100) and accept `?type=Warning` or `?type=Normal`.

Live usage needs [metrics-server](https://github.com/kubernetes-sigs/metrics-server) in the cluster.
Without it, the metrics endpoints answer `404` with `"code": "metrics_unavailable"` and the UI shows
how to install it. OpScope samples every cluster every 15 seconds in the background and keeps the
last 15 minutes in memory for the sparklines; the history starts empty after a restart.

Gateway API types are read with client-go's dynamic client (`gateway.networking.k8s.io/v1`). On a
cluster without Gateway API they answer `404` with `"code": "not_installed"`, and the overview's
`gatewayAPI` field is `null`.

Secret detail pages show key names too; their YAML has every value replaced with
`(hidden, about N bytes)` and leaves out kubectl's `last-applied-configuration` annotation,
which would otherwise contain the values.

The secrets list only ever contains key names. A value is sent only by the endpoint above, one key
at a time, when someone clicks "Reveal" in the UI.

Errors look like `{"error": "readable message", "detail": "original error"}`.
