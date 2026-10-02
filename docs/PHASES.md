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
- [x] Top bar shows the backend status by calling `/api/health` (moved to the sidebar footer in Phase 1)
- [x] Vite dev server proxies `/api` to the Go server
- [x] Multi-stage `Dockerfile` (Node build → Go build → small runtime image). Verified at the start of Phase 1
- [x] `Makefile` with dev, build and docker targets
- [x] `README.md` with how to run in dev and in Docker

## Phase 1: Connect to clusters

Goal: the backend can talk to one or more clusters, chosen from the environment or added in the UI.

No cluster is built into the code, and OpScope never falls back to `~/.kube/config` on its own.
If nothing is configured, the UI starts on an "Add a cluster" screen.

**Where clusters come from**

- [x] Environment: `OPSCOPE_KUBECONFIG` (path to a kubeconfig file), optional `OPSCOPE_CONTEXT` and `OPSCOPE_CLUSTER_NAME`.
      If set, that cluster is loaded at startup and marked "from environment" (it can't be removed in the UI).
      We use our own variable name instead of `KUBECONFIG` so a cluster is never picked up by accident.
- [x] UI: paste a kubeconfig or upload the file, pick a context, give it a display name
- [x] The backend tests the connection before saving and shows a clear error if it fails
- [x] Clusters added in the UI are saved to `DATA_DIR` (default `../data`, relative to `backend/`; `/data` in Docker as a volume),
      one file per cluster, readable only by the OpScope user (`0600`)
- [x] Remove a UI-added cluster
- [x] Kubeconfigs from the UI may not run login commands (`exec`, `auth-provider`) or point at files on disk,
      since either would let a pasted file run commands or read files on the OpScope server

**Backend**

- [x] Add `client-go`; a small `clusters` package keeps a client per cluster
- [x] `GET /api/clusters`: list clusters (id, name, source, context, server URL). Never returns credentials
- [x] `POST /api/clusters/inspect`: read a pasted kubeconfig and return its context names
- [x] `POST /api/clusters`: add a cluster (name, kubeconfig, context)
- [x] `DELETE /api/clusters/{id}`: remove a UI-added cluster
- [x] `GET /api/clusters/{id}`: name, server URL, Kubernetes version, reachable or not
- [x] `GET /api/clusters/{id}/namespaces`: list namespaces

**Frontend**

- [x] Small `api.js` helper for fetching JSON and handling errors
- [x] "Add a cluster" page (paste or upload, pick context, name, test and save)
- [x] Cluster switcher in the top bar, plus a "Manage clusters" page to remove ones you added
- [x] The selected cluster and namespace are part of the URL, so links and refresh keep them
- [x] Top bar shows cluster name, version and a connected/unreachable badge
- [x] Namespace picker ("All namespaces" + list)
- [x] Clear error state when a cluster can't be reached, naming the likely cause
      (address unreachable, certificate doesn't match the address, credentials rejected)

**Docker and safety**

- [x] README: mount a kubeconfig and set `OPSCOPE_KUBECONFIG`, or mount a volume at `/data` and add clusters in the UI
- [x] `make docker-run` publishes the port on `127.0.0.1` only. OpScope has no login, so anyone who can
      open the page can read every saved cluster; it should not be exposed on a network as is
- [x] Check that the container can reach the multipass VMs (Docker Desktop networking on macOS)
- [x] Tested against the multipass cluster (kubemaster, kubeworker01, kubeworker02)
- [x] Go tests for the clusters package, using a fake API server (no real cluster needed)

## Phase 2: Workloads

Goal: list the main workload types in tables.

- [x] One generic list endpoint shape: `GET /api/clusters/{id}/{resource}?namespace=...`
- [x] Backend returns small, flattened objects (only the fields the UI shows), not raw Kubernetes objects
- [x] Pods: name, namespace, status, ready containers, restarts, node, age
- [x] Pod status worked out like `kubectl get pods` (CrashLoopBackOff, Init:1/2, Terminating, ...),
      including native sidecar containers
- [x] Deployments: name, namespace, ready/desired replicas, up-to-date, available, age
- [x] StatefulSets: name, namespace, ready/desired replicas, age
- [x] DaemonSets: name, namespace, desired/current/ready, node selector, age
- [x] Jobs: name, namespace, completions, duration, status, age
- [x] CronJobs: name, namespace, schedule, suspended, last run, active jobs, age
- [x] Reusable `ResourceTable` component (sortable columns, text filter, empty and error states)
- [x] Status badges with consistent colors (Running, Pending, Failed, Succeeded, CrashLoopBackOff, ...)
- [x] Manual refresh button, plus auto-refresh every 10 seconds
- [x] Namespace column hidden when one namespace is selected
- [x] Go tests for the listers and pod/job status, using client-go's fake client

## Phase 3: Nodes and cluster overview

Goal: see the cluster at a glance.

- [x] Nodes list: name, status, roles, version, internal IP, OS/arch, CPU and memory capacity, age
- [x] Overview page: counts per resource type, node health summary, pods by status (kubectl-style status rather
      than the raw phase, so crash loops show up)
- [x] Overview respects the namespace picker; nodes and the namespace count stay cluster-wide
- [x] Nodes page ignores the namespace picker ("Cluster-wide")
- [x] Recent warning events across the cluster (`GET /api/clusters/{id}/events?type=Warning`)
- [x] `GET /api/clusters/{id}/overview` builds the summary in one request by reusing the listers
- [x] Go tests for nodes, events and the overview counts

## Phase 4: Config and networking

Goal: cover the remaining common resource types.

- [x] ConfigMaps: name, namespace, number of keys, age
- [x] Secrets: name, namespace, type, number of keys, age
- [x] Secret values hidden by default; revealing a value is an explicit click per key
      (click a secret's name to open its keys; `GET /api/clusters/{id}/secrets/{namespace}/{name}/{key}`)
- [x] The secrets list never contains values (tested on the JSON); the value endpoint sends `Cache-Control: no-store`
- [x] Binary secret values are shown as base64 and labelled as such
- [x] Services: name, namespace, type, cluster IP, external IP, ports, age
- [x] Ingresses: name, namespace, class, hosts, address, age, TLS
- [x] A missing object now returns 404 with "It doesn't exist (any more)" instead of a generic 502
- [x] Go tests for all four listers and secret values

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
- Docker build verified at the start of Phase 1: 16 MB image, `/api/health` and deep links work.

### Phase 1

- Every item was checked by hand against the multipass cluster, both locally and in Docker:
  environment cluster, adding through the API and the UI form, removing, namespaces, saved clusters
  surviving a container restart, and the error messages for an unreachable address, a certificate
  mismatch and an invalid kubeconfig.
- Added `OPSCOPE_CLUSTER_NAME`, because a raw context name like `kubernetes-admin@kubernetes`
  makes a poor display name and URL.
- The OpScope server status moved from the top bar to the sidebar footer, to make room for the
  cluster switcher, namespace picker and cluster badge.
- Security: kubeconfigs added in the UI can't use `exec`/`auth-provider` or file paths (see above).
  The environment path has no such limit, since whoever sets it already controls the server.
- Only the chosen context is saved (`clientcmdapi.MinifyConfig`), so other credentials in a
  pasted file are never written to disk.
- The image grew to about 42 MB because of client-go. It's still a single static binary on distroless.
- client-go is v0.37 and the test cluster runs v1.31. That's outside client-go's official version
  skew, but the read-only core APIs OpScope uses are stable; worth keeping in mind if something odd shows up.
- Cluster status is re-checked every 30 seconds; a cluster that doesn't answer takes up to 10 seconds
  (the client timeout) before showing "Unreachable".

### Phase 2

- Checked against the multipass cluster: 44 pods (Running, CrashLoopBackOff, ContainerCreating,
  Completed), 20 deployments, 2 statefulsets and 3 daemonsets, in the browser and in Docker.
  The cluster has no Jobs or CronJobs, so those pages were only seen empty; their logic is
  covered by unit tests.
- The namespaces endpoint moved into the same generic `/{resource}` route as the workloads, so every
  list goes through `internal/resources`.
- Rows are sorted by namespace and name on the server. Clicking a header sorts in the browser;
  a third click goes back to the server order.
- The filter matches any text column (name, namespace, status, node, ...), so typing "crash"
  finds crash-looping pods.
- Restart counts are shown as plain numbers. Colouring every non-zero count was noise on a
  cluster whose VMs have rebooted.
- A table only loads once its cluster is known to be reachable; otherwise the page shows only the
  cluster banner, instead of the same error twice.

### Phase 3

- Checked against the multipass cluster in the browser and in Docker: 3 nodes (arm64, 2 cores and
  2.9 GiB each), the overview with and without a namespace selected, and 4 real warnings
  (crash loops, a failing readiness probe, a missing ConfigMap).
- Listers are now typed functions (`[]Pod`, `[]Node`, ...) wrapped by `asLister` for the URL map,
  so the overview can reuse them without type assertions. They take a `Query` struct
  (namespace, and event type) instead of a bare namespace string.
- The overview's list calls run one after another; on this cluster the whole thing takes about 11 ms.
- Events are filtered by type on the API server (field selector), sorted newest first, capped at 100.
- Pods-by-status bar: colours checked with the dataviz palette validator. The text amber was too
  close to red for side-by-side segments (ΔE 11), so marks use a lighter `--warn-mark` (#bf8a1e,
  ΔE 20 from red). Every segment has a legend entry with its count and a hover tooltip, so colour is
  never the only cue.
- Fixed: cluster pages could render before the cluster list had loaded (the overview crashed on
  `cluster.name`). Layout now shows "Loading…" until the cluster is known, for every page.
- Nodes without a role label show "–" in both the Nodes table and the overview (Kubernetes has no
  "worker" role; it's only a convention).

### Phase 4

- Checked against the multipass cluster in the browser and in Docker: 38 ConfigMaps, 20 Secrets
  (Opaque, TLS and Helm release types) and 22 Services, including NodePorts shown as `80:32049/TCP`.
  The cluster has no Ingresses (it uses nginx-gateway / the Gateway API), so that page was only seen
  empty; its logic is covered by unit tests.
- Reveal was tested on a TLS certificate (`tls.crt`, public by design) so no real secret ended up in
  screenshots. Only the clicked key was fetched; the other key stayed masked. A revealed value survives
  the 10-second refresh, and Hide or collapsing the row forgets it.
- `ResourceTable` gained an optional `expand` prop: rows whose name is clicked open a panel underneath.
  Secrets use it now; Phase 5's detail view may replace or reuse it.
- ConfigMaps show only the key count (key names on hover). Their values arrive with the detail view in Phase 5.
- Gateway API resources (Gateway, HTTPRoute) aren't covered. They'd be a natural addition later, since
  this cluster uses them instead of Ingress.
