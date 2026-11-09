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

**Status (2026-11-07):** L0–L7 done, released in v1.4.0.

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
  - kubectl's `last-applied-configuration` annotation is dropped from every kind, not only
    Secrets: it's a copy of the whole object, ConfigMap values included, and nothing reads it from
    the cache. (Found in L6.)
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
  | `forbidden`     | `list` forbidden for a cluster-wide kind            | The existing permission message |
  | `pending`       | not probed yet, or the probe got no answer          | A list per request, as `direct` |

  The collector finds the mode with a `list` (limit 1) and a short `watch` before it starts an
  informer, and checks again every 5 minutes for kinds that weren't available, so installing Gateway
  API or granting a permission is picked up without a restart. `direct` keeps Opscope working for a
  kubeconfig user who may read only some namespaces.
- **A kill switch: `OPSCOPE_LIVE=false`** turns collectors off and every kind runs in `direct`
  mode, exactly as v1.3.0. It is the fallback the engine plan asks for when loading changes, and it
  is how L3 compares the two paths on a real cluster. It's documented in `docs/deployment.md` as a
  fallback users can choose, for at least v1.4 and v1.5. (Answered in review, 2026-11-06.)
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
- **Events are cached like every other kind.** An event takes about 1.3 KiB, a tenth of a pod, so
  10,000 events are about 13 MiB (measured in L6). (Answered in review, 2026-11-09.)
- **No `/metrics` endpoint in v1.4.** Opscope's own metrics for Prometheus (collector lag, cache
  size) come with the Helm chart and multiple replicas in v1.8; L6's measurements didn't need them.
  (Answered in review, 2026-11-09.)

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
  "state": "live",           // live, partial (some kinds polled), connecting, unreachable, off
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
- [x] Open questions answered and moved into Decisions (the last two after the release; see notes)
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

- [x] `Source` and `Query.At`; list functions read from a `Source`; `ErrNoHistory` for a non-zero
      `At`
- [x] The cache `Source` and the direct `Source`, with the 5-second wait for a kind's first sync
      (and none for a kind whose list failed, see notes)
- [x] The overview, the `/names` index, detail pages' related lists and usage's node and pod lists
      read from the cache; the detail object, YAML, logs and Secret values stay direct
- [x] A Go test that runs every list through both sources and compares the rows
- [~] On the test clusters, every endpoint gives the same answer with `OPSCOPE_LIVE=false` and with
      collectors on (two servers, ports 8090 and 8091), with two clusters connected; the second
      cluster was multipass again under another name (see notes)
- [x] No change in the browser: light and dark, keyboard, 375 px and wide

## Phase L4: Permissions and fallbacks

Goal: without `watch`, or with only some namespaces readable, Opscope still works and says how.

- [x] Probe per kind (`list` with limit 1, then a short `watch`) and the modes `watch`, `poll`,
      `direct`, `not_installed` and `forbidden`; probing again every 5 minutes
- [x] `poll` relists every 30 seconds into the same cache and publishes the differences
- [x] `CollectorGap` on relist, while a cluster is unreachable, and when a collector stops
- [x] An unreachable cluster backs off and reconnects without filling the log
- [x] `watch` in `deploy/kubernetes/opscope.yaml` and `secrets-access.yaml`
- [x] `docs/deployment.md` (permissions table, "it polls rather than watching" replaced, the
      modes), `CONTRIBUTING.md` (principles, adding a resource type) and the README
- [x] Go tests with fakes for every mode, and for a mode changing when a permission is granted
- [x] Checked on a kind cluster with a role that allows `list` but not `watch`, and with a
      namespace-only role

## Phase L5: Live pages

Goal: lists and the overview update within a couple of seconds of a
change, without a timer.

- [x] `GET /api/clusters/{id}/live/stream`: `changed` at most once a second per resource, `status`,
      a heartbeat every 15 s; closes cleanly when the client goes or the server stops
- [x] `src/live.js`: one stream per cluster per tab, paused while the tab is hidden, a full refetch
      on reconnect
- [~] `useApi` `live` option with the 2-second limit and the polling fallback; lists and the overview
      use it (the fallback rule changed; see notes)
- [~] `LiveStatus` in `/kit` (every state, both themes), then in the top bar (opens on click, not
      hover; see notes)
- [x] Works through `kubectl port-forward` and the Vite dev proxy
- [x] Frontend tests for matching messages to paths and for the fallback
- [x] `docs/api.md` for both `live` endpoints
- [x] Browser checks: light and dark, keyboard, screen reader label, 375 px and wide

## Phase L6: Scenarios and sizing

Goal: checked against failure cases, with published memory numbers.

- [x] `test/scenarios/workloads.yaml`: a CrashLoopBackOff pod, an OOMKilled pod, an
      ImagePullBackOff pod, and a Service whose selector matches nothing, with a table in the README
      of what each page should show
- [x] On a kind cluster: each scenario appears and changes live (restart counts rise without a
      reload)
- [x] A Go benchmark that fills the cache with realistic objects and reports memory per 1,000
      objects per kind
- [x] Memory measured on the scenario cluster and on multipass (read-only), with one and with two
      clusters connected; the run's length agreed before it starts (10 minutes)
- [~] The manifest's requests and limits updated, and a sizing table in `docs/deployment.md` (the
      limit stays; `GOMEMLIMIT` added instead; see notes)

## Phase L7: Release

Goal: v1.4.0 is ready to tag.

- [x] Bugs found on the way fixed, one commit each
- [x] README feature list, screenshots that show the indicator
- [x] Every check CI runs: `make test`, `go vet`, `gofmt`, the frontend build, `make docker-build`
- [x] Full browser pass: light and dark, keyboard only, 375 px and wide, with and without
      metrics-server and Gateway API, two clusters connected
- [x] Image tag in `deploy/kubernetes/opscope.yaml` and the manifest URLs in the README and
      `docs/deployment.md` set to v1.4.0
- [x] Status line, design record table, release notes drafted (the tag is pushed by the maintainer)

## Not in this release

- **Topology (v1.5)** builds on the cache and the bus. Before it starts, its record writes down why
  the G0–G7 attempt was removed.
- **Opscope's own `/metrics` for Prometheus** (collector lag, cache size): planned with the
  storage design's cloud-native work, in v1.8 (see Decisions).
- **Watching only some namespaces** (`OPSCOPE_NAMESPACES`): added only if L6's numbers show the
  need. Until then, a user who can't list across namespaces gets `direct` mode.
- **Live detail pages:** detail pages keep polling; a later release can listen for their own object.

## Open questions

1. ~~**Secrets in the cache.**~~ Answered: watched, values dropped in the transform (see
   Decisions).
2. ~~**Kill switch.**~~ Answered: documented as a fallback (see Decisions).
3. ~~**`/metrics` endpoint.**~~ Answered: not in v1.4; it comes with the Helm chart and multiple
   replicas in v1.8 (see Decisions).
4. ~~**Events in the cache.**~~ Answered: kept in the cache, from L6's numbers (see Decisions).

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

### L3: Listers on the cache (2026-11-06)

- `resources.Source` has one method, `List(ctx, resource, q)`, returning typed objects (or
  `*unstructured.Unstructured` for Gateway API). `ListOf[T]` casts them and refuses a non-zero
  `Query.At` with `ErrNoHistory`. Every list function now starts with `ListOf` instead of a client
  call; the flattening below it is untouched apart from `&x` becoming `x`, since the objects are
  pointers now. `asDynamicLister` is gone: every lister takes a `Source`.
- `resources.Direct` is v1.3's way: one list request per call, with a small table of typed list
  calls (`typedLists` in `source.go`) and the dynamic client for Gateway API, which still turns a
  404 into `ErrGatewayAPINotInstalled`. Adding a resource type now also needs a line there;
  `CONTRIBUTING.md` says so in L4, with the rest of its changes.
- The cache source (`collector.Set.Source`) reads from the collector's informer, by namespace index
  when the query has one, and falls back to `Direct` for a kind it can't serve. Changed from the
  plan: a kind whose list or watch has already failed doesn't wait the 5 seconds; it's read from
  the cluster at once. Without that, every overview on a cluster without Gateway API waited 5
  seconds for a sync that would never come.
- Small changes in behaviour, so both sources give the same answer:
  - Events are filtered by type in Go instead of with a field selector, and a detail page's events
    by the object's UID in Go too.
  - Events seen at the same moment are ordered by namespace and name. Before, their order among
    themselves was whatever the API returned.
  - A claim's "Used by" pods are ordered by name, as the API happened to list them.
  - A claim's StorageClass is checked against the list of classes instead of one `get`.
- Usage: `metrics.ReadNodes` and `Volumes.Read` take a `Source` for the nodes and pods; usage
  itself still comes from metrics-server and the kubelets. The 15-minute history sampler gets each
  cluster's source from the collector set.
- Known gap until L4: once a kind has synced, the cache keeps serving it if the watch later breaks
  (the cluster becomes unreachable, say), without saying the data may be old. L4's modes and gaps
  cover it.
- Tests: `TestCacheAndClusterGiveTheSameAnswers` builds a fake cluster with something of every kind
  in two namespaces (a claim in use, claims from a StatefulSet, a claim with a missing class,
  Secrets with values, events of both types at one moment, Gateway API objects) and checks that
  every lister (six queries each), the overview, the name index and five detail pages give the
  same JSON from the cache as from the cluster. The cache side fails the test if it falls back to
  the cluster at all, and every answer must be a real answer, not two equal errors. Ignoring the
  namespace in the cache source makes 39 comparisons fail, checked once. Other tests: a failed
  kind is read from the cluster without waiting; an unsynced kind waits up to the limit, then reads
  the cluster; `At` is refused. The existing tests now run on `Direct`. All pass five times in a
  row with `-race`.
- Checked against multipass (read-only), with two servers: collectors on (8090) and
  `OPSCOPE_LIVE=false` (8091). A script compared 237 endpoints: every list across all namespaces
  and in each of the 9 namespaces, both event types, the overview (all and per namespace), the
  name index, the detail page of every pod and storage object and one object of each other kind,
  and the node list behind usage. All matched; once, an endpoint matched only on a second read
  because the cluster changed in between. With a second cluster added to both servers, its 237
  endpoints matched too. Docker wasn't running, so the second cluster was multipass again under
  another name: it has its own collector and cache, which is what the check is about, but not a
  different cluster.
- From the cache, a request takes about half a millisecond on multipass: the overview 0.5 ms
  instead of 23 ms, the name index 0.4 ms instead of 69 ms, the pod list 0.5 ms instead of 6 ms
  (median of five, on the same network as the cluster; a cluster further away gains more).
- In the browser: the overview, lists, Secrets, a claim's "Used by", a StorageClass's counts and
  the second cluster's pages look as before, in dark and in light at 375 px. The only console
  errors are the `metrics/volumes` 404s that usage-off has always given.

### L4: Permissions and fallbacks (2026-11-06)

- Open question 2 answered in review: `OPSCOPE_LIVE=false` is documented as a fallback
  (`docs/deployment.md`, "Turning live data off", and the settings table). Moved into Decisions.
- Each kind is probed through the dynamic client: a `list` with `limit=1`, then a `watch` with a
  one-second timeout from that list's resourceVersion. Forbidden for a namespaced kind gives
  `direct`, for a cluster-wide kind `forbidden`; 404 gives `not_installed`; a forbidden watch gives
  `poll`. Added to the plan: `pending`, for a kind not probed yet or whose probe got no answer
  (the cluster didn't respond). It's probed again after 10 s, then 20, 40, ... up to every 5
  minutes. Kinds not watched are probed again every 5 minutes; a watched kind only when its list or
  watch fails with forbidden or not found (a permission removed, Gateway API uninstalled).
- Every probe runs at once when a collector starts, so a cluster's modes are known in one round
  trip. Only one goroutine per collector changes modes, so two changes never overlap.
- `poll` lists every 30 seconds into its own cache, through the same `Direct` lists the pages use,
  trimmed the same way. It publishes adds, updates and deletes by resourceVersion, with a gap
  before them when anything changed, since several changes between two polls arrive as one. Its
  first list is published as initial adds, as an informer's is.
- Relists: client-go doesn't tell a watch-error handler when a watch ends and it lists again, so
  the collector notices relists through the informer's list-options hook, which sees every list and
  watch it starts (a streamed "watch list" counts as a list). A list after the first starts a gap,
  from the last moment the watch was known to work (its start or its last event); the next watch
  ends it. So a gap can be wider than the real break, never narrower.
- Unreachable clusters: every 10 seconds the collector asks the cluster for its version, with the
  10-second client. When it doesn't answer, the state is `unreachable` and pages read the cluster
  directly, so they show the cluster's error as in v1.3 instead of a copy that may be old. When it
  answers again, a gap covers the time in between, for every kind. That's one warning and one info
  line in the log per outage.
- A collector that stops publishes a gap with no end (`To` zero): nothing is recorded until a
  collector for that cluster starts again. A kind that changes mode publishes a gap, and its new
  cache's first list comes as initial adds.
- Changed from the plan: the cluster's `state` counts only polled kinds as `partial`. A `direct`
  kind is read from the cluster on each request, so it's never behind; counting it would have made
  every install without `secrets-access.yaml` permanently `partial`.
- Found on the kind cluster: when a connection drops, client-go logged a "watch ended with error"
  line for every kind, through klog in its own format. klog now writes to Opscope's logger at debug
  level (`klog.SetSlogLogger` in `main.go`), which makes `k8s.io/klog/v2` a direct dependency at
  the version client-go already used. The collector's own mode messages are one line per change
  ("how kinds are read", grouped by mode) instead of one per kind, and only at info level when some
  kind isn't watched.
- Found on the kind cluster too: for a user without Gateway API permissions, the API server answers
  403 before it checks that the kind exists, so those kinds show as `direct` or `forbidden`, not
  `not_installed`. That's what v1.3 got as well; the page says it isn't allowed.
- Docs: the ClusterRole and `secrets-access.yaml` have `watch` (not `metrics.k8s.io`, which has no
  watch). `docs/deployment.md` explains the copy in memory, the three ways to read with less
  access, what `watch` means for Secret values, and `OPSCOPE_LIVE`. `CONTRIBUTING.md` has the new
  principle wording, the `typedLists` line for a new kind, and the two new packages. The README says
  `get`, `list` and `watch`.
- Tests: the modes for every case (secrets direct, nodes forbidden, pods polled, gateways not
  installed, configmaps watched) with their reasons; a pod added while polled arriving after a gap
  and in the cache; kinds the cache doesn't hold read from the cluster; a granted `watch` picked up
  by the next probe; an unreachable cluster read directly and leaving a gap; an expired watch
  leaving a relist gap; a stopped collector's open gap. Removing the relist gap or the unreachable
  fallback makes those tests fail, checked once each. All pass ten times in a row with `-race`.
- `test/scenarios/permissions.yaml` (with a README section) sets up two users on a kind cluster:
  `no-watch` (`get` and `list` on everything `opscope.yaml` grants) and `team-a` (one namespace).
  Checked on a throwaway kind cluster, deleted afterwards:
  - The admin: every kind `watch`, Gateway API `not_installed`, state `live`.
  - `no-watch`: polled kinds, Secrets `direct`, state `partial`; a new pod appeared after 26 s.
  - `team-a`: pods in `team-a` list; across all namespaces, and nodes, the API's own forbidden
    message, as v1.3.
  - Granting `no-watch` `watch` on the core kinds moved them to `watch` at the next 5-minute probe
    (23:52 to 23:57), without a restart; the kinds in other rules stayed polled.
  - Pausing the kind node: `unreachable` within 15 s, pages showed the cluster's timeout error;
    after the unpause, `live` again, with one warning and one info line in the log.
- The L3 comparison against multipass was run again on this code: all 237 endpoints still match.

### L5: Live pages (2026-11-07)

- `GET /api/clusters/{id}/live/stream` (`server/live.go`) subscribes to the bus with a queue of
  1,024 and keeps one cluster's changes per resource, with the namespaces they were in. A resource
  is sent at most once a second; the first change after a quiet second goes out within 250 ms.
  Every 250 ms it also checks the cluster's state and modes (`Set.Modes`, the status without object
  counts, which are the costly part) and sends `status` when they change. A heartbeat goes every
  15 s. The stream ends when the browser goes, when Opscope shuts down (the set's context), or
  when the cluster is removed. Shutting down with a stream open took no time, not the 10 s timeout.
- Added to the plan:
  - A `resync` message, for a stream that fell behind and lost events (a `subscriber_lagged`
    gap). The browser fetches everything on screen again. Other gaps need nothing: a relist's
    changes are published as changes, and an unreachable cluster shows in `status`.
  - With live data off the stream answers `204`, which tells `EventSource` not to reconnect.
  - A cluster whose collector hasn't started yet answers `503`.
  - The heartbeat is an event, not a comment, so the browser can see it: a stream silent for 45 s
    (a port-forward that died without closing) is reopened.
- `src/live.js`: `LiveConnection` holds one `EventSource` per cluster for the tab, shared through
  `LiveProvider` in `Layout`. It closes while the tab is hidden, and a reopened stream sends
  `resync` to every page. When `EventSource` gives up (any answer but a stream), it asks `/live`
  whether live data is off. If it isn't, it tries again after 5 s, then 10, 20, 30.
- `useApi(path, { live, refreshMs })`: the first load, the timer and the stream are three separate
  effects, so the stream opening doesn't load the page twice. A matching change loads again at
  once, or 2 s after the last load, with one load covering everything in between. Lists, the
  overview (its kinds listed in `Overview.jsx`), its nodes and warnings, and the namespace picker
  use it. Detail pages and usage keep their timers.
- Changed from the plan: the fallback timer runs only for kinds the stream can't speak for:
  `direct` and `pending`, or every kind while the stream is down or the cluster unreachable.
  Polled kinds don't need it, since each poll publishes what changed. `forbidden` and
  `not_installed` kinds don't either: their answer stays the same until a probe finds otherwise,
  which arrives as a new `status`. A hidden tab runs no live timers.
- Changed from the plan: the indicator opens a panel on click instead of a tooltip on hover, so
  touch and keyboard reach it. The panel names the kinds that aren't watched, grouped by how
  they're read, with the reason. Its button's accessible name is "Updates: Live" (and so on); a
  warning or error gets an icon as well as a colour, as `StatusBadge` does. Nothing moves.
- Changed from the plan: the indicator's label is about the page on screen, not the whole cluster.
  The slowest of the page's kinds decides: **Every 30 s** if one is polled, **Every 10 s** if one
  is `direct`, otherwise **Live**. The overview counts its kinds and Events, and a detail page
  its list's kind. The panel still lists every kind that isn't watched. A label for the whole
  cluster was wrong both ways:
  - A user who can read one namespace saw "Live", although every page refreshed on the 10-second
    timer.
  - A default install, without `secrets-access.yaml`, showed "Every 10 s" with a warning on every
    page, for Secrets alone.
- `/kit` has a "Live status" section with every state in both themes, including the panel.
- Tests: the stream on a fake feed with a real bus (headers, first status, changes from two
  namespaces in one message, another cluster's left out, a cluster-wide kind without namespaces, a
  mode change, `resync` on a lagged gap and not on a relist gap, the end on shutdown and on a
  removed cluster), its answers (`204` off, `404`, `503`), and the once-a-second limit. Removing
  the limit or the `resync` makes them fail, checked once each. Frontend: matching messages to
  paths (namespaces, several kinds, `resync`), the fallback rule for every mode and connection
  state, the descriptions, and `LiveConnection` with a fake `EventSource`, document and timers
  (messages, resync on reconnect only, pause and resume, the 45-second watchdog, off, the backoff).
- Checked on multipass (read-only) and a throwaway kind cluster, both through the Vite dev proxy,
  deleted afterwards:
  - multipass: every kind watched; the stream works through the proxy.
  - kind: scaling a deployment from 1 to 3 showed the new pods 184 ms later, and their Running
    status 2 s after that. A pod created in another namespace didn't reload the page, and no
    10-second timer ran for 25 s. A new deployment that can't pull its image showed on the
    overview without a reload.
  - `no-watch` (polled): "Every 30 s"; a scale-down appeared 13 s later, at the next poll, with
    one fetch.
  - `team-a` (direct): the 10-second timer ran, and the indicator said "Live", which led to the
    label change above.
  - The default install's own service account: "Live" on Pods and the overview, "Every 10 s" on
    Secrets.
  - Backend stopped and started: "Reconnecting" at once, the 10-second timer meanwhile, then
    "Live" and a full refetch.
  - Through `kubectl port-forward` to Opscope running in the cluster (this branch's image): a
    change reached the stream in about 250 ms, three times out of three, and the same after the
    stream had been idle for 50 s. The page fetched 168 ms after a scale. The app's browser pane
    hides the page when it's in the background, which paused the stream as designed. No request
    was made for the 8 minutes it was hidden, and showing it again reopened the stream within
    70 ms, with one refetch.
  - Light and dark, Enter and Escape on the indicator (focus returns to it), and 375 px wide.
    Found and fixed: the panel ran off the left edge on phones, where the indicator starts the
    second row; the open button lost its warning colour; "Not installed" repeated its reason; and
    Events and Namespaces showed their API names.

### L6: Scenarios and sizing (2026-11-07)

- `test/scenarios/workloads.yaml` has a healthy `web` Deployment and Service next to a
  CrashLoopBackOff Deployment, an OOMKilled pod (a shell doubling a string past a 16 MiB limit),
  an ImagePullBackOff pod (an image on `registry.invalid`, which never resolves) and a Service
  whose selector has a typo. The README's table says what each page should show.
- Checked on a throwaway kind cluster, deleted afterwards. Every page showed what kubectl showed,
  and the Pods page kept up without a reload: restart counts rose and each status change (Error,
  Running, CrashLoopBackOff, ErrImagePull, ImagePullBackOff) was fetched within about a second
  of kubectl seeing it, five changes out of five. The overview counted the failures by status and
  one Deployment of two not ready; the warnings listed BackOff and Failed.
- Found: nothing says that `no-endpoints` matches no pods. The detail page shows its selector, and
  that's all. Left for the issues feed in a later release; noted in the README.
- The app's browser pane reports a background tab as hidden, which pauses its stream (as designed
  in L5). For the check above, the page's `document.hidden` was overridden from the console.
- `BenchmarkCacheMemory` (`collector/memory_test.go`) fills a cache through the transform with
  2,000 made-up objects per kind, written as JSON and read back so every string is its own
  allocation, and reports the heap per 1,000. With `OPSCOPE_BENCH_DIR`, it reads
  `kubectl get <kind> -A -o json` files instead. Per 1,000 objects (made up / multipass / kind):

  | Kind        | Made up   | multipass | kind      |
  | ----------- | --------- | --------- | --------- |
  | Pods        | 12.8 MiB  | 14.3 MiB  | 10.7 MiB  |
  | Deployments | 11.6 MiB  | 8.7 MiB   | 6.2 MiB   |
  | Nodes       | 19.1 MiB  | 9.9 MiB   | 7.1 MiB   |
  | Jobs        | 8.2 MiB   | (none)    | (none)    |
  | Claims      | 3.8 MiB   | 3.7 MiB   | (none)    |
  | Services    | 2.4 MiB   | 2.3 MiB   | 2.0 MiB   |
  | ConfigMaps  | 1.9 MiB   | 1.1 MiB   | 1.4 MiB   |
  | Secrets     | 1.9 MiB   | (not saved) | (not saved) |
  | Events      | 1.4 MiB   | 1.2 MiB   | 1.2 MiB   |
  | Namespaces  | 1.3 MiB   | 1.2 MiB   | 1.1 MiB   |

  The real columns are after the fix below. multipass has 3 nodes, 46 pods and 19 Deployments,
  so its rows are averages of few objects.
  The made-up objects are a production-like pod with an app container and a mesh sidecar, probes,
  env and a projected token volume; nodes report 50 images, which is most of their size. In
  memory an object takes about twice its JSON. Secrets weren't saved to disk from the real
  clusters, since that would write their values out.
- Found and fixed: kubectl's `last-applied-configuration` annotation was kept on every kind but
  Secrets. On a ConfigMap it holds the values the transform drops, and on an applied Deployment
  it's a full copy. `trim` now drops it from every kind (`trimMeta`), with a test for a
  ConfigMap, a Deployment and a Gateway API object. Made-up ConfigMaps went from 4.9 to 1.9 KiB,
  Deployments from 15.6 to 11.6, Services from 3.1 to 2.4.
- Memory of the real process: this branch's image in Docker, four containers side by side for
  10 minutes, sampled every 5 seconds with `docker stats`, while a loop loaded six lists from each
  every 10 seconds. kind had the scenarios plus 3,000 ConfigMaps, 1,000 Secrets and 500 Services
  in ten namespaces (about 4,600 objects); multipass had about 250.

  | Container                       | First minute | Last 2.5 minutes |
  | ------------------------------- | ------------ | ---------------- |
  | kind                            | 36–42 MiB    | 47 MiB           |
  | multipass                       | 24 MiB       | 27 MiB           |
  | both                            | 39–46 MiB    | 55 MiB           |
  | both, `OPSCOPE_LIVE=false`      | 27–32 MiB    | 37 MiB           |

  All four rose a little over the 10 minutes, the one with no cache too. To tell growth from a
  leak, a container ran with `GODEBUG=gctrace=1` while 200 ConfigMaps were relabelled for 3
  minutes (about 76,000 updates) and pages loaded every 5 seconds. The live heap after each
  collection stayed at 10–12 MB from the first 30 seconds to the last: no leak. Those 11 MB match
  the benchmark (about 7 MiB for the cluster's objects) plus the server itself. A fresh container
  on the same cluster peaked at 34 MiB during its first sync, so startup isn't the constraint.
- From these: memory is about 25 MiB plus twice the copy, since Go lets the heap reach twice
  what's live before collecting. With the 256 MiB limit that's around 3,500 pods.
- Changed from the plan: the limit stays at 256 MiB, and the manifest sets `GOMEMLIMIT=230MiB`,
  so the collector works harder near the limit instead of the pod being killed. The request
  goes from 48 to 64 MiB, above the 27–55 MiB measured. `docs/deployment.md` has a "Memory"
  section: memory per kind, and a table from 500 to 10,000 pods with the limit to set. Applied
  on kind with this branch's image: the pod ran at a 28 MiB working set with about 3,500
  objects.
- Open question 4 (Events): an event takes a tenth of a pod's memory, so the proposal is to keep
  them cached; it's marked for review. Question 3 (`/metrics`) is unchanged: these measurements
  didn't need it.

### L7: Release (2026-11-07)

- Every check CI runs, on the branch: `go vet`, `gofmt`, `go test ./...` (also with `-race`), the
  102 frontend tests, the frontend build, `make docker-build`, and the image for `linux/amd64` as
  CI builds it. `.github/scripts/check-manifest-tag.sh v1.4.0` passes on the manifest.
- Browser pass on a build served by the Go server as v1.4.0, with two clusters connected:
  multipass (read-only), which has metrics-server and Gateway API, and a throwaway kind cluster
  with `test/scenarios/workloads.yaml`, which has neither (deleted afterwards).
  - Every page on both clusters: the indicator read "Live", no console errors. On kind, the usage
    and Gateway API pages said what isn't installed, as before.
  - Keyboard only: the skip link, back into the top bar, Enter opens the indicator's panel,
    Escape closes it and returns focus, ⌘K finds the `oom` pod and opens it.
  - 375 px: the panel stays on screen and the page doesn't scroll sideways. Light and dark in
    `/kit` and in the screenshots.
- Found and fixed (its own commit): detail pages said "Live", but they're read on their 10-second
  timer, since live detail pages aren't in this release. A detail page now passes no live kinds,
  and the indicator says "Every 10 s" in a neutral tone, explaining that lists and the overview
  update live. The panel still lists the kinds that aren't watched. In `/kit` as "Detail page",
  with a test; `docs/deployment.md` says so.
- README: a "Live updates" feature, tables no longer described as refreshing on their own, and
  Secret values dropped from the live copy in the Security section. The overview, pods and pod
  screenshots were retaken on multipass, light and dark, so they show the indicator, the same way
  as before (1440 × 900 at 2×, scaled to 1600 × 1000), now from a small script driving headless
  Chrome over its DevTools protocol, which waits until the page has settled.
- Version: the manifest's image, the manifest URLs in the README and `docs/deployment.md`, the
  image tag table, the health example in `docs/api.md` and the bug report placeholder say v1.4.0.
  The record is in the design records table.
- Open questions 3 and 4 are still open, with their proposals.
- After the release (2026-11-09): both answered in review as proposed, before v1.5 starts, and
  moved into Decisions.
