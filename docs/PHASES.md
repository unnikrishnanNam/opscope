# OpScope: Build Phases

OpScope is a small, read-only Kubernetes dashboard. It has a Go backend that talks to the
cluster and a React frontend that shows what the backend returns. Everything ships as one
Docker image.

We build it one phase at a time. Each phase ends with a working app and a short review.
After a phase is done, we come back here, tick the boxes, and note anything that changed.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Guiding rules

- **Read-only.** OpScope only reads from the cluster. It never creates, edits or deletes anything.
- **Keep it boring.** Use the Go standard library where possible, plain CSS, and few dependencies.
  Every file should make sense to someone learning the stack.
- **One image.** The Go binary serves the API (`/api/*`) and the built React app (everything else).
- **No built-in cluster.** Clusters come from the environment or are added in the UI. Every cluster endpoint lives under `/api/clusters/{id}/`.
- **Fail clearly.** If the cluster or metrics-server can't be reached, show a plain message, not a blank screen.

---

## Phase 0: Project setup

Goal: the skeleton runs end to end (browser → React → Go → JSON) with no Kubernetes code yet.

- [x] Folder layout: `backend/`, `frontend/`, `docs/`
- [x] Go module with a small HTTP server built on `net/http` (no framework)
- [x] Config from environment variables (`PORT`, `STATIC_DIR`)
- [x] `GET /api/health` returns `{"status":"ok","version":"..."}`
- [x] Request logging middleware (`log/slog`)
- [x] Go serves the built frontend, and unknown paths fall back to `index.html` so client-side routing works
- [x] React app built with Vite (plain JavaScript)
- [x] Fonts installed locally (IBM Plex Sans + IBM Plex Mono via Fontsource, so it also works offline)
- [x] Design tokens (colors, spacing, type) as CSS variables in one stylesheet
- [x] App shell: sidebar navigation, top bar, content area
- [x] Placeholder page for every planned section, each saying which phase it arrives in
- [x] Top bar shows the backend status by calling `/api/health`
- [x] Vite dev server proxies `/api` to the Go server
- [~] Multi-stage `Dockerfile` (Node build → Go build → small runtime image). Written; build not yet verified (see notes)
- [x] `Makefile` with dev, build and docker targets
- [x] `README.md` with how to run in dev and in Docker

## Phase 1: Connect to clusters

Goal: the backend can talk to one or more clusters, chosen from the environment or added in the UI.

No cluster is built into the code, and OpScope never falls back to `~/.kube/config` on its own.
If nothing is configured, the UI starts on an "Add a cluster" screen.

**Where clusters come from**

- [ ] Environment: `OPSCOPE_KUBECONFIG` (path to a kubeconfig file) and optional `OPSCOPE_CONTEXT`.
      If set, that cluster is loaded at startup and marked "from environment" (it can't be removed in the UI).
      We use our own variable name instead of `KUBECONFIG` so a cluster is never picked up by accident.
- [ ] UI: paste a kubeconfig or upload the file, pick a context, give it a display name
- [ ] The backend tests the connection before saving and shows a clear error if it fails
- [ ] Clusters added in the UI are saved to `DATA_DIR` (default `./data`; `/data` in Docker as a volume),
      one file per cluster, readable only by the OpScope user (`0600`)
- [ ] Remove a UI-added cluster

**Backend**

- [ ] Add `client-go`; a small `clusters` package keeps a client per cluster
- [ ] `GET /api/clusters`: list clusters (id, name, source, context, server URL). Never returns credentials
- [ ] `POST /api/clusters/inspect`: read a pasted kubeconfig and return its context names
- [ ] `POST /api/clusters`: add a cluster (name, kubeconfig, context)
- [ ] `DELETE /api/clusters/{id}`: remove a UI-added cluster
- [ ] `GET /api/clusters/{id}`: name, server URL, Kubernetes version, reachable or not
- [ ] `GET /api/clusters/{id}/namespaces`: list namespaces

**Frontend**

- [ ] Small `api.js` helper for fetching JSON and handling errors
- [ ] "Add a cluster" page (paste or upload, pick context, name, test and save)
- [ ] Cluster switcher in the top bar, plus a "Manage clusters" page to remove ones you added
- [ ] The selected cluster and namespace are part of the URL, so links and refresh keep them
- [ ] Top bar shows cluster name, version and a connected/unreachable badge
- [ ] Namespace picker ("All namespaces" + list)
- [ ] Clear error state when a cluster can't be reached, naming the likely cause
      (address unreachable, certificate doesn't match the address, credentials rejected)

**Docker and safety**

- [ ] README: mount a kubeconfig and set `OPSCOPE_KUBECONFIG`, or mount a volume at `/data` and add clusters in the UI
- [ ] `make docker-run` publishes the port on `127.0.0.1` only. OpScope has no login, so anyone who can
      open the page can read every saved cluster; it should not be exposed on a network as is
- [ ] Check that the container can reach the multipass VMs (Docker Desktop networking on macOS)
- [ ] Tested against the multipass cluster (kubemaster, kubeworker01, kubeworker02)

## Phase 2: Workloads

Goal: list the main workload types in tables.

- [ ] One generic list endpoint shape: `GET /api/clusters/{id}/{resource}?namespace=...`
- [ ] Backend returns small, flattened objects (only the fields the UI shows), not raw Kubernetes objects
- [ ] Pods: name, namespace, status, ready containers, restarts, node, age
- [ ] Deployments: name, namespace, ready/desired replicas, up-to-date, available, age
- [ ] StatefulSets: name, namespace, ready/desired replicas, age
- [ ] DaemonSets: name, namespace, desired/current/ready, node selector, age
- [ ] Jobs: name, namespace, completions, duration, status, age
- [ ] CronJobs: name, namespace, schedule, suspended, last run, active jobs, age
- [ ] Reusable `ResourceTable` component (sortable columns, text filter, empty and error states)
- [ ] Status badges with consistent colors (Running, Pending, Failed, Succeeded, CrashLoopBackOff, ...)
- [ ] Manual refresh button, plus auto-refresh every 10 seconds

## Phase 3: Nodes and cluster overview

Goal: see the cluster at a glance.

- [ ] Nodes list: name, status, roles, version, internal IP, OS/arch, CPU and memory capacity, age
- [ ] Overview page: counts per resource type, node health summary, pods by phase
- [ ] Recent warning events across the cluster (`GET /api/clusters/{id}/events?type=Warning`)

## Phase 4: Config and networking

Goal: cover the remaining common resource types.

- [ ] ConfigMaps: name, namespace, number of keys, age
- [ ] Secrets: name, namespace, type, number of keys, age
- [ ] Secret values hidden by default; revealing a value is an explicit click per key
- [ ] Services: name, namespace, type, cluster IP, external IP, ports, age
- [ ] Ingresses: name, namespace, class, hosts, address, age

## Phase 5: Resource details

Goal: click any row to see more about it.

- [ ] `GET /api/clusters/{id}/{resource}/{namespace}/{name}` returns details for one object
- [ ] Detail panel: metadata, labels, annotations, owner references
- [ ] Type-specific sections (containers for pods, replica info for deployments, keys for ConfigMaps, ...)
- [ ] YAML view (read-only, with managed fields removed)
- [ ] Events for the object
- [ ] Pod logs: pick a container, show the last N lines, optional follow (streamed)

## Phase 6: Live resource usage

Goal: basic live CPU and memory numbers.

- [ ] Read metrics from metrics-server (`metrics.k8s.io` API)
- [ ] `GET /api/clusters/{id}/metrics/nodes` and `.../metrics/pods?namespace=...`
- [ ] Detect when metrics-server is missing and show how to install it instead of failing
- [ ] Nodes table: CPU and memory usage as a bar against allocatable
- [ ] Pods table: CPU and memory usage columns
- [ ] Overview: cluster-wide CPU and memory usage
- [ ] Short in-memory history (last ~15 minutes) kept in the backend, shown as small sparklines
- [ ] Live updates via polling every few seconds (kept simple; no WebSockets)

## Phase 7: Packaging and running in a cluster

Goal: OpScope can run inside the cluster it watches.

- [ ] In-cluster mode: with `OPSCOPE_IN_CLUSTER=true`, use the pod's service account as a cluster
      "from environment" (moved here from Phase 1, since it only matters when running inside a cluster)
- [ ] Kubernetes manifests: Namespace, ServiceAccount, read-only ClusterRole + binding, Deployment, Service
- [ ] Health and readiness probes using `/api/health`
- [ ] Image runs as a non-root user with a read-only filesystem
- [ ] Graceful shutdown on SIGTERM
- [ ] README: run with Docker, run in a cluster, required permissions
- [ ] Final pass over this document

---

## Phase notes

Things that came up while building, decisions made, and anything that moved between phases.

### Phase 0

- Frontend uses plain JavaScript (not TypeScript) to keep the learning curve small.
- Go serves the frontend from a folder (`STATIC_DIR`) rather than embedding it in the binary.
  This is easier to follow; embedding with `go:embed` is a possible later improvement.
- Routing uses React Router. Styling is plain CSS with variables; no CSS framework.
- Light theme only for now.
- Confirmed after review: plain JavaScript, serving from a folder, and light theme only all stay as they are.
- Changed after review: no cluster is built in or picked up by default. Phase 1 now covers clusters from
  the environment (`OPSCOPE_KUBECONFIG`) or added in the UI, and all cluster endpoints moved under
  `/api/clusters/{id}/`. In-cluster config moved to Phase 7.
- Decided: clusters added in the UI are saved to disk, and in Docker a volume is mounted at `/data`
  so they survive restarts. `data/` is in `.gitignore` and `.dockerignore` so credentials never
  reach git or an image.
- Test cluster: multipass kubeadm cluster (1 control plane + 2 workers, v1.31, metrics-server installed).
  From the Mac, the API server is reachable at `192.168.252.2:6443`, but its certificate only lists
  `192.168.73.101` and `kubemaster`, so the kubeconfig needs `tls-server-name: kubemaster`.
- The Docker image was not built during Phase 0 because Docker Desktop wasn't running. Everything
  the image does was checked locally: the Go server serving `frontend/dist`, `/api/health`, the
  JSON 404 for unknown API routes, and the `index.html` fallback for deep links. Run
  `make docker-build && make docker-run` to finish this item.
