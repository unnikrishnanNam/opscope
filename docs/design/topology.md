# Opscope: Topology and diagnosis

Opscope shows *what* is unhealthy: a pod in `CrashLoopBackOff`, a Deployment at 0/3, a route that
isn't accepted. This record adds *why*. Opscope builds a graph of the cluster in which every object
is connected to what it runs on, what it uses and what sends traffic to it. It then follows that
graph from a symptom to the object that is broken for its own reasons, the root cause, and shows
the path with the evidence for each step.

The feature has three parts, built in this order:

1. **The model:** one snapshot of a cluster turned into nodes and typed edges, including objects
   that are referred to but don't exist.
2. **Diagnosis:** rules that turn raw status into findings with evidence and name the object to
   blame, and a walk that chains findings from symptom to root cause.
3. **Views:** a diagnosis on every detail page, a map around one object, a topology page for an
   app, a namespace or the whole cluster, and a list of problems on the overview.

Most of the value is in the second part. A graph shows what is connected; the rules say why
something fails. They are built and tested before anything is drawn.

What stays the same: Opscope stays read-only and uses only `get` and `list`. Every existing page,
endpoint and shortcut keeps working as it does. A timeline of changes is planned separately; this
work lays its foundation (see [Toward the timeline](#toward-the-timeline)).

Phases are numbered **G0–G7** so they don't mix with the build phases 0–8 or the phases lettered
R, P and D. All work happens on the `feature/topology` branch, and each phase is committed there
once it works, its tests pass and its notes are written. This feature has no issue of its own.

**Status (2026-10-12):** G0 (the snapshot), G1 (the graph and `GET /topology`), G2 (the diagnosis on detail pages), G3 (the remaining rules and problems on the overview), G4 (the map), G5 (the Topology page) and G6 (background snapshots and the cluster's map) done. G7 is next.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

- **Still read-only, and still polling.** Opscope uses only `get` and `list`. It doesn't use
  `watch` or informers. A snapshot of a cluster comes from list calls, and a later snapshot from
  more list calls.
- **One snapshot per cluster, covering the whole cluster.** Every view (a detail page's diagnosis,
  a namespace map, the cluster map, the problems list) is computed from the same snapshot. There is
  no snapshot per namespace, because references cross namespaces (an HTTPRoute and its Gateway, a
  cross-namespace backend and its ReferenceGrant, pods and their nodes) and because the cluster
  view needs everything anyway.
- **Snapshots are taken on demand first, then in the background.** Up to G5, a snapshot is taken
  when a view asks for one and reused for up to 10 seconds, the UI's refresh interval. In G6
  Opscope takes one for every cluster every 30 seconds, **even when nobody is looking**. The
  timeline needs that, and it makes the cluster-wide view cheap. Clusters that don't answer are
  retried less often, backing off to once every 5 minutes.
- **No secret values in the snapshot.** Secrets are listed metadata only (`PartialObjectMetadata`,
  through client-go's metadata client), so their values never even reach Opscope. ConfigMaps keep
  their key names and drop their values. Without the optional Secrets role, Secrets are "unknown"
  in the graph, not "missing".
- **New kinds, read with `get` and `list` only:** ReplicaSets, EndpointSlices,
  PersistentVolumeClaims, PersistentVolumes, StorageClasses, ServiceAccounts, ResourceQuotas,
  IngressClasses, HorizontalPodAutoscalers, PodDisruptionBudgets and Gateway API ReferenceGrants.
  The ClusterRole in `deploy/kubernetes/opscope.yaml` grows by these, and
  [Permissions](../deployment.md#permissions) lists them. A kind Opscope can't read is skipped, the
  same way `GET /names` skips one, and its objects become "unknown".
- **Missing and unknown are first-class.** An object that is referred to but doesn't exist becomes
  a node in the **missing** state. One Opscope isn't allowed to read, or whose API isn't installed,
  is **unknown**. A missing Secret is often the whole answer, and a list page can never show it.
- **Edges point from the dependent to its dependency** ("A needs B"): Deployment → ReplicaSet →
  Pod → Node, Pod → Secret, Service → Pod, HTTPRoute → Service, HTTPRoute → Gateway. Diagnosis walks
  edges forward to find a cause, and backward to find the impact. The layout ignores direction and
  places nodes by kind (see [Drawing](#drawing)).
- **Diagnosis uses explicit rules that name the culprit.** "Something this depends on is unhealthy,
  so that's the cause" isn't used: it blames a busy node for every crash loop on it. Each rule says
  which object it blames and with what evidence. When no rule explains a failure, Opscope says so
  and lists unhealthy neighbours as "possibly related", marked as a guess.
- **Every finding has a confidence:** `certain` (read from structured status: a container state, a
  condition, a missing object), `likely` (read from event text, which changes between Kubernetes
  versions) or `possible` (the fallback above). The UI shows anything less than certain.
- **No AI.** Explanations are deterministic, come with their evidence, and are the same every time
  for the same cluster.
- **Suggested next steps are read-only.** A step is a link to an Opscope page or tab (previous logs,
  an event, the node) or a read-only kubectl command to copy (`get`, `describe`, `logs`), as the
  palette already offers. A fix ("create the Secret", "raise the memory limit") is described in
  prose, never as a command.
- **The text comes first.** The diagnosis is an indented trace on the detail page, which works
  with the keyboard, a screen reader and a 375 px screen. Every graph view also has a list form, so
  no information exists only in a picture.
- **Zoom levels follow the namespace picker.** With "All namespaces" selected, the topology page
  shows the cluster: namespaces holding apps. With a namespace selected, it shows that namespace's
  objects. `?app=` narrows it to one app. A detail page's Map tab shows the object's neighbourhood.
- **Our own renderer and layout.** The graph is SVG drawn by our own component. The layout is our
  own layered layout: one column per kind, rows ordered to reduce crossings. Kubernetes kinds
  already have a natural left-to-right order, so a general graph layout library isn't needed. The
  layout is one function, `layout(graph) → positions`. If it doesn't look good on real clusters by
  the end of G4, [elkjs](https://github.com/kieler/elkjs) (its layered algorithm, loaded only on
  graph pages) replaces that function and the rest stays. **Decided in G4: our own layout stays**
  (see G4's notes).
- **No new Go dependencies.** The metadata client is part of client-go. Snapshot loading uses the
  standard library for running one load at a time per cluster.
- **Apps are grouped by labels, then by what selects what.** An app is the first label of
  `app.kubernetes.io/part-of`, `app.kubernetes.io/instance`, `app.kubernetes.io/name` and `app` that
  a workload has, or the workload itself. Services, routes and config join the app of the
  workloads they select or are used by.
- **Problems sit at the top of the "Recent warnings" card** on the overview, not in a card of
  their own. Warnings that no finding explains (a one-off probe failure) stay visible below them.
  This may change once the feature has been used for a while.
- **The Map tab's shortcut is `5` on every kind.** Logs keeps `4` (pods only), as documented in the
  README, so on other kinds `4` does nothing. A tab keeps the same key everywhere.
- **Scenarios run on the `opscope-test` kind cluster.** The broken-app manifests in
  `test/scenarios/` are applied there in their own namespace (`opscope-scenarios`) and deleted
  afterwards. The multipass cluster is only read.
- **Out of scope:** the recorded timeline (its own record), dependencies inferred from
  configuration (an env var pointing at `postgres.db.svc`), NetworkPolicy analysis, and kinds
  beyond those listed. They're under [Later](#later).

---

## Design

### Packages

```
backend/internal/
  kube/         shared by resources and snapshot: clients, Gateway API types, pod status, skipped kinds
  snapshot/     loads every kind a cluster has into one Snapshot; keeps the latest per cluster
  graph/        Build(snapshot) → Graph: nodes, edges, states, apps; views of it (cluster, namespace, app, neighbourhood)
  diagnose/     the rules and the engine: findings, traces, impact, problems
    testdata/   scenarios: objects as YAML, and the root causes expected for them
  server/       topology.go: the new endpoints
```

The `snapshot` package is the only one that talks to a cluster. `graph` and `diagnose` are pure
functions over a `Snapshot`, so their tests build objects in Go or read them from YAML and need no
fake clients. `resources.podStatus` and the local Gateway API structs move to a small shared
package (`internal/kube`) so `resources` and `graph` both use them.

### The snapshot

```go
type Snapshot struct {
	Taken   time.Time
	Skipped []kube.Skipped // kinds that couldn't be read, and why (not_installed, forbidden, failed)

	Namespaces     []corev1.Namespace
	Nodes          []corev1.Node
	Pods           []corev1.Pod
	ReplicaSets    []appsv1.ReplicaSet
	Deployments    []appsv1.Deployment
	// ... StatefulSets, DaemonSets, Jobs, CronJobs, Services, EndpointSlices, Ingresses,
	// IngressClasses, PVCs, PVs, StorageClasses, ServiceAccounts, ResourceQuotas, HPAs, PDBs,
	// Gateways, HTTPRoutes, GatewayClasses, ReferenceGrants
	ConfigMaps     []ConfigMap                     // metadata and key names; no values
	Secrets        []metav1.PartialObjectMetadata  // metadata only; values never fetched
	Events         []corev1.Event                  // Warning events only

	index // lookups by kind/namespace/name, by UID, by owner, events by object UID
}
```

- All kinds are listed in parallel across all namespaces, like `ListNames` does. One kind failing
  doesn't fail the snapshot. If nothing could be read, the cluster itself is the problem and the
  error is returned as today (`clusters.Explain`).
- Objects are slimmed as they arrive: `managedFields` and the `last-applied-configuration`
  annotation are dropped. This keeps a large cluster's snapshot in tens of megabytes, not hundreds.
- `snapshot.Store` keeps the latest snapshot per cluster. `Get(ctx, cluster)` returns it when it is
  younger than the maximum age, and loads a new one otherwise. Concurrent callers wait for the
  same load instead of starting their own. When a cluster is removed, its snapshot is dropped,
  like `metrics.History.Forget`.
- From G6, `Store.Collect` refreshes every cluster on a ticker, the same pattern as
  `metrics.History.Collect`, and views always use the latest snapshot.
- Every response built from a snapshot says when it was taken and which kinds were skipped, so the
  UI can show "as of 12 s ago" and "Opscope can't read Secrets in this cluster".

Target: a cluster of 5,000 pods loads in under 2 seconds and is built and diagnosed in under
200 ms. A Go benchmark with a generated snapshot of that size checks the second number.

### The graph

```go
type Node struct {
	ID        string `json:"id"`        // "pods/shop/checkout-7d9f-x2k": resource/namespace/name, as in URLs
	Resource  string `json:"resource"`  // "pods"; links to the detail page when Opscope has one
	Kind      string `json:"kind"`      // "Pod"
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
	App       string `json:"app,omitempty"`
	State     string `json:"state"`     // ok, progressing, degraded, failing, missing, unknown
	Status    string `json:"status"`    // what the list page shows: "CrashLoopBackOff", "2/3", "Accepted"
}

type Edge struct {
	From     string `json:"from"`  // the dependent
	To       string `json:"to"`    // what it needs
	Type     string `json:"type"`  // see below
	Via      string `json:"via,omitempty"` // where the reference is: "env STRIPE_KEY", "volume config", "backendRef 2"
	Optional bool   `json:"optional,omitempty"` // optional: true references can't break anything
}
```

**States.** `ok`; `progressing` (a rollout or a pod starting, within its deadline); `degraded`
(working, but not fully: 2/3 ready, a node under memory pressure); `failing`; `missing`;
`unknown`. Each kind's state comes from what its list page already works out (`podStatus`,
ready/desired, route and gateway conditions), so the graph and the tables always agree.

**Edges:**

| Type        | From → To                                                                           | Read from                                                                       |
| ----------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `owns`      | Deployment → ReplicaSet → Pod; StatefulSet, DaemonSet, Job → Pod; CronJob → Job     | `ownerReferences` (reversed)                                                    |
| `runs-on`   | Pod → Node                                                                          | `spec.nodeName`                                                                 |
| `uses`      | Pod or pod template → ConfigMap, Secret, PVC, ServiceAccount                        | volumes (incl. projected), `env.valueFrom`, `envFrom`, `imagePullSecrets`, `serviceAccountName` |
| `uses`      | PVC → PV → StorageClass; Ingress → IngressClass, TLS Secret; Gateway → TLS Secret   | `volumeName`, `storageClassName` (or the default class), `tls`, `certificateRefs` |
| `selects`   | Service → Pod; PDB → Pod                                                            | label selectors, same namespace; readiness from EndpointSlices                  |
| `routes-to` | Ingress → Service; HTTPRoute → Service                                              | backends, `defaultBackend`, `backendRefs`                                       |
| `attaches`  | HTTPRoute → Gateway → GatewayClass                                                  | `parentRefs`, `gatewayClassName`                                                |
| `permits`   | HTTPRoute → ReferenceGrant (for a backend in another namespace)                     | the grant in the backend's namespace                                            |
| `scales`    | HPA → Deployment or StatefulSet                                                     | `scaleTargetRef`                                                                |

Workloads get `uses` edges from their pod template as well as their pods. A Deployment whose pods
are never created (a quota, Pod Security admission) still shows what it needs.

**Views of the graph** are functions that return a subgraph, already shaped for drawing:

- `Neighbourhood(id, depth)`: the object and everything within `depth` edges (default 2), plus
  the whole chain behind any failing node in it.
- `Namespace(ns)` and `App(ns, app)`: every object, with each workload's pods collapsed into one
  node ("3/4 ready") unless asked to expand. ReplicaSets are hidden unless they matter (more than
  one with pods during a rollout, or one with a FailedCreate). Nodes (machines) are added only when
  a pod's problem is its node.
- `Cluster()`: namespaces as groups, apps as nodes with a summary ("4 workloads, 1 failing"),
  Gateways and GatewayClasses as shared entry points, routes between them as edges, and nodes
  (machines) as a strip of health underneath. A namespace with nothing wrong in it collapses to
  one tile. As built (G6): an app's state counts the problems the diagnosis found in it, not only
  its objects' states, and namespaces no edge joins to anything are laid out as a grid under the
  columns, the worst first.

Each view can be limited to problems: only nodes that aren't `ok`, the chains between them, and
their direct parents for context.

### Diagnosis

A **rule** looks at one object and returns findings:

```go
type Rule struct {
	ID       string // "pod.config-missing"
	Resource string // the kind it looks at: "pods"
	Check    func(c *Context, n *graph.Node) []Finding
}

type Finding struct {
	ID         string     `json:"id"`         // stable: rule + object + blamed objects
	Rule       string     `json:"rule"`
	Object     string     `json:"object"`     // node ID the finding is about
	Severity   string     `json:"severity"`   // failing, degraded, info
	Confidence string     `json:"confidence"` // certain, likely, possible
	Title      string     `json:"title"`      // "Secret shop/stripe-key doesn't exist"
	Detail     string     `json:"detail"`     // a sentence or two of explanation; the fix in prose
	Evidence   []Evidence `json:"evidence"`   // condition, container state, event, or reference, with times
	Blames     []string   `json:"blames"`     // node IDs that cause this one
	Next       []Step     `json:"next"`       // links and read-only kubectl commands
}
```

`Context` gives rules the snapshot's lookups (by name, by UID, events for an object, a pod's
owners) and the graph's edges. Rules live in one file per kind (`rules_pods.go`,
`rules_workloads.go`, `rules_network.go`, ...), so each is easy to find and test.

The **engine** runs every rule on every object once per snapshot and keeps the findings by object.
Then:

- **Trace** for an object: its findings, then for each blamed object its findings, and so on, as a
  tree. A finding that blames nothing, or blames only objects with no findings, is a **root cause**.
  The walk stops at cycles and at a depth of 10.
- **Grouping:** findings with the same rule and the same blamed objects are merged. A Deployment
  with 30 pods failing the same way shows "30 pods: CreateContainerConfigError" once, not 30 times.
- **Impact** for an object: the reverse walk. For a missing Secret, that's every pod, workload,
  Service and route whose trace leads to it.
- **Problems** for a namespace or the cluster: the root causes, each with the objects it explains,
  sorted by severity and then by how much each one explains. Headline: "3 problems explain 17
  unhealthy objects."
- **Fallback:** an object that is `failing` or `degraded` with no finding from any rule gets one
  from the `unexplained` rule (confidence `possible`): its conditions and recent warnings as
  evidence, and its unhealthy neighbours as "possibly related".

Diagnosis runs once per snapshot and is cached with it, so every view is a lookup.

### Rules

The first set (G2) covers the failures people hit most. The second (G3) completes the catalogue.

| Rule                         | Phase | Looks at          | Detects                                                                                                      | Blames                                         |
| ---------------------------- | ----- | ----------------- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------------------- |
| `pod.config`                 | G2    | Pod               | `CreateContainerConfigError`: a ConfigMap, Secret or key that doesn't exist                                  | the missing object (or key)                    |
| `pod.mount`                  | G2    | Pod               | stuck in `ContainerCreating` with `FailedMount`: a volume's ConfigMap, Secret or PVC                         | the missing or unbound object                  |
| `pod.image`                  | G2    | Pod               | `ErrImagePull`, `ImagePullBackOff`, `InvalidImageName`, split into not found, unauthorized, unreachable      | the pull Secret when unauthorized; else itself |
| `pod.crash`                  | G2    | Pod               | `CrashLoopBackOff`, with the last exit: `OOMKilled` (the memory limit), an exit code, a failed liveness probe | itself (root), with a link to previous logs    |
| `pod.unschedulable`          | G2    | Pod               | `PodScheduled=False`: not enough CPU or memory, taints, node selector or affinity matching no node, unbound PVC | the nodes, the PVC                             |
| `pod.node`                   | G2    | Pod               | its node is NotReady or unreachable, so the pod is `Unknown` or stuck `Terminating`                          | the node                                       |
| `node.not-ready`             | G2    | Node              | `Ready` is False or Unknown; when the kubelet last reported                                                  | itself (root)                                  |
| `workload.unavailable`       | G2    | Deployment, StatefulSet, DaemonSet | fewer replicas ready than wanted                                                            | its pods that aren't ready, grouped            |
| `create-failed`              | G2    | ReplicaSet, StatefulSet, DaemonSet, Job | `ReplicaFailure` or `FailedCreate`: a ResourceQuota, Pod Security admission, a missing ServiceAccount        | the quota, the namespace, the ServiceAccount   |
| `service.selector`           | G2    | Service           | the selector matches no pods                                                                                 | itself (root)                                  |
| `service.endpoints`          | G2    | Service           | pods match, but none are ready                                                                               | those pods                                     |
| `route.backend`              | G2    | HTTPRoute, Ingress | a backend Service missing, without a ReferenceGrant, or with no ready endpoints                             | the Service or the missing grant               |
| `pod.not-ready`              | G3    | Pod               | running but not ready: a failing readiness probe                                                             | itself (root)                                  |
| `pod.evicted`                | G3    | Pod               | evicted for memory or disk pressure                                                                          | the node                                       |
| `pod.init`                   | G3    | Pod               | an init container failing (`Init:Error`, `Init:CrashLoopBackOff`), with the same detail as the main containers | itself, or what the init container needs       |
| `node.pressure`              | G3    | Node              | memory, disk or PID pressure; cordoned                                                                       | itself (root)                                  |
| `deployment.rollout`         | G3    | Deployment        | `ProgressDeadlineExceeded`: the new revision's pods fail while the old one still serves                      | the new ReplicaSet's pods                      |
| `statefulset.ordered`        | G3    | StatefulSet       | waiting for a lower ordinal to become ready                                                                  | that pod                                       |
| `job.failed`                 | G3    | Job, CronJob      | `BackoffLimitExceeded`, `DeadlineExceeded`; a CronJob's last run failing                                     | the failed pods, the failed Job                |
| `pvc.pending`                | G3    | PVC               | no StorageClass, a missing class, `ProvisioningFailed`                                                       | the StorageClass, or itself                    |
| `hpa.scaling`                | G3    | HPA               | `ScalingActive=False` (often missing metrics), at its maximum                                                | the target, or itself                          |
| `route.accepted`             | G3    | HTTPRoute         | `Accepted=False`: no matching listener, hostname, or a missing Gateway                                       | the Gateway                                    |
| `gateway.programmed`         | G3    | Gateway           | `Programmed` or `Accepted` is False                                                                          | the GatewayClass, or itself                    |
| `gatewayclass.accepted`      | G3    | GatewayClass      | no controller accepted it: is its controller installed?                                                      | itself (root)                                  |
| `ingress.class`              | G3    | Ingress           | a missing IngressClass, or a missing TLS Secret                                                              | the missing object                             |
| `unexplained`                | G3    | anything failing  | the fallback above                                                                                           | possibly related neighbours                    |

Rules read structured fields first: container states, conditions, references between objects.
They read event text only where Kubernetes puts the reason nowhere else (scheduling, mounts,
image pulls), and then they set `likely`.

### API

All `GET`, all built from the cluster's snapshot, and each response includes
`"snapshot": {"taken": "...", "skipped": [...]}`.

| Endpoint                                                            | Returns                                                                     |
| ------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `/api/clusters/{id}/diagnosis/{resource}/{namespace}/{name}`        | the object's state, its trace (findings with nested causes) and its impact |
| `/api/clusters/{id}/diagnosis/{resource}/{name}`                    | the same, for cluster-wide kinds (nodes, GatewayClasses)                   |
| `/api/clusters/{id}/topology?namespace=&app=&focus=&depth=&problems=` | `{nodes, edges, groups, findings}` for one view (see above)                |
| `/api/clusters/{id}/problems?namespace=`                            | the root causes, each with the objects it explains                          |

The `diagnosis` prefix keeps these from clashing with `/{resource}/{namespace}/{name}`: a fixed
segment beats a placeholder, as for `overview` and `names`. `docs/api.md` documents all three.

### Views

**Diagnosis on the detail page (G2).** At the top of the Summary tab, when the object or anything
behind it has a finding:

```
✕ Deployment shop/checkout                          0/3 ready
  └ ✕ 3 pods: CreateContainerConfigError
      └ ⚠ Secret shop/stripe-key doesn't exist       root cause · certain
          Container api reads env STRIPE_KEY from secretKeyRef stripe-key/api-key.
          Create the Secret in shop, or change the reference.
          Evidence: "secret \"stripe-key\" not found" (container api, 2 min ago)
          Next: kubectl get secrets -n shop   [Copy]

  Also affected: Service shop/checkout (no ready endpoints), HTTPRoute shop/storefront
```

Each object in the trace links to its page; a missing one links to nothing and says so. An
object that is fine shows one quiet line ("Opscope found nothing wrong with this object or what it
needs"). An object that is a root cause shows what it affects.

**Map tab (G4).** The object's neighbourhood, drawn, with "Steps away" from 1 to 3. Its shortcut is
`5`. Selecting a card opens a panel over the map's right side with its state, its findings, its
connections and links to its page and its own map; the map stays usable, so the next card can be
picked without closing it. A List view shows the same content as text, and is where the tab starts
on a phone.

**Topology page (G5, G6).** A new page in the Cluster group (`g t`). It follows the namespace
picker: a namespace (G5) or, with "All namespaces", the whole cluster (G6). It has an app picker
(`?app=`, for a namespace), a "Problems only" switch (`?problems=true`), a List view that shows the
same content as an outline, and the same panel, which can also show a group's pods one by one, or
in the cluster's map a namespace's apps. On screens narrower than 768 px it opens in List view.
The overview's problems card links to it ("Show on map").

**Problems on the overview (G3).** At the top of the "Recent warnings" card: the root causes, each
with how many objects it explains, linking to the root object's diagnosis. The warnings follow
below them.

**Command palette.** On a detail page: "Show diagnosis", "Show map", "Copy root cause". From
anywhere: "Problems", and Topology as a page.

### Drawing

- **Columns by kind,** left to right, roughly in the direction traffic flows: GatewayClass,
  IngressClass | Gateway, Ingress | HTTPRoute | Service, HPA, PDB, ReferenceGrant | Deployment,
  StatefulSet, DaemonSet, CronJob, Job | ReplicaSet and a CronJob's Jobs | Pod | ConfigMap,
  Secret, PVC, ServiceAccount, ResourceQuota | PV | StorageClass | Node. Empty columns are left
  out. Each column has a quiet heading (the kind, or the column's name when it holds several),
  which stays in sight when the map is panned down.
- **Rows** are ordered to reduce crossings: sweeps of the barycenter heuristic (each card moves to
  the average row of its neighbours in the column just compared), each followed by swapping
  neighbours where that uncrosses edges, starting from an order grouped by app and kind and sorted
  by name. Then each card moves toward the middle of what it connects to, keeping its order and
  its distance from its neighbours (a least-squares fit, solved by isotonic regression). The same
  view always gets the same layout. Given the previous layout, cards keep their order across a
  refresh; new ones find their place among them.
- **Long edges** run through a lane in each column they cross, between cards rather than over
  them. Long edges that share an end share their lanes, at whichever end more of them meet: a
  Gateway's edges to its data plane are one line until they part, and the pods' edges to their
  machine join before reaching it.
- **Cards** are buttons over an SVG layer of edges: the kind's icon and name, the object's name,
  and its status with a mark. A failing or degraded card has a coloured bar down its left edge, as
  a diagnosis does. Missing cards have a dashed outline and a "missing" tag; unknown ones are
  dashed and grey with "can't read". A group of pods shows cards stacked behind and "3/4 ready".
  The map's own object has the orange line, as the open tab does. A card holding a root cause is
  tagged "root cause". A card shows the worse of its state and its own findings: an Ingress has
  no status to read, so its finding about a missing Service makes it "failing".
- **Edges** are smooth curves. Solid for `owns`, `routes-to` and `attaches`, dashed for `uses`,
  thin for `selects` and `runs-on`, dotted for `blames`; optional references are fainter. The
  chain from the selected card (or the map's own object) to its root causes is drawn thick in the
  colour of the problem, with dots running toward the cause; where the blamed object isn't next to
  it on the map (a Deployment blames its new ReplicaSet's pods), the chain follows the map between
  them. The dots stand still for people who ask for less motion.
- **Pan and zoom** by dragging, scrolling (with ⌘/Ctrl to zoom), a pinch, or buttons for zoom in,
  zoom out, fit and "back to this object". The first view shows the whole map if it can be read
  that way (at 80% or more), otherwise the object at 80%, with as little empty space as possible.
- **Keyboard:** the map is one Tab stop; arrow keys move along edges (left and right) or within a
  column (up and down), Enter opens a card's panel, Escape closes it and returns to the card, `+`,
  `−` and `0` zoom and fit, Home goes back to the object. Each card's accessible name says its
  kind, name, state and status, and whether it's a root cause or the map's own object.
- Built on `/kit` first with sample maps (broken, healthy, missing and unknown, large), in both
  themes, before it reached a page. Colours are the existing tokens; edges are drawn at full mark
  strength, which the kit checks at 3:1.

### Toward the timeline

The timeline is planned in its own record, which decides storage. This work prepares for it:

- G6's background snapshots are exactly what the timeline compares: the difference between two
  consecutive snapshots is the list of changes. The timeline adds the comparison and storage, not
  a second way of reading the cluster.
- Findings have stable IDs, so the timeline can record when each one appeared and cleared ("this
  started at 14:03").
- Decided for the timeline, so the snapshot keeps what it needs: ConfigMap changes are stored as
  before/after values, since the UI already shows ConfigMap values to anyone who can open it, with
  a setting to store only hashes instead. Secret changes are stored as one hash per key, never a
  value. Logs are never stored. The snapshot therefore gains ConfigMap values (G6) only when the
  timeline needs them, not before.

---

## Phase G0: Snapshot

Goal: Opscope can take a snapshot of a whole cluster, with nothing visible changing yet.

- [x] `internal/snapshot`: every kind listed in parallel, across all namespaces, into `Snapshot`
- [x] Secrets through the metadata client; ConfigMaps reduced to key names; Warning events only
- [x] Objects slimmed as they arrive (`managedFields`, `last-applied-configuration`)
- [x] Kinds that can't be read go in `Skipped` (`not_installed`, `forbidden`, `failed`); nothing read at all is an error
- [x] Lookups: by kind/namespace/name, by UID, by owner, events by object
- [x] `Store`: the latest snapshot per cluster, a maximum age (10 s), one load at a time per cluster, dropped with its cluster
- [x] `podStatus` and the Gateway API structs moved to `internal/kube`, with `resources` using them
- [x] RBAC: the new kinds added to `deploy/kubernetes/opscope.yaml`, and the list in `docs/deployment.md`
- [x] Go tests with client-go's fake clients: all kinds, a forbidden kind, Gateway API missing, the store's reuse and single load
- [x] Benchmark: a generated 5,000-pod snapshot; load time measured on the multipass cluster

## Phase G1: Graph model

Goal: `GET /topology` returns a correct graph for a namespace and a neighbourhood. No UI yet.

- [x] `internal/graph`: nodes with states, every edge type in the table above, missing and unknown nodes
- [x] Pod-template edges for workloads; optional references marked
- [x] App grouping by labels, then by selection
- [x] Views: `Neighbourhood`, `Namespace`, `App`, with pods collapsed per workload and the problems-only filter
- [x] `GET /api/clusters/{id}/topology` for a namespace, an app and a focus object
- [x] Go tests: one per edge type, missing and unknown references, cross-namespace references, app grouping
- [x] Checked against the multipass cluster's `topology-test` namespace (Gateway → routes → services → pods)

## Phase G2: Diagnosis and the first rules

Goal: a broken object's detail page explains why, down to the root cause.

- [x] `internal/diagnose`: `Rule`, `Finding`, `Context`, the engine, traces, grouping, impact
- [x] The G2 rules from the table
- [~] Scenarios in `diagnose/testdata/`: objects captured from the kind cluster, and the root
      causes expected for each (one capture for all scenarios, expectations in `scenario_test.go`; see notes)
- [x] Broken-app manifests in `test/scenarios/` (missing Secret, bad image, OOM, crash loop,
      unschedulable, selector matching nothing, route to a missing Service, quota), applied to the
      `opscope-test` kind cluster in the `opscope-scenarios` namespace to reproduce each scenario and
      capture its objects as test data, then deleted
- [x] `GET /api/clusters/{id}/diagnosis/...`, both forms
- [x] The diagnosis at the top of the Summary tab: trace, evidence, next steps, impact, "as of"
- [x] Palette: "Show diagnosis", "Copy root cause"
- [x] `docs/api.md`: the diagnosis endpoint
- [~] Checked in the browser against the scenarios: light and dark, keyboard only, 375 px; not yet
      with a screen reader (see notes)

## Phase G3: Complete rules and problems

Goal: every common failure is explained, and the overview lists root causes.

- [x] The G3 rules from the table, each with a scenario where a one-node cluster can show it, and
      unit tests for the rest (see notes)
- [x] The `unexplained` fallback and confidence shown in the UI
- [x] `GET /api/clusters/{id}/problems`
- [x] Problems at the top of the overview's "Recent warnings" card, following the namespace picker
- [x] Palette: "Problems"
- [x] Checked against both test clusters, including what the multipass cluster already has wrong (argocd-server's crash loop)

## Phase G4: Graph renderer and the Map tab

Goal: any object's neighbourhood is drawn, and it looks good.

- [x] `graph/layout.js`: kind columns, barycenter ordering, a stable layout; Vitest tests (no overlaps, the same input gives the same layout, collapsed pods)
- [x] `TopologyGraph` component: cards, curved edges, the chain to a root cause highlighted, missing and unknown styles
- [x] Pan, zoom, fit; keyboard movement along edges; accessible names
- [x] `/kit` page with sample graphs, both themes
- [~] A drawer for the selected node (reusing `Drawer`): a panel over the map instead (see notes)
- [x] The Map tab on detail pages (shortcut `5`); palette "Show map"
- [x] Decide: keep our own layout, or switch the layout function to elkjs (recorded in the notes)

## Phase G5: Topology page for namespaces and apps

Goal: a namespace or one app can be seen whole, with its problems.

- [x] The Topology page in `sections.js` (`g t`), following the namespace picker
- [x] App picker (`?app=`), "Problems only" toggle, List view; List by default under 768 px
- [x] Collapsed pods expand on request; ReplicaSets shown only when they matter
- [x] Refreshes every 10 seconds without moving the layout
- [x] Checked on the multipass cluster: `topology-test`, a large namespace, and a namespace with problems

## Phase G6: Background snapshots and the cluster view

Goal: the whole cluster can be seen at once, and snapshots are kept fresh for the timeline.

- [x] `Store.Collect`: every cluster every 30 s, backing off to 5 minutes for clusters that don't answer
- [x] `OPSCOPE_SNAPSHOT_INTERVAL` setting, documented in `docs/deployment.md`
- [x] The `Cluster()` view: namespaces holding apps, Gateways as entry points, the node strip, healthy namespaces collapsed
- [x] The Topology page with "All namespaces" selected shows it
- [x] Memory and API load measured on the multipass cluster over an hour; noted below
- [x] Checked with several clusters connected, one of them unreachable

## Phase G7: Documentation and release

Goal: the feature is documented and ready to release.

- [ ] README: features, a screenshot of a diagnosis and of a topology, the new shortcut
- [ ] `docs/api.md`: topology and problems endpoints; `docs/deployment.md`: permissions and the setting
- [ ] Screenshots in light and dark
- [ ] A final pass over this record; the status line updated; the record added to `docs/design/README.md` once released

---

## Later

- **The timeline**, in its own record: comparing snapshots, storage, the before/after view, and
  findings on the timeline ("this started 3 minutes after revision 7 rolled out").
- **Recent changes without storage:** Kubernetes keeps some history itself. ReplicaSets and
  ControllerRevisions hold rollout history, `managedFields` records which tool changed which field
  and when, and conditions and container states have times. A "What changed recently" section on a
  diagnosis could use these before the timeline exists.
- **Inferred dependencies:** env vars and ConfigMap values naming a Service (`postgres.db.svc`,
  `http://api:8080`) as `inferred` edges, marked as a guess. They'd show which app calls which.
- **NetworkPolicy:** which policies select a pod and whether they could block a route's traffic.
- **More kinds:** GRPCRoute, TLSRoute, CRDs with standard conditions (cert-manager Certificates,
  Argo CD Applications) through a generic conditions rule.

## Open questions

None at the moment. The four questions raised while planning were answered on 2026-10-10 and moved
into Decisions: scenarios run on the kind cluster, the Map tab's shortcut is `5`, background
snapshots are taken every 30 seconds, and problems sit at the top of the "Recent warnings" card.

## Phase notes

### G0

- **`internal/kube`** now holds what `resources` and `snapshot` share: `Clients` (with a new
  metadata client on every cluster), `Skipped` and `Skip` (which tell "not installed" and
  "forbidden" from a failure), `PodStatus`, the event time and count helpers, and the Gateway API
  types. `resources.Clients` and `resources.Skipped` are aliases of these, so no caller changed.
- The Gateway API types embed `metav1.ObjectMeta`, like Kubernetes' own types. Every object in a
  snapshot is therefore a `metav1.Object`, and one generic function indexes all 26 kinds. Their
  one-word statuses became methods (`Gateway.Summary()`, `HTTPRoute.Summary()`,
  `GatewayClass.Summary()`), used by the tables, the detail pages and later the graph. They also
  gained the fields the graph needs (listener certificate refs, each parent's ref and
  controller in a route's status) and a `ReferenceGrant` type. ReferenceGrants are read as `v1`,
  falling back to `v1beta1` on clusters that serve only that. The multipass cluster (Gateway API
  v1.5.1) serves them as `v1`.
- Lists are read in pages of 500, so a large cluster never needs one huge answer or runs into the
  client's 10-second timeout on a single call. Events are filtered to Warnings on the API server
  and checked again after, so a proxy or fake that ignores field selectors can't let Normal events
  in.
- `Store` ties each snapshot to the exact `*clusters.Cluster` it was taken from. A cluster that
  is removed and added again under the same ID never sees the old one's snapshot. The load runs
  on its own with a one-minute limit, so a caller who gives up doesn't fail the others waiting for
  it. Failed loads aren't kept, so the next request tries again. `Forget(keep)` drops removed
  clusters, as `metrics.History.Forget` does; the server starts using the store, and calling
  `Forget`, in G1 with the first endpoint.
- `TestClusterRolesCoverSnapshot` reads the manifests in `deploy/kubernetes/` and fails if the
  snapshot reads a kind no ClusterRole lets Opscope list. The new ClusterRole was checked with a
  server-side dry run on the kind cluster (nothing was applied). The Opscope pod already running
  in that cluster keeps its old role until the manifests are applied again.
- **Speed.** Building the lookups for a generated snapshot of 9,050 objects (5,000 pods, 1,000
  Deployments, ReplicaSets, Services and ConfigMaps, 50 nodes) with 10,000 events takes 3.2 ms and
  7.5 MB of allocations on an M4 Pro (`BenchmarkBuildIndex`), well inside the 200 ms budget.
  Reading the kind cluster (95 objects, 10 pods) takes 27 ms and holds about 0.6 MiB; the four
  Gateway API kinds are skipped as `not_installed` there. `TestLoadLive` measures any cluster when
  `OPSCOPE_TEST_KUBECONFIG` is set, and only reads.
- Reading the multipass cluster (338 objects, 50 pods, 8 warnings, Gateway API installed) takes
  33–39 ms and holds about 1.8 MiB, with nothing skipped. It was measured once its VMs were
  started again, after the rest of G0 was committed.
- Nothing visible changed. The existing endpoints were checked against the kind cluster after
  the refactor: lists, `/names`, a pod's detail page, the overview, and Gateway API's
  `not_installed` answer.

### G1

- **The graph is built once per snapshot.** `Snapshot.Derived` keeps values computed from a
  snapshot (the graph now, the diagnosis in G2) and drops them with it. `graph.Of(s)` builds on
  first use. Building the graph of a generated cluster of 5,000 pods, 1,000 Deployments and
  Services takes 24 ms (`BenchmarkBuild`); a namespace view of it takes 7 ms
  (`BenchmarkNamespaceView`). On the multipass cluster a `/topology` request takes under a
  millisecond once the snapshot is read.
- **Things the real cluster taught**, each with a test:
  - Every pod has an automatic volume that refers to the `kube-root-ca.crt` ConfigMap. Edges to
    it are left out; they would tie every pod in a namespace to one object and say nothing.
  - Every pod has a service account, nearly always `default`. Only one that's named gets an edge.
  - A Gateway's controller (NGINX Gateway Fabric here) owns the Deployment, Service and
    ConfigMaps it runs the Gateway with. The Gateway gets `owns` edges to them, which shows its
    data plane. The Deployment is still an app's root, since only an owner that is itself a
    workload makes something part of another app.
  - Static pods (etcd, kube-apiserver) name their Node as owner. That owner reference is left
    out; the pod's `runs-on` edge already says where it is, and grouping a machine's static pods
    as if the machine had made them was wrong.
  - A routing object that sends traffic to two apps (`web-route` to `web-app` and `api-app`)
    joins the one whose name is most like its own, then the first by name.
- **What a view leaves out**, so it stays readable:
  - A ReplicaSet is shown only during a rollout (more than one of a Deployment's ReplicaSets has
    pods), when it can't create pods (`ReplicaFailure`), or when nothing owns it. One whose pods
    are failing isn't: the pods already say so.
  - A workload's pod-template edges are dropped when its pods have the same edges. They're kept
    when it has no pods, where they're the only way to see what it needs.
  - ConfigMaps, Secrets and ServiceAccounts nothing uses (each namespace's `kube-root-ca.crt`
    and `default`, Helm's release Secrets) aren't shown, unless one is the focused object.
  - Machines are shown only when they aren't OK, or next to the focused object.
  - An edge lists at most five of its references, then "and N more". The graph keeps them all
    for the diagnosis.
- **Views follow edges with care.** From inside the namespace or app, every dependency is
  followed. From outside it, only to cluster-wide objects: a namespace view shows a Gateway in
  another namespace and that Gateway's class, but not the other apps that Gateway serves. A
  neighbourhood also includes everything behind any unhealthy node in it, so the cause of a
  problem is never cut off by the depth.
- **States.** A workload is unhealthy when its pods are failing *or* have been running but not
  ready for more than 5 minutes; Argo CD's application controller showed the second kind. A
  Deployment takes on a ReplicaSet's `ReplicaFailure` message, but not its pods' failures, which
  it judges itself. A missing ReferenceGrant is a node named `(none)`, a name Kubernetes can't
  give anything.
- **The store is in use.** `server.New` takes the snapshot store, removing a cluster drops its
  snapshot, and `snapshot.NewStoreReading` lets tests give the server snapshots made by hand.
  The Gateway API types gained named structs (`ListenerTLS`, `HTTPRouteRule`, `GrantFrom`,
  `GrantTo`) so tests and rules can build them.
- **Checked against multipass,** read only: `topology-test` (Gateway → two routes and an Ingress
  → two Services → their pods, and the Gateway's own data plane), every other namespace, and the
  problems filter on `argocd` (two crash-looping Deployments and their Services with no ready
  pods) and `default`, where a pod has been stuck in ContainerCreating for 97 days because the
  ConfigMap it mounts doesn't exist; the graph shows that edge. Also on the kind cluster, where
  the four Gateway API kinds are reported as skipped.
- Not done: the cluster-wide view (`Cluster()`) stays in G6 as planned; `/topology` without a
  namespace or focus answers `400` until then.

### G2

- **The engine.** Rules run once per snapshot (`diagnose.Of`, kept with the snapshot like the
  graph). A missing object gets a built-in finding that is always a root cause; an unknown one gets
  an `info` finding that says Opscope can't check it. A trace merges alike findings (same rule,
  title and culprits), so the crash-loop scenario's two pods are one step. The response carries
  every object the steps name (`nodes`), so the page can show "Pod web-1 · CrashLoopBackOff" and
  link it without another request, and every list is a JSON list, never `null` (a test checks).
- **Rules as built,** with the IDs in the table above: `pod.config`, `pod.mount`, `pod.image`,
  `pod.crash`, `pod.unschedulable`, `pod.node`, `node.not-ready`, `workload.unavailable`,
  `create-failed`, `service.selector`, `service.endpoints`, `route.backend`. Changed from the plan:
  - `create-failed` covers StatefulSets, DaemonSets and Jobs as well as ReplicaSets: they all
    report `FailedCreate` events the same way.
  - `pod.crash` already covers init containers, a failing liveness probe and a container that
    exits with code 0, which the plan left for G3's `pod.init-failed`; they read from the same
    status. G3 still adds init containers that fail without restarting.
  - `workload.unavailable` reports a rollout or a scale-up that is still under way as `info`, not
    as a fault, so its pods' problems show up during the first five minutes too.
- **Scenarios.** Thirteen manifests in `test/scenarios/` were applied to the kind cluster, left
  for seven minutes, captured (119 objects, 75 of them events) with `test/scenarios/capture.sh`,
  and deleted. Instead of a folder per scenario, one capture holds them all, since they share a
  namespace and a quota, and `scenario_test.go` lists the root cause each object must reach. A
  second capture 15 minutes later passed the same tests. Every failure reached the expected root
  cause on the first run; the wording was then polished ("Its only replica isn't ready", a
  quota's amounts said once, a missing object's users listed by workload rather than pod by pod).
- **What the real cluster showed:** the exact formats of the scheduler's message
  (`0/1 nodes are available: 1 Insufficient cpu.`), the quota and service account admission
  errors, the kubelet's `couldn't find key` message, and image pull errors for a tag that doesn't
  exist (`code = NotFound`) and a registry that doesn't (`no such host`). Crash-looping pods are
  often caught between restarts with a terminated container rather than `CrashLoopBackOff`; the
  rule reads both. The wrong-selector Service finds its near miss: "Did you mean
  app=crash-loop? 2 pods have that label."
- Branches a one-node cluster without Gateway API can't show have unit tests: a node that
  stopped reporting, a registry refusing credentials, an invalid image name, exit codes, an
  unbound claim and a taint at scheduling, Pod Security, a quota requiring limits, a LimitRange,
  a route's missing backend and missing ReferenceGrant, a partly ready Service, a Secret Opscope
  isn't allowed to read, and a cycle in a trace. `TestCommandsAreReadOnly` checks that every
  suggested command is `kubectl get`, `describe` or `logs`.
- **Fixed on the way:** `Snapshot.Derived` held its lock while building, and the diagnosis builds
  the graph through it: a deadlock, found by the server test. Each derived value now builds once on
  its own (`TestDerived`).
- **The page.** `Diagnosis` sits at the top of the Summary tab and was built on `/kit` first,
  with sample traces in both themes. The edge takes the colour of the worst problem, evidence
  opens by itself only on root causes, and an object with nothing wrong gets one quiet line, or
  "It's part of a problem in …" when another object's trace leads to it. "Previous logs" opens
  the Logs tab on the crashing container's previous run (`?container=…&previous=true`, which the
  log viewer now reads). The overview's `focusCard` moved to `focus.js` as `focusSection`, used by
  "Show diagnosis" too; `focusSectionSoon` waits for the Summary tab with timers, since browsers
  pause animation frames in a tab that isn't on screen.
- **Checked in the browser** against the kind scenarios and the multipass cluster, in both themes,
  at 375 px, and with the palette from the keyboard. On multipass the diagnosis explains the two
  problems G1 found: `default/app-frontend` waits on ConfigMap `app-config`, which doesn't exist,
  and `argocd-server` exits with code 20 after 1,103 restarts. A pass with a real screen reader is
  still to do; the structure is there (a heading, nested lists, each step's severity as hidden
  text, labelled copy buttons).

### G3

- **Rules as built,** with the IDs in the table above. Changed from the plan:
  - `pod.init` reports an init container that keeps running (waiting for something) for more
    than 5 minutes. Init containers that fail were already covered by `pod.crash` in G2, which
    now also reports a container that failed and won't be restarted (a Job's pod).
  - `deployment.rollout` reports every rollout, not only a stuck one: `info` while it goes well,
    `degraded` when the new revision's pods fail while the old one serves, `failing` when nothing
    serves. It also says **what the revision changed**, by comparing the two ReplicaSets' pod
    templates ("the image of container app (busybox:1.36 → busybox:no-such-tag-opscope)"), which
    the plan had under Later. `workload.unavailable` leaves Deployments in a rollout to it.
  - `statefulset.ordered` became part of `workload.unavailable`'s explanation for a StatefulSet
    ("db-1 won't be created until db-0 is"), since it's the same finding with a better reason.
  - `node.pressure` also notes a cordoned node (`info`) and a node whose network isn't set up.
  - `hpa.scaling` also notes an autoscaler at its maximum (`info`).
  - `unexplained` lists unhealthy neighbours as `related`, a separate field from `blames`, so a
    guess is never followed as a cause.
- **Scenarios.** Nine more manifests (readiness probe, init container waiting, eviction, stuck
  rollout, failed Job, failing CronJob, claim with a missing class, autoscaler without metrics,
  Ingress with a missing class and TLS Secret) ran on the kind cluster with the G2 ones and were
  captured together (`capture.sh` now also reads Jobs, CronJobs, claims, autoscalers and
  disruption budgets). Every one reached its expected root cause, as did a second capture nine
  minutes later; the namespace was then deleted. The rollout needs a second step (an image
  update), described in `test/scenarios/README.md`.
- **What the scenarios showed:** an evicted pod's container is killed with exit code 137, which
  `pod.crash` reported as well as the eviction; evicted pods are now left to `pod.evicted`. Every
  ReplicaSet whose pods failed ended up `unexplained`, because no rule looked at ReplicaSets with
  pods; `workload.unavailable` now covers them. A Deployment created broken has a single revision
  and isn't a rollout, even though its counts look like one.
- **What the real cluster showed.** On multipass the first problems list had 37 entries, most of
  them false: the VMs had rebooted 11 hours earlier and nearly every pod had restarted once with
  exit code 255. A running container whose last stop was more than an hour ago is no longer a
  problem; a failing liveness probe still is, since it restarts a container every few minutes.
  `StorageClass manual doesn't exist` was false too: a PersistentVolume made by hand can name a
  class only to match claims, so a volume's class, and a bound claim's, are optional references
  now. What's left are five real problems: argocd-server and argocd-applicationset-controller
  crash-looping, Argo CD's application controller failing its readiness probe, `default/app-config`
  missing, and three Services in `default` that select nothing.
- **Problems.** `Diagnosis.Problems(namespace)` collects the findings that nothing explains, merges
  alike ones, and lists what each affects (pods left out when their workload is there). A note
  doesn't make a finding non-root: a pod whose Secret Opscope can't read is still a problem. The
  overview's card is titled "Problems and recent warnings" when there are any, with
  "22 problems, affecting 46 objects" beside it, the first six shown and a button for the rest.
  Each problem shows its subject above its title (several titles say "It …"), and the title links
  to the page whose diagnosis explains it: the object itself, or, for a missing one, the first
  thing it affects that has a page.
- **"Problems" in the palette** links to the overview with `#problems`. The list's id is
  `problem-list`, not `problems`: with a matching id, the browser jumped to it by itself on page
  load and scrolled the boxes around it, pushing the top bar out of view, the problem
  `focusSection` exists to avoid.
- Object names in the diagnosis and the problems leave out the namespace being looked at, and keep
  it for others ("Deployment argocd/argocd-server" with all namespaces selected).
- **Speed.** Building the graph and running every rule for a generated cluster of 5,000 pods, a
  tenth of them broken, takes 41 ms (`BenchmarkBuildAndDiagnose`), once per snapshot, inside the
  plan's 200 ms. A problems request for one namespace then takes 0.2 ms (`BenchmarkProblems`).
- Checked in the browser on both clusters, in both themes, at 375 px.

### G4

- **Our own layout stays; elkjs isn't needed.** On the real clusters (multipass's `topology-test`
  Gateway, Argo CD, a machine with 60 objects around it, and the kind scenarios) the columns read
  naturally and the edges rarely cross. It lays out a generated namespace of 416 cards in 20 ms and
  one of 1,000 cards in 60 ms (in Node), keeps cards in place across refreshes (which elkjs doesn't offer),
  and adds no dependency. Two things made the difference on real clusters, both in
  `graph/layout.js` with tests: lanes for long edges, shared at their busier end, and a
  transpose pass after each sweep. The layout reports its crossings, which the tests check.
- **The panel instead of the `Drawer`.** The `Drawer` is modal: picking the next card would mean
  closing it first. The panel sits over the map's right side and leaves the map usable; Escape or
  its close button returns focus to the card. On a phone it covers the map, which starts as a list
  there anyway.
- **The topology endpoint carries findings** about everything shown (the pods in groups included),
  so the map can draw chains and fill the panel without another request. What they blame is added
  to the view when nothing else would put it there, with a new `blames` edge: a ResourceQuota
  has no edges, so the over-quota scenario's map ended at its ReplicaSet until then. A test checks
  that every scenario's map reaches its root causes and has their findings.
- **Neighbourhoods stop at what many objects share.** The first map of `default/app-frontend` on
  multipass had 24 objects: two steps out, through its machine, it reached every pod on that
  machine and their problems. A neighbourhood now reaches a node, a namespace or a class but doesn't
  go on through it, unless it's the map's own object; the same map has three objects (the pod, its
  missing ConfigMap, its machine).
- **States.** A workload whose rollout is stuck while its old pods still serve was "failing" (red,
  with "1/1"), though its diagnosis said "degraded". A workload with a failure is now failing only
  when none of its replicas are ready, and degraded otherwise. An Ingress can't say anything about
  its own health, so a card shows the worse of its state and its own findings.
- **What a card doesn't do** (yet): animate when the layout changes on a refresh (cards keep their
  order, so little moves), or show an edge's `via` on hover; the panel lists them.
- **Checked in the browser** on both clusters, in both themes, at 375 px (the tab opens as a list;
  the panel covers the map), with the keyboard (Tab into the map, arrows along edges, Enter, Escape,
  `5` from anywhere on the page). On the kind cluster with the scenarios running, a script fetched
  the map and the diagnosis of each of the 26 workloads, Services, Ingresses, Jobs and autoscalers
  in the namespace: every root cause was on its map, with its finding. A request takes under a
  millisecond once the cluster has been read. The scenarios namespace was deleted afterwards.

### G5

- **The page** is `pages/Topology.jsx`, built from G4's `TopologyView`: the map or the list, the
  panel, the legend. Its own controls sit in the same toolbar: the app picker (each app with its
  state's mark, and a search box when there are many), "Problems only", and "Group all pods" once
  any are shown one by one. The app and the filter are in the URL, so a link shows the same map;
  which pods are shown one by one isn't, and resets with the namespace.
- **"All namespaces"** lists the namespaces to map, those with problems first and how many each
  has (`problemsByNamespace` in `diagnosis.js`: a problem counts in every namespace its objects or
  what it affects are in). G6 replaces this with the cluster's own map.
- **Pods one by one.** The panel of a group of pods offers "Show each pod"; the workload's card is
  then selected, offering "Group its pods". It's the endpoint's `expand`.
- **An app view lists the namespace's apps too** (it didn't), so the picker works while an app is
  shown. Apps of nothing but configuration nobody uses (the `gridops` Secret on multipass) aren't
  listed: their view is empty. An app that isn't in the namespace (after switching namespaces with
  one picked) says so, with a button to show the whole namespace.
- **Switching** the app or the filter keeps the last map on screen until the next one arrives, and
  then fits it; a refresh fits nothing and moves nothing. On kind, with the scenarios running and a
  CronJob starting a Job every minute, six refreshes in a row added two cards and changed the order
  of none.
- With no object to start from, the first view shows the top left of the map, or all of it when it
  can be read that way. Column headings may use the gap beside their column, so they still fit when
  zoomed out.
- **Checked in the browser** on multipass: `topology-test` (19 objects: the Gateway, routes, an
  Ingress, the data plane), `argocd`, its largest namespace (32 objects; 8 with "Problems only":
  two Services, three workloads and their crash-looping or unready pods, each a root cause),
  `default` with its nine apps (`statefull`: a StatefulSet's three pods, claims, volumes and class,
  shown one by one and grouped again), and on kind the scenarios namespace (57 objects). In both
  themes, at 375 px (it opens as a list), and with `g t` and the palette. The scenarios namespace
  was deleted afterwards.

### G6

- **Background snapshots.** `Store.Collect` (`snapshot/collect.go`) checks every second which
  clusters are due and reads each on its own, so one that doesn't answer holds up no other. A
  cluster is due 30 seconds after its latest snapshot, and one a page asked for counts: while
  someone looks at a cluster, pages read it up to every 10 seconds and the background adds nothing.
  A cluster that doesn't answer is tried after 1, 2 and 4 minutes, then every 5, and every 30
  seconds again once it answers, also when a page's reading is what found it answering. The first
  failure is logged as a warning and the recovery as information; each retry, and each snapshot
  with its size and how long it took, at `debug`. Tests run the schedule on a fake clock.
- **Settings.** `OPSCOPE_SNAPSHOT_INTERVAL` (`30s` by default; `10s` at least, the pages' own
  interval; `0` or `off` for none) and, added for watching it, `OPSCOPE_LOG_LEVEL`. Both stop
  Opscope at startup with a clear message when they can't be used, like the other settings.
- **Measured on multipass** for half an hour rather than the planned hour (cut short on request,
  the numbers having settled): a built Opscope with only that cluster, nobody looking. It took 55
  snapshots of its 338 objects in 30 minutes (one every 32 seconds; that build checked for due
  clusters every 5 seconds, since made every second), each in 12 to 44 ms, 16 ms typically. Each
  snapshot is 27 `list` calls, one per kind (multipass has no kind with over 500 objects), so about
  50 calls a minute. The process used 43 MB after two minutes, 46.6 MB after twenty and 46.7 MB
  after twenty-nine: it settles rather than grows, holding one snapshot. The cluster's map of a
  generated cluster of 5,000 pods takes 4 ms to build (`BenchmarkClusterView`), 5 ms with its
  problems (`BenchmarkClusterRequest`).
- **The cluster's map.** With "All namespaces", the Topology page shows `Cluster()`: each app is a
  card ("7 workloads, 3 failing") in its namespace's box, a namespace with nothing wrong is one
  card ("4 apps") that its panel can open ("Show its apps"), Gateways and classes are shared entry
  points, and the machines are a strip under everything. Edges are the graph's own, merged between
  cards, with `via` naming what's inside ("HTTPRoute web-route"). Only root causes come with it,
  and each card lists the objects whose findings it shows; a missing StorageClass, which belongs
  to no namespace, shows on the app whose claim needs it. A test checks every scenario problem is
  on a card.
- **A grid, not one tall column.** Drawn as plain columns, the 17 namespaces of multipass were one
  column 25 cards tall beside two nearly empty ones. Now only what's joined to an entry point (here
  `topology-test`, through its routes) stays in the columns; every other namespace is a block in a
  grid under them, those with problems first, and the machines below that. The whole cluster fits
  on one screen. `layout()` does both (`grid`, `strip`), with `sections` for their headings, and
  the keyboard moves through them: arrows along the strip, by column in the grid, and up and down
  between the parts.
- **Problems the diagnosis found count.** A container that restarts is running between restarts,
  so its pod, app and namespace were "ok" and the namespace folded away while its problem was
  listed (`tigera-operator`, which had exited with code 1). The server now passes the diagnosis's
  problems in (`Options.Troubled`), and they count toward the app and the namespace.
- **Restarts from a machine restart aren't crashes.** Multipass had restarted shortly before, so
  for an hour nearly every container on it counted as having just crashed with exit code 255: 34
  problems on the overview, and every namespace open on the map. The runtime records such a stop
  as reason `Unknown` (it never saw the container exit); `pod.crash` now leaves out a running
  container whose last stop was `Unknown`. The overview went back to its six real problems.
- **Checked** on multipass (28 cards; with "Problems only", the six apps with problems in their
  three namespaces), on kind (everything in the grid: no Gateway API), in both themes, at 375 px
  (the list, with each app's namespace written out), and with three clusters connected, one an
  address nothing answers at. That one failed at 20:14 and was tried again at 20:15, 20:17, 20:21 and 20:26 (1, 2, 4 and 5
  minutes apart, the next set for 5 minutes later), while the other two were read every 30 seconds;
  its pages show the usual "Can't reach" banner.
