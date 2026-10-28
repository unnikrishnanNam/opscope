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

**Status (2026-10-29):** S0–S2 done; S3 next.

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

- [ ] Optional `resource`/`namespace`/`name` link on a detail `Field` and on a table cell, rendered
      as a link by the detail page; plain text stays the default
- [ ] PVC, PV and StorageClass sections as described above
- [ ] PVC "Used by": pods in the claim's namespace that mount it
- [ ] Pod: Volumes table, with mounts per container
- [ ] StatefulSet: volume claim templates and the PVCs made from them
- [ ] Go tests for each section

## Phase S4: Overview

Goal: storage problems show up on the cluster's front page.

- [ ] `Storage` in the overview response: PVCs (not bound) and PVs (released or failed)
- [ ] Overview card that links to the filtered list pages, hidden when the cluster has no PVCs and
      no PVs
- [ ] Tests for the counts

## Phase S5: Volume usage (opt-in)

Goal: with usage turned on, PVC pages show how full each volume is; with it off, nothing calls the
kubelet and the pages say how to turn it on.

- [ ] `OPSCOPE_VOLUME_USAGE` setting, off by default, documented with the other settings
- [ ] Read `stats/summary` only from the nodes that run pods mounting claims in the current scope,
      in parallel, with a short timeout per node and a short cache, so a big cluster isn't asked
      for every node on every refresh
- [ ] Match volumes to claims through the summary's `pvcRef` (namespace and name)
- [ ] A node that can't be read (forbidden, timeout, node down) leaves its claims without usage
      and doesn't fail the list; a forbidden answer is reported so the page can explain it
- [ ] PVC list: "Used" column with the existing `UsageBar` ("2.1 GiB of 10 GiB" on hover)
- [ ] PVC detail: used, free, and inodes; PV detail shows the same when bound
- [ ] Overview: claims nearly full (space or inodes)
- [ ] Friendly notes: usage off (how to turn it on, and what it means), on but not permitted (which
      permission is missing), claim not mounted (usage is only known while a pod uses it)
- [ ] `deploy/kubernetes/volume-usage.yaml`, the optional ClusterRole and binding for `nodes/proxy`
      `get`, with a header comment in the same tone as `secrets-access.yaml`
- [ ] Go tests with a stubbed stats source: matching, partial failures, forbidden, cache

## Phase S6: Test scenarios, docs and screenshots

Goal: checked against real failure cases, documented and ready to release.

- [ ] Test scenarios on a kind cluster: a bound claim, a claim pending on `WaitForFirstConsumer`, a
      claim pending on a StorageClass that doesn't exist, a `Released` PV with `Retain`, a
      StatefulSet with claim templates, a claim being deleted while in use (`Terminating`), a volume
      filled close to full (usage on), and usage on without the permission
- [ ] Check against the multipass cluster too, read-only: only whatever storage it has now (it was
      rebuilt, so it may have no StorageClasses at all, which is a useful empty case)
- [ ] README feature list, `docs/api.md` (new resource names, usage), `docs/deployment.md` (RBAC,
      `volume-usage.yaml`, `OPSCOPE_VOLUME_USAGE`, and what the permission allows)
- [ ] Screenshots for the README and `docs/screenshots`
- [ ] Phase notes, status line, design record table

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
