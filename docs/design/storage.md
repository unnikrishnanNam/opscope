# Opscope: Storage

Opscope shows workloads, config and networking, but nothing about storage: when a pod is stuck in
`Pending` because its claim can't bind, or a volume was left `Released` after its claim was deleted,
you still have to reach for kubectl. This record adds a **Storage** section with
PersistentVolumeClaims, PersistentVolumes and StorageClasses, links them to each other and to the
pods that use them, counts storage problems on the overview, and, when you turn it on, shows how
full each volume is.

It stays read-only, like the rest of Opscope: new kinds need only `get` and `list`. Volume usage is
opt-in, because it needs one extra permission (see Decisions). Existing pages keep working as they
do; the pod and StatefulSet detail pages gain a volumes section.

Phases are numbered **S0–S6** so they don't mix with the build phases 0–8, the redesign phases
R0–R8, the command palette phases P0–P6, the repositioning phases D0–D6 or the earlier topology
phases G0–G7. All work happens on the `feature/storage` branch, one commit per phase, and the next
phase starts only after the previous one is reviewed.

**Status (2026-10-29):** S0–S6 done, for the next release.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

- **Three kinds first.** PersistentVolumeClaims (namespaced), PersistentVolumes and StorageClasses
  (cluster-wide). They are built into every cluster, have typed clients in client-go, and cover
  nearly every storage question an operator asks. CSIDrivers, VolumeAttachments and
  VolumeSnapshots are left for a later release (see Later).
- **Typed client, like the other built-in kinds.** PVCs and PVs are in `core/v1`, StorageClasses in
  `storage.k8s.io/v1`. No new dependency. Detail views keep using the dynamic client, as every kind
  does today.
- **A new sidebar group, "Storage",** after Network: PersistentVolumeClaims, PersistentVolumes,
  StorageClasses. PVCs come first because they're the kind people look at most and the only
  namespaced one.
- **Statuses use the existing badge colours.** PVC: `Bound` ok, `Pending` warn, `Lost` bad.
  PV: `Bound` ok, `Available` neutral (ready but unused), `Released` warn (its claim is gone and the
  data is still there), `Failed` bad. A PVC or PV that's being deleted shows `Terminating`, as pods
  do.
- **Volume usage is opt-in.** How full a volume is isn't stored on any Kubernetes object; only the
  kubelet on each node knows, through its stats summary. Opscope reads it through the API server
  (`/api/v1/nodes/{node}/proxy/stats/summary`), which needs `get` on `nodes/proxy`. That permission
  is broader than its name suggests: it reaches the whole kubelet API, including running commands
  in pods. So usage is off by default and turned on in two deliberate steps, the same way Secret
  access is:
  - `OPSCOPE_VOLUME_USAGE=true` tells Opscope to read usage at all. Without it, Opscope never calls
    the kubelet, whatever the credentials allow.
  - In a cluster, the optional `deploy/kubernetes/volume-usage.yaml` grants the permission to
    Opscope's service account. With a kubeconfig, the user's own credentials decide.

  The docs and the UI explain this plainly, as a trade-off to choose, not a warning: what you get
  (used and free space per claim), what the permission allows, and how to turn it off again. When
  usage is off, the pages show capacity only, with a short note on how to turn usage on.
- **Through the API server, not straight to the kubelet.** Calling each kubelet directly would need
  only the narrower `nodes/stats`, but Opscope would have to reach every node on port 10250 and trust
  each kubelet's certificate, which often isn't possible from outside the cluster. The API server
  route works the same in a cluster and from a laptop. This can be revisited if the narrower
  permission matters more later.
- **No keyboard shortcut** for the storage pages; the command palette finds them (`pvc`, `pv`, `sc`).
- **Links between objects go through the existing detail data.** Today fields and tables are plain
  text, and only owners are links. S3 adds an optional link to a field and to a table cell, so a
  PVC can link to its PV and StorageClass, and a pod's volume to its claim. Other kinds can use it
  later.

## Design

### Rows

```go
// PersistentVolumeClaim is one row of the PVC list.
type PersistentVolumeClaim struct {
	Meta
	Status       string   `json:"status"`       // Bound, Pending, Lost or Terminating
	Volume       string   `json:"volume"`       // the bound PV, "" while pending
	Capacity     int64    `json:"capacity"`     // provisioned bytes; the request while pending
	Requested    int64    `json:"requested"`    // bytes asked for in spec.resources.requests
	AccessModes  []string `json:"accessModes"`  // short form, like kubectl: RWO, ROX, RWX, RWOP
	StorageClass string   `json:"storageClass"` // "" means none set
	VolumeMode   string   `json:"volumeMode"`   // Filesystem or Block
}

// PersistentVolume is one row of the PV list (cluster-wide).
type PersistentVolume struct {
	Meta
	Status        string   `json:"status"`        // Available, Bound, Released, Failed or Terminating
	Capacity      int64    `json:"capacity"`      // bytes
	AccessModes   []string `json:"accessModes"`
	ReclaimPolicy string   `json:"reclaimPolicy"` // Delete, Retain or Recycle
	Claim         string   `json:"claim"`         // "namespace/name" of the bound claim, if any
	StorageClass  string   `json:"storageClass"`
	Source        string   `json:"source"`        // e.g. "CSI: ebs.csi.aws.com", "HostPath", "NFS"
	Message       string   `json:"message"`       // why it's Failed (status.message, else reason), shown on hover
}

// StorageClass is one row of the StorageClass list (cluster-wide).
type StorageClass struct {
	Meta
	Provisioner    string `json:"provisioner"`
	ReclaimPolicy  string `json:"reclaimPolicy"`
	BindingMode    string `json:"bindingMode"` // Immediate or WaitForFirstConsumer
	AllowExpansion bool   `json:"allowExpansion"`
	Default        bool   `json:"default"` // the is-default-class annotation
}
```

### API

The existing endpoints, with three new resource names: `persistentvolumeclaims`,
`persistentvolumes`, `storageclasses` (the plural names the Kubernetes API uses, as for the other
kinds). Nothing else changes in the API shape until S3 adds links to detail fields and tables, and S5 adds
usage to PVC rows:

```go
// On PersistentVolumeClaim, filled in only when volume usage is on and readable:
Usage *VolumeUsage `json:"usage"` // null when off, not mounted, or not readable

type VolumeUsage struct {
	Used       int64 `json:"used"`       // bytes in use on the filesystem
	Capacity   int64 `json:"capacity"`   // filesystem size, which can differ a little from the claim
	Available  int64 `json:"available"`
	InodesUsed int64 `json:"inodesUsed"` // a volume can be "full" on inodes with space left
	Inodes     int64 `json:"inodes"`
}
```

Whether usage is on, and whether it could be read, is reported once in the list response's
headers or a small field the page reads (decided in S5), so the page can show the right note.

```
GET /api/clusters/{id}/persistentvolumeclaims?namespace=...
GET /api/clusters/{id}/persistentvolumes
GET /api/clusters/{id}/storageclasses
GET /api/clusters/{id}/{resource}/{namespace}/{name}   (detail, unchanged)
```

### Pages

| Page | Columns |
| --- | --- |
| PersistentVolumeClaims | Name, Namespace, Status, Volume, Capacity, Used (with usage on), Access modes, StorageClass, Age |
| PersistentVolumes | Name, Status, Claim, Capacity, Access modes, Reclaim policy, StorageClass, Source, Age |
| StorageClasses | Name (with a "default" tag), Provisioner, Reclaim policy, Binding mode, Expansion, Age |

Detail pages:

- **PVC:** status, volume (link), StorageClass (link), requested and provisioned capacity, access
  modes, volume mode, used and free space and inodes (with usage on), and **Used by**: the pods
  that mount the claim (links). Conditions (like
  `FileSystemResizePending`) and events come from the generic detail code.
- **PV:** status (and reason), claim (link), StorageClass (link), capacity, access modes, reclaim
  policy, volume mode, the source with its details (CSI driver and volume handle, NFS server and
  path, host path, ...), node affinity (for local volumes, which node the data is on).
- **StorageClass:** provisioner, default or not, reclaim policy, binding mode, expansion, mount
  options, parameters (a table), and how many PVs and PVCs use it.
- **Pod (new section):** a **Volumes** table: name, type (PVC, ConfigMap, Secret, emptyDir, ...),
  what it points to (linked when Opscope has a page for it), and where each container mounts it.
- **StatefulSet (new section):** its volume claim templates, and the PVCs created from them.

### Overview

A **Storage** row on the overview, like the Gateway API one: PVC count with how many aren't `Bound`,
and PV count with how many are `Released` or `Failed`. It follows the namespace filter for PVCs; PVs
are cluster-wide, like nodes. With usage on, it also counts claims that are nearly full (90% or
more of space or inodes).

## Phase S0: Plan

Goal: this record, reviewed.

- [x] Branch `feature/storage` from `main`
- [x] Write this design record and list it in `docs/design/README.md`
- [x] Review, answer the open questions, commit

## Phase S1: Listers and RBAC

Goal: the API lists PVCs, PVs and StorageClasses, and their detail endpoints work.

- [x] `storage.go`: `listPersistentVolumeClaims`, `listPersistentVolumes`, `listStorageClasses`,
      registered in `Listers` and in the detail `kinds`
- [x] Derived values in Go: statuses (including `Terminating`), short access modes, the PV source
      text, the default StorageClass
- [x] RBAC in `deploy/kubernetes/opscope.yaml`: `persistentvolumeclaims`, `persistentvolumes`, and
      `storage.k8s.io` `storageclasses`
- [x] Go tests with client-go's fake clientset: each status, a pending claim with no volume, a PV of
      each common source, the default annotation (both the current and the old beta key)
- [x] The command palette index picks the new kinds up automatically (check it does)
- [x] Checked with curl against the test cluster

## Phase S2: Storage pages

Goal: the three list pages work in the UI, with plain detail pages.

- [x] Sidebar group "Storage" in `sections.js`, with kubectl short names as aliases (`pvc`, `pv`,
      `sc`) and new icons
- [~] Columns in `columns.jsx`; capacity with ~~the existing `bytes` formatter~~ a new `size`
      formatter (see notes); the "default" tag on StorageClasses; the PV message on hover over a
      `Failed` badge
- [x] Badge tones for `Bound`, `Available`, `Released` and `Lost`
- [x] kubectl commands on detail pages use the right names
- [x] Frontend tests for anything with logic (status tones, formatting)
- [x] Empty states read sensibly (a cluster with no PVs is normal)

## Phase S3: Detail sections and links

Goal: storage objects explain themselves and link to each other.

- [x] Optional `resource`/`namespace`/`name` link on a detail `Field` and on a table cell, rendered
      as a link by the detail page; plain text stays the default
- [x] PVC, PV and StorageClass sections as described above
- [x] PVC "Used by": pods in the claim's namespace that mount it
- [x] Pod: Volumes table, with mounts per container
- [x] StatefulSet: volume claim templates and the PVCs made from them
- [x] Go tests for each section

## Phase S4: Overview

Goal: storage problems show up on the cluster's front page.

- [x] `Storage` in the overview response: PVCs (not bound) and PVs (released or failed)
- [~] ~~Overview card~~ A "Storage" group in the Resources card that links to the list pages,
      hidden when the cluster has no PVCs and no PVs (see notes)
- [x] Tests for the counts

## Phase S5: Volume usage (opt-in)

Goal: with usage turned on, PVC pages show how full each volume is; with it off, nothing calls the
kubelet and the pages say how to turn it on.

- [x] `OPSCOPE_VOLUME_USAGE` setting, off by default, documented with the other settings
- [x] Read `stats/summary` only from the nodes that run pods mounting claims in the current scope,
      in parallel, with a short timeout per node and a short cache, so a big cluster isn't asked
      for every node on every refresh
- [x] Match volumes to claims through the summary's `pvcRef` (namespace and name)
- [x] A node that can't be read (forbidden, timeout, node down) leaves its claims without usage
      and doesn't fail the list; a forbidden answer is reported so the page can explain it
- [~] PVC list: "Used" column with the existing `UsageBar` ("2.1 GiB used, 7.7 GiB free of 10 GiB"
      on hover), from ~~`usage` on each row~~ a separate endpoint (see notes)
- [x] PVC detail: used, free, and inodes; PV detail shows the same when bound
- [~] Overview: claims nearly full (space or inodes), as a "Measured claims" row (see notes)
- [x] Friendly notes: usage off (how to turn it on, and what it means), on but not permitted (which
      permission is missing), claim not mounted (usage is only known while a pod uses it), and,
      added, claim mounted but not measured (see notes)
- [x] `deploy/kubernetes/volume-usage.yaml`, the optional ClusterRole and binding for `nodes/proxy`
      `get`, with a header comment in the same tone as `secrets-access.yaml`
- [x] Go tests with a stubbed stats source: matching, partial failures, forbidden, cache

## Phase S6: Test scenarios, docs and screenshots

Goal: checked against real failure cases, documented and ready to release.

- [x] Test scenarios on a kind cluster: a bound claim, a claim pending on `WaitForFirstConsumer`, a
      claim pending on a StorageClass that doesn't exist, a `Released` PV with `Retain`, a
      StatefulSet with claim templates, a claim being deleted while in use (`Terminating`), a volume
      filled close to full (usage on), and usage on without the permission; added: a node whose
      kubelet can't be reached. Kept in `test/scenarios/` (see notes)
- [x] Check against the multipass cluster too, read-only: only whatever storage it has now (it was
      rebuilt, so it may have no StorageClasses at all, which is a useful empty case)
- [x] README feature list, `docs/api.md` (new resource names, usage), `docs/deployment.md` (RBAC,
      `volume-usage.yaml`, `OPSCOPE_VOLUME_USAGE`, and what the permission allows)
- [x] Screenshots for the README and `docs/screenshots`
- [x] Phase notes, status line, design record table

## Later

Not in this release; each would follow the same pattern as S1–S3:

- **CSIDrivers:** which CSI drivers are installed, and what they support.
- **VolumeAttachments:** which volume is attached to which node, and attach errors (useful when a
  pod is stuck in `ContainerCreating`).
- **VolumeSnapshots and VolumeSnapshotClasses:** custom resources, read with the dynamic client and
  shown only when installed, like Gateway API.

## Open questions

None at the moment. Answered in review (2026-10-28): the extra kinds move to a later release, volume
usage is in as an opt-in (S5), no keyboard shortcut, and both test clusters are used (multipass
read-only).

## Phase notes

### S0: Plan (2026-10-28)

- Branched from `main` at `d019e61`, after topology was taken out of `main`; nothing here depends on
  it.
- First review moved CSIDrivers, VolumeAttachments and VolumeSnapshots to Later, added volume usage
  as an opt-in phase (S5), dropped the keyboard shortcut, and added the multipass cluster to S6.

### S1: Listers and RBAC (2026-10-28)

- `backend/internal/resources/storage.go` has the three listers; they're registered in `Listers`
  (so the API, the empty-cluster test and the command palette index pick them up) and in the detail
  `kinds` (so the generic detail view, YAML and events work for them already).
- Changed from the plan: the PV row has `message` instead of `reason`, holding `status.message` and
  falling back to `status.reason`. The message is the part that says what went wrong, and it matches
  the `message` Gateway API rows already show on hover.
- A PVC's capacity is the provisioned size from `status.capacity`, and the request while it's
  pending (kubectl leaves it blank then). Both are in the row, since a provisioner can round up.
- The storage class follows Kubernetes' own rule: the old `volume.beta.kubernetes.io/storage-class`
  annotation, when present, wins over `storageClassName`. Same for PVs.
- The PV source is the name of whichever source field is set (`HostPath`, `NFS`, `Local`, ...),
  found with reflection so new or rare sources still get a name; CSI volumes add their driver.
- `docs/deployment.md`'s permission table got the two new rows now, so it matches the manifest.
  The rest of the docs is still S6.
- Tests: `storage_test.go` covers bound, pending and terminating claims, the beta annotation,
  Released / Failed / Available volumes with CSI, HostPath and NFS sources, the reason fallback,
  both default-class annotations and the API defaults, and the detail view for all three kinds. The
  command palette index test now includes a PVC and a StorageClass.
- Checked against the rebuilt multipass cluster (v1.37.1, read-only): it has a `local-path` default
  StorageClass (`WaitForFirstConsumer`) and two bound claims in `ecommerce` with their PVs. All three
  lists, the namespace filter, the detail views and the name index match `kubectl get`.
  `local-path` volumes show as `HostPath`. `data/multipass.kubeconfig` still points at the cluster's
  old address, so the check used a fresh export of the `multipass` context.

### S2: Storage pages (2026-10-29)

- The Storage group is driven entirely by `sections.js`: the sidebar, routes, command palette pages,
  kubectl lines and detail URLs all follow from the three entries. Nothing else in the frontend
  keeps its own list of kinds.
- Icons from Lucide v1.51.0, like the others: `hard-drive-download` for claims, `hard-drive` for
  volumes, `layers` for StorageClasses.
- Changed from the plan: capacity uses a new `size()` formatter, which is `bytes()` without a
  trailing ".0", so a 1Gi claim reads "1 GiB" rather than "1.0 GiB". `bytes()` itself is unchanged,
  so memory figures elsewhere look the same as before.
- A pending claim shows its requested size, muted, with "Requested; no volume yet" on hover, so it
  isn't mistaken for provisioned storage. When a provider rounds up, the request is on hover.
- Hover text explains the terse values: each access mode (`RWO` → "ReadWriteOnce: read-write from
  one node"), the reclaim policy and the binding mode. A PV's `message` is on its status badge.
- Badge tones: `Bound` ok, `Released` warn, `Lost` bad; `Available` stays neutral; `Pending`,
  `Terminating` and `Failed` already had theirs.
- Aliases: `pvc`/`claim`, `pv`/`volume`, `sc`, plus the singular names.
- Tests: `columns.test.js` (every sidebar page with a table has columns, access mode text, storage
  status tones) and `format.test.js` (`size`). 75 frontend tests pass, and the build is clean.
- Checked in the browser against multipass at 1440×900, dark theme: all three lists, the Default
  tag, a PV detail page (kubectl lines without `-n`) and a PVC one (`kubectl -n ecommerce get
  persistentvolumeclaims redis-data -o yaml`), `pvc` in the command palette, the empty state for a
  namespace with no claims ("No persistentvolumeclaims in default."), the sidebar scrolling to the
  new group, and no console errors. Pending, Released and Lost badges couldn't be seen on multipass
  (all claims are bound, and it's read-only); they're covered by tests and come back in the S6 kind
  scenarios.
- Seen, not changed: the count above every table uses the page's plural noun, so one object reads
  "1 storageclasses". That's how every page already works (for example "1 gatewayclasses"), so it's
  left for a separate fix rather than changed only for storage.

### S3: Detail sections and links (2026-10-29)

- **Links.** A detail `Field` has an optional `link` (`resource`, `namespace`, `name`). A `Table`
  has an optional `links` grid parallel to `rows`, only present when some cell links, so every
  existing table keeps its plain `[][]string` shape and nothing else had to change. Go code adds
  rows with `addRow(cells...)`, which keeps the grid aligned. The detail page renders both through
  one small `ObjectLink`; plain cells stay strings, so empty cells still show their dash.
- **Related objects** are a separate step, `addRelated`, after the kind's own sections, because
  they need other API calls: the pods using a claim, the claims of a StatefulSet, how many volumes
  and claims use a StorageClass, and whether a named StorageClass exists. If listing those is
  forbidden, the section is left out instead of failing the page.
- Added beyond the plan, after seeing it on kind: a claim or volume that names a **StorageClass that
  doesn't exist** shows `does-not-exist (not found)`, without a link. That's exactly why such a claim
  stays Pending, and the link would only have led to an error page. Static volumes often name a
  class that's never created (like `manual`), and read the same way.
- A **Released** volume shows its old claim as `ns/name (no longer bound)`, without a link, since the
  claim is gone. Bound volumes link to their claim.
- **Source settings** on a volume are read generically (the source as YAML would show it, flattened
  to `key.subkey` rows, empty values left out), so CSI, NFS, HostPath, local and the rarer types
  all get their details without a case per type. Node affinity is one line, e.g.
  `kubernetes.io/hostname in (kubeworker1)`.
- **Pod volumes:** type, what it points to (claims, ConfigMaps and Secrets link; generic ephemeral
  volumes link to their `<pod>-<volume>` claim), and every mount per container with subPath and
  read-only.
- **StatefulSets:** claim templates (a template without a class shows `(default)`) and the claims
  made from them, matched by `<template>-<statefulset>-<ordinal>` like the controller does.
- Tests: `storage_detail_test.go` covers the claim page (fields, links, "Used by" only for its own
  namespace), a missing class, forbidden pod access, bound and Released volumes with source settings
  and node affinity, StorageClass counts and parameters, pod volumes and mounts, StatefulSet claims
  (ignoring look-alike names), `addRow` alignment and `sizeText`. Go and frontend tests pass.
- **Checked in the browser** against both clusters:
  - multipass (read-only, v1.37.1): `postgres-data` links to its volume, `local-path` and the pod
    `postgres-0`; the volume shows its HostPath settings and node affinity; `local-path` counts 2
    volumes and 2 claims; `postgres-0` lists its claim and two emptyDirs.
  - A throwaway kind cluster, `opscope-storage` (v1.35.0), with scenarios in the `storage-test`
    namespace: a StatefulSet with two template claims, a pod with an ephemeral volume, ConfigMap,
    Secret, memory emptyDir and the projected service account volume, a claim waiting for a pod,
    a claim on a missing class, and a Released `Retain` volume. Pending and Released badges show in
    amber on the lists, which S2 couldn't check.
  - Light and dark theme; links look like the existing "Owned by" links.
- Seen, not changed: in narrow windows (around 800 px) short words in detail tables can break
  mid-word ("Ephem/eral"), because detail tables use `overflow-wrap: anywhere` so long paths don't
  widen the page. That's shared by every detail table (container ports do it too), so it's left
  for a separate look rather than changed here.

### S4: Overview (2026-10-29)

- The overview response has `storage`, two counts in the same shape as `workloads` and
  `gatewayAPI`: claims not `Bound` (Pending, Lost or Terminating) and volumes `Released` or
  `Failed`. Claims follow the namespace filter; volumes are cluster-wide, like nodes, and the
  namespace note on the overview now says so.
- Changed from the plan: not a separate card, but a **Storage** group inside the Resources card,
  under Workloads and Gateway API. It reuses the same rows (count, bar, what needs a look), which is
  what the Design section described ("like the Gateway API one"). The rows link to the list pages
  and keep the selected namespace; the list pages have no status filter, so "filtered" means the
  namespace only.
- `storage` is `null`, and the group hidden, when the cluster has no claims and no volumes, and also
  when Opscope isn't allowed to list them. Storage is new, so credentials that could read the
  overview before may not have the permission; they keep the overview instead of getting an error.
- The health chip next to the cluster's name counts storage problems too, e.g.
  "2 PersistentVolumeClaims not bound · 1 PersistentVolume released or failed".
- Tests: `storage_test.go` covers the counts for each status, the namespace filter (claims only),
  a cluster with no storage, and a forbidden volume list. Go and frontend tests pass.
- Checked in the browser:
  - kind (`opscope-storage`): 5 claims with 2 not bound, 4 volumes with 1 released, matching
    `kubectl get`; the chip reads "3 need attention"; with `default` picked, claims show 0 and
    volumes still 4; the claims row opens the claim list.
  - multipass (read-only): 2 claims "All bound", 2 volumes "None released or failed".
  - Light and dark theme, and 820 px wide, where the longest text still fits on one line.

### S5: Volume usage (2026-10-29)

- **Changed from the plan: a separate endpoint**, `GET /api/clusters/{id}/metrics/volumes?namespace=`,
  instead of a `usage` field on PVC rows. It's built like the metrics-server endpoints, which the
  pages already read next to the lists: the PVC list stays exactly as it was, and "off" and "not
  allowed" are answered with codes (`volume_usage_off` 404, `volume_usage_forbidden` 403), which
  settles the open point of how the page learns either. With the setting off, the handler answers
  before even looking up the cluster, so nothing can reach a kubelet. The answer is
  `{volumes, unread, unmeasured}`: claims with numbers, nodes that couldn't be read (with their
  claims), and claims that are mounted but have no numbers.
- **The reader** lives in `internal/metrics/volumes.go`. It lists the pods in scope, takes the nodes
  of running pods that mount claims (generic ephemeral volumes included), and asks those nodes at
  once, each with a 5-second timeout. Each node's answer, failures included, is kept for 30
  seconds per cluster, so a node that's down doesn't slow every refresh; the kubelet itself only
  measures about once a minute. One forbidden node answers for all, since RBAC is cluster-wide.
- **Found while checking: the kubelet doesn't measure hostPath volumes.** Both test clusters use
  the local-path provisioner, whose volumes are hostPath folders, and their kubelets report no
  numbers for any claim. The first version said "Not mounted" for those, which was wrong. The
  backend now also returns `unmeasured` (mounted on a node that answered, but no numbers), shown as
  "Not measured", with the reason on hover and on the detail page. The deployment docs say which
  volumes are measured (CSI and `local`, not hostPath).
- **Overview:** a "Measured claims" row in the Storage group, counting claims with numbers and how
  many are 90% full or more (space or inodes), also counted in the health chip. It's left out when
  nothing is measured, rather than saying "none nearly full" about claims it knows nothing about.
  An earlier "Mounted claims" version counted unmeasured claims and did exactly that on multipass.
- **Fullness** is measured from what's still available (`1 - available / capacity`), not from used:
  filesystems keep some space back, so "used" alone understates how close a pod is to "disk full".
- The PVC list's "Used" column is left out entirely when usage is off or not allowed, rather than a
  column of dashes; the note above the table says why. Block volumes show "Block", pending claims a
  dash, and claims on a node that couldn't be read a dash with the node's error on hover, plus a
  note above the table.
- The refused node-proxy call came back as just "unknown": the typed client expects protobuf and
  doesn't decode the API server's JSON answer. `fetchSummary` decodes it, so the details read like
  kubectl's ("User ... cannot get resource nodes/proxy ...").
- `opscope.yaml` has the setting as commented lines. `docs/deployment.md` now says to apply
  `opscope.yaml` alone from a clone, because applying the whole folder also applies both optional
  files. It also has a "Volume usage" section: what it shows, why it's off by default (what
  `nodes/proxy` allows), the two steps to turn it on and how to turn it off. The rest of the docs is
  S6.
- Tests: `volumes_test.go` covers which nodes are asked, namespace filtering, claims on several
  nodes, volumes without claims or numbers, unread and unmeasured claims, the per-cluster cache and
  its expiry, forbidden, pods not listable, a bad summary, and `fetchSummary` against a stand-in API
  server (path, refusal message). Server tests cover "off" and both forbidden cases;
  `volumeUsage.test.js` covers matching and fullness. Go (with `-race`) and frontend tests pass.
- **Checked in the browser:**
  - kind (`opscope-storage`), with new scenarios: a `local` PV on a 64 MiB tmpfs inside the node,
    filled to 92% by a pod. The list shows a red 92% bar; the claim and its volume show used, free,
    filesystem size and inodes; the overview shows "Measured claims 1, 1 nearly full", and the chip
    adds "1 PersistentVolumeClaim nearly full". The local-path claims read "Not measured".
  - Usage off: no "Used" column, the note above the list and on the claim page, and no row on the
    overview.
  - On without the permission: Opscope's own service account with only `opscope.yaml`'s ClusterRole
    (the same RBAC, applied to kind on its own), added as a second cluster. The pages show the
    `nodes/proxy` note, the rest works, and after applying `volume-usage.yaml` the numbers appear.
  - multipass (read-only; reading stats changes nothing): both claims "Not measured", and no row on
    the overview.
  - Light and dark theme, 900 px wide.

### S6: Test scenarios, docs and screenshots (2026-10-29)

- **Scenarios in the repository:** `test/scenarios/storage.yaml` and `storage-usage.yaml`, with a
  README that says how to run them on a throwaway kind cluster, the deletions that finish the setup,
  what each page should show, and how to check usage without the permission and with a node that
  can't be read. They're the S3 and S5 scenarios, plus a claim deleted while a pod still uses it.
  `CONTRIBUTING.md` points to them.
- **All scenarios checked** on `opscope-storage` (kind v1.35.0), usage on:
  - Terminating: `in-use`, deleted while `holder` uses it, shows Terminating with its pod under
    "Used by".
  - A node that can't be read: with the kind node's kubelet stopped for a minute, the claims list
    says which node couldn't be read, its claims show a dash with "connection refused" on hover, and
    the claim page says the same. The kubelet was started again and the node came back Ready.
  - The S3–S5 scenarios (template claims, pod volumes, waiting and missing-class claims, a Released
    volume, a nearly full volume, usage off, usage without the permission) still read as before.
- **Two fixes from those checks:**
  - The overview counted the Terminating claim as "not bound", but a Terminating claim is still
    bound. The count is now Pending and Lost only; Terminating keeps its own badge on the list. The
    S4 test has a Terminating claim to keep it that way.
  - A Terminating claim's page had no Usage section, although the list showed its usage state. The
    section now appears for a claim with a volume that's Bound or Terminating.
- **multipass** (read-only), last check: two bound claims and volumes on `local-path`, matching
  `kubectl get`; the claim pages link their volumes, class and pods; `postgres-0` lists its volumes;
  usage reads "Not measured" (hostPath); the command palette finds all three kinds.
- **Docs:**
  - README: storage in the features and the resource list, a security note on volume usage, the
    two new screenshots.
  - `docs/api.md`: the three resource names, `storage` in the overview, links in detail fields and
    tables, the volume usage endpoint, and its two codes.
  - `docs/deployment.md` already had the permissions, `volume-usage.yaml` and the setting from S5.
  - `CONTRIBUTING.md`: the new files in the layout.
- **Screenshots:** the claims list and a nearly full claim's page, light and dark
  (`docs/screenshots/claims-*.png`, `claim-*.png`), taken like the earlier ones: headless Chrome at
  1440 × 900 and 2× density, scaled to 1600 × 1000. They show a `shop` namespace made for them on
  the kind cluster: a StatefulSet's two claims, redis and media claims on `local` volumes on tmpfs
  filled to 41%, 66% and 93%, and a backup claim pending on a missing NFS class. The server ran
  from a `screenshots-storage` launch configuration on :8091, built as v1.3.0 like the existing
  `screenshots` one, with usage on and the cluster named "demo". The existing overview, pods and pod
  screenshots weren't retaken.
- The kind cluster `opscope-storage` is still there, for the review; delete it with
  `kind delete cluster --name opscope-storage` when it's no longer needed.

### Final check (2026-10-31)

A last round over everything, before opening the pull request, on multipass (read-only) and a kind
cluster rebuilt from `test/scenarios/` (deleted afterwards). Seven issues came up, all fixed:

- **Sorting with empty values:** descending order put rows without a value first, so sorting
  claims by Used, fullest first, started with every claim that has no number. Empty values now
  stay last both ways. This was in the shared table sort, so pod CPU and memory sort better too.
  The sort moved to `frontend/src/sort.js`, with tests.
- **Wide storage tables:** with real names, the claims and volumes lists were 18–28 px wider than
  their box at 1440 px, hiding Age. A claim's volume and a volume's claim are now cut at 22
  characters with an ellipsis, and the full name shows on hover.
- **Overview bars:** a row where everything needs attention still started with a 3 px green
  segment. The healthy segment is now left out when nothing is healthy (shared with the other
  overview rows).
- **Words broken mid-word in detail tables** ("Ephem/eral", "fille/r"): detail tables break only
  words too long for their cell, and a table that still doesn't fit scrolls sideways in its box,
  like the lists. Checked on storage, pod, container and service tables at 375, 900 and 1440 px.
- **Scenarios on a new cluster:** the first apply could be refused for the two pods while the
  namespace's `default` service account was being created. The README now applies in a retry loop
  and says why.
- **Polling while usage is off:** the pages asked every 15 or 30 seconds and got the same 404.
  `useApi` takes `stopOn` codes, and the usage requests stop at `volume_usage_off`, which only
  changes when Opscope restarts. Refresh still asks again.
- A comment in `columns.jsx` had ended up above the wrong function.
