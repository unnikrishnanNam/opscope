# Opscope: Live data

Every page in Opscope today asks the cluster for its data when it loads, and again every 10
seconds. Two pages open on the same cluster make two sets of calls, and each page can show a
slightly different moment. This record adds one **collector** per connected cluster, inside the same
server, which lists and then watches the kinds Opscope shows and keeps them in a **watch cache**.
Pages read from the cache, and the browser is told when something changed instead of asking on a
timer. An in-process **bus** carries every change, so later engines (Topology, the Issues feed, the
Change log) can subscribe without touching the collectors.

Users get faster pages that update as the cluster changes. Nothing else about Opscope changes: it
stays read-only, one image and one process, and every page shows the same data as in v1.3.0. The
only new permission is `watch`, a read verb; without it, Opscope polls and says so.

This is v1.4.0 in the engine release plan. Phases are numbered **L0–L7** so they don't mix with the
build phases 0–8 or the D, G, P, R and S phases. All work happens on the `docs/live-data` branch,
one commit per phase; each phase is reviewed before it is committed, and the next phase starts only
after that.

**Status (2026-11-02):** planned.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

- **One collector per connected cluster, started and stopped by `clusters.Manager`.** A cluster
  loaded from the environment or from `data/` starts its collector at startup; one added in the UI
  starts it when it's added; removing a cluster stops it and drops its cache. Collectors start
  eagerly, not when a page first opens, because the Change log (v1.7) must record changes while
  nobody is looking.
- **client-go informers, no new dependency.** Typed shared informers for the built-in kinds, the
  dynamic informer for Gateway API. Both are in `k8s.io/client-go`, which Opscope already uses.
  Informers use clients without the 10-second request timeout (a timeout covers the whole response
  and would cut every watch), like the log-stream client does today. No periodic resync: it only
  replays objects that didn't change.
- **The cached objects are trimmed before they're stored,** with an informer transform:
  - `managedFields` keeps only each entry's manager, operation and time, without `fieldsV1` (the
    bulk of it). The Change log needs the manager names for actor attribution in v1.7.
  - **Secrets keep their type and key names, never their values.** Secrets are watched like the
    other kinds, so their values do reach Opscope's memory on every change, as they do today
    whenever the Secrets page loads. The transform drops them, with kubectl's
    `last-applied-configuration` annotation (which holds them too), before the object reaches the
    cache or the bus, so "Secret values are never cached" stays true. Revealing a value stays a
    direct, one-key call. `docs/deployment.md` says this plainly. Keeping Secrets in `direct` mode
    was considered and rejected: no live Secrets list, and no Secret metadata for the Change log in
    v1.7. (Answered in review, 2026-11-05.)
  - ConfigMaps keep their key names and a content hash, not their values: the list only shows key
    names, the detail page reads the object directly, and the Change log (v1.7) tracks ConfigMap
    content by hash.
- **Pages read from the cache; three things stay direct.** Lists, the overview, the `/names` index,
  the related lists on detail pages (events for an object, pods using a claim, claims made from a
  StatefulSet, volumes, StorageClass lookups) and the node and pod lists that usage reads all come
  from the cache. The detail page's object itself, its YAML view, logs and revealing a Secret value
  stay direct, on-request calls, as the engine plan's contract 5 says. Metrics from metrics-server
  and volume usage from the kubelets stay polled: neither has a watch.
- **The row code doesn't change, only where the objects come from.** Each list function reads its
  objects from a `Source` instead of a client. The watch cache is one `Source`; a direct `Source`
  that lists from the cluster, as v1.3 does, is the other. Both run through the same flattening
  code, so a test can check that they give the same rows.
- **The mode is chosen per kind and per cluster, and the collector says which:**

  | Mode            | When                                                | What pages get                  |
  | --------------- | --------------------------------------------------- | ------------------------------- |
  | `watch`         | `list` and `watch` allowed                          | Live data from the cache        |
  | `poll`          | `list` allowed, `watch` forbidden                   | The cache, relisted every 30 s  |
  | `direct`        | listing across all namespaces is forbidden          | v1.3 behaviour: a list per request, in the selected namespace |
  | `not_installed` | the kind isn't in the cluster (Gateway API)         | The existing calm note          |
  | `forbidden`     | `list` forbidden altogether (often Secrets)         | The existing permission message |

  The collector finds the mode with a `list` (limit 1) and a short `watch` before it starts an
  informer, and checks again every 5 minutes for kinds that weren't available, so installing Gateway
  API or granting a permission is picked up without a restart. `direct` keeps Opscope working for a
  kubeconfig user who may read only some namespaces.
- **A kill switch: `OPSCOPE_LIVE=false`** turns collectors off and every kind runs in `direct`
  mode, exactly as v1.3.0. It is the fallback the engine plan asks for when loading changes, and it
  is how L3 compares the two paths on a real cluster (see Open questions, 2).
- **Before a kind's first sync, a request waits up to 5 seconds,** then reads that kind directly for
  that one request. A slow first sync never shows an empty page.
- **The UID is in every row (contract 1).** `Meta` gains `uid`, so every list row, the `/names`
  index and the frontend's row keys have it. History in later releases is keyed by UID, because
  names come back when an object is recreated and UIDs don't.
- **Engine queries take a time (contract 2).** `Query` gains `At time.Time`; zero means now, and
  anything else is refused with `ErrNoHistory` until the Time machine (v1.8). The HTTP API doesn't
  take `?at=` yet.
- **The bus is plain Go:** typed events, one buffered channel per subscriber, no broker. A
  publisher never blocks; a subscriber that falls behind loses events and is sent a `Gap` that says
  so, rather than slowing the collector. v1.4 defines `ObjectChanged` and `CollectorGap`; the other
  event types in the engine plan come with their engines.
- **Reconnects are recorded, never hidden.** When a watch breaks and the informer has to relist,
  the changes in between are collapsed into one. The collector publishes a `CollectorGap` with the
  time range, so the Change log can show a gap marker instead of implying a complete history.
- **The browser is told what changed, then asks for it.** One server-sent events stream per
  cluster (standard library) sends `changed` messages naming a resource and namespace, at most once
  a second per resource. The page fetches the list again from the cache. This keeps one way of
  building rows, sorting and filtering, and the stream stays small whatever the cluster's size.
  Pushing row diffs was considered and rejected: it would need a second, incremental copy of every
  list's logic in the frontend.
- **One stream per browser tab, not per page.** Browsers allow only six connections per host over
  HTTP/1.1, which a port-forward uses. Every component shares the tab's stream, and the stream
  closes while the tab is hidden.
- **Polling stays as the fallback in the browser.** When the stream is down, or a kind is in `poll`
  or `direct` mode, `useApi` keeps its timer as today. A reconnect fetches everything on screen
  again, so a message missed while disconnected never leaves a page out of date.
- **The page says how fresh it is.** A small indicator in the top bar reads "Live", "Every 30 s"
  or "Reconnecting", and explains on hover which kinds aren't live and why (for example, "watch
  isn't allowed for pods"). It's built in `/kit` first.
- **`watch` joins `get` and `list` in the ClusterRole.** It is a read verb, but it changes what
  Opscope asks for, so the permissions table in `docs/deployment.md`, the principles in
  `CONTRIBUTING.md` and the README say so in the same phase. `secrets-access.yaml` gains `watch`
  too; without it Secrets run in `poll` mode.
- **Memory is measured, and the manifest follows the numbers.** The cache holds every object of the
  listed kinds, so L6 measures memory per 1,000 objects and on the test clusters, and updates the
  manifest's requests and limits (256Mi today) and a sizing table in `docs/deployment.md`.

## Design

### Packages

```
backend/internal/bus/         typed events and subscriptions; standard library only
backend/internal/collector/   one Collector per cluster: probes, informers, transforms, modes, status
backend/internal/resources/   unchanged rows; list functions read from a Source
```

`clusters.Manager` gets two hooks, called when a cluster is added and when it's removed, so the
collectors package can follow it without the clusters package importing it.

### One kind table

Today the kinds live in three places: `Listers` (lists), `kinds` in `detail.go` (detail pages) and
the ClusterRole. The collector needs a fourth list: what to watch. Rather than add it, the
collector reads the same table as the listers, so adding a resource type is still one entry in Go
(plus the ClusterRole and the docs, as `CONTRIBUTING.md` says). Each entry already has, or gains:
the URL name, the GroupVersionResource, whether it's namespaced, and an optional transform.

### Source

```go
// Source is where list functions read objects from: the watch cache, or
// the cluster directly when the cache can't serve that kind (see modes).
type Source interface {
	// List returns the objects of one kind: typed objects for built-in
	// kinds (*corev1.Pod, ...), *unstructured.Unstructured for Gateway API.
	// The objects are shared and must not be changed.
	List(ctx context.Context, resource string, q Query) ([]runtime.Object, error)
}

type Query struct {
	Namespace string    // "" means all namespaces
	Type      string    // events only: "Warning" or "Normal"; "" means both
	At        time.Time // zero means now; anything else is ErrNoHistory until v1.8
}
```

A list function changes only at its top:

```go
func listPods(ctx context.Context, src Source, q Query) ([]Pod, error) {
	pods, err := listOf[*corev1.Pod](ctx, src, "pods", q)
	// ... the same flattening as today
}
```

### Bus events

```go
// ObjectChanged is published for every add, update and delete the collector sees.
type ObjectChanged struct {
	Cluster         string
	Resource        string // the URL name, e.g. "pods"
	Type            ChangeType // Added, Updated or Deleted
	UID             string
	Namespace, Name string
	ResourceVersion string
	Old, New        runtime.Object // trimmed as in the cache; nil for Added / Deleted
	Initial         bool           // from the collector's first list: where it started, not a change
	Seen            time.Time      // when Opscope saw it, not when it happened
}

// CollectorGap says that changes to Resource between From and To may have
// been collapsed or missed: a watch broke and was relisted, the cluster was
// unreachable, or the collector was stopped. Resource is "" for all kinds, and
// Cluster is "" for all clusters (a lagging subscriber can have missed anything).
type CollectorGap struct {
	Cluster, Resource string
	From, To          time.Time
	Reason            string // "relist", "unreachable", "stopped", "subscriber_lagged"
}
```

An update whose resourceVersion didn't change isn't published. A subscriber that fell behind gets
its gap together with the next event that fits, never alone, so every queue holds at least two.

### API

| Endpoint                                  | What it returns                                                     |
| ----------------------------------------- | ------------------------------------------------------------------- |
| `GET /api/clusters/{id}/{resource}`       | Unchanged, plus `uid` on every row                                  |
| `GET /api/clusters/{id}/names`            | Unchanged, plus `uid` on every object                               |
| `GET /api/clusters/{id}/live`             | The collector's state, per kind (below)                             |
| `GET /api/clusters/{id}/live/stream`      | Server-sent events: `changed`, `status`, and a heartbeat every 15 s |

```jsonc
// GET /api/clusters/{id}/live
{
  "enabled": true,           // false when OPSCOPE_LIVE=false
  "state": "live",           // live, partial (some kinds not live), connecting, unreachable, off
  "kinds": [
    { "resource": "pods", "mode": "watch", "synced": true, "objects": 412, "lastChange": "2026-11-02T10:04:11Z" },
    { "resource": "secrets", "mode": "forbidden" },
    { "resource": "gateways", "mode": "not_installed" }
  ]
}
```

```
event: changed
data: {"resource":"pods","namespaces":["default","shop"]}

event: status
data: {"state":"partial"}
```

The stream sets `Cache-Control: no-cache` and `X-Accel-Buffering: no`, so an nginx Ingress in
front of Opscope doesn't hold messages back.

### Frontend

- `src/live.js`: one `EventSource` per cluster for the tab, shared through React context; closed
  while the tab is hidden, reopened (with a full refetch) when it's shown again.
- `useApi(path, { live: "pods", refreshMs })`: refetches when a `changed` message matches the
  resource (and the namespace, when the path has one), at most once every 2 seconds; falls back to
  `refreshMs` when the stream is down or the kind isn't in `watch` mode.
- Lists and the overview use `live`. Detail pages keep polling in v1.4, because their object is
  read directly; their related lists are cheap now that they come from the cache.
- `LiveStatus` in the top bar, built in `/kit` first: "Live", "Every 30 s", "Reconnecting", with
  the reasons on hover. No animation, so reduced motion needs no special case.

## Phase L0: Plan

Goal: this record is reviewed and merged before the work starts.

- [x] Record written on `docs/live-data` and reviewed
- [ ] Open questions answered and moved into Decisions
- [~] ~~Merged into `main`~~ Kept on this branch with the rest of v1.4.0 (see notes)

## Phase L1: Object UIDs

Goal: every row and every indexed name carries its UID.

- [x] `uid` on `Meta`, filled by `metaOf` and the Gateway API rows
- [x] `uid` on `ObjectName` in the `/names` index
- [~] Frontend: table rows ~~and palette results~~ keyed by UID where they're keyed by name today;
      event lists too, palette results stay keyed by name (see notes)
- [x] Go tests check the UID for every kind; `docs/api.md` mentions it
- [x] No visible change in the UI, checked in the browser

## Phase L2: Bus and collectors

Goal: a collector runs for every connected cluster and keeps a synced
cache, alongside the existing code; no page reads from it yet.

- [x] `internal/bus`: `ObjectChanged`, `CollectorGap`, subscribe and unsubscribe, a non-blocking
      publish, `Gap` for a subscriber that fell behind
- [x] `internal/collector`: informers for every kind in the kind table, on clients without the
      request timeout, no periodic resync
- [x] Transforms: trimmed `managedFields`, Secrets without values, ConfigMaps with key names and a
      content hash; a test that fails if a Secret value reaches the cache or the bus
- [x] Collectors start and stop with `clusters.Manager` (startup, add, remove) and stop on shutdown
- [x] Every add, update and delete published on the bus; updates with an unchanged
      resourceVersion skipped
- [x] `GET /api/clusters/{id}/live` with the state per kind
- [x] `OPSCOPE_LIVE=false` turns collectors off
- [x] Go tests with client-go fakes: sync, events, stop frees the cache, two clusters at once

## Phase L3: Listers on the cache

Goal: every page reads from the cache and shows the same data as v1.3.0.

- [ ] `Source` and `Query.At`; list functions read from a `Source`; `ErrNoHistory` for a non-zero
      `At`
- [ ] The cache `Source` and the direct `Source`, with the 5-second wait for a kind's first sync
- [ ] The overview, the `/names` index, detail pages' related lists and usage's node and pod lists
      read from the cache; the detail object, YAML, logs and Secret values stay direct
- [ ] A Go test that runs every list through both sources and compares the rows
- [ ] On the test clusters, every endpoint gives the same answer with `OPSCOPE_LIVE=false` and with
      collectors on (two servers, ports 8090 and 8091), with two clusters connected
- [ ] No change in the browser: light and dark, keyboard, 375 px and wide

## Phase L4: Permissions and fallbacks

Goal: without `watch`, or with only some namespaces readable, Opscope still works and says how.

- [ ] Probe per kind (`list` with limit 1, then a short `watch`) and the modes `watch`, `poll`,
      `direct`, `not_installed` and `forbidden`; probing again every 5 minutes
- [ ] `poll` relists every 30 seconds into the same cache and publishes the differences
- [ ] `CollectorGap` on relist, while a cluster is unreachable, and when a collector stops
- [ ] An unreachable cluster backs off and reconnects without filling the log
- [ ] `watch` in `deploy/kubernetes/opscope.yaml` and `secrets-access.yaml`
- [ ] `docs/deployment.md` (permissions table, "it polls rather than watching" replaced, the
      modes), `CONTRIBUTING.md` (principles, adding a resource type) and the README
- [ ] Go tests with fakes for every mode, and for a mode changing when a permission is granted
- [ ] Checked on a kind cluster with a role that allows `list` but not `watch`, and with a
      namespace-only role

## Phase L5: Live pages

Goal: lists and the overview update within a couple of seconds of a
change, without a timer.

- [ ] `GET /api/clusters/{id}/live/stream`: `changed` at most once a second per resource, `status`,
      a heartbeat every 15 s; closes cleanly when the client goes or the server stops
- [ ] `src/live.js`: one stream per cluster per tab, paused while the tab is hidden, a full refetch
      on reconnect
- [ ] `useApi` `live` option with the 2-second limit and the polling fallback; lists and the overview
      use it
- [ ] `LiveStatus` in `/kit` (every state, both themes), then in the top bar
- [ ] Works through `kubectl port-forward` and the Vite dev proxy
- [ ] Frontend tests for matching messages to paths and for the fallback
- [ ] `docs/api.md` for both `live` endpoints
- [ ] Browser checks: light and dark, keyboard, screen reader label, 375 px and wide

## Phase L6: Scenarios and sizing

Goal: checked against failure cases, with published memory numbers.

- [ ] `test/scenarios/workloads.yaml`: a CrashLoopBackOff pod, an OOMKilled pod, an
      ImagePullBackOff pod, and a Service whose selector matches nothing, with a table in the README
      of what each page should show
- [ ] On a kind cluster: each scenario appears and changes live (restart counts rise without a
      reload)
- [ ] A Go benchmark that fills the cache with realistic objects and reports memory per 1,000
      objects per kind
- [ ] Memory measured on the scenario cluster and on multipass (read-only), with one and with two
      clusters connected; the run's length agreed before it starts
- [ ] The manifest's requests and limits updated, and a sizing table in `docs/deployment.md`

## Phase L7: Release

Goal: v1.4.0 is ready to tag.

- [ ] Bugs found on the way fixed, one commit each
- [ ] README feature list, screenshots that show the indicator
- [ ] Every check CI runs: `make test`, `go vet`, `gofmt`, the frontend build, `make docker-build`
- [ ] Full browser pass: light and dark, keyboard only, 375 px and wide, with and without
      metrics-server and Gateway API, two clusters connected
- [ ] Image tag in `deploy/kubernetes/opscope.yaml` and the manifest URLs in the README and
      `docs/deployment.md` set to v1.4.0
- [ ] Status line, design record table, release notes drafted (the tag is pushed by the maintainer)

## Not in this release

- **Topology (v1.5)** builds on the cache and the bus. Before it starts, its record writes down why
  the G0–G7 attempt was removed.
- **Opscope's own `/metrics` for Prometheus** (collector lag, cache size): planned with the
  storage design's cloud-native work (see Open questions, 3).
- **Watching only some namespaces** (`OPSCOPE_NAMESPACES`): added only if L6's numbers show the
  need. Until then, a user who can't list across namespaces gets `direct` mode.
- **Live detail pages:** detail pages keep polling; a later release can listen for their own object.

## Open questions

1. ~~**Secrets in the cache.**~~ Answered: watched, values dropped in the transform (see
   Decisions).
2. **Kill switch.** Proposed: keep `OPSCOPE_LIVE=false`, documented as a fallback, for at least
   v1.4 and v1.5. Or leave it undocumented, for testing only?
3. **`/metrics` endpoint.** Proposed: not in v1.4; it comes with the Helm chart and multiple
   replicas (v1.8). Bring it forward if you'd rather measure the collectors with it in L6.
4. **Events in the cache.** Events change constantly on busy clusters and are the largest kind by
   count. Proposed: cache them like the rest (the overview's warnings then update live), and move
   them back to `direct` if L6 shows they cost too much memory.

## Phase notes

### L0: Plan (2026-11-02)

- Written from the engine release plan (v1.4: Live data) and the storage design. The storage
  design's work starts in v1.7, so nothing in v1.4 needs a store; the cache lives in memory.
- Branched from `main` at `f6b4174` (v1.3.0). There were no open issues after v1.3.0, so there is
  no backlog to clear first; the only open pull request is a Dependabot update of the CI actions.
- The plan was approved as written. The four open questions are still to be answered; L1 doesn't
  depend on any of them, L2 needs 1, 2 and 4, and L6 needs 3.
- Changed after review (2026-11-03): the plan first split v1.4.0 over six branches, one per
  feature. All of it now stays on `docs/live-data`, one commit per phase, and reaches `main` as a
  whole.

### L1: Object UIDs (2026-11-03)

- `Meta` has `uid` first, filled by `metaOf`. The Gateway API rows decode their metadata into the
  same `metav1.ObjectMeta`, so they needed no change of their own.
- The detail view already had its own `uid` field next to the embedded `Meta`. It's folded into
  `Meta` now, so the detail JSON has `uid` once, as before.
- Changed from the plan: palette results stay keyed by name. Their ids are what the palette
  remembers as recent, and a recent entry should still find "web" after it's deleted and made
  again; the detail URL is by name too. Table rows and event lists use the UID as their React key,
  and fall back to namespace and name for rows without one (the `/kit` samples).
- Tests: `TestEveryRowHasItsUID` gives one object of every listed kind a UID named after its
  resource and checks that each lister returns it; the `/names` test and the Secret detail test
  check UIDs too. Both fail when `metaOf` leaves the UID out, checked by removing it once.
- Checked against multipass (read-only): all 178 objects in `/names` and every row of the lists
  tried have a `uid`, and a pod's UID matches `kubectl get -o jsonpath='{.metadata.uid}'` in its row
  and its detail view. In the browser, the overview, pods list, a pod's detail page and palette
  search look as before in dark and light at 375 px, with no React key warnings in the console.

### L2: Bus and collectors (2026-11-05)

- Open question 1 answered in review: Secrets are watched, values dropped in the transform. Moved
  into Decisions. Questions 2 and 4 are still open; L2 is built the way they propose
  (`OPSCOPE_LIVE=false` exists but isn't documented yet, and Events are cached), and either is a
  small change if the answer differs.
- `internal/bus` is standard library only. Found while testing: a lagging subscriber was first sent
  its gap alone when one slot was free, which filled that slot and lost the next event, so a reader
  keeping pace one event at a time never caught up. The gap now goes only together with the next
  event, and every queue holds at least two.
- `internal/collector` runs one informer per kind rather than one shared factory, so L4 can stop
  and restart a kind on its own. The typed informers still come from client-go's factory
  (`ForResource`), one factory per kind; Gateway API uses the dynamic informer. The kind list is
  `resources.Kinds()`, built from `Listers` and the detail `kinds` (Events, which have no detail
  page, are in a small `listOnly` table), with a test that every lister has an entry.
- Added to the bus event: `Initial`, for the objects of a collector's first list. They're published
  as adds, but they're the state it started from, not changes; the Change log will skip them.
- Transforms: `managedFields` keeps manager, operation, API version, time and subresource. Secrets
  keep type and key names; values, `stringData` and the `last-applied-configuration` annotation
  go. ConfigMaps keep key names; values go, and the cached copy gets an `opscope/content-hash`
  annotation (SHA-256 over every key and value, length-prefixed so no two contents run together).
  The transform is idempotent, as client-go asks.
- client-go's default watch-error handler logs every failed list or watch at error level, which
  would fill the log for an unreachable cluster or a kind it doesn't have. The collector keeps the
  error for the status instead and logs it at debug level. The modes in L4 build on this.
- Clusters gained a dynamic client without a timeout (`StreamDynamic`), next to the typed
  `Stream` one. Both stream clients now get Opscope's request limit (50 a second, bursts of 100);
  before, they were copied before it was set and had client-go's 5 a second, which would make a
  collector's start take several seconds.
- `clusters.Manager.Watch` calls the collectors on every add, load and removal, with the manager
  locked so no change is missed between replaying the existing clusters and the next one.
- `go mod tidy` added `github.com/pmezard/go-difflib` as an indirect module. It comes with
  client-go's `tools/cache`, the informers' own package, so it's part of the Kubernetes client
  libraries, not a new choice.
- `GET /api/clusters/{id}/live` works now; it goes into `docs/api.md` with the stream in L5, as
  planned. In L2 every kind's mode is `watch`, and a kind that can't be listed shows its error
  while the state is `partial`; L4 turns those into `not_installed`, `forbidden` and the rest.
- Tests: the bus (fan-out, close, gaps, concurrent use under `-race`), the transforms (managers
  kept, Secret values and the annotation gone, ConfigMap hash stable, changed by every kind of
  edit, unchanged by trimming twice), and the collector on client-go fakes (sync and counts,
  initial adds, add / update / delete, an update with the same resourceVersion skipped, a Secret
  value never in any event or the cache, stop, two clusters on one bus, a kind that can't be
  listed). Removing the Secret transform or the resourceVersion check makes them fail, checked
  once each. The manager test covers Watch. All pass ten times in a row with `-race`.
- Checked against multipass (read-only): every kind synced within a few seconds of starting, and
  the count of each of the 16 kinds that have objects there matches `kubectl get -A`. Adding the
  same kubeconfig again as a second cluster started a second collector, which synced; removing it
  stopped it. No client-go messages in the log. With `OPSCOPE_LIVE=false`, `/live` says `off` and
  no collector starts. Pages don't read from the cache yet (L3), so they're unchanged.
