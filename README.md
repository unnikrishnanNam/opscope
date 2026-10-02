# OpScope

A small, read-only Kubernetes dashboard. Go backend, React frontend, one Docker image.

The project is built in phases; see [docs/PHASES.md](docs/PHASES.md) for the plan and progress.

## Layout

```
backend/                  Go server
  main.go                 entry point: reads env vars, loads clusters, starts the server
  internal/clusters/      known clusters: from env or added in the UI, one client each
  internal/resources/     one file per resource type: lists it and flattens it into table rows
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

`{resource}` is one of `namespaces`, `pods`, `deployments`, `statefulsets`, `daemonsets`,
`jobs` or `cronjobs` (see `backend/internal/resources/resources.go`).

Errors look like `{"error": "readable message", "detail": "original error"}`.
