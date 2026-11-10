# Opscope: Topology

Opscope's detail pages already link an object to some of what it's connected to: a pod to its
owner and node, a claim to its volume and StorageClass, a StatefulSet to its claims. Each link is
worked out on its own, on the page that shows it, and most connections aren't shown anywhere: which
pods use a Secret, which Services select a pod, which routes send traffic to a Service. This record
adds the **Graph engine**: one model per cluster of every object Opscope reads and the typed edges
between them, kept up to date from the watch cache and the bus that v1.4 added. Every edge records
where it was read from. A reference to something that doesn't exist is kept as an edge to a
**missing** node, because the gaps are what diagnosis needs later.

The graph is a model you can query first and a picture second. Detail pages read their links from
it, ConfigMaps and Secrets gain a "Used by" section, and the command palette can answer "pods using
db-creds" or "what runs on node-3". Only then does the new **Topology** page draw it. Opscope stays
read-only, one image and one process; the only new permissions are `get`, `list` and `watch` on
three more kinds.

This is v1.5.0 in the engine release plan. Phases are numbered **T0–T7** so they don't mix with the
build phases 0–8 or the D, G, L, P, R and S phases. All work happens on the `docs/topology` branch,
one commit per phase, as in v1.4; each phase is reviewed before it is committed, and the next phase
starts only after that.

**Status (2026-11-10):** T0–T3 done; T4 next.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Why the first attempt was removed

Topology was built once before, in phases G0–G7 on `feature/topology` (2026-10-10 to 10-12), and
taken out of `main` on 2026-10-28 by resetting it to `d019e61`. The branch and its record are still
there, as is the old `main` in the local branch `backup/main-with-topology`.

**It wasn't built to a plan.** When G0–G7 was built there was no plan for the engines that come
after it: no diagnosis engine, no time machine, no change log. Topology made its own choices for
all of them, and those choices don't fit the engine release plan written afterwards. In detail:

- **It read the cluster its own way.** It took a full snapshot with `list` calls, on demand and
  then every 30 seconds in the background. The plan has one collector and one watch cache per
  cluster (v1.4), which every engine reads; a second way of reading the cluster would mean two
  versions of it.
- **Topology and diagnosis were one feature.** Its diagnosis code (26 rules written in Go,
  root-cause traces and a problems list) was as large as its graph code. The plan splits that over
  four releases: signals and
  health (v1.6), rules as data with their own tests (v1.9), and a diagnosis pipeline that weighs
  changes before state (v1.10). Its rules couldn't be reused as they were.
- **None of the contracts existed.** Nodes were keyed by `resource/namespace/name`, not UID, so a
  recreated object looked like the same one. There was no time in any query, edge IDs weren't
  derived from their content (so two graphs couldn't be compared), and an edge's only record of
  where it came from was a short `via` label.
- **It made choices for later releases early.** It planned to store ConfigMap values for a
  timeline; the plan tracks ConfigMap content by hash (contract 11). Views were shaped for drawing
  on the server, so what a graph *is* and how it's *drawn* were mixed.
- **It was too big to review as one release:** about 36,000 added lines over 150 files, half of
  them captured test data.

**What this attempt does differently:**

- The graph is built only from the watch cache and the bus, never by reading the cluster itself.
- The graph has no opinion on health. Each node has a health slot that stays empty until the
  Signal engine fills it in v1.6. There are no rules, findings or root causes in this release.
- The contracts come first: UID keys, `at` on every query, content-derived IDs, evidence on every
  edge, and edge builders in a registry.
- The engine, the detail pages' links and "Used by" ship before anything is drawn, so if the
  picture slips, the release still stands.

**What carries over:** what G0–G7 learned on real clusters, each with a test this time too (see
[Lessons kept](#lessons-kept)), and its own SVG layout code, which worked. Nothing is merged back
as a whole; each piece taken from `feature/topology` is named in the phase notes.

## Decisions

- **One graph per cluster, kept up to date from the bus.** The Graph engine subscribes to
  `ObjectChanged` and `CollectorGap` events. Each change updates only the nodes and edges of the
  object that changed and of the objects that refer to it; the graph is never rebuilt whole for one
  change. The initial list's adds (`Initial: true`) build the first graph. The Time machine (v1.8)
  replays the same events.
- **A gap rebuilds what it covers.** A `CollectorGap` for one kind (a relist, a mode change)
  replaces every node of that kind from the cache. A lagging subscriber (`subscriber_lagged`, which
  can have missed anything) rebuilds the whole cluster's graph. A stopped collector's gap drops the
  cluster's graph.
- **One builder, two ways to run it.** Building the graph is one function over objects: adding
  objects one at a time gives the same graph as building from all of them at once, and a test
  checks that. Normally the graph is kept up to date from the bus, as above. When the graph can't
  be kept that way (live data is off, or a kind it needs is in `direct` or `pending` mode), a
  request builds the part it needs from a `resources.Source` instead, the namespace asked about
  plus cluster-wide kinds, and keeps it for 10 seconds, as pages did in v1.3. The answer says which
  way it was built. This keeps `OPSCOPE_LIVE=false` a working fallback, as v1.4 promised for v1.5.
  The alternative, Topology saying it needs live data and detail pages keeping their own lookups
  for that case, would mean two ways of finding links. (Answered in review, 2026-11-09.)
- **Three kinds join the cache, with `get`, `list` and `watch`:** ReplicaSets, EndpointSlices and
  ServiceAccounts. They get no list or detail pages in this release and aren't in the `/names`
  index; they're in the graph, where selecting one opens the panel. A list page for each is a small
  change later. They are added to the ClusterRole, the permissions table in `docs/deployment.md`,
  and a new `graphOnly` table next to `listOnly`, so the collector and the `Direct` source pick
  them up from `resources.Kinds()` as they do every other kind. (Answered in review, 2026-11-09.)
- **ReplicaSets are trimmed in the cache.** Their pod template goes, keeping their owner, selector,
  replica counts, status and revision annotation. A Deployment keeps ten old ReplicaSets by
  default, each about the size of the Deployment, and nothing reads their templates: the
  Deployment's own template is the current one, and the Change log (v1.7) reads Deployments.
- **Node IDs are UIDs (contract 1).** An object that is referred to but doesn't exist is a
  **missing** node with the ID `missing:<resource>/<namespace>/<name>`. One whose kind Opscope
  can't read (forbidden, not installed, not cached) is **unknown**:
  `unknown:<resource>/<namespace>/<name>`. When a missing object is created, its placeholder goes
  and the edges point at the real node. Without the Secret role, Topology still shows which pods
  reference a Secret, read from the pod spec, as contract 12 asks.
- **Edge IDs are derived from their content (contract 3):** a SHA-256 over the source and target
  node IDs and the edge kind, length-prefixed like the ConfigMap hash, cut to 16 bytes. There is
  one edge per source, target and kind. Two env vars and a volume naming the same Secret are one
  edge with three pieces of evidence. Two graphs from two moments can then be compared by ID.
- **Every edge has evidence (contract 4):** for each field that makes it, the builder's name, the
  field path (`spec.volumes[2].secret.secretName`), the resourceVersion of the object it was read
  from and when Opscope saw it. Diagnosis in v1.10 links these; it never makes up its own.
- **Edges point from the dependent to its dependency: "A needs B".** A Deployment needs its
  ReplicaSet, a pod needs its Secret and its node, a Service needs the pods it selects, a route
  needs its Service and its Gateway. **Upstream** of X follows edges forward (what X depends on);
  **Downstream** follows them backward (what depends on X). Changed from the plan's table: there,
  the routes row reads "Gateway → HTTPRoute", in the direction traffic flows. An HTTPRoute
  depends on its Gateway, not the other way round, so here it's an `attaches` edge
  `HTTPRoute → Gateway`. The path tracer in v1.9 walks in both directions anyway.
- **A selector that matches nothing is recorded on its node.** A Service whose selector matches no
  pods has no edge to draw, so its node carries a **dangling** entry: the edge kind, the field
  (`spec.selector`) and why ("matches no pods"). A reference by name to something that doesn't
  exist is an edge to a missing node. Diagnosis needs both kinds of gap.
- **References from pod templates too.** Deployments, StatefulSets, DaemonSets, Jobs and CronJobs
  get `references` edges from their templates as well as their pods, so a workload scaled to zero,
  or whose pods can't be created, still shows what it needs.
- **The graph holds no health.** Each node has a `health` field that stays `null` until the Signal
  engine (v1.6), and a `status`: the text its list page already shows ("CrashLoopBackOff", "2/3"),
  for display only. Topology draws no health colours in this release. That way the overview, the
  Issues feed and Topology get their health from one place when it comes.
- **Edge builders are a registry (contract 7).** A builder names the kind it reads and returns
  references (edge kind, target, field, optional or not). Adding an edge is one more entry, the
  same way a lister is, so a rule pack in v1.13 can add entries too.
- **Every query takes a time (contract 2).** `at` zero means now; anything else is `ErrNoHistory`
  until v1.8. The HTTP API doesn't take `?at=` yet, as in v1.4.
- **The query API is plain JSON** under `/api/clusters/{id}/graph/…` (contract 8): Neighbors,
  Upstream, Downstream, Path and Subgraph, as the plan lists them, plus a lookup by name for pages
  that know only names.
- **Every answer says what it couldn't see.** A response lists the kinds the graph couldn't read
  and why (forbidden, not installed, read on request), so a page can say "Secrets can't be read in
  this cluster, so they show as unknown" rather than looking complete (contract 12).
- **Detail pages read their links from the graph.** Owners, a pod's node, a claim's volume and
  class, a pod's volumes, a claim's users, a StatefulSet's claims and a StorageClass's counts all
  become graph queries, so a detail page and Topology can never disagree. ConfigMaps and Secrets
  gain **"Used by"**: every workload that uses them, and every pod that no workload owns, with how
  (env, envFrom, volume).
- **The detail page's own object stays a direct read,** as in v1.4; only its links come from the
  graph. Contract 5 lists three direct reads (YAML, logs, a Secret's value); the object is a fourth,
  kept because the YAML and ConfigMap values come from it and the cache doesn't hold them. The
  object and the graph can then be a moment apart, but never in their links. (Answered in review,
  2026-11-09.)
- **Graph search understands two phrases:** `using <name>` ("pods using db-creds": everything with
  an edge to an object of that name) and `on <node>` ("what runs on node-3"), with an optional kind
  in front to narrow the answer. Anything richer waits until people use it. (Answered in review,
  2026-11-09.)
- **Drawn with our own SVG layout, no library.** Kubernetes kinds have a natural left-to-right
  order, so a layered layout in our own code is enough. G4's `graph/layout.js` laid out 1,000
  cards in 60 ms, kept cards in place across refreshes, and read well enough on real clusters that
  G4 found elkjs wasn't needed. It's the starting point here, with its tests, adjusted to this
  model. No new frontend dependency.
- **The Topology page shows one namespace, or one object's neighbourhood.** It follows the
  namespace picker; with "All namespaces" it lists the namespaces to pick from, with their object
  counts. `?focus=<uid>` shows one object and what's around it, which is where "Show in topology"
  lands. A whole-cluster map is left for later (see [Not in this release](#not-in-this-release)).
- **The text comes first.** Everything the map shows is also in a list view of the same graph, which
  works with a screen reader and is where the page opens on screens narrower than 768 px. The map
  itself is one Tab stop: arrow keys move along edges and within a column, Enter opens a card's
  panel, Escape closes it and returns to the card.
- **Built in `/kit` first**, with sample graphs (small, with missing and unknown nodes, and a
  generated namespace of 1,000 objects), in both themes, before it reaches a page.
- **Memory is measured, as in v1.4.** The three new kinds and the graph itself both add to memory.
  T2 measures them per 1,000 objects, and the "Memory" table in `docs/deployment.md` and the
  manifest follow the numbers.

## Design

### Packages

```
backend/internal/graph/       the model, the builders (one file per area), the engine and queries
backend/internal/server/      graph.go: the /graph endpoints
frontend/src/graph/           layout.js (from G4), the map and list components
frontend/src/pages/           Topology.jsx
frontend/src/commands/sources graph.js: graph search in the palette
```

`graph` imports `resources` (for `Source`, `Kinds()` and the rows' status text) and `bus`; nothing
imports `graph` but the server. The collector doesn't know the graph exists.

### Model

```go
type Node struct {
	ID              string    `json:"id"`        // the UID; "missing:…" or "unknown:…" otherwise
	Resource        string    `json:"resource"`  // URL name: "pods"
	Kind            string    `json:"kind"`      // "Pod"
	Namespace       string    `json:"namespace,omitempty"`
	Name            string    `json:"name"`
	Presence        string    `json:"presence"`  // present, missing, unknown
	Status          string    `json:"status,omitempty"` // what its list page shows; display only
	Health          *Health   `json:"health"`    // null until the Signal engine (v1.6)
	Dangling        []Dangling `json:"dangling,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"`
}

type Edge struct {
	ID       string     `json:"id"`   // hash(from, to, kind)
	From     string     `json:"from"` // the dependent
	To       string     `json:"to"`   // what it needs
	Kind     string     `json:"kind"`
	Optional bool       `json:"optional,omitempty"` // every reference behind it is optional
	Evidence []Evidence `json:"evidence"`
}

type Evidence struct {
	Builder         string    `json:"builder"`         // "pods.volumes"
	Field           string    `json:"field"`           // "spec.volumes[2].secret.secretName"
	ResourceVersion string    `json:"resourceVersion"` // of the object the field is in
	Seen            time.Time `json:"seen"`
	Optional        bool      `json:"optional,omitempty"`
}

type Dangling struct {
	Kind   string `json:"kind"`   // "selects"
	Field  string `json:"field"`  // "spec.selector"
	Reason string `json:"reason"` // "matches no pods"
}
```

### Edges

| Kind         | From → To                                                                   | Read from                                                                  | In v1.4 |
| ------------ | --------------------------------------------------------------------------- | -------------------------------------------------------------------------- | ------- |
| `owns`       | Deployment → ReplicaSet → Pod; StatefulSet, DaemonSet, Job → Pod; CronJob → Job; Service → EndpointSlice; a Gateway → its data plane | `metadata.ownerReferences` (controller), reversed | Owner links on detail pages |
| `references` | Pod and pod templates → ConfigMap, Secret, PVC, ServiceAccount; Ingress, Gateway → TLS Secret | volumes (projected too), `env.valueFrom`, `envFrom`, `imagePullSecrets`, `serviceAccountName`; `tls[].secretName`, `listeners[].tls.certificateRefs` | Pod volumes section |
| `binds`      | PVC → PV → StorageClass                                                     | `spec.volumeName`, `storageClassName` (or the default class, which the evidence says) | Storage links |
| `runs-on`    | Pod → Node                                                                  | `spec.nodeName`                                                            | Node field |
| `selects`    | Service → Pod                                                               | `spec.selector`, same namespace                                            | New |
| `endpoints`  | EndpointSlice → Pod                                                         | `endpoints[].targetRef`, with `ready` in the evidence                      | New |
| `routes`     | Ingress → Service; HTTPRoute → Service                                      | rules' backends and `defaultBackend`; `rules[].backendRefs`                | Shown as text |
| `attaches`   | HTTPRoute → Gateway; Gateway → GatewayClass                                 | `parentRefs`; `gatewayClassName`                                           | Shown as text |

`endpoints` isn't in the plan's table. It's added because the path tracer in v1.9 walks Service →
EndpointSlice → Pod, and an EndpointSlice says which pods are ready, which a selector can't.

### Lessons kept

From G0–G7's notes, each with a test:

- Every pod mounts the `kube-root-ca.crt` ConfigMap through its automatic token volume, and almost
  every pod runs as the `default` ServiceAccount. Edges to these are left out: they tie every pod in
  a namespace to one object and say nothing. A ServiceAccount that is named gets its edge.
- Static pods (etcd, kube-apiserver) name their Node as owner. That owner reference is left out:
  `runs-on` already says where the pod is, and the machine didn't make it.
- A Gateway's controller owns the Deployment, Service and ConfigMaps that run the Gateway. Those
  `owns` edges stay: they show the Gateway's data plane.
- A neighbourhood stops at what many objects share (a node, a StorageClass, a GatewayClass) unless
  it starts there. Two steps out through a node reached every pod on it.
- In a namespace view, a workload's pods are one card ("3/4") unless asked to show each one, and
  ReplicaSets are hidden unless a rollout has more than one with pods.
- The layout keeps cards in the same order across a refresh; new cards find their place among them.

### Engine

```go
// Query is how every graph query is asked. At zero means now; anything
// else is ErrNoHistory until the Time machine (v1.8).
type Query struct {
	At        time.Time
	Depth     int    // steps out; 0 means the default (2)
	Direction string // "up", "down" or "both"
	Kinds     []string // edge kinds to follow; empty means all
}

func (g *Graph) Node(id string, q Query) (Node, bool, error)
func (g *Graph) Find(resource, namespace, name string, q Query) (Node, bool, error)
func (g *Graph) Neighbors(id string, q Query) (Subgraph, error)
func (g *Graph) Upstream(id string, q Query) (Subgraph, error)   // what id depends on
func (g *Graph) Downstream(id string, q Query) (Subgraph, error) // what depends on id
func (g *Graph) Path(from, to string, q Query) (Subgraph, error) // shortest, either direction
func (g *Graph) Subgraph(id string, q Query) (Subgraph, error)
func (g *Graph) Namespace(ns string, q Query) (Subgraph, error)

type Subgraph struct {
	Nodes   []Node        `json:"nodes"`
	Edges   []Edge        `json:"edges"`
	Built   string        `json:"built"`   // "live" or "on_request"
	Partial []PartialKind `json:"partial"` // kinds it couldn't read, and why
}
```

- One goroutine per cluster applies events; queries take a read lock and return copies, so a
  page never sees half an update.
- The graph keeps an index of references it couldn't resolve, by target. When a missing object is
  created, the objects that wanted it are found there, not by scanning the graph.
- Selectors are checked again only in the namespace that changed: when a pod's labels change, or
  a Service's selector does.

### API

| Endpoint                                                          | Returns                                     |
| ----------------------------------------------------------------- | ------------------------------------------- |
| `GET /api/clusters/{id}/graph/find?resource=&namespace=&name=`    | the node, and its neighbours                |
| `GET /api/clusters/{id}/graph/nodes/{uid}`                        | the node, and its neighbours (Neighbors)    |
| `GET /api/clusters/{id}/graph/nodes/{uid}/upstream?depth=&kinds=` | what it depends on                          |
| `GET /api/clusters/{id}/graph/nodes/{uid}/downstream?depth=&kinds=` | what depends on it                        |
| `GET /api/clusters/{id}/graph/path?from=&to=`                     | the shortest path between two nodes         |
| `GET /api/clusters/{id}/graph/subgraph?uid=&depth=&direction=`    | one object's neighbourhood                  |
| `GET /api/clusters/{id}/graph/subgraph?namespace=`                | a namespace, for the Topology page          |
| `GET /api/clusters/{id}/graph/search?q=`                          | graph search for the palette (see below)    |

Every answer is a `Subgraph`. All of them go into `docs/api.md`, which says, as for the rest of the
API, that it's the API behind the UI and isn't declared stable.

### Pages

- **Detail pages** get their links from `find` and `downstream` instead of the code in
  `detail_sections.go` and `storage_detail.go`. The detail object itself, its YAML, logs and Secret
  values stay direct, on-request reads (see Decisions).
- **"Used by"** on ConfigMaps and Secrets: a table of what uses it, by kind and name, with how
  (env `STRIPE_KEY`, envFrom, volume `config`), workloads first and their pods counted under them.
- **Topology page** in the Cluster group, after the overview (`g t`). Toolbar: the namespace from
  the picker, "Map" and "List", and the panel for a selected card with its connections and a link
  to its page. Kinds without a page of their own (ReplicaSets, EndpointSlices, ServiceAccounts)
  open only the panel.
- **Palette:** "Topology" as a page; "Show in topology" on any detail page; graph search. Graph
  search understands two phrases: `using <name>` ("pods using db-creds": everything with an edge to
  an object of that name) and `on <node>` ("what runs on node-3"). The words before them name a
  kind and narrow the answer.
- **Live:** the Topology page uses the stream as lists do, for every kind it shows, and refetches at
  most once every 2 seconds. The layout doesn't move cards that are still there.

## Phase T0: Plan

Goal: this record is reviewed before the work starts.

- [x] Record written on `docs/topology`, starting with why G0–G7 was removed
- [x] Reviewed, open questions answered and moved into Decisions

## Phase T1: Graph model and edge builders

Goal: a graph can be built from a set of objects, with every edge kind and its evidence; nothing
reads it yet.

- [x] `internal/graph`: `Node`, `Edge`, `Evidence`, `Dangling`; node and edge IDs; the builder
      registry
- [x] Builders for every edge kind in the table, and the dangling selector
- [x] Missing and unknown nodes; the lessons kept, each with a test
- [~] `Build(objects)` and ~~`Apply(change)`~~ `Upsert` and `Delete`, and a test that applying
      objects one at a time in any order gives the same graph, IDs included, as building from all
      of them (see notes)
- [x] Tests with objects in Go for every edge kind and its evidence; a benchmark for building a
      namespace of 1,000 objects and a cluster of 5,000 pods

## Phase T2: The graph on the cache

Goal: a graph per cluster is kept up to date from the bus, with the three new kinds cached.

- [x] ReplicaSets, EndpointSlices and ServiceAccounts: the `graphOnly` table, `typedLists`, the
      ReplicaSet trim, the ClusterRole and the permissions table in `docs/deployment.md`
- [x] The engine follows the bus: initial adds, changes, gaps (one kind, a lagging subscriber, a
      stopped collector); a cluster's graph goes with its collector
- [~] Built on request from a `Source` when live data is off or ~~a kind it needs is `direct` or
      `pending`~~ a kind is `pending` or pods are `direct` (see notes); kept for 10 seconds
- [~] `test/scenarios/topology.yaml`: a workload using a ConfigMap, a Secret, a claim and a named
      ServiceAccount; a pod referring to a Secret and a ConfigMap that don't exist; an Ingress to a
      Service that doesn't exist; a Service whose selector matches nothing (~~the existing
      `no-endpoints` too~~). ~~`test/scenarios/gateway-api/`~~ `topology-gateway-api.yaml`: a
      route to a missing Service, for a cluster with the Gateway API CRDs. A table in the README of
      the edges each should give
- [x] Every edge kind found on the scenario cluster and on multipass (read-only); the graph after a
      change matches a graph built from scratch on the same cluster
- [~] Memory per 1,000 objects for the new kinds and for the graph; ~~the manifest and~~ the
      "Memory" section follows the numbers (the manifest's limit stays; see notes)

## Phase T3: Query API

Goal: every query in the plan answers over HTTP.

- [x] `Node`, `Find`, `Neighbors`, `Upstream`, `Downstream`, `Path`, `Subgraph`, `Namespace`;
      `ErrNoHistory` for a non-zero `at`; hubs stop a walk
- [~] The `/graph` endpoints, with `partial` and `built` in every answer (node IDs in the query
      string; see notes)
- [x] Graph search: `using <name>` and `on <node>`
- [x] `docs/api.md`
- [x] Go tests on fakes for every endpoint, including a cluster without the Secret role and one with
      live data off

## Phase T4: Detail pages on the graph

Goal: detail links come from the graph, and ConfigMaps and Secrets show what uses them.

- [ ] Owners, node, storage links, pod volumes, a claim's users, a StatefulSet's claims and a
      StorageClass's counts read from the graph; the old lookups removed
- [ ] "Used by" on ConfigMaps and Secrets
- [ ] A test that every detail page gives the same links as before this phase, on the same objects
- [ ] On multipass and the scenario cluster: the same links as v1.4, and "Used by" matches what
      kubectl shows
- [ ] Browser: light and dark, keyboard, 375 px and wide

## Phase T5: The map in `/kit`

Goal: the map and the list are built and checked on sample graphs before any page uses them.

- [ ] `graph/layout.js` from G4 with its tests, adjusted to this model (edge kinds, missing and
      unknown nodes, dangling marks, grouped pods)
- [ ] The map: cards over an SVG layer of edges, pan and zoom, the panel; no health colours
- [ ] The list view of the same graph
- [ ] Keyboard (one Tab stop, arrows along edges, Enter, Escape), accessible names for cards, and
      reduced motion
- [ ] `/kit` samples: small, missing and unknown, and a generated namespace of 1,000 objects, in
      both themes, at 375 px and wide

## Phase T6: Topology page and palette

Goal: the Topology page, "Show in topology" and graph search work on real clusters.

- [ ] `pages/Topology.jsx`: a namespace, `?focus=<uid>`, the namespace list for "All namespaces",
      map and list, live updates
- [ ] Sidebar entry and `g t`; "Topology", "Show in topology" and graph search in the palette
- [ ] The page says what it couldn't read (`partial`) and when it was built on request
- [ ] Browser: light and dark, keyboard only, a screen reader on the list, 375 px and wide, with and
      without Gateway API, two clusters connected; the 1,000-object namespace stays usable

## Phase T7: Release

Goal: v1.5.0 is ready to tag.

- [ ] Bugs found on the way fixed, one commit each
- [ ] README features and screenshots; `CONTRIBUTING.md` (adding an edge builder, the new kinds)
- [ ] Every check CI runs: `make test`, `go vet`, `gofmt`, the frontend build, `make docker-build`
- [ ] Image tag in `deploy/kubernetes/opscope.yaml` and the manifest URLs set to v1.5.0
- [ ] Status line, design record table, release notes drafted (the tag is pushed by the maintainer)

## Not in this release

- **Health on nodes** and the Issues feed come with the Signal engine in v1.6.
- **Rules, findings and root causes:** the rule engine in v1.9, diagnosis in v1.10.
- **The graph at another time** and graph diffs: the Time machine in v1.8.
- **A whole-cluster map** of apps and namespaces, as G6 drew. Its grid layout is on
  `feature/topology` if it's wanted later.
- **IngressClasses, HPAs, PodDisruptionBudgets, ReferenceGrants and NetworkPolicies** in the
  graph. ReferenceGrants and NetworkPolicies are for the path tracer in v1.9.
- **Inferred edges** (an env var naming `postgres.db.svc`): they would be guesses, and every edge
  here is read from a field.
- **Live detail pages** stay as in v1.4.

## Open questions

1. ~~**The detail page's own object.**~~ Answered: it stays a direct read; only its links come from
   the graph (see Decisions).
2. ~~**Pages for the new kinds.**~~ Answered: none in v1.5; they're in the graph and open its panel
   (see Decisions).
3. ~~**Graph search phrases.**~~ Answered: `using <name>` and `on <node>`, with an optional kind in
   front (see Decisions).
4. ~~**Built on request.**~~ Answered: when a kind the graph needs isn't cached, a request builds
   the part it needs from the `Source` and keeps it for 10 seconds (see Decisions).

## Phase notes

### T0: Plan (2026-11-09)

- Written from the engine release plan (v1.5: Topology) and the G0–G7 record on
  `feature/topology`. Branched from `main` at `cd09b21` (v1.4.0 and the Actions update).
- The reason G0–G7 was removed is the maintainer's (2026-11-09): it wasn't built to a plan, since
  there was no plan yet for the diagnosis engine, the time machine and the rest. The details under
  [Why the first attempt was removed](#why-the-first-attempt-was-removed) are from comparing its
  record with the engine plan.
- No open issues after v1.4.0, so there's no backlog to clear first.
- Reviewed the same day: the four open questions were answered as proposed and moved into
  Decisions (the detail object stays direct, no pages for the new kinds, two search phrases, and
  building on request when a kind isn't cached).

### T1: Graph model and edge builders (2026-11-09)

- `internal/graph` has the model (`model.go`), the builder registry (`refs.go`), the builders in
  three files by area (`build_workloads.go`, `build_network.go`, `build_storage.go`) and the graph
  itself (`graph.go`). Nothing outside the package uses it yet.
- `resources` gained `Lookup(resource)` and `LookupKind(group, kind)` over every kind it knows, a
  `KindName` on `Kind` ("Pod"), and the `graphOnly` table for ReplicaSets, EndpointSlices and
  ServiceAccounts. Those three aren't in `Kinds()` yet, so nothing collects them until T2; a test
  says so.
- Changed from the plan: there's no `Apply(change)`. The graph has `Upsert(object)` and
  `Delete(uid)`, and T2 turns bus events into those. `Build` is `New` followed by an `Upsert` per
  object, so the two can't drift apart.
- Builders are named by kind and part (`pods.spec`, `pods.node`, `deployments.template`,
  `services.selector`, `owners`, ...), not by field as the example in the design (`pods.volumes`);
  the field path is in each piece of evidence anyway. One builder, `owners`, reads every kind.
- Added to the plan:
  - Evidence has a `note`: what the field is, briefly ("env DB_PASSWORD", "volume config",
    "envFrom", "listener https", "port 80", "controller"; "ready" or "not ready" for an
    EndpointSlice's pod). "Used by" in T4 shows it as how a ConfigMap or Secret is used.
  - `references` from a StatefulSet to the claims made from its templates
    (`<template>-<statefulset>-<ordinal>`), found by name since they outlive the pods. T4's "claims
    made from a StatefulSet" needs it. A claim whose name has no ordinal after the prefix isn't
    one, with a test.
  - `binds` from a claim to its StorageClass as well as from its volume, so a pending claim shows
    the class it waits for, or that the class doesn't exist. kubectl's old
    `volume.beta.kubernetes.io/storage-class` annotation wins over the field, as on the storage
    pages.
  - A pod's generic ephemeral volume refers to its claim, `<pod>-<volume>`.
  - An owner of a kind Opscope doesn't know (an Argo Rollout, say) is an unknown node keyed by its
    kind and group: `unknown:Rollout.argoproj.io/<namespace>/<name>`.
- Changed from the plan: no default StorageClass. A claim without a class gets the default one
  written into `storageClassName` by the API server when it's made, so the field already names it;
  a claim that still has none has no class edge.
- A reference with a UID (owner references, an EndpointSlice's pods) only reaches the object with
  that UID. A Deployment deleted and made again under the same name doesn't own the old one's
  ReplicaSet; the ReplicaSet's owner is missing, with a test.
- `status` isn't filled in yet: nothing shows it until the API in T3, which fills it.
- **Selectors are checked one object at a time.** The first version checked every selector in a
  namespace again whenever a pod came, went or changed its labels. Building a namespace of 1,000
  objects took 424 ms that way, since each new pod checked 110 Services against every pod. Now the
  graph keeps what each selector picks, and a pod that changes is checked against each selector on
  its own: the same namespace builds in 5.5 ms, and a pod changing its labels there takes 13 µs
  instead of 3.9 ms.
- Benchmarks (Apple M4 Pro): a namespace of 1,000 objects (110 apps of 3 pods) builds in 5.5 ms;
  a cluster of 5,000 pods (11,050 objects in 50 namespaces on 50 nodes) in 50 ms; one pod's status
  changing in that cluster takes 4 µs. Memory is measured in T2, with the real cache.
- Tests: every edge kind with its evidence, field by field; optional references; missing, unknown
  (a kind the graph isn't given, and one Opscope doesn't know) and dangling; the lessons kept
  (`kube-root-ca.crt`, the `default` ServiceAccount, static pods, a Gateway's data plane); an object
  made after what refers to it, and deleted again; an owner made again; selectors following label
  changes; a kind that stops and starts being readable; deleting everything leaving no node, edge
  or index entry behind; and `TestOrderDoesntMatter`: 40 random orders of the test namespace, with
  earlier versions of half the objects and three objects that come and go, each giving the same
  JSON as building from the final objects at once. Each of these fails when its part is taken out,
  checked once each: not resolving again when a wanted object appears, not checking selectors on a
  new object, a label change or a delete, keeping an edge a selector no longer gives, not skipping
  `kube-root-ca.crt`, and never dropping placeholders. All pass five times in a row with `-race`.

### T2: The graph on the cache (2026-11-10)

- ReplicaSets, EndpointSlices and ServiceAccounts are in `resources.Kinds()` now, so every
  collector keeps them; `typedLists` lists them for the `Direct` source. They have no lister, so no
  page or `/names` entry, with a test. The ClusterRole grants `get`, `list` and `watch` on them,
  and the permissions table says they're for the topology. The live panel names them
  ("ReplicaSets", not "replicasets"). `test/scenarios/permissions.yaml`'s two users can read them
  too, as they can everything else `opscope.yaml` grants.
- Added: `TestClusterRolesCoverEveryKind` reads the manifests and fails when a collected kind lacks
  `get`, `list` or `watch` (Secrets from `secrets-access.yaml`). Taking ReplicaSets out of the
  ClusterRole makes it fail.
- The transform empties a ReplicaSet's pod template: 11.6 MiB per 1,000 made-up ReplicaSets
  without it, 3.3 MiB with it. Owner, selector, replicas, status and the revision annotation stay,
  with a test.
- `collector.Set.Cached(cluster, resource)` hands the engine a kind's whole cache, when that kind
  is cached and has its first list. The graph imports `collector` for this and for the modes; the
  collector still doesn't know the graph exists.
- The engine (`graph/engine.go`) subscribes when it's made, which `main.go` does before the first
  collector starts. One goroutine applies events: adds and updates are `Upsert`, deletes `Delete`.
  Every second, and after each gap, it compares each kind's mode with the graph: a kind newly in
  the cache, or marked stale by a gap, is read in full from the cache (`Graph.Replace`, which adds
  what's there and deletes what isn't); one that can't be read is dropped. A gap for one kind marks
  that kind, an unreachable cluster's gap every kind of that cluster, a lagging subscriber's every
  kind of every cluster. A stopped collector's gap drops the cluster's graph; a collector started
  again for it is read from its own cache when it's next needed. Events that reach the engine
  before the cache they're in has its first list are ignored, and the cache is read once it has.
- `Engine.Read(ctx, cluster, src, namespace, fn)` runs a query on the live graph under a read
  lock, or on a graph built on request.
- **Changed from the plan: a `direct` kind doesn't stop the graph being live.** A default install,
  without `secrets-access.yaml`, reads Secrets `direct`, so by T0's rule it would never have a live
  graph, and would list 20 kinds again for every view after 10 seconds. Now a `direct` kind is left
  out of the live graph like a forbidden one: named in `partial` (with the collector's reason,
  "listing across all namespaces isn't allowed") and its objects Unknown. That's contract 12's
  example: Topology without Secret access still shows which pods reference a Secret, read from the
  pods. The exception is pods: when pods are read per namespace (a user who can read only some
  namespaces), a live graph would hold almost nothing, so requests build the graph on request, in
  the namespace asked about. Live data off, an unreachable cluster, a kind not probed yet and a cache
  on its first list also build on request.
- On request, every kind is listed from the pages' `Source` in parallel, in the namespace asked
  about, and the graph is kept for 10 seconds; one build at a time per cluster and namespace, and
  graphs nobody has asked for in a minute are dropped. Forbidden and not installed kinds are left
  out and named; any other error fails the request ("reading pods: connection refused").
- **Found: a graph built on request would have kept Secret values.** The graph kept each object it
  was given, for its matches, and a `Direct` list of Secrets has their values. Now the graph keeps
  only each object's name and labels, which is all a `Match` sees (`Selects(name, labels)`), with a
  test that no Secret value reaches it. It saved memory too (below).
- **Found on multipass: owners of kinds Opscope doesn't know were split by namespace.** Tigera's
  cluster-wide `APIServer` named `default` owns objects in two namespaces and became two unknown
  nodes. Opscope can't tell from an owner reference whether a kind is namespaced, but the reference
  has the owner's UID, so such an owner is keyed by it now: `unknown:APIServer.operator.tigera.io/default@<uid>`,
  one node.
- **Memory.** The graph's own structures were 4.1 MiB per 1,000 objects at first: each edge had a
  map of evidence by object, and each object a map of the edges it gave, though nearly every edge
  has one or two pieces from one object. Small slices instead brought it to 2.9 MiB, and keeping
  names and labels rather than an object to 2.7 MiB (`BenchmarkGraphMemory`, 11,050 made-up
  objects). Building that cluster takes 46 ms; one pod's status changing, 3.5 µs. The cache's
  benchmark measures the three new kinds: ReplicaSets 3.3, EndpointSlices 2.7 and ServiceAccounts
  1.5 MiB per 1,000.
- `docs/deployment.md`'s "Memory" section has the three kinds and the graph, and its table is
  worked out again: 500 pods need about 17 MiB for the copy and the graph (12 before), 2,000 pods
  70 MiB (50), 5,000 pods 185 MiB (130). Changed from the plan: the manifest stays as it is. The
  256 MiB default still covers 2,000 pods with room, and the request (64 MiB) is above what the
  scenario cluster used.
- Measured on a kind cluster with the scenarios, 40 more apps and 1,500 more ConfigMaps (about
  1,950 objects in the graph): v1.4.0 and this branch side by side for 4 minutes, built as
  binaries, while a loop loaded five lists from each every 5 seconds and scaled a Deployment every
  30. v1.4.0 went from 44 to 51 MiB and stayed there; this branch from 51 to 60 MiB, level for the
  last 3 minutes. 9 MiB more matches the graph's 5.7 MiB with the garbage collector's room on top.
  Shorter than L6's 10 minutes: the benchmarks give the numbers, and this checks they hold in a
  running process that settles.
- `test/scenarios/topology.yaml` (namespace `graph-test`) and `topology-gateway-api.yaml`, with a
  table in the README. Changed from the plan: one Gateway API file rather than a folder, and the
  dangling Service is `ghost` in the topology scenario rather than `no-endpoints` from the
  workloads, so one file covers every edge kind. A CronJob that runs every minute keeps the graph
  changing. `TestLiveCluster` (skipped unless `OPSCOPE_TEST_KUBECONFIG` is set) runs a collector
  and the engine against a real cluster, as Opscope does, and checks that the live graph is the
  graph built from the cluster directly, trying again for up to 45 seconds since polled kinds are
  up to 30 seconds behind.
- Checked on multipass (read-only, through the `multipass` context in the default kubeconfig; the
  one in `data/` points at an older cluster): live in 1.2 s, 296 objects, every edge kind (159
  `owns`, 172 `references`, 28 `selects`, 28 `endpoints`, ...), and the same graph as one built
  from the cluster. Its only missing objects are two ConfigMaps that MetalLB and Tigera refer to as
  optional, which the edges say.
- Checked on a throwaway kind cluster with the scenarios and the Gateway API CRDs copied from
  multipass, deleted afterwards: every edge kind and every gap in the README's table. For two
  minutes, a script scaled `api` to 3, created `stripe-key`, relabelled a pod (so `ghost` selected
  it and stopped dangling), deleted `app-config` and deleted `broken`, while the CronJob made and
  removed Jobs: the live graph still matched one built from the cluster. As Opscope's own service
  account from `opscope.yaml`: live, Secrets left out as "listing across all namespaces isn't
  allowed" and the pods' Secrets Unknown. As `no-watch` (everything polled): live, the same graph
  as one built from the cluster. As `team-a`: built on request in `team-a`, its Deployment,
  ReplicaSet and pod, with nodes and the cluster-wide kinds named as not allowed.
- Tests: the engine on a fake feed with a real bus: reading the cache; following adds, updates
  (a relabel) and deletes on two clusters; reading a kind again after each kind of gap; dropping a
  stopped cluster and reading the new cache; kinds it can't read, and one that becomes readable;
  building on request for each reason, reusing the graph for 10 seconds and not longer; failing
  clearly. Each of these fails when its part is taken out, checked once each: gaps not marking
  kinds stale, `Replace` keeping what's gone, an unreachable cluster counting as complete, a
  stopped collector not dropped. All pass five times in a row with `-race`.

### T3: Query API (2026-11-10)

- `graph/query.go` has the queries, each taking a `Query` with `At` (anything but zero is
  `resources.ErrNoHistory`), `Depth` (2 by default, at most 10) and `Kinds` (edge kinds to follow).
  `Neighbors` is `Subgraph` one step both ways; `Upstream` and `Downstream` are `Subgraph` up and
  down. A subgraph is the nodes a walk reached and every edge between them that it follows, with
  `focus`, the node it started from.
- A walk reaches a Node, StorageClass or GatewayClass but doesn't go on through it, unless it
  starts there; `Path` doesn't go through one either. With the stop taken out, a pod's
  neighbourhood reached every other pod on its node, which the tests check.
- `Path` is breadth first in either direction, in order from `from`; `ErrNoPath` when nothing
  joins the two. `Namespace` is everything in a namespace and what it's joined to outside it one
  edge away (nodes, volumes, classes), so the Topology page gets one namespace's picture.
- The graph keeps an index of edges by node, both ways, so a walk doesn't scan every edge.
  `TestDeletingEverythingLeavesNothing` checks it empties too.
- Nodes have their `status` now, from `resources.StatusOf(resource, object)`: the status each
  list page shows, worked out by the same code (`podStatus`, `nodeStatus`, `jobStatus`, ready
  counts as "2/3", the storage phase, the Gateway API conditions), with a test per kind. It's set
  when an object is added or changes, since the graph keeps no object.
- Search: `ParseSearch` reads `[kind] using <name>` and `[kind] on <node>`; "uses" counts as
  "using", and filler words ("what", "runs", "show", ...) are skipped, so "what runs on node-3"
  and "what uses db-creds" read naturally. The kind can be a resource, its singular, its kind or
  kubectl's short name (`deploy`, `svc`, `pvc`). `<name>` can be `namespace/name`. "using" matches
  every edge to an object of that name, whatever its kind; "on" only `runs-on` edges to a node. The
  answer has the matches, what they matched and the edges between, with `matches` listing the
  first. A phrase it can't read is an error that says what it can.
- Changed from the plan: node IDs go in the query string (`graph/node?id=`,
  `graph/upstream?id=`), not the path (`graph/nodes/{uid}`): a placeholder's ID has slashes
  (`missing:secrets/shop/stripe-key`). The paths are `find`, `node`, `upstream`, `downstream`,
  `path`, `subgraph` and `search`, all under `/api/clusters/{id}/graph/`. Each answer is `built`,
  `partial`, then the subgraph's `focus`, `nodes`, `edges` and, for search, `matches`.
- Every endpoint passes `?namespace=` to the engine, so a graph built on request (pods readable only
  per namespace, or live data off) is built for the namespace the page shows.
- Errors: a node that isn't there and two that nothing joins are `404`; a bad depth, edge kind,
  direction or search phrase `400`, with what's allowed; the cluster's own errors as on every other
  page (`403` for forbidden, `502` for the rest).
- `server.New` takes the engine (`graphReader`, an interface, so tests pass a fake).
- `docs/api.md` has a "Graph" section: the answer's shape, the edge kinds, each endpoint, and the
  three new kinds under "Resource types".
- Tests: the queries on the T1 test namespace (finding objects and placeholders, what a Deployment
  needs, only `owns`, what depends on a Secret, a pod's neighbours, a walk stopping at a node and
  one starting there, a path in order and one only a node joins, a namespace without another
  namespace's pods, every query refusing an earlier time, and searches with and without a kind and
  a namespace). The endpoints on a fake reader: every endpoint's answer, every error, a cluster
  error, and an answer built on request without the Secret role, with Secrets unknown and the
  namespace passed on. Taking out the hub stop in walks or in paths, or the namespace passed to
  the engine, each makes a test fail, checked once each.
- Checked on multipass (read-only), with this branch's server on port 8090: a Service in
  `calico-system` found with its pods, EndpointSlice and the Tigera owner; a pod's upstream (its
  node, Secret and ServiceAccount) and downstream (its ReplicaSet, Deployment, Service,
  EndpointSlice); the namespace (91 nodes, 184 edges); the path from the pod to its Service;
  "what runs on kubeworker1" (15 pods) and "pods using kube-proxy" (3). Every answer took under a
  millisecond, `built` was `live`, and the log had no warnings.
