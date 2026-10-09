# Contributing to Opscope

Thank you for your interest in Opscope. Bug reports, feature requests and pull requests are all
welcome. This guide covers what Opscope will and won't do, how to set up a development environment,
and how changes are checked and released.

- [Principles](#principles)
- [Issues and proposals](#issues-and-proposals)
- [Development setup](#development-setup)
- [Repository layout](#repository-layout)
- [Tests and checks](#tests-and-checks)
- [Code style](#code-style)
- [Common changes](#common-changes)
- [Documentation](#documentation)
- [Releases](#releases)
- [License](#license)

## Principles

Every change keeps these properties. A pull request that would break one needs a discussion in an
issue first.

- **Read-only.** Opscope uses only `get` and `list`. It never creates, changes or deletes anything
  in a cluster, and it doesn't offer commands that would, not even as text to copy.
- **Safe by default.** Opscope has no login, so it listens on `127.0.0.1` in every example and is
  reached through a port-forward in a cluster. Secret values are fetched one key at a time and never
  cached; logs are never cached.
- **Few dependencies.** The Go standard library and the Kubernetes client libraries on the
  backend; React, React Router and two bundled fonts on the frontend, with plain CSS. A new runtime
  dependency needs a clear reason.
- **One image.** The Go server serves both the API (`/api/*`) and the built web UI.
- **No built-in cluster.** Clusters come from the environment, the pod's service account, or the UI.
  Opscope never reads `~/.kube/config` on its own.
- **Fail clearly.** A missing permission, an unreachable cluster or a missing optional feature
  (metrics-server, Gateway API) is explained on the page that needs it; the rest keeps working.

## Issues and proposals

- **Bugs:** open an issue with the bug report form. Leave out kubeconfigs, tokens and Secret values.
- **Features:** open an issue with the feature request form. The
  [`enhancement`](https://github.com/unnikrishnanNam/opscope/issues?q=is%3Aissue+is%3Aopen+label%3Aenhancement)
  issues are also Opscope's roadmap.
- **Pull requests:** small, focused changes are easiest to review. For anything larger than a fix,
  such as a new page or a change to how data is loaded, open an issue first to agree on the approach.
- **Larger features** are planned in a short design record in [`docs/design/`](docs/design/) before
  the work starts; that folder explains how.

## Development setup

You need:

- Go 1.26 or newer
- Node.js 24 or newer
- A Kubernetes cluster to point Opscope at. A local [kind](https://kind.sigs.k8s.io/) cluster
  works; for usage numbers, install metrics-server in it.
- Docker, to build the image (optional)

Run the backend and the frontend in two terminals:

```bash
make dev-backend OPSCOPE_KUBECONFIG=$HOME/.kube/config
```

```bash
make dev-frontend
```

Open http://localhost:5173. Vite reloads the page when frontend files change and forwards `/api/*`
to the Go server on port 8080; set `OPSCOPE_API` (for example `OPSCOPE_API=http://localhost:8090`)
to use a server on another address. Restart `make dev-backend` after Go changes.

`OPSCOPE_KUBECONFIG` is optional: without it, add clusters in the UI. In development they are
saved under `data/`, which git and Docker ignore. `make help` lists every Makefile target.

### The component kit

http://localhost:5173/kit shows every design token and component in both themes, with sample data
and no cluster needed. Build or change a component there before using it on a page. The kit exists
only in development and is left out of production builds.

## Repository layout

```
backend/                  Go server
  main.go                 entry point: reads env vars, loads clusters, serves, shuts down cleanly
  internal/clusters/      known clusters (environment, in-cluster, added in the UI), clients for each
  internal/resources/     one file per resource type: lists it and flattens it into table rows;
                          detail.go builds detail pages, gatewayapi.go reads Gateway API resources,
                          names.go builds the search index
  internal/metrics/       live usage from metrics-server, and the in-memory history
  internal/server/        HTTP routes, middleware, static file serving
frontend/                 React app (Vite, plain JavaScript)
  src/api.js              fetch helper and the useApi hook
  src/clusters.jsx        shared list of clusters (React context)
  src/sections.js         every page, with its icon, short names and shortcut; drives the sidebar,
                          the routes and the command palette
  src/columns.jsx         table columns for each resource type
  src/commands/           the command registry behind the palette and the shortcuts (sources/ has
                          the commands available on every page)
  src/components/         shared components, each with its own CSS file
  src/pages/              one file per page
  src/styles/             design tokens (both themes) and page-wide base styles
  src/kit/                the component kit at /kit (development only)
  src/format.js           ages, durations, sizes
  src/kubectl.js          read-only kubectl commands for one object, for copying
  src/theme.js            light, dark or system theme, remembered in the browser
  src/toast.js            short confirmations ("Copied …")
deploy/kubernetes/        manifests for running Opscope in a cluster
docs/                     deployment guide, API reference, design records
Dockerfile                the single image (multi-platform)
Makefile                  common commands
.github/                  CI and release workflows, issue forms, Dependabot
```

## Tests and checks

```bash
make test
```

runs the Go tests and the frontend tests. Before opening a pull request, also check what CI checks:

| Check                      | Command                                     |
| -------------------------- | ------------------------------------------- |
| Go vet                     | `cd backend && go vet ./...`                |
| Go formatting              | `cd backend && gofmt -l .` (prints nothing) |
| Go tests                   | `cd backend && go test ./...`               |
| Frontend tests             | `cd frontend && npm test`                   |
| Frontend build             | `cd frontend && npm run build`              |
| Image                      | `make docker-build`                         |

- **Go tests** use client-go's fake clients, so they need no cluster. A new lister or detail section
  gets a test next to it.
- **Frontend tests** use [Vitest](https://vitest.dev) and cover plain functions: matching and
  ranking, the command registry, shortcuts, kubectl lines. Test files sit next to the code they
  test (`match.test.js`).
- **In the browser:** check a UI change in light and dark, with the keyboard only, at 375 px wide and
  on a wide screen, against a cluster with and without metrics-server and Gateway API where it
  matters.

## Code style

**Go**

- `gofmt`, and the standard library before a new dependency.
- Comments explain why, and what isn't obvious from the code.
- Errors from a cluster go through `clusters.Explain`, so the UI can show a readable message.

**Frontend**

- Plain JavaScript and React function components.
- Plain CSS: one file per component, next to it, built on the tokens in `src/styles/tokens.css`
  (colours for both themes, type, spacing, radius, motion). Use the colour tokens, so both themes
  work, and the other tokens wherever one fits.
- Icons come from `src/components/icons.jsx`, a small set copied from Lucide. Add one there when
  needed rather than adding an icon package.
- Accessibility is part of done: keyboard access, visible focus, labels for screen readers, WCAG 2.1
  AA contrast in both themes, and reduced motion respected.

## Common changes

### Adding a resource type

1. **Backend:** a file in `internal/resources/` that lists it and flattens each object into a row
   (rows embed `Meta`), and one line in `Listers` in `resources.go`. The type is then in
   `GET /api/clusters/{id}/{resource}` and in the search index.
2. **Detail page:** an entry in `kinds` in `detail.go`, and kind-specific sections in
   `detail_sections.go` if it needs any.
3. **Permissions:** the resource in the ClusterRole in `deploy/kubernetes/opscope.yaml`, with only
   `get` and `list`.
4. **Frontend:** an entry in `src/sections.js` (with `aliases` for kubectl's short names) and its
   columns in `src/columns.jsx`. The sidebar, the route, the palette's "Go to" and object search
   follow from these.
5. **Docs:** the resource list in the README, the permissions table in
   [`docs/deployment.md`](docs/deployment.md) and the resource table in [`docs/api.md`](docs/api.md).

### Adding commands

Every command comes from one registry (`src/commands/`), which feeds the command palette, the
keyboard shortcuts and the `?` help. A command is a plain object: an `id`, a `title`, a `group`, an
optional `icon`, and one of a link (`to`), an action (`run`) or a sub-list (`items`), plus optional
`shortcut` and search words. The full shapes of commands and sources are documented at the top of
`src/commands/registry.jsx`.

| To add                                          | Do this                                                         |
| ----------------------------------------------- | --------------------------------------------------------------- |
| A page                                          | An entry in `sections.js`; "Go to" and object search follow. Add `aliases` for short names and `shortcut` for a key |
| A resource the backend lists                    | Nothing more: it's in `/names`, so its objects can be found once it has a page |
| A command available on every page               | A file in `src/commands/sources/` and one line in `src/commands/index.js` |
| A command that belongs to one page or component | `useCommands("id", [...commands], [deps])` in that component    |
| A keyboard shortcut                             | `shortcut: "g x"` (or a single key) on the command              |

`ctx`, what every command can read and do (the cluster, the namespace, `navigate`, …), is built in
`src/commands/context.js`. Commands that copy kubectl lines offer read-only commands only (`get`,
`describe`, `logs`).

## Documentation

Update the documentation in the same pull request as the change it describes:

| Document                                    | Audience and contents                                      |
| ------------------------------------------- | ---------------------------------------------------------- |
| [`README.md`](README.md)                    | Users: what Opscope does, quick start, configuration       |
| [`docs/deployment.md`](docs/deployment.md)  | Operators: Docker, Kubernetes, permissions, exposure       |
| [`docs/api.md`](docs/api.md)                | The HTTP API                                               |
| [`docs/design/`](docs/design/)              | Design records for larger features                         |
| `CONTRIBUTING.md`                           | Contributors: this guide                                   |

Write plainly and precisely: short sentences, the reader addressed directly, the behaviour first and
then the reason. Avoid marketing language.

## Releases

Releases are made by the maintainer. Each version tag publishes the image to
`ghcr.io/unnikrishnannam/opscope` for amd64 and arm64, tagged `vX.Y.Z`, `X.Y` and `latest`; a
pre-release such as `v1.3.0-rc.1` gets only its own tag.

1. Change the image tag in `deploy/kubernetes/opscope.yaml` to the new version and merge that into
   `main`.
2. Tag the merged commit and push the tag:

   ```bash
   git tag -a v1.3.0 -m "Release v1.3.0"
   ```

   ```bash
   git push origin v1.3.0
   ```

3. When the "Release image" workflow has finished, publish the release notes on GitHub. GitHub
   Releases are Opscope's changelog.

The release workflow (`.github/workflows/release.yml`) runs the tag's tests and checks that the
manifest uses the image being released (`.github/scripts/check-manifest-tag.sh`) before anything is
pushed; if either fails, nothing is published. It can also be run by hand from the Actions tab for
an existing tag.

CI (`.github/workflows/ci.yml`) runs on every pull request and every push to `main`. GitHub Actions
are pinned to commits, and Dependabot proposes updates weekly.

## License

Opscope is licensed under the [Apache License 2.0](LICENSE). Contributions are accepted under the
same licence, as section 5 of the licence describes.
