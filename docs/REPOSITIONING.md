# Opscope: Repositioning

Opscope started as a learning project. It is now a lightweight, read-only tool for day-to-day
Kubernetes operations, and its documentation, project files and public descriptions should say so.
This plan makes that change visible everywhere Opscope describes itself: the README, the docs, the
app's own copy, the image metadata and the GitHub repository.

It changes no behaviour. The backend, the API, the manifests and every page work exactly as in
v1.2.0. New features, fixes and optimizations for the next release are planned separately.

Phases are numbered **D0–D6** so they don't mix with the build phases 0–8, the redesign phases
R0–R8 or the command palette phases P0–P6. All work happens on the `docs/repositioning` branch.

**Status (2026-10-09):** D0–D3 done; D4 next.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

- **Positioning.** "Opscope is a lightweight, read-only Kubernetes dashboard for day-to-day
  operations." Read-only is presented as the main feature: Opscope needs only `get` and `list`, and
  nothing in it can change a cluster, so it is safe to give to anyone who needs to look.
- **Licence: Apache-2.0**, the usual choice in the Kubernetes ecosystem. It includes a patent grant.
- **The three journals are archived, not rewritten.** `PHASES.md`, `UI-REDESIGN.md` and
  `COMMAND-PALETTE.md` move to `docs/design/` with their content unchanged and a short note at the
  top. They remain the record of how and why Opscope was built. This plan joins them when it's done.
- **User documentation and contributor documentation are separate.** The README is for people who
  run Opscope. Building, testing, extending and releasing it move to `CONTRIBUTING.md`, and the
  longer reference material to `docs/`.
- **GitHub Releases stay the changelog.** No `CHANGELOG.md`.
- **No version bump** in this work. The version is decided when the next release is planned.
- **The showcase video is not committed.** At 19 MB it would stay in the git history of every clone
  for good. It is uploaded to GitHub and embedded by URL (see D5). `docs/showcase/` is git-ignored.
- **The API is documented but not declared stable.** It exists to serve the web UI and may change
  between minor versions.
- **The roadmap lives in GitHub issues.** The ideas collected in the design records ("Possible next
  steps", "Possible later") become issues labelled `enhancement`, and the README links to that list
  instead of keeping its own.
- **No vulnerability reporting channel for now,** and so no `SECURITY.md`. The security model is
  described in the README and `docs/deployment.md`. A reporting channel can be added later.

---

## Writing style

Everything written or rewritten in this plan follows these rules. The archived journals are left
as they are.

- **Plain and precise.** Short sentences and concrete statements. No marketing language
  ("blazing fast", "powerful", "seamless").
- **Address the reader.** Instructions are in the second person ("Set `OPSCOPE_KUBECONFIG` to…").
  No "we", no notes addressed to the maintainer, and no learning-project framing.
- **Say what it does, then why.** Lead with the behaviour, then the reason, especially for security
  decisions.
- **Consistent terms:** Opscope (never OpScope), read-only, kubeconfig, metrics-server, Gateway API,
  command palette. British spelling in prose, as in the existing docs ("colour", "licence" as a noun),
  except in fixed names (`LICENSE`, `Apache-2.0`, Kubernetes field names).
- **Every page links onwards.** A short section points to the document with the full details
  rather than repeating them.

---

## Target layout

```
README.md                 for users: what it is, video, features, quick start, connecting clusters,
                          the palette, security model, configuration, screenshots, links
CONTRIBUTING.md           for contributors: repo layout, development, /kit, tests, adding commands,
                          CI and releases
LICENSE                   Apache-2.0
docs/
  deployment.md           Docker, kind networking, Kubernetes manifests, permissions, Secret access
  api.md                  the HTTP API reference
  design/                 design records, kept for history
    README.md             index of the records, and how to write a new one
    build-phases.md       was docs/PHASES.md
    ui-redesign.md        was docs/UI-REDESIGN.md
    command-palette.md    was docs/COMMAND-PALETTE.md
    repositioning.md      this plan, once done
  screenshots/            the README's pictures
.github/
  ISSUE_TEMPLATE/         bug report and feature request
  pull_request_template.md
```

---

## Workflow for each phase

1. Make the changes listed for the phase.
2. Check every relative link in the changed files resolves, and read the rendered Markdown.
3. When app copy changes (D3, D4): `make test` and `npm run build` pass, and the change is checked
   in the browser in light and dark.
4. Tick the boxes here and write the phase notes.
5. Review, then commit as "Phase Dn: …". Nothing is pushed or changed on GitHub without approval.

---

## Phase D0: Licence and project files

Goal: Opscope can legally be used, and the project files a user or contributor looks for exist.

- [x] `LICENSE`: the Apache-2.0 text, copyright Unnikrishnan Namboothiri
- [x] Third-party assets bundled in the app and their licences, listed for the README's licence
      section: Lucide icons (ISC, already noted in `icons.jsx`), Manrope and JetBrains Mono (SIL OFL 1.1)
- [x] `.github/ISSUE_TEMPLATE/`: a bug report (Opscope version, Kubernetes version, how it runs:
      Docker, in a cluster or from source; what happened and what was expected) and a feature request
- [x] `.github/pull_request_template.md`: what changed, how it was checked, docs updated
- [x] `.gitignore`: `docs/showcase/`

## Phase D1: Archive the design records

Goal: the journals are kept as history and stop presenting themselves as current documentation.

- [x] `git mv` the three journals to `docs/design/` (so their history follows them), with lowercase
      names as in the target layout
- [x] A note at the top of each: what it records, that it describes Opscope as it was built, and
      where the current documentation is
- [x] Links between them and to the README updated to the new paths
- [x] Code comments that cite phase names ("phase R3", "P0 and P1" in `frontend/src/kit/*` and
      `Field.jsx`) reworded to describe the code itself
- [x] No other changes to the journals' content

## Phase D2: Reference and contributor docs

Goal: everything that leaves the README has a proper home first, so nothing is lost in D4.

- [x] `docs/deployment.md`: running with Docker (published image and own build), kind networking,
      running in a cluster, the permissions table, turning Secret access off, probes and shutdown,
      keeping clusters across restarts (PersistentVolumeClaim)
- [x] `docs/api.md`: the endpoint table, resource names, error format, the two optional-feature codes,
      the background sampling; a note that the API serves the web UI and isn't stable yet
- [x] `CONTRIBUTING.md`: requirements, repo layout, running in development, `/kit`, tests
      (`make test`), code style (gofmt, plain CSS on tokens, no new runtime dependencies without
      reason), adding commands (the command shape and sources, from the palette's design record),
      CI, the release steps, and how changes are proposed (issues, pull requests, design docs)
- [x] A design doc template or short guidance in `docs/design/` for future features, in the
      writing style above

## Phase D3: App and image copy

Goal: the app, the image and the code describe Opscope the same way the docs do.

- [x] `frontend/index.html`: the `description` meta tag
- [x] `frontend/public/site.webmanifest`: a `description`
- [x] `frontend/src/pages/Welcome.jsx`: the introduction on the first-run screen
- [x] `backend/main.go`: the package comment
- [~] `.github/workflows/release.yml`: the image's `org.opencontainers.image.description` label;
      confirm the `licenses` label is set from the repository once GitHub detects the licence
- [x] Checked in the browser (welcome screen, light and dark, 375 px and wide); `make test` and the
      build pass

## Phase D4: README

Goal: a README that tells someone new what Opscope is, why it's safe, and how to run it in a minute.

- [ ] Title, tagline and badges: CI, latest release, image, licence
- [ ] A placeholder for the video (filled in D5) and a short paragraph on who Opscope is for and
      why it's read-only
- [ ] Features, grouped: overview and health, resources, detail pages (YAML, events, logs), live
      usage, command palette and shortcuts, multiple clusters, themes and small screens
- [ ] Quick start: one Docker command; one `kubectl apply` and port-forward; links to
      `docs/deployment.md`
- [ ] Connecting clusters; the command palette and shortcuts (the user-facing part)
- [ ] Security model: no login, read-only RBAC, Secret values only on request and never cached,
      logs never cached; the full details in `docs/deployment.md`
- [ ] Configuration (environment variables)
- [ ] Roadmap: GitHub issues for the ideas in the design records (opened with approval at the
      time), and a short README section linking to them
- [ ] Screenshots retaken with the current top bar (they predate the palette's search button), in
      light and dark, from the test clusters
- [ ] Documentation, Contributing and License sections, including the third-party assets from D0

## Phase D5: Showcase video

Goal: the video plays inline at the top of the README.

- [ ] The video is back in `docs/showcase/` (it is not tracked, so it may need adding again)
- [ ] Compressed with macOS's `avconvert` to 1280×720 and under 10 MB (GitHub's limit for videos on
      free plans); the audio track removed if it's silent
- [ ] Uploaded to GitHub by hand (dropped into an unsent comment box), giving a
      `github.com/user-attachments/assets/…` URL. GitHub's API can't upload these, so this step is manual
- [ ] The URL on its own line under the tagline, where GitHub renders it as a player, with a
      sentence for anyone whose viewer doesn't play video
- [ ] Checked on github.com once the branch is pushed

## Phase D6: GitHub repository and final review

Goal: the repository page matches the docs, and the branch is ready to merge.

- [ ] With approval at the time: the repository description (the tagline) and topics: `kubernetes`, `dashboard`, `read-only`, `k8s`,
      `devops`, `go`, `react`
- [ ] The social preview image uploaded (outstanding since R8; GitHub's web UI only)
- [ ] Unused repository features reviewed (the Wiki is on and empty)
- [ ] A last read of every changed file for tone, terms and broken links
- [ ] This plan moved to `docs/design/repositioning.md`, marked done, and listed in
      `docs/design/README.md`
- [ ] Pull request opened; merged after CI and review

---

## Open questions

None. The tagline, the roadmap in GitHub issues, leaving out a reporting channel for now and
retaking the screenshots were settled on 2026-10-08 and are recorded above. Changes to the GitHub
repository are approved one at a time in D4 and D6.

---

## Phase notes

Notes on each phase are added here as it's done.

### Phase D0

- `LICENSE` is the canonical Apache-2.0 text (byte-identical to apache.org's before editing), with
  the appendix's copyright line filled in: "Copyright 2026 Unnikrishnan Namboothiri". There is no
  `NOTICE` file; Apache-2.0 requires one only when a project chooses to have it.
- Third-party assets for the README's licence section (D4):

  | Asset                                     | Version          | Licence | Copyright                          |
  | ----------------------------------------- | ---------------- | ------- | ---------------------------------- |
  | Lucide icons (a subset, in `icons.jsx`)   | 1.51.0           | ISC     | Lucide Icons and Contributors      |
  | Manrope (variable)                        | Fontsource 5.3.0 | OFL-1.1 | The Manrope Project Authors        |
  | JetBrains Mono (variable)                 | Fontsource 5.3.0 | OFL-1.1 | The JetBrains Mono Project Authors |

- **Found:** the production build strips comments, so the Lucide notice in `icons.jsx` and the
  licence comments of bundled npm packages (React, React Router) don't reach `frontend/dist` or the
  image. The source repository carries every notice; the built app does not. Shipping a notices file
  with the app would be a behaviour change, so it's left out of this plan and noted for a later
  release.
- Issue forms (YAML) rather than Markdown templates, so the required fields are enforced. The bug
  report asks for the Opscope version (the sidebar shows it), the Kubernetes version (the cluster
  switcher shows it), how Opscope runs and the browser, and warns against pasting kubeconfigs,
  tokens or Secret values. The feature request states that changing cluster state is out of scope.
  Both use the repository's existing `bug` and `enhancement` labels. Blank issues stay allowed.
- The pull request template asks what changed, how it was checked, and confirms three things:
  `make test` passes, Opscope stays read-only, and the docs follow the change.
- `.gitignore` has `docs/showcase/`; the video is still on disk and now ignored rather than untracked.

### Phase D1

- The journals moved with `git mv`, so `git log --follow` still shows their history:
  `docs/PHASES.md` → `docs/design/build-phases.md`, `docs/UI-REDESIGN.md` → `docs/design/ui-redesign.md`,
  `docs/COMMAND-PALETTE.md` → `docs/design/command-palette.md`.
- Each starts with a "Design record" note: which phases it covers, when they were completed, which
  release they became (v1.0.0, v1.1.0, v1.2.0), that it describes Opscope as it was then, and a link
  to the README.
- Only clickable links were changed inside the journals (six, between the three of them). Plain
  mentions such as "README and PHASES.md updated" are part of what was recorded at the time and were
  left as written. Their links within the same file (`#possible-next-steps`, `#after-p6`,
  `#decisions`) are unaffected by the move.
- README: the three links now point to `docs/design/`, and its layout lists `docs/design/` as one
  entry. The surrounding text, including "learning project", is rewritten in D4.
- Code comments: the four `/kit` demos and `Field.jsx` no longer cite R1, R2, R3, P0–P3. The
  commands demo now says its sample sources are shaped like those in `commands/sources/`, and
  `Field.jsx` points to `Combobox.jsx`. "Phase" elsewhere in the code is the Kubernetes pod phase and
  stays.
- Checked: every relative link and anchor in the README, this plan and the three records resolves
  (a small script, not committed); 70 frontend tests pass and the build is unchanged.

### Phase D2

- Everything was checked against the code rather than copied from the README: routes in
  `server.go`, status codes, query parameters, response fields, limits (1 MiB request bodies, 500 and
  10,000 log lines, 100 events, 30-second log requests, 15-second sampling, 15 minutes of history),
  how the overview counts "unhealthy", and the kubeconfig rules. The raw manifest URLs in
  `deployment.md` were fetched to confirm they serve the v1.2.0 manifests.
- **`docs/api.md`** describes every endpoint with its parameters and response shape, the error
  format and status codes, the two optional-feature codes and the resource types. It states that the
  API serves the UI and isn't stable, that it has no authentication, and that the only endpoints that
  change anything (`POST` and `DELETE` on `/api/clusters`) change Opscope's own list of clusters,
  never a cluster.
- **`docs/deployment.md`** covers the image tags, Docker with the UI or a kubeconfig file, reaching
  the API server from a container and kind, building the image, the Kubernetes manifests (applied
  from a release tag or a clone), how the pod runs, keeping UI-added clusters with a
  PersistentVolumeClaim (a new example), permissions, Secret access, exposing Opscope safely,
  configuration and upgrading. New compared with the README: the image tags table, applying the
  manifests straight from a release tag, the PVC example, that a mounted kubeconfig must be readable
  by uid 65532 on Linux, that login commands in `OPSCOPE_KUBECONFIG` need the command in the image
  (it has none), and that a broken environment cluster stops Opscope at startup.
- **`CONTRIBUTING.md`** has the project's principles (read-only, safe by default, few dependencies,
  one image, no built-in cluster, fail clearly, from the build plan's guiding rules), issues and
  proposals, development setup, the repository layout, the checks CI runs, code style, step-by-step
  "Adding a resource type" (new) and "Adding commands" (from the README), documentation, the release
  steps and the licence. Two statements were corrected while checking: the backend depends on
  `sigs.k8s.io/yaml` as well as client-go, and component CSS uses many fixed sizes, so only colours
  are required to come from tokens.
- **`docs/design/README.md`** indexes the three records and explains when to write one, its
  structure (a template) and how phases are lettered. D6 adds this plan to its table.
- The README is unchanged in this phase; D4 replaces its long sections with links to these documents.
- Checked: every relative link and anchor in the nine Markdown files resolves.

### Phase D3

- The tagline, "A lightweight, read-only Kubernetes dashboard for day-to-day operations.", is now the
  page's `description` meta tag, the web manifest's new `description`, the image's
  `org.opencontainers.image.description` label and the package comment in `main.go`. The welcome
  screen opens with it and keeps its list of what Opscope shows.
- **Changed from the plan:** the published v1.2.0 image has an empty
  `org.opencontainers.image.licenses` label (checked in its config on GHCR): `metadata-action` takes
  it from the licence GitHub detects, and the repository had none. Rather than depend on that
  detection, `release.yml` now sets `org.opencontainers.image.licenses=Apache-2.0` explicitly. Both
  labels take effect with the next release; v1.2.0's image keeps its labels.
- "Small, read-only Kubernetes dashboard" remains only in the README (D4) and the archived design
  records.
- Checked with a new `built-empty` launch configuration: the production build served by the Go
  server on :8091 with an empty data folder and no cluster, so the welcome screen shows. The page
  description, the manifest and the welcome text were read from the page; the screen was looked at
  in light and dark at 1280 px and at 375 px (the introduction wraps to three lines wide and five on
  a phone, with no sideways scrolling). No server errors. `go vet`, `gofmt`, the Go tests and the
  70 frontend tests pass; the web manifest and `release.yml` parse.
