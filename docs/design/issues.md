# Opscope: Issues feed

Opscope already shows when something is wrong, but in pieces. The overview counts pods that aren't
running and Deployments that aren't ready. The pod list says `CrashLoopBackOff`, and a warning
event says the image couldn't be pulled. Each page works this out for itself, some of it in Go and
some of it in the browser, and none of it is kept: nobody can ask when a pod started failing, or
whether that is one problem or three. This record adds the **Signal engine**. It turns statuses,
conditions, events and a few log lines into a short list of live problems in plain words, called
**signals**. From those signals it works out one **health** per object (healthy, degraded or
failing) and keeps a timestamped history of how that health changes. The new **Issues** page shows
the list. The overview counts, Topology's cards and the detail pages read their health from the same
engine, so the overview and the feed always agree.

The release also gives pages to the three kinds v1.5 added to the cache only for the graph:
ServiceAccounts get a list and a detail page, a Deployment's ReplicaSets show as its revisions, and
a Service shows the endpoints behind it. They come first because the signals point at these
objects, and each signal's evidence needs a page to link to.

Opscope stays read-only, one image and one process. It adds no new permissions: `pods/log` `get` is
already in the role for the Logs tab. Signals and health history live in memory, like today's
metrics history. They become persistent with the State store in v1.7.

This is v1.6.0 in the engine release plan. Phases are numbered **I0–I7** so they don't mix with the
build phases 0–8 or the D, G, L, P, R, S and T phases. All work happens on the `docs/issues` branch,
one commit per phase, as in v1.4 and v1.5. Each phase is reviewed before it is committed, and the
next phase starts only after that.

**Status (2026-11-16):** in progress: I0–I3 done, I4 next.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

### Pages for the graph-only kinds

- **They ship in this release, before the detectors.** v1.5 left ReplicaSets, EndpointSlices and
  ServiceAccounts in the graph only, where selecting one opens the Topology panel. The signals in
  this release name these objects (a Service with no ready endpoints, a rollout stuck between two
  ReplicaSets), and every piece of evidence links to a page. (Decided with the maintainer,
  2026-11-13.)
- **Each kind gets the treatment that suits how it's used**, not the same three pages:
  - **ServiceAccounts: a list page and a detail page**, in the Config group, in `/names` and the
    palette (`sa`, `serviceaccount`). The detail page shows "Used by" (workloads, and pods no
    workload owns, read from the graph's `references` edges), `automountServiceAccountToken`, and
    `imagePullSecrets` and `secrets` linked to their Secrets. RBAC bindings are left out because
    they would need `list` on RoleBindings and ClusterRoleBindings, a separate permissions decision
    (see [Not in this release](#not-in-this-release)).
  - **ReplicaSets: no sidebar entry. A Revisions section on the Deployment page, and a plain detail
    page.** A Deployment keeps up to ten old ReplicaSets, so a list of them all would be mostly
    noise. The Revisions section lists each ReplicaSet the Deployment owns: its revision number,
    its pods (ready and desired), its age, and which one is current. Each row links to the
    ReplicaSet's detail page, which shows its owner, selector, replicas, conditions, events and
    pods. The cache drops the pod template (v1.5), so the page doesn't compare templates: the YAML
    tab is a direct read and has it, and comparing revisions is the Change log's job in v1.7.
    ReplicaSets go in `/names` (`rs`, `replicaset`), because people do search for a hash they saw
    in an event.
  - **EndpointSlices: no list page and not in `/names`. An Endpoints section on the Service page,
    and a small detail page.** The Endpoints section lists every endpoint behind the Service, from
    all of its slices: the pod (linked), its address, whether it is ready, serving or terminating,
    its node and zone, and the ports. That answers "where does this Service send traffic, and is it
    ready?", which is the reason anyone looks at an EndpointSlice. The detail page exists so links
    from events, the graph panel and signal evidence have somewhere to go.
- **The pages are wired the usual way.** The kinds move from `graphOnly` into `kinds` (detail
  views), and ServiceAccounts also into `Listers`. `sections.js` gets one sidebar entry
  (ServiceAccounts). ReplicaSets and EndpointSlices get a `hidden: true` page entry, so
  `detailPath` and the router find them while the sidebar leaves them out. Topology's panel then
  links to their pages, as it does for every other kind.

### Signals

- **A signal is one problem with one object, in a fixed vocabulary.** It has this shape:
  `{id, type, severity, resource, firstSeen, lastSeen, count, evidence[]}`, as in the plan.
  `type` is a word from the [vocabulary](#vocabulary). Rules in v1.9 match on these words, so the
  vocabulary is part of the API: words are added, never renamed. A word that's no longer used stays
  reserved. The first set is the 28 types in the [Vocabulary](#vocabulary) table, more than the
  plan's "top ~15", because each one is a case the pages already show or the graph already finds.
  (Answered in review, 2026-11-13.)
- **The dedup key is `type + UID` (the plan), and the ID is derived from it.** It is a hash of the
  cluster, the UID and the type, like edge IDs (contract 3). A crash loop is one signal whose count
  rises. It is never a new signal for every restart.
- **Signals are raised on the object that shows the symptom, and nothing is inferred from
  relations.** A crash-looping pod raises `crash_loop` on the pod. Its Deployment raises
  `replicas_unavailable` on itself, from its own status, and the two are not merged. Working out
  that one causes the other is diagnosis (v1.10). The Issues page **groups** pod signals of one
  type under one owner into a row with a count, which opens to show each pod. The engine keeps the
  signals apart. (Answered in review, 2026-11-13.)
- **A signal opens when its detector first sees the condition and resolves after the condition has
  been gone for 60 seconds.** A probe that fails one check in three doesn't open and close a signal
  every few seconds. A resolved signal comes back with the same ID and a new `firstSeen` if the
  condition returns. Each one is an **occurrence** in the history. The delay is the same for every
  type. A type that should stay visible longer says so in its own condition: `oom_killed` holds
  while the container's last termination was an OOM kill within the last hour, so a pod that
  recovered still shows it for that hour. (Answered in review, 2026-11-13.)
- **Evidence on every signal (contract 4).** Each piece of evidence records where the signal was
  read from: the field path (`status.containerStatuses[0].state.waiting.reason`), the object's
  resourceVersion, an event (its UID, reason and count), or a log pattern's name and how many
  lines matched. It also records when Opscope saw it. Evidence is never made up from a guess.
- **Severity has three levels.** `critical` means it isn't working (CrashLoopBackOff, a node
  NotReady). `warning` means it's working but at risk or partly broken (probes failing on one of
  three pods, a volume 90% full). `info` is worth knowing and not a problem yet (a certificate
  expiring in 25 days). Each type has a default severity in the vocabulary. A detector may raise
  it (for example, `replicas_unavailable` with zero ready is `critical`), but never lowers it below
  the default.

### Detectors

- **Detectors are a registry (contract 7).** Each detector names the kinds it reads and the signal
  types it can raise, and is a pure function: one object, plus read access to the cache for the
  objects it needs (a pod's events, a Service's EndpointSlices), in; the signals that hold now,
  out. Adding a detector is one more entry, so a rule pack in v1.13 can add entries too. The
  first topology attempt's diagnosis rules (`pod.crash`, `pod.mount`, `service.selector`, …, on
  `feature/topology`) are a checklist of cases to cover. They aren't code to copy: they mixed
  detection with root causes.
- **The overview's "needs attention" moves into detectors.** Today the backend counts Deployments
  with fewer replicas ready than wanted, failed Jobs, unbound claims and Gateways that aren't
  Programmed (`overview.go`). The browser counts pods "not Running, Completed or Succeeded"
  (`Overview.jsx`) and claims nearly full. After this release, every one of those counts is "how
  many objects of this kind have an open signal", served by the backend. The overview and the
  Issues feed can't disagree because they read the same thing.
- **Status text stays where it is.** `podStatus` and `StatusOf` keep producing what the tables
  show ("CrashLoopBackOff", "2/3"). Detectors read the same fields, often through the same
  helpers, but status is for display and a signal is a judgement. A pod that is `Completed` has a
  status and no signal.
- **Events are evidence, and sometimes the only source.** `FailedScheduling`, `FailedMount`,
  `FailedAttachVolume`, `Unhealthy` (probes), `BackOff` and `FailedCreate` give signals that an
  object's status doesn't show, or shows only as `Pending`. Events are already in the cache. A
  detector finds an object's events by the involved object's UID, through an index the engine
  keeps, and doesn't scan every event.
- **Some detectors run on a timer as well as on changes.** A certificate's expiry and a volume's
  usage change without any object changing. These detectors are re-run every 60 seconds over the
  objects they apply to. Every other detector runs only when one of its objects changes.

### Logs

- **Logs are read only for pods that already have a critical signal, and only a few lines.**
  Reading every pod's logs would be heavy on the API server and would cross the line the plan
  draws. So for a pod with `crash_loop` (and the previous run's logs) or `probe_failing`, the
  engine reads the last 200 lines or 64 KiB, whichever is less, of each container that is failing.
  It reads at most once per container per 5 minutes, and at most 5 reads at once per cluster.
  It is **on by default**: it reads only pods that are already failing, within these limits, with
  a permission the role already has. `OPSCOPE_LOG_SIGNALS=false` turns it off, and
  `docs/deployment.md` says what is read and why. (Answered in review, 2026-11-13.)
- **Lines are matched in memory and dropped (contract 11).** The patterns are a short built-in list
  (`connection refused`, `no such file or directory`, `permission denied`, an out-of-memory
  message from a runtime, `address already in use`, TLS handshake and DNS failures, panics and
  uncaught exceptions). A match becomes evidence on the pod's signal: the pattern's name and how
  many lines matched, never the line. Lines that match no pattern are grouped into templates in
  memory (numbers, hex, IPs and quoted strings replaced), counted, and dropped after the read. Only
  the count of distinct unknown templates is kept, so v1.12 can ask about them later without
  breaking the logs rule.
- **A test checks that no log text is kept.** It feeds log streams through the engine and then
  looks through everything the engine holds, its API answers and what it writes to the process
  log, for the lines it read.
- **The patterns are data, not code**: a table of name, expression and signal type, which a rule
  pack can extend in v1.13. They use Go's `regexp` and need no new dependency.

### Health

- **Each object's health comes from its open signals.** `failing` if any signal is critical,
  `degraded` if any is a warning, `healthy` otherwise. An `info` signal leaves the object healthy.
  An object without a detector for its kind is `unknown`, not healthy. Health is never inferred
  from an object's neighbours.
- **Health changes are recorded with timestamps**, as the plan asks: `{uid, from, to, at,
  signals}`. Last known good (v1.7) and the failure time in diagnosis (v1.10) both read from this
  history. Until v1.7 it lives in memory, per cluster, capped at 10,000 transitions with the
  oldest dropped first. It is lost when Opscope restarts, and pages say so ("history since
  10:42").
- **The graph's health slot is filled.** `Node.Health` becomes `{state, since, signals}` from the
  engine, so Topology can colour cards and v1.9's rules can read health from the graph without a
  second lookup.

### The engine

- **One Signal engine per process, one state per cluster, kept up to date from the bus**, like the
  graph. It subscribes to `ObjectChanged` and `CollectorGap`. For a changed object it runs the
  detectors for that kind, and for the objects whose signals depend on it (a pod's events, a
  Service's EndpointSlices), and compares the result with what's open. Then it opens, updates or
  resolves signals, and publishes `SignalRaised`, `SignalResolved` and `HealthChanged` on the bus
  (contract 6). The Change log and diagnosis will subscribe to those later.
- **A gap re-runs what it covers.** A gap for one kind re-runs that kind's detectors over the
  cache, a lagging subscriber re-runs everything, and a stopped collector drops the cluster's
  signals. A signal is never resolved because data is missing: during a gap, a signal stays open
  and is marked `stale` until the detector has seen the object again.
- **Without live data there is no engine**, and the pages say so. With `OPSCOPE_LIVE=false`, a
  request runs the detectors over a `Source` for the namespace asked about and keeps the answer
  for 10 seconds, as the graph does. That keeps the overview's counts working. There is no history
  then, and no first-seen time beyond "now", and the Issues page explains why.
- **Every query takes `at` (contract 2).** Zero means now. Anything else is `ErrNoHistory` until
  v1.8, as for the graph.

### Pages

- **Issues page** in the Cluster group, after Topology (`g i`, palette "Issues"). Open issues come
  first, newest first within each severity. Pod signals of the same type under one owner are
  **grouped**: "crash_loop · 3 pods of Deployment web" opens to the three pods. Filters cover
  severity, type, kind and the namespace picker. A "Resolved" tab shows the last hour's resolved
  signals from memory. Every row links to the object, and its evidence to the field, event or
  page it came from. The page updates live, like lists. It is built in `/kit` first.
- **The overview's counts link to the feed, filtered.** "2 Deployments not ready" opens Issues with
  `kind=deployments`. The recent warnings list stays as it is: events and issues are different
  things, and a warning that raised a signal says so with a link.
- **Detail pages get an Issues section** at the top when the object has open signals. It shows
  each signal in plain words, how long it has been open and its evidence. It also gets a health
  badge next to the status.
- **Topology colours cards by health**, with the same badge and a text label (never colour alone),
  and the panel lists the object's open signals. The list view says health in words.
- **Lists get a health column only where it adds something.** Pods and workloads already show a
  status, so they don't get one. The Issues page is where problems are listed.

### Notifications and trends

- **No notifications in this release.** Sending a webhook or a Slack message is the first thing
  Opscope would ever send outside the cluster on its own, and doing it well (deduplication, quiet
  hours, retries, secrets for the webhook URL) is a feature of its own. It waits until signals have
  run on real clusters for a release and their noise is known: v1.13 at the earliest, with its own
  record. (Answered in review, 2026-11-13.)
- **Prometheus as a source of usage trends is not in this release.** The storage design puts it at
  "v1.6 or later". None of this release's signals needs it: volume usage comes from the kubelet,
  as today. Adding a data source with its own configuration would double the release, so it gets
  its own record.

### Memory

- **Measured, as in v1.4 and v1.5.** Signals are small, but the event index and the health history
  grow with the cluster. I2 measures the engine per 1,000 pods with a tenth of them failing, and the
  "Memory" table in `docs/deployment.md` follows the numbers.

## Design

### Packages

```
backend/internal/signals/       the model, the vocabulary, the engine, health and its history
backend/internal/signals/detect_*.go  detectors, one file per area (pods, workloads, network, storage, nodes)
backend/internal/signals/logs/  reading, pattern matching and template counting; nothing kept
backend/internal/server/        issues.go: the /issues endpoints
frontend/src/pages/             Issues.jsx
frontend/src/components/        HealthBadge, SignalList, Evidence
```

`signals` imports `resources` (for `Source`, `Kinds()` and the status helpers), `bus` and `graph`
(to fill the health slot). The collector doesn't know it exists. `graph` doesn't import `signals`:
the engine sets health on the graph through a small interface that the graph exposes.

### Model

```go
type Signal struct {
	ID        string     `json:"id"`        // hash(cluster, uid, type)
	Type      string     `json:"type"`      // from the vocabulary: "crash_loop"
	Severity  string     `json:"severity"`  // critical, warning, info
	Resource  Ref        `json:"resource"`  // {cluster, uid, resource, kind, namespace, name}
	Owner     *Ref       `json:"owner,omitempty"` // the controlling workload, for grouping only
	Message   string     `json:"message"`   // plain words: "Restarted 14 times; last exit Error (exit 1)"
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	Count     int64      `json:"count"`     // restarts, event count, or times seen; type-specific
	State     string     `json:"state"`     // open, resolving, resolved
	Stale     bool       `json:"stale,omitempty"` // open across a collector gap, not re-checked yet
	Evidence  []Evidence `json:"evidence"`
}

type Evidence struct {
	Source          string    `json:"source"`  // "field", "event", "log", "metric"
	Detector        string    `json:"detector"` // "pods.containers"
	Field           string    `json:"field,omitempty"`  // "status.containerStatuses[0].lastState.terminated.reason"
	Value           string    `json:"value,omitempty"`  // "OOMKilled"; never a Secret value or a log line
	Event           *EventRef `json:"event,omitempty"`  // {uid, reason, count, lastSeen}
	Pattern         string    `json:"pattern,omitempty"` // a log pattern's name, with Matches
	Matches         int       `json:"matches,omitempty"`
	ResourceVersion string    `json:"resourceVersion,omitempty"`
	Seen            time.Time `json:"seen"`
}

type Health struct {
	State   string    `json:"state"`   // healthy, degraded, failing, unknown
	Since   time.Time `json:"since"`
	Signals []string  `json:"signals"` // IDs of the open signals behind it
}

type Transition struct {
	UID     string    `json:"uid"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	At      time.Time `json:"at"`
	Signals []string  `json:"signals"`
}

type Detector struct {
	Name    string   // "pods.containers"
	Kinds   []string // what it reads: "pods"
	Types   []string // what it can raise
	Timed   bool     // also re-run every 60 s
	Detect  func(obj runtime.Object, c Context) []Found
}
```

### Vocabulary

The first set. Each type has a default severity, the
detector that raises it and where it is read from.

| Type                      | Sev.     | Kind            | Read from                                                                    |
| ------------------------- | -------- | --------------- | ---------------------------------------------------------------------------- |
| `crash_loop`              | critical | Pod             | a container waiting in `CrashLoopBackOff`; count = restarts                  |
| `oom_killed`              | critical | Pod             | `lastState.terminated.reason` `OOMKilled` within the last hour               |
| `container_error`         | critical | Pod             | `terminated` with a non-zero exit and no restart policy to retry it          |
| `image_pull_failed`       | critical | Pod             | waiting in `ErrImagePull`, `ImagePullBackOff`, `InvalidImageName`            |
| `config_missing`          | critical | Pod             | waiting in `CreateContainerConfigError` (a Secret, ConfigMap or key missing) |
| `init_failing`            | critical | Pod             | an init container that failed or is crash-looping                            |
| `unschedulable`           | critical | Pod             | condition `PodScheduled=False`, reason `Unschedulable`; `FailedScheduling`    |
| `mount_failed`            | critical | Pod             | events `FailedMount`, `FailedAttachVolume` for 2 minutes or more             |
| `probe_failing`           | warning  | Pod             | `Unhealthy` events for readiness or liveness; running but not ready          |
| `evicted`                 | warning  | Pod             | `status.reason` `Evicted`                                                    |
| `replicas_unavailable`    | warning  | Deployment, StatefulSet, DaemonSet, ReplicaSet | fewer ready than desired for 2 minutes; critical at 0 |
| `rollout_stuck`           | critical | Deployment      | condition `Progressing=False`, reason `ProgressDeadlineExceeded`             |
| `job_failed`              | warning  | Job             | condition `Failed=True`                                                      |
| `create_failed`           | critical | ReplicaSet, StatefulSet, DaemonSet, Job | `FailedCreate` events (quota, admission, a missing ServiceAccount) |
| `selector_matches_nothing` | warning | Service         | the graph's dangling `selects`                                               |
| `no_ready_endpoints`      | critical | Service         | it selects pods, and none of its endpoints is ready                          |
| `backend_missing`         | critical | Ingress, HTTPRoute | a `routes` edge to a missing Service                                      |
| `route_not_accepted`      | critical | HTTPRoute       | a parent's `Accepted=False`, or `ResolvedRefs=False`                         |
| `gateway_not_programmed`  | critical | Gateway         | `Programmed=False`                                                           |
| `gatewayclass_not_accepted` | warning | GatewayClass   | `Accepted=False`                                                             |
| `pvc_unbound`             | critical | PVC             | `Pending` for 2 minutes or more, or `Lost`                                   |
| `volume_released`         | info     | PV              | `Released` or `Failed`; `Failed` is a warning                                |
| `volume_nearly_full`      | warning  | PVC             | the kubelet's usage at 90% or more, only when volume usage is on; critical at 98% |
| `node_not_ready`          | critical | Node            | condition `Ready` not `True`                                                 |
| `node_pressure`           | warning  | Node            | `MemoryPressure`, `DiskPressure` or `PIDPressure` `True`                     |
| `node_unschedulable`      | info     | Node            | `spec.unschedulable` (cordoned)                                              |
| `cert_expiring`           | info     | Secret (TLS)    | `tls.crt` expires within 30 days; warning at 7 days, critical once expired   |
| `reference_missing`       | warning  | Pod, workloads  | the graph's `references` edge to a missing ConfigMap, Secret or claim, when not optional |

Not in the first set: `dependency_unreachable`, which the plan lists. Telling that a pod can't
reach something is only possible from its logs ("connection refused" to a host) or from network
checks Opscope doesn't make. The log patterns add `connection refused` as evidence on the pod's
`crash_loop` or `probe_failing`. The word is reserved, and a later release can raise it once there's
a way to say *what* is unreachable.

`cert_expiring` reads a certificate, which is Secret data. It follows contract 11. The cache's
transform reads the expiry date from a `kubernetes.io/tls` Secret's `tls.crt` before it drops the
values, and keeps only that date (`opscope/cert-not-after`, like the ConfigMap content hash).
Without the optional Secret role there are no Secrets in the cache, so there is no signal, and the
Issues page says so.

### Durations

Several types wait before they open (`replicas_unavailable`, `pvc_unbound`, `mount_failed`: 2
minutes), so a normal rollout or a slow provisioner doesn't raise a signal. The wait starts from the
earliest timestamp the object itself gives (a condition's `lastTransitionTime`, an event's
`firstTimestamp`), not from when Opscope saw it. A pod that has been Pending for an hour when
Opscope starts is a signal at once.

### API

| Endpoint                                              | Returns                                                         |
| ----------------------------------------------------- | --------------------------------------------------------------- |
| `GET /api/clusters/{id}/issues?namespace=&severity=&type=&kind=&state=` | signals, grouped by owner where they share a type |
| `GET /api/clusters/{id}/issues/{signalId}`            | one signal with its full evidence                               |
| `GET /api/clusters/{id}/health?resource=&namespace=&name=` | one object's health, its open signals and its transitions  |
| `GET /api/clusters/{id}/health/counts?namespace=`     | per kind, how many objects have an open signal: the overview's counts |
| `GET /api/clusters/{id}/serviceaccounts`, …           | the new list, and detail views through the existing endpoints   |

Answers say how they were built (`live` or `on_request`), what they couldn't see (`partial`, as for
the graph: no Secret role means no certificate checks; volume usage off means no `volume_nearly_full`),
and since when the history goes back (`historySince`). The stream sends `issues` events, so the
Issues page and the overview update without polling. All of it goes into `docs/api.md`.

## Phase I0: Plan

Goal: this record is reviewed before the work starts.

- [x] Record written on `docs/issues`
- [x] Reviewed, open questions answered and moved into Decisions

## Phase I1: Pages for ServiceAccounts, ReplicaSets and EndpointSlices

Goal: every kind in the cache has a page, and the signals to come have somewhere to link.

- [~] The three kinds move from `graphOnly` to `kinds`; ServiceAccounts also to `Listers`, with a
      row (name, namespace, secrets, age) and a column set in `columns.jsx` (ReplicaSets too; see
      notes)
- [x] ServiceAccount detail: "Used by" from the graph, automount, `imagePullSecrets` and
      `secrets` linked
- [~] Deployment detail: a Revisions section ~~from the graph's `owns` edges and~~ from the cache's
      ReplicaSets and each one's revision annotation; ReplicaSet detail: owner, selector, replicas,
      conditions, events, pods (see notes)
- [x] Service detail: an Endpoints section from its EndpointSlices; EndpointSlice detail
- [x] `sections.js`: a ServiceAccounts entry in Config; hidden page entries for ReplicaSets and
      EndpointSlices; `detailPath` and the Topology panel link to all three
- [x] `/names` and the palette: ServiceAccounts and ReplicaSets, with kubectl's short names
- [x] Go tests on fakes for every new section; `test/scenarios/` gets a rollout with two live
      ReplicaSets and a Service with one ready and one unready endpoint (`rollouts.yaml`)
- [x] Browser: light and dark, keyboard, 375 px and wide

## Phase I2: Signal engine

Goal: signals open, update and resolve from the bus, with health and its history; nothing shows
them yet.

- [x] `internal/signals`: `Signal`, `Evidence`, `Health`, `Transition`, IDs; the vocabulary as a
      table with each type's default severity; the detector registry
- [x] The engine follows the bus: initial adds, changes, the event index by involved UID, the
      60-second resolve, timed detectors, gaps (stale signals, re-runs, a stopped collector)
- [x] Health from signals; transitions recorded and capped; `SignalRaised`, `SignalResolved` and
      `HealthChanged` on the bus; the graph's health slot filled
- [x] Built on request from a `Source` when live data is off, kept for 10 seconds
- [x] Two detectors to prove the shape: `crash_loop` and `replicas_unavailable`
- [x] Tests: the same objects in any order give the same signals; a flapping condition is one
      signal; a gap never resolves a signal; IDs are stable across restarts
- [x] Memory per 1,000 pods with a tenth failing; the "Memory" section follows the numbers

## Phase I3: Detectors

Goal: every type in the vocabulary is raised, and the overview's counts come from the engine.

- [x] The detectors for every type in the table, each with a test that raises it and one that
      doesn't (the plan's v1.9 rule, started here)
- [x] The cache transform keeps a TLS Secret's expiry date and nothing else of its data
- [~] `/health/counts`; the overview reads its "needs attention" counts ~~from it~~ from the same
      engine, and the backend and browser code that counted before is removed (see notes)
- [x] `test/scenarios/`: `issues.yaml` with a case for every type a kind cluster can show (all but
      `node_*` and `volume_nearly_full`, which are tested on fakes), and a table in the README of
      the signal each should raise
- [x] On the scenario cluster: every case raises its signal within 30 seconds and no other; on
      multipass (read-only) the signals match what `kubectl` shows, with no duplicates

## Phase I4: Log signals

Goal: failing pods' logs add evidence, and no log text is kept anywhere.

- [ ] `internal/signals/logs`: the read limits, the pattern table, template counting
- [ ] Reads only for pods with `crash_loop` or `probe_failing`; previous run's logs for crash
      loops; the per-container and per-cluster limits
- [ ] Matches become evidence on the pod's signal; the count of unknown templates is kept
- [ ] The test that no log line is kept, in memory, in an answer or in Opscope's own log
- [ ] Without `pods/log`, the signals stay and say their logs couldn't be read

## Phase I5: Issues page

Goal: the Issues page works on real clusters, live.

- [ ] `/issues` and `/health` endpoints; `issues` events on the stream; `docs/api.md`
- [ ] `/kit`: the issue row, grouping, the evidence list, the health badge, in both themes, at
      375 px and wide
- [ ] `pages/Issues.jsx`: open and resolved, filters, grouping, the namespace picker, live updates
- [ ] Sidebar entry and `g i`; "Issues" and "Issues in <namespace>" in the palette
- [ ] The overview's counts link to the filtered feed; a warning event that raised a signal links
      to it
- [ ] The page says what it couldn't see (`partial`), how it was built and how far back history goes

## Phase I6: Health across Opscope

Goal: detail pages and Topology show health from the same engine.

- [ ] Detail pages: the health badge and an Issues section with evidence
- [ ] Topology: cards and the list view show health in colour and in words; the panel lists open
      signals
- [ ] A test that the overview, the feed, a detail page and Topology give the same health for the
      same object
- [ ] Browser: light and dark, keyboard only, the accessibility tree, 375 px and wide, with and
      without Gateway API and metrics, two clusters connected

## Phase I7: Release

Goal: v1.6.0 is ready to tag.

- [ ] Bugs found on the way fixed, one commit each
- [ ] README features and screenshots; `CONTRIBUTING.md` (adding a detector, adding a signal type)
- [ ] `docs/deployment.md`: what's read from logs and why, memory, the in-memory history
- [ ] Every check CI runs: `make test`, `go vet`, `gofmt`, the frontend build, `make docker-build`
- [ ] Image tag in `deploy/kubernetes/opscope.yaml` and the manifest URLs set to v1.6.0
- [ ] Status line, design record table, release notes drafted (the tag is pushed by the maintainer)

## Not in this release

- **Notifications** (webhook, Slack): v1.13 at the earliest, with their own record.
- **Prometheus usage trends:** their own record, later.
- **RBAC on ServiceAccount pages** (which Roles an account is bound to): it needs `list` on
  RoleBindings and ClusterRoleBindings, which the role doesn't have yet.
- **Persistent signals and health history:** the State store in v1.7. Last known good comes with
  it.
- **Root causes** ("the pods crash because the ConfigMap changed"): diagnosis in v1.10. This release
  says what is wrong, not why.
- **`dependency_unreachable`:** reserved, see [Vocabulary](#vocabulary).
- **Best-practice findings** (no limits, no probes, `:latest`): they aren't live problems, and they
  come with the rule engine in v1.9.

## Open questions

1. ~~**Log-read limits.**~~ Answered: the limits as proposed, on by default (see Decisions).
2. ~~**The first vocabulary.**~~ Answered: the 28 types in the table, with `dependency_unreachable`
   reserved (see Decisions).
3. ~~**Notifications.**~~ Answered: not in core for v1.6 (see Decisions).
4. ~~**Grouping on the Issues page.**~~ Answered: grouped by owner and type for display; the
   engine keeps every signal separate (see Decisions).
5. ~~**The resolve delay.**~~ Answered: 60 seconds for every type (see Decisions).

## Phase notes

### I0: Plan (2026-11-13)

- Written from the engine release plan (v1.6: Issues feed), the storage design, and a decision with
  the maintainer to add pages for the three graph-only kinds to this release. Branched from `main`
  at `539d504` (v1.5.0 and the namespace fix).
- No open issues after v1.5.0, so there's no backlog to clear first.
- Read the code the signals grow out of: `overview.go` (backend counts), `Overview.jsx` (pod and
  volume counts in the browser), `podStatus` and `StatusOf`, `events.go`, the graph's dangling
  selectors and missing nodes, and the first topology attempt's rule list on `feature/topology`
  as a checklist of cases.
- Reviewed the same day: the five open questions were answered as proposed and moved into
  Decisions (the log-read limits, on by default with `OPSCOPE_LOG_SIGNALS=false` to turn it off;
  the 28-type vocabulary; no notifications in core; grouping by owner on the Issues page; a
  60-second resolve delay for every type, with `oom_killed`'s one-hour window in its condition).

### I1: Pages for ServiceAccounts, ReplicaSets and EndpointSlices (2026-11-13)

- `graphOnly` is gone. The three kinds are in `kinds` (detail views), and `Kinds()` now collects
  every kind in `Listers` or `kinds`, so a kind with a detail view and no list (EndpointSlices) is
  still cached. ServiceAccounts and **ReplicaSets** are in `Listers`. Changed from the plan:
  ReplicaSets needed a lister to be in `/names`, which builds the index from `Listers`. Their list
  page exists, with no sidebar entry, and is where a ReplicaSet page's breadcrumb leads.
- **Revisions come from the cache, not the graph's edges.** The revision number is an annotation,
  which the graph doesn't hold, so `deploymentRevisions` lists the cached ReplicaSets whose
  controller is the Deployment (by UID), newest revision first. Each is `current` (its revision is
  the Deployment's), `old, still running` (it still has pods: a rollout under way or stuck) or
  `old`. A ReplicaSet's own page reads its pods from the graph's `owns` edges, and its containers
  from the object itself, which is read from the cluster with its template.
- **A Service's endpoints come from the cache too**, because the graph's `endpoints` edges keep only
  ready or not, and the page shows serving, terminating, node, zone and ports. An unset `ready` or
  `serving` counts as ready, and an unset `terminating` as not, as the EndpointSlice API says.
- **The `default` ServiceAccount's "Used by" is a sentence, not a table.** The graph leaves out
  edges to `default` (a lesson kept from G0–G7), so a table would be empty or misleading. Its page
  says "every pod in <namespace> that names no other account".
- `sections.js` has `hiddenPages`: ReplicaSets (`workloads/replicasets`) and EndpointSlices
  (`network/endpointslices`, `noList`). They're in `allPages`, so routes, `detailPath`, `pageFor`,
  the Topology panel and the palette's object search find them. They aren't in the sidebar or the
  palette's "Go to". For EndpointSlices there's no list route, and the breadcrumb, the namespace
  link and the "EndpointSlices in <namespace>" command are left out.
- The v1.4 links comparison (`TestDetailLinksMatchV14`) lists two intended changes: a pod's
  ReplicaSet owner now links to its page, and Deployment `web` has a Revisions table.
- Checked on a throwaway kind cluster (`opscope-rollouts`) with `rollouts.yaml` and the stuck
  rollout: revision 2 current at 0/1 and revision 1 "old, still running" at 2/2; `split` with one
  Ready and one Not ready endpoint; `shop`'s pull Secret and its Deployment with 3 pods in "Used by";
  47 ServiceAccounts in `/names` (most of them `kube-system`'s). In the browser: the ServiceAccounts
  list, the Deployment's Revisions linking to the ReplicaSet page, the Service's Endpoints and the
  EndpointSlice page (breadcrumb without a link), dark and light, and 375 px (the tables scroll
  inside their box; the page doesn't). The only console errors were `metrics/nodes` 404s, since kind
  has no metrics-server.
- Docs: the resource table and the note under it in `docs/api.md`, the memory note and permission
  reasons in `docs/deployment.md`, the ClusterRole's comments, the README's detail pages line.

### I2: Signal engine (2026-11-13)

- `internal/signals` has the model (`model.go`), the vocabulary (`vocabulary.go`, 28 types and
  `dependency_unreachable` reserved, each with its default severity and wait), the registry
  (`registry.go`), one cluster's state (`world.go`), the engine around it (`engine.go`) and the
  read side (`view.go`). Changed from the plan: the detectors are files in the same package
  (`detect_pods.go`, `detect_workloads.go`), registered from `init` into `signals.Default`. A
  `detect` package would have to import `signals` for its types while the engine needs the
  detectors, which only works with registration from `main`; one package is simpler and keeps
  "adding a detector is one more entry".
- **The world is pure and takes the time from its caller**, so the tests decide what time it is:
  open at once, the 2-minute wait, the 60-second resolve and the one-hour memory of resolved
  signals are all tested without sleeping. The engine wraps it with the bus, the cache and a
  lock, as the graph engine does, and runs `tick` every second.
- **A signal opens with the earliest start the object gives**, so a Deployment unavailable for an
  hour when Opscope starts is a signal at once, with that hour in its first-seen time. A type
  whose start the object doesn't say waits from when Opscope first saw it; an answer built on
  request has no history, so it takes the wait as over.
- **Deleting an object resolves its signals at once**, without the 60 seconds: the condition can't
  come back on that UID. **A kind that can no longer be read keeps its signals, marked stale**,
  as a gap does: a signal never resolves because data is missing.
- **Owners are worked out when signals are read**, not stored, so a pod's owner (its ReplicaSet's
  Deployment) never depends on which arrived first. The order test builds the same objects in 20
  random orders and compares every field.
- **Health for the graph**, without locks between the two engines: the Signal engine shares, per
  cluster, a `sync.Map` of the objects whose health changed and whether its world is complete. The
  graph engine gets a lookup (`SetHealth`) that it calls while reading nodes, with the node's kind:
  an object of a kind a detector reads, with no entry, in a complete world, is healthy; anything
  else is `null`. Healthy objects aren't stored one by one. `Since` is left out of the JSON when
  the health hasn't changed since Opscope started.
- `bus` has `SignalRaised`, `SignalResolved` and `HealthChanged`; `collector.Set` has `Publish`.
  The engine publishes after letting go of its lock. Nothing subscribes to them yet.
- The severity "info" is the constant `Notice` in Go, since `Info` is the answer's type; the JSON
  says `info`, as the record does.
- `View.Signals` puts open signals before resolved ones, then the worst, then the newest, then by
  ID. `View.Counts` (per kind, how many objects have an open signal) is ready for the overview in
  I3.
- **Memory:** about 786 KiB per 1,000 pods on top of the cache (`BenchmarkEngineMemory`: 10,000
  pods in Deployments of five, a tenth crash-looping with two events each, with their Deployments
  and ReplicaSets), most of it the per-object maps. The "Memory" section says "under 1 MiB per
  1,000 pods".
- Checked on the `opscope-rollouts` kind cluster with a crash-looping pod added (`kubectl run
  crash`): through the graph API, `crash` is failing with its signal, `split` (1 of 2 ready) turned
  degraded 2 minutes after the engine started, and the rest are healthy. The pod in
  ImagePullBackOff is healthy until I3 has `image_pull_failed`. After a restart, `crash`'s signal
  had the same ID. All Go tests pass with `-race`.

### I3: Detectors (2026-11-16)

- Every type in the vocabulary is raised, by 13 detectors in five files: `detect_pods.go`
  (containers, OOM kills, status, events), `detect_workloads.go` (replicas, conditions, events),
  `detect_network.go` (Services' endpoints, routes' backends, Gateway API conditions),
  `detect_infra.go` (claims, volumes, nodes) and `detect_config.go` (certificates, missing
  references). `TestEveryTypeIsDetected` fails if a type has no detector. `detectors_test.go` has,
  for every type, objects that raise it and objects that look close and don't.
- **Missing references and backends use the graph's own references** (`graph.RefsOf`, a new
  export), so a ConfigMap the Topology page shows as missing is the one `reference_missing` names.
  A reference is only judged when its kind can be read: without the Secret role, a Secret a pod
  names may well exist, and isn't reported. Ephemeral volumes' claims and a StatefulSet's claim
  templates are left out, since they're made after the pod. A Deployment's pods don't repeat
  their Deployment's `reference_missing`; a pod no workload owns has its own.
- **Events count while they're recent**: within 10 minutes. Kubernetes keeps events for an hour,
  long after most problems are over. The detectors that read events (probes, mounts, the
  scheduler, `FailedCreate`) are timed, so their signals resolve as the events grow old, with
  nothing else changing. `oom_killed` is timed too, and lasts an hour after the kill.
- Choices made while writing them, each with a test:
  - `container_error` leaves a Job's pods to the Job (`job_failed`): a Job retries, and keeps the
    pods that failed before it succeeded.
  - `replicas_unavailable` leaves a Deployment's ReplicaSets to the Deployment: they come and go
    in every rollout.
  - `pvc_unbound` leaves alone a claim whose class binds on first use (`WaitForFirstConsumer`)
    while no pod uses it; a pod that does makes the wait a problem. Its 2-minute wait counts from
    when the claim was made.
  - `unschedulable` comes from the pod's condition, with the scheduler's recent
    `FailedScheduling` events as more evidence. An event alone counts only while the pod has no
    node.
  - `probe_failing` needs recent `Unhealthy` probe events and a running container that isn't
    ready. `mount_failed` needs recent mount events on a pod that's still pending.
  - `no_ready_endpoints` counts an endpoint with no `ready` as ready, as the API says, and says
    nothing until the endpoints controller has made a slice.
- **Detectors can name kinds they only look up** (`Detector.Reads`): the engine holds only the
  kinds detectors read, and `pvc_unbound` needs StorageClasses. A Trigger's `Targets` now gets the
  Context, so a changed pod re-checks only the Services whose selector matches it, before or after
  the change, rather than every Service in its namespace.
- **The cache keeps a TLS Secret's expiry date** as the annotation `opscope/cert-not-after`, read
  from `tls.crt` before the values are dropped (`collector.CertNotAfter`). An answer built on
  request reads it from the Secret's data the same way and keeps nothing.
- **Volume usage for `volume_nearly_full`**: with `OPSCOPE_VOLUME_USAGE` on, the engine reads the
  kubelets' numbers once a minute (`SetVolumeUsage`, with the same `metrics.Volumes` code the pages
  use), away from its own loop. A cluster whose usage can't be read keeps its claims' signals as
  they are.
- **The overview's counts come from the engine.** `GetOverview` takes a `resources.Attention`
  (an interface, since `signals` imports `resources`). The server reads the engine for the same
  namespace and passes it in, so the overview and the Issues feed see the same moment. Changed
  from the plan: the overview doesn't call `/health/counts`. The endpoint exists, and both read
  the same engine. Each kind's count is made of the types its row names ("not ready" is
  `replicas_unavailable`, plus `rollout_stuck` for Deployments), and `pods.unhealthy` is pods with
  any open signal. The overview also gets `signals`, objects per type, and its "Measured claims"
  row counts `volume_nearly_full`. Removed: `countWorkloads` and the status checks behind it in
  `overview.go`, and `unhealthyPods` and the nearly-full count in `Overview.jsx`.
- `historySince` is left out of answers built on request.
- **Checked on the kind cluster** (`opscope-rollouts`) with `issues.yaml` and a TLS Secret
  expiring in 20 days, with the new `TestLiveCluster` (`OPSCOPE_TEST_EXPECT=issues`). Every signal
  in the README's table was there within 25 seconds of the engine starting, and nothing else.
  `fine` had none, the Job's failed pod had none, and `no-config`'s missing ConfigMap was one
  signal on the Deployment. The first run, a minute after applying, was missing the four types
  that wait 2 minutes, as it should. `oom`'s `crash_loop` comes and goes between kills, so the
  test treats it as optional. Two messages were tidied on the way: the scheduler's, cut to 200
  characters, and an exec probe's "failed: " with nothing after it. `/health/counts` and the
  overview of `issues-test` agree: 11 of 13 pods unhealthy, 3 Deployments not ready, 1 Job failed,
  1 claim not bound.
- **On multipass (read-only), a bug found and fixed.** The first run raised 11 `reference_missing`
  signals for Secrets that exist (calico's `typha-certs`, `ecommerce`'s `order-db-credentials`, …)
  and a `backend_missing` for HTTPRoute `shop`'s Service `frontend`, which exists too. All were
  already resolving. Reading a kind in full (`replace`) added its objects one at a time, and checked
  what each bore on as it arrived: a Deployment naming two Secrets was checked when only the first
  was in place. `replace` now puts every object in place, then checks everything they bear on
  once, in a fixed order (`store`, `forget`, `bearsOn`, `check`).
  `TestReplaceChecksOnceEverythingIsThere` covers it.
- After the fix, multipass gives six signals, all in `ecommerce`, matching `kubectl`: `crash_loop`
  on `order-service`'s three pods (CrashLoopBackOff, 641–652 restarts), `replicas_unavailable`
  (0/2) and `rollout_stuck` on its Deployment, and `no_ready_endpoints` on its Service. Nothing
  else: the calico pods' `Unhealthy` probe events are from the VMs' reboot, and those pods are
  ready again; no claim is unbound; every node is Ready and schedulable. One signal per object and
  type, no duplicates.
