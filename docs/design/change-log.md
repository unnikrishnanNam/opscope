# Opscope: Change log

Opscope shows what the cluster looks like now, and since v1.6 what is wrong with it now. It can't
say what changed. When a Deployment starts crash-looping, the first question is usually "what was
different an hour ago?", and today the answer is in someone's shell history, a Git log or nowhere.
This record adds the **Change log**: a persistent ledger of every change Opscope sees to the
objects it watches. Each change is kept as a field-level diff, with **who made it** (a person with
kubectl, Helm, Argo CD or Flux, or a controller) read from the object's own `managedFields`, and
**risk tags** (image, limits, probes, config, …) that say what kind of change it was. On top of
the ledger it works out each workload's **last known good** version: the newest one that ran
healthy.

People see it in three places. A new **Changes** page lists what changed across the cluster, with
filters by namespace, kind, actor and risk. A **History** tab on every detail page shows one
object's versions and compares any two side by side. **"Diff vs last known good"** on an unhealthy
workload shows what changed since it last worked. The signals and health history from v1.6, which
are kept in memory today, are kept in the same store, so they survive a restart.

This is Opscope's first persistent data beyond saved clusters, and its first database. It is the
**State store, part 1** in the engine release plan: a `Store` interface with an embedded SQLite
backend in `/data`, as the storage design decided. The Postgres backend, multiple replicas, the
Helm chart and the time machine are v1.8. Opscope stays read-only, one image and one process, and
asks for no new permission in any cluster. The manifest gains a PersistentVolumeClaim, because
history in an `emptyDir` is lost whenever the pod is replaced.

This is v1.7.0 in the engine release plan. Phases are numbered **C0–C7** so they don't mix with the
build phases 0–8 or the D, G, I, L, P, R, S and T phases. All work happens on the `docs/change-log`
branch, one commit per phase, as in v1.4 to v1.6. Each phase is reviewed before it is committed,
and the next phase starts only after that.

**Status (2026-11-23):** planned. C0 is written and waiting for review.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

### The store

- **A `Store` interface, and one backend in this release: embedded SQLite.** The engines talk to
  the interface, never to a database. Only `internal/store` imports `database/sql`. This follows
  the storage design (2026-11-02), which settled the choice the engine plan left to this record
  (contract 10: "a single pure-Go dependency with a written reason, or standard-library files").
- **The written reason for SQLite (`modernc.org/sqlite`, pure Go):**
  - **One query layer for two backends.** The schema and the SQL are nearly the same as
    Postgres, which comes in v1.8. With bbolt, v1.8 would mean writing the query layer a second
    time.
  - **The queries are relational.** "Every change to these UIDs between T1 and T2", "the newest
    version of X before T", "when was X last healthy?" are indexed range queries. A key-value store
    or plain files would need indexes written by hand, plus compaction for retention.
  - **Pure Go keeps the image as it is.** The cgo driver would break `CGO_ENABLED=0` and the static
    multi-architecture build. The pure-Go driver doesn't.
  - **Measured cost:** the stripped `linux/arm64` server binary is 34.2 MB today. The driver adds
    about 4.4 MB (13%). It brings `modernc.org/libc`, `mathutil` and `memory`, plus four small
    modules (`go-humanize`, `go-isatty`, `go-strftime`, `bigfft`); `google/uuid` and `x/sys` are
    already in the tree.
  - **Rejected:** bbolt (hand-written indexes, and a second query layer in v1.8); standard-library
    files (the same, plus compaction written by hand); the cgo SQLite driver (breaks the build);
    a time-series database (the storage design explains why the ledger isn't time series).
- **One conformance suite, written now.** `internal/store/storetest` runs every `Store` method's
  contract against a backend. SQLite runs it in this release; Postgres runs the same suite in v1.8,
  so the two can't drift apart.
- **Migrations are embedded and run at startup.** Numbered SQL files in an `embed.FS`, with the
  schema version in the database. An older Opscope that finds a newer schema doesn't touch it: it
  runs without history, and the pages say why (fail clearly). A downgrade never corrupts a
  database.
- **One file: `/data/opscope.db`, in WAL mode.** Saved clusters stay as files in `/data/clusters`;
  moving them into the database would change something that works for no gain in this release.
- **One writer, batched.** The ledger writes from one goroutine, one transaction per second or per
  500 rows, whichever comes first. SQLite's page cache is capped (8 MiB to start; C1 measures it),
  so the store stays inside the memory budget (contract 13).
- **Configuration:** `OPSCOPE_STORE=embedded` (the default) or `off`. `postgres` is added in v1.8,
  with `OPSCOPE_DATABASE_URL`. `off` keeps Opscope as stateless as v1.6: no Changes page, no
  History tab, and they say why.
- **When `/data` can't be written,** Opscope starts without history and says so on every page that
  needs it, as it does for a missing permission. When live data is off (`OPSCOPE_LIVE=false`) there
  is no bus and so no ledger, and the pages say that too.

### What the ledger records

- **A change is what someone or something set, not what the cluster reports.** The ledger records
  changes to `metadata` (labels, annotations, owner references) and to `spec` (or `data` for
  ConfigMaps and Secrets). It doesn't record `status`: status changes are what the Signal engine
  turns into signals and health transitions, which are recorded separately. Two status fields are
  exceptions, because they say which version of something is actually running: a pod's
  `containerStatuses[].imageID` (the image digest) and each container's start time (see
  [effective time](#effective-time)).
- **Every kind in the cache is recorded, except Events and EndpointSlices.** Events are
  observations, not changes, and v1.6 already reads them as evidence. EndpointSlices change with
  every pod that starts or stops; the endpoints behind a Service follow from its selector and
  pods, which are recorded. Leases aren't watched at all.
- **Per-kind noise rules, as data.** A table of field paths to drop, per kind, before diffing:
  `metadata.resourceVersion`, `generation`, `managedFields` (the actor is stored separately),
  `deployment.kubernetes.io/revision`, `endpoints.kubernetes.io/last-change-trigger-time`, a
  ReplicaSet's `spec.replicas` (the Deployment controller's rollout steps), a node's heartbeats and
  everything else under `status`. `kubectl.kubernetes.io/restartedAt` is kept: it is how
  `kubectl rollout restart` shows up. Adding a rule is one more entry, and C3 measures noise on the
  scenarios against the plan's target: under one irrelevant entry per real change.
- **Objects are versioned.** Each change stores the field-level diff and the whole normalized
  object after it, compressed, as a new **version** of that UID. Normalized means trimmed by the
  noise rules and the cache's transform. Keeping whole versions, not only diffs, makes "this object
  as it was at T" and "compare version 3 with version 7" a single read, and gives v1.8 most of what
  its keyframes need. C1 measures what that costs per 1,000 objects.
- **The diff is Opscope's own, in Go, with no dependency.** It walks two JSON trees and lists
  `{path, op, old, new}`. Lists that Kubernetes merges by a key (containers, env, ports, volumes,
  volume mounts, tolerations by key) are matched by that key, so reordering isn't a change and the
  path reads `spec.template.spec.containers[web].resources.limits.memory`.
- **Creation and deletion are changes too.** A created object's version 1 is the whole object; a
  deleted one gets a deletion entry with its last version kept. A UID that comes back never does,
  so a Deployment deleted and made again with the same name is two histories. The History tab
  links them by name ("an earlier `web` was deleted at 10:02").

### Changes made while Opscope wasn't watching

- **A restart or a broken watch is never a silent hole.** The collector already publishes
  `CollectorGap` (relists, unreachable clusters, stopped collectors, lagging subscribers). The
  ledger stores each gap with its time range and reason. While running, the store writes a
  heartbeat once a minute; on startup, the time since the last heartbeat is a gap too
  (`opscope_stopped`).
- **After a gap, the ledger reconciles.** The objects in the collector's first list are marked
  `Initial`, so they aren't changes. The ledger compares each one with the newest version it
  stored. Anything different becomes a change that happened **"between"** the gap's start and end,
  not at the moment Opscope noticed it. A UID it knew that is no longer listed becomes a deletion
  in the same range. The same comparison runs after a relist and after the ledger's own
  subscription fell behind.
- **Pages never imply the history is complete.** The Changes page and the History tab draw each
  gap as a line in the list ("Opscope wasn't watching from 02:10 to 02:40; changes in that time are
  shown as one"). Every answer says how far back history goes (`historySince`).

### Who made a change

- **The actor comes from `managedFields`, which the cache already keeps** without `fieldsV1` (v1.4
  kept the manager names for this release). The entries whose `time` moved between the old and the
  new object, or that are new, are the managers of this change. Entries for the `status`
  subresource are ignored, since status isn't recorded.
- **Managers map to four classes, through a table that is data:**

  | Class        | Managers (examples)                                                              |
  | ------------ | -------------------------------------------------------------------------------- |
  | `person`     | `kubectl`, `kubectl-client-side-apply`, `kubectl-edit`, `kubectl-patch`, `kubectl-rollout`, `kubectl-scale`, `kubectl-set`, `k9s` |
  | `automation` | `helm`, `argocd-controller`, `argocd-application-controller`, `kustomize-controller`, `helm-controller`, `terraform-provider-kubernetes` |
  | `controller` | `kube-controller-manager`, `kube-scheduler`, other `*-controller` and `*-operator` managers |
  | `unknown`    | anything not in the table, shown with its manager name                           |

  An unknown manager's name is still shown, so nothing is hidden by the classification. A rule pack
  in v1.13 can add rows.
- **Where it came from, when the object says so.** Helm's `meta.helm.sh/release-name`, Argo CD's
  tracking annotation or `app.kubernetes.io/instance` label, and Flux's
  `kustomize.toolkit.fluxcd.io/name` and `helm.toolkit.fluxcd.io/name` labels give the release or
  application the change came from. The ledger keeps this as the change's `source`. Fix guidance
  in v1.11 reads it to say where to fix something.
- **Never who, as a person.** `managedFields` names a program, not a user, and that is all Opscope
  can know without reading the API server's audit log, which is out of scope. The pages say
  "kubectl", never a name. The plan's word "human" becomes `person` for the same reason: it means
  someone ran a command, not who.
- **The time is when Opscope saw the change.** `managedFields` times have one-second resolution and
  only say when a manager last wrote, so they are shown as evidence, not used as the change's time.

### Risk tags

- **Each change gets the tags of the fields it touches**, from a table of field paths: `image`,
  `resources` (requests and limits), `probes`, `env`, `config` (a ConfigMap's or Secret's content,
  or which ones a pod uses), `selector`, `replicas`, `ports`, `scheduling` (node selectors,
  affinity, tolerations, taints, cordons), `storage` (claims, volumes, classes), `routing`
  (Ingress rules, routes, backends), `restart` (`restartedAt`), `lifecycle` (created, deleted) and
  `metadata` (labels and annotations only). The plan's list, plus `ports`, `scheduling`, `storage`,
  `routing`, `restart` and `lifecycle`, so every change has at least one tag.
- **Tags are an API, like the signal vocabulary.** Diagnosis in v1.10 scores suspects by them, so
  tags are added, never renamed.

### ConfigMaps and Secrets

- **ConfigMaps: which keys changed, not their values.** The cache keeps only a content hash today
  (v1.4). The transform will also keep a short hash per key, so the ledger can say "keys `LOG_LEVEL`
  and `DB_HOST` changed, `FEATURE_X` added" without storing a value. Values stay out of the cache
  and the store, as now: people put things in ConfigMaps that should have been Secrets, and the
  detail page reads values directly when someone looks. See [open question 3](#open-questions).
- **Secrets: key names always; which keys changed only with the optional Secret role.** Without
  the role, Secrets aren't in the cache, so there is nothing to record. With it, the transform
  computes an HMAC-SHA256 of each value, keyed with a random secret Opscope generates once and
  keeps in `/data` (mode 0600), and drops the values as it does now. A plain hash of a weak
  password could be guessed offline from a copy of the database; a keyed one can't without the key.
  The API only ever answers "changed" or "unchanged" per key; it never returns a hash. Contract 11
  holds: no Secret value reaches `/data` or an answer.
- **A test proves it,** like v1.6's log test. It runs Secrets and ConfigMaps with known values
  through the whole pipeline, then searches the database file, every API answer and Opscope's own
  log for those values.

### Effective time

- **A change and the moment it takes effect are recorded separately**, as the plan asks. Editing a
  ConfigMap that a pod reads through `env` changes nothing until the pod's containers start again.
  So the ledger also records, for each container start, what the container started with: its image
  digest and the content hashes of the ConfigMaps and Secrets it references (Secrets only with the
  Secret role; without it, which Secrets it references and nothing more). Diagnosis can then
  link "ConfigMap edited at 10:00" to "the container that started at 14:00 with the new hash is
  the one failing".
- **Only when something differs.** A crash-looping container restarts every few minutes with the
  same image and config. An effective record is written only when the digest or a hash differs
  from that container's previous start, so a crash loop writes nothing new.
- **Image digests catch moved tags.** When pods of one workload report a different `imageID` for
  the same image reference, the workload gets an `image` change: "`web:latest` now resolves to
  `sha256:4f1…` (was `sha256:9ab…`)", with the actor `unknown`: nobody changed the object, the
  registry did.

### Last known good

- **A workload's last known good (LKG) is the newest version that was healthy for five minutes
  in a row** while it was current. It covers Deployments, StatefulSets and DaemonSets, the
  workloads whose health v1.6 reads from their own signals (`replicas_unavailable`,
  `rollout_stuck`, …): never inferred from neighbours. A CronJob's only signal is
  `reference_missing`, which says nothing about whether its runs succeed, and a Job runs once, so
  neither gets an LKG in this release. Five minutes keeps a rollout
  that is briefly healthy before it starts crash-looping from counting. See
  [open question 5](#open-questions).
- **LKG is computed from stored history, not kept as a flag.** It is a query over versions and
  health transitions, so it is correct after a restart and can be asked for any time
  (`LKG(uid, at)`, contract 2), which diagnosis needs in v1.10.
- **For objects that aren't workloads**, "Diff vs last known good" follows the object to what it
  belongs to: a pod uses its owner's, and a ConfigMap or Secret lists the keys changed since the
  LKG of each workload that uses it, from the graph's `references` edges.
- **No LKG is said plainly.** A workload that has never been healthy for five minutes since history
  began has none, and the page says that, with how far back history goes.

### Signals and health history

- **They move into the store.** v1.6 keeps health transitions in memory, capped at 10,000 per
  cluster, and resolved signals for an hour. The ledger subscribes to `HealthChanged`,
  `SignalRaised` and `SignalResolved` and stores transitions and each signal **occurrence** (its
  open and resolve times, count and evidence). `HealthChanged` gains the IDs of the signals behind
  it, which `Transition` already has.
- **After a restart, history continues.** The Signal engine works out what is open from the cluster
  again, as now. An occurrence that was open when Opscope stopped and is still open after it is
  continued, with the restart drawn as a gap; one that's no longer open is closed at "between the
  stop and the restart".
- **The in-memory cap stays** for what the engine needs live. History queries read the store, so
  the Issues page's Resolved tab can go back further than an hour (its range follows retention).

### Retention

- **By age, per class, with a size cap underneath.** Starting defaults, which C3 checks against
  measured growth: changes and versions 14 days; pod creations, deletions and effective records 3
  days (pods churn); health transitions, signal occurrences and gaps 30 days. A version that is a
  workload's current LKG is kept until a newer one replaces it, however old.
- **The store never fills its volume.** Retention also deletes the oldest rows once the database
  passes 80% of the filesystem it lives on (read with `statfs` from the standard library), and the
  store status says it did. `OPSCOPE_HISTORY_DAYS` changes the age for changes and versions;
  `OPSCOPE_HISTORY_MAX_BYTES` sets an absolute cap.
- **Retention runs hourly**, deletes in small batches so the writer isn't held up, and reclaims
  space with SQLite's incremental vacuum.
- **No summaries yet.** The plan mentions "full diffs for N days and a summary after that". A
  summary is only worth having once someone wants history older than two weeks; it can come with
  the Postgres backend's time partitions in v1.8.

### Clusters

- **Everything is keyed by cluster and UID** (contract 1). History of several clusters lives in one
  database.
- **A cluster is recognised by its `kube-system` namespace's UID,** recorded when its history
  starts. If a cluster ID later points at a different cluster (a kubeconfig context reused for a
  rebuilt cluster, as happened with multipass), the old history isn't mixed in: it is closed, and
  the new one starts fresh, with a note. Without `get` on namespaces this check is skipped, and the
  store status says so.
- **Removing a cluster in the UI deletes its history,** and the confirmation dialog says so. See
  [open question 6](#open-questions).

### Pages

- **Changes page** in the Cluster group, after Issues (`g h`, palette "Changes" and "Changes in
  <namespace>"; `g c` is taken by Manage clusters). Newest first. Each row: when, the object
  (linked), a one-line summary ("memory limit 512Mi → 256Mi", "image web:1.4 → web:1.5", "3 keys
  changed"), the actor and source ("helm · release shop"), and the risk tags. Filters: namespace
  picker, kind, actor class, risk tag, and a time range (last hour, 24 hours, 7 days, all).
  Gaps appear as lines in the list. Updates live, like the Issues page. Built in `/kit` first.
- **A rollout is one row, not thirty.** The pods, ReplicaSets and effective records a Deployment's
  change causes are grouped under it (by owner and a short time window, like v1.6 groups pod
  signals), and open to show them. Pod changes don't show as rows of their own by default; a
  "Show pods" filter shows them. See [open question 7](#open-questions).
- **History tab** on every detail page, after Events. Its shortcut is `5` everywhere, so Logs keeps
  `4` on pods and each tab keeps one key on every page. It shows the object's versions on a
  timeline with its health transitions alongside, each version with its actor, source and tags.
  Picking two versions shows a side-by-side diff in the existing YAML style, with only the changed
  parts expanded. Deleted predecessors with the same name are linked.
- **"Diff vs last known good"** on the detail page of an unhealthy workload, on its pods, and on
  Issues rows. It opens the History tab with the LKG and the current version picked, or says why
  there's no LKG.
- **The detail page's summary says when the object last changed**, and by whom ("Changed 12 min
  ago by helm"), linking to the History tab.
- **Palette:** "Changes", "Changes in <namespace>", "History" and "Diff vs last known good" on
  detail pages.
- **Topology doesn't change in this release.** Drawing the graph as it was at a time is the time
  machine (v1.8).

### Security

- **History without login.** Anyone who can open Opscope can now read what changed and when, for as
  long as retention keeps it, not only what is there now. `docs/deployment.md` says so in
  [Giving people access](../deployment.md#giving-people-access), next to the port-forward and proxy
  options, and the security notes in the README say it too, as the plan asks.
- **What is stored is listed in the docs:** object metadata and specs as the cache keeps them, key
  names and keyed hashes for Secrets (with the role), per-key hashes for ConfigMaps, actors,
  health and signals. Never Secret values, ConfigMap values or log lines.

### Memory and disk

- **Measured, as in every release.** C1 measures the store's memory (page cache, writer queue) and
  C3 measures growth: rows and bytes per 1,000 pods per day on the scenario cluster, and changes
  per hour on multipass (read-only), which the storage design asked for before deciding anything
  further. The "Memory" section of `docs/deployment.md` gets a "Disk" section next to it, and the
  manifest's PVC size follows the numbers.

## Design

### Packages

```
backend/internal/store/              Store interface, models, retention
backend/internal/store/sqlite/       the embedded backend and its migrations
backend/internal/store/storetest/    the conformance suite every backend runs
backend/internal/changes/            normalize, diff, noise rules, actors, risk tags, the ledger, LKG
backend/internal/collector/          transform.go: per-key ConfigMap hashes, keyed Secret hashes
backend/internal/server/             changes.go, history.go: the endpoints
frontend/src/pages/                  Changes.jsx
frontend/src/components/             ChangeList, DiffView, HistoryTimeline
```

`changes` imports `bus`, `store`, `resources` (for `Kinds()`) and `graph` (for "who uses this
ConfigMap"). `signals` doesn't import `store`: the ledger persists signals and health from the bus,
and the engine reads its history back through a small interface the ledger provides. The collector
doesn't know the ledger exists.

### Model

```go
type Change struct {
	ID        string    `json:"id"`        // hash(cluster, uid, resourceVersion) (contract 3)
	Cluster   string    `json:"cluster"`
	Resource  Ref       `json:"resource"`  // {uid, resource, kind, namespace, name}
	Owner     *Ref      `json:"owner,omitempty"` // the controlling workload, for grouping
	Op        string    `json:"op"`        // created, updated, deleted, effective
	Version   int       `json:"version"`   // per UID, from 1
	ResourceVersion string `json:"resourceVersion"`
	Seen      time.Time `json:"seen"`      // when Opscope saw it
	Between   *Range    `json:"between,omitempty"` // set when it happened during a gap
	Actor     Actor     `json:"actor"`
	Risk      []string  `json:"risk"`      // risk tags
	Summary   string    `json:"summary"`   // "memory limit 512Mi → 256Mi"
	Diff      []Field   `json:"diff,omitempty"`
}

type Actor struct {
	Class    string   `json:"class"`    // person, automation, controller, unknown
	Managers []string `json:"managers"` // from managedFields: "helm"
	Source   *Source  `json:"source,omitempty"` // {tool: "helm", name: "shop"}
	Evidence []string `json:"evidence"` // "managedFields[helm].time 14:02:11"
}

type Field struct {
	Path string          `json:"path"` // spec.template.spec.containers[web].resources.limits.memory
	Op   string          `json:"op"`   // add, remove, change
	Old  json.RawMessage `json:"old,omitempty"` // never a Secret or ConfigMap value
	New  json.RawMessage `json:"new,omitempty"`
}

type Gap struct {
	Cluster, Resource string
	From, To          time.Time
	Reason            string // relist, unreachable, stopped, subscriber_lagged, opscope_stopped
}

// Store is what engines use; backends implement it.
type Store interface {
	AppendChanges(ctx context.Context, cs []Change, versions []Version) error
	Changes(ctx context.Context, q ChangeQuery) ([]Change, Cursor, error)
	Version(ctx context.Context, cluster, uid string, n int) (Version, error)
	LatestVersions(ctx context.Context, cluster string) (iter.Seq[Version], error) // for reconciling
	AppendGaps(…); Gaps(…)
	AppendTransitions(…); Transitions(…)
	AppendOccurrences(…); Occurrences(…)
	Retain(ctx context.Context, p Policy) (Deleted, error)
	Status(ctx context.Context) Status // backend, size, since, schema, persistent
	Close() error
}
```

The exact methods are settled in C1 with the conformance suite. Every query that reads state takes
a time (contract 2). Evidence (contract 4): each change records the resourceVersion it came from
and the `managedFields` entries behind its actor.

### Schema (SQLite; Postgres in v1.8 is the same shape)

| Table         | Key                                | Holds                                               |
| ------------- | ---------------------------------- | --------------------------------------------------- |
| `clusters`    | cluster                            | fingerprint (`kube-system` UID), history start       |
| `changes`     | id; index (cluster, uid, seen), (cluster, seen), (cluster, namespace, seen) | op, version, actor, risk, summary, diff (JSON) |
| `versions`    | (cluster, uid, version)            | the normalized object, gzip-compressed               |
| `gaps`        | (cluster, from)                    | resource, to, reason                                 |
| `transitions` | (cluster, uid, at)                 | from, to, signals                                    |
| `occurrences` | (cluster, signal, opened)          | type, uid, severity, resolved, count, evidence       |
| `meta`        | key                                | schema version, heartbeat, the HMAC key's ID         |

### API

| Endpoint                                                         | Returns                                              |
| ---------------------------------------------------------------- | ---------------------------------------------------- |
| `GET /api/clusters/{id}/changes?namespace=&kind=&actor=&risk=&since=&until=&pods=&cursor=` | changes, grouped by rollout, with gaps in the range |
| `GET /api/clusters/{id}/changes/{change}`                        | one change with its full diff                        |
| `GET /api/clusters/{id}/history?resource=&namespace=&name=`      | one object's versions, transitions, gaps and LKG     |
| `GET /api/clusters/{id}/history/diff?resource=&namespace=&name=&from=&to=` | two versions' diff; `from=lkg`, `to=current` allowed |
| `GET /api/health`                                                | gains a `store` section: backend, size, history since, persistent or not |

Answers say what they couldn't see (`partial`: no Secret role, no store, live data off) and how far
back history goes (`historySince`). The stream sends `changes` events so the Changes page updates
without polling. All of it goes into `docs/api.md`.

### Deployment

- **The manifest gets a PersistentVolumeClaim `opscope-data`** (1 GiB to start; C3's numbers set
  it), mounted at `/data` in place of the `emptyDir`. The name is the one `docs/deployment.md`
  already tells people to create for saved clusters, so an existing claim is reused and saved
  clusters survive the upgrade.
- **The Deployment stays a Deployment, with `strategy: Recreate`.** A ReadWriteOnce volume can't be
  mounted by the old and the new pod at once, and SQLite wants one writer. The storage design's
  StatefulSet behaves the same with one replica, but switching kinds would leave the old Deployment
  running after `kubectl apply`; it can come with the Helm chart in v1.8. See
  [open question 2](#open-questions).
- **Clusters without a default StorageClass:** the claim stays Pending and so does the pod.
  `docs/deployment.md` says how to see that and how to switch back to an `emptyDir` (history then
  lasts until the pod is replaced, and the pages say "history since 10:42").
- **Docker:** the docs show `-v opscope-data:/data`. Without it, history lasts as long as the
  container.

## Phase C0: Plan

Goal: this record is reviewed before the work starts.

- [x] Record written on `docs/change-log`
- [ ] Reviewed, open questions answered and moved into Decisions

## Phase C1: Store

Goal: a tested store in `/data`, with nothing writing to it yet.

- [ ] `internal/store`: the interface, models and policy; `store/sqlite` with embedded migrations,
      WAL, the capped page cache and the batched writer
- [ ] `storetest`: the conformance suite (ordering, range queries, cursors, retention, a newer
      schema refused, a full disk)
- [ ] `OPSCOPE_STORE`; `/data` not writable and `off` both start without history and say so;
      `/api/health` reports the store
- [ ] The dependency's reason in `CONTRIBUTING.md`'s principles section, next to the client
      libraries
- [ ] Measured: binary and image size, the store's memory with an empty and a 100 MB database;
      changes per hour on multipass (read-only), as the storage design asked

## Phase C2: Diffs, actors and tags

Goal: given an old and a new object, Opscope can say what changed, who changed it and what kind of
change it was. Pure functions, nothing wired yet.

- [ ] Normalization: the noise rules per kind as a table
- [ ] The diff, with merge keys for containers, env, ports, volumes, mounts and tolerations, and a
      one-line summary for the common changes
- [ ] Actors from `managedFields` with the manager table; sources from Helm, Argo CD and Flux
      metadata
- [ ] Risk tags from field paths
- [ ] The transform keeps per-key ConfigMap hashes and, with the Secret role, keyed Secret hashes;
      the HMAC key is made once in `/data`
- [ ] Tests on recorded objects: `kubectl edit`, `kubectl apply`, `kubectl set image`,
      `kubectl scale`, `kubectl rollout restart`, Helm upgrade, Argo CD sync, HPA scaling, a
      reordered env list (no change), status-only updates (no change)

## Phase C3: The ledger

Goal: every change is recorded, with gaps and reconciliation, and nothing records Secret values.

- [ ] `changes.Ledger` per cluster, subscribed to the bus: changes, versions, deletions; gaps and
      the heartbeat; reconciliation after a gap, a relist, a lag and a restart
- [ ] Effective records for container starts; image digest changes on workloads
- [ ] Cluster fingerprints; removing a cluster deletes its history
- [ ] Retention by age and by size, hourly
- [ ] `test/scenarios/changes.yaml` and a script that makes each change with the right field
      manager (`kubectl --field-manager=helm`, …): a lowered memory limit, a bad image tag, a
      ConfigMap key edit followed by a restart, a broken selector, a scale-up, a Secret key added,
      a delete and recreate
- [ ] On the scenario cluster: every scenario's change is recorded with the right actor and tags;
      noise under one irrelevant entry per real change; Opscope stopped for ten minutes while
      changes are made, and they show up "between" with a gap
- [ ] The test that no Secret or ConfigMap value reaches the database, an answer or the log
- [ ] Measured: rows and bytes per 1,000 pods per day; retention defaults and the PVC size follow

## Phase C4: Health history and last known good

Goal: health and signals survive a restart, and every workload has an LKG when it has one.

- [ ] The ledger stores transitions and occurrences; `HealthChanged` carries signal IDs
- [ ] The Signal engine reads history back after a restart; open occurrences continue or close
      "between"
- [ ] `LKG(uid, at)` from versions and transitions; following pods to owners and ConfigMaps and
      Secrets to the workloads that use them
- [ ] The Issues page's Resolved tab reads the store
- [ ] Tests: a rollout that breaks after three minutes isn't an LKG; a restart doesn't lose or
      duplicate an occurrence; LKG is the same before and after a restart

## Phase C5: Changes page

Goal: the Changes page works on real clusters, live.

- [ ] `/changes` endpoints and `changes` events on the stream; `docs/api.md`
- [ ] `/kit`: the change row, rollout grouping, gap lines, actor and tag labels, in both themes, at
      375 px and wide
- [ ] `pages/Changes.jsx`: filters, time range, grouping, "Show pods", live updates, paging
- [ ] Sidebar entry and `g h`; "Changes" and "Changes in <namespace>" in the palette
- [ ] The page says what it couldn't see, and when it has no history and why

## Phase C6: History and last known good on detail pages

Goal: every detail page shows its object's history, and unhealthy workloads show what changed
since they last worked.

- [ ] `/history` endpoints
- [ ] `/kit`: the history timeline with health alongside, the side-by-side diff
- [ ] The History tab (`5`) on every detail page; deleted predecessors linked
- [ ] "Diff vs last known good" on unhealthy workloads, their pods and Issues rows; "Changed …
      ago by …" in the summary
- [ ] Palette commands on detail pages
- [ ] Browser: light and dark, keyboard only, the accessibility tree, 375 px and wide, two clusters
      connected, with and without the Secret role, with the store off

## Phase C7: Release

Goal: v1.7.0 is ready to tag.

- [ ] The manifest's PVC and `Recreate`; upgrading from v1.6 with and without an existing
      `opscope-data` claim, on the scenario cluster
- [ ] `docs/deployment.md`: storage and the PVC, Disk next to Memory, what is stored, retention
      settings, history without login, Docker's volume; Upgrading
- [ ] README features and screenshots; `CONTRIBUTING.md` (adding a noise rule, a manager, a risk
      tag)
- [ ] Bugs found on the way fixed, one commit each
- [ ] Every check CI runs: `make test`, `go vet`, `gofmt`, the frontend build, `make docker-build`
- [ ] Image tag in `deploy/kubernetes/opscope.yaml` and the manifest URLs set to v1.7.0
- [ ] Status line, design record table, release notes drafted (the tag is pushed by the maintainer)

## Not in this release

- **The Postgres backend, multiple replicas, advisory-lock leader election, the Helm chart:**
  v1.8, as the storage design plans.
- **Keyframes, `StateAt(T)`, Topology at a time, comparing two moments:** the time machine, v1.8.
- **Root causes** ("it broke because the limit was lowered"): diagnosis, v1.10. This release says
  what changed, not whether that's why something broke.
- **ConfigMap values in diffs:** see open question 3.
- **Who, as a person:** that needs the API server's audit log.
- **History of Events and EndpointSlices:** see [What the ledger records](#what-the-ledger-records).
- **Summaries of old history:** see [Retention](#retention).
- **Rollback, or a command to restore a version:** never. Opscope doesn't offer commands that change
  a cluster, not even as text to copy. Fix guidance in v1.11 says where to make the change.
- **Prometheus usage trends and notifications:** their own records, later.

## Open questions

1. **SQLite.** The storage design picked embedded SQLite over bbolt and plain files, and the engine
   plan left the choice to this record. Proposed: SQLite (`modernc.org/sqlite`), with the reason
   and the measured 4.4 MB above. Is that cost acceptable?
2. **The manifest.** Proposed: a PVC named `opscope-data` (reusing the documented name), 1 GiB to
   start, a Deployment with `strategy: Recreate`, and the StatefulSet left for the Helm chart in
   v1.8. Or switch to a StatefulSet now?
3. **ConfigMap values.** Proposed: per-key hashes only, so a diff says which keys changed but not
   from what to what. Storing values would make "was / now" possible for config in v1.11, at the
   cost of keeping them in the cache and the store. Hashes only for now?
4. **Retention defaults.** Proposed: changes and versions 14 days, pod records 3 days, health,
   signals and gaps 30 days, capped at 80% of the volume. C3 checks them against measured growth.
5. **What counts as last known good.** Proposed: the newest version that was healthy for five
   minutes in a row while current.
6. **Removing a cluster.** Proposed: its history is deleted with it, and the dialog says so. The
   alternative is keeping it until retention removes it, which keeps data nobody can see.
7. **Pods on the Changes page.** Proposed: grouped under their workload's change and hidden as rows
   of their own unless "Show pods" is on.

## Phase notes

### C0: Plan (2026-11-23)

- Written from the engine release plan (v1.7: Change log), the storage design (v1.7: the `Store`
  interface, embedded SQLite, schema, conformance tests), and what v1.4 to v1.6 left for this
  release: `ObjectChanged` with `Old`, `New` and `Initial`; `CollectorGap`; `managedFields` kept
  without `fieldsV1`; ConfigMaps hashed and Secrets without values in the cache; health
  transitions in memory with signal IDs. Branched from `main` at `01638e7` (v1.6.0 and its
  follow-ups).
- No open issues after v1.6.0, so there's no backlog to clear first. The only open pull request is
  Dependabot's update of `docker/login-action` (#21).
- Measured the store's binary cost: a stripped `linux/arm64` build of the server is 34.2 MB; a
  program with only `modernc.org/sqlite` v1.63.0 is 6.2 MB against 1.8 MB without it, so about
  4.4 MB.
- Changes per hour on a real cluster, which the storage design asked for, couldn't be measured
  today: multipass wasn't reachable. It moved into C1.
- The plan's actor class "human" is named `person` here, and the plan's risk tags gained `ports`,
  `scheduling`, `storage`, `routing`, `restart` and `lifecycle` (see Decisions).
