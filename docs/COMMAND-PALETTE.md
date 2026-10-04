# Opscope: Command palette

A ⌘K (Ctrl+K) command palette: one box to jump to any page, any object in the cluster, another
cluster or namespace, and to run whatever the current page offers (open its logs, copy its name,
follow logs, ...). It was listed under "Possible later" in [UI-REDESIGN.md](UI-REDESIGN.md).

The palette itself is small. Most of the work is the **command registry** under it: one place that
every feature, now and later, hands its commands to, so adding a feature to Opscope never means
editing the palette.

We work the same way as before: one small phase at a time, each ending with a working app, a review,
ticked boxes and notes at the bottom of this file. Phases are numbered **P0–P6** so they don't mix
with the build phases 0–8 or the redesign phases R0–R8.

**Status (2026-10-04):** P0–P6 done. Left for you: Safari and Firefox; one real copy to the
clipboard, pressing `/` and `?` yourself, and a look at the shortcuts help (see the P6 notes for why).

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions

- **Still read-only.** No command changes anything in a cluster. Commands navigate, change what's on
  screen (a tab, a namespace, the theme), or copy text to the clipboard.
- **No new runtime dependencies.** Fuzzy matching is a small function of our own, not a library.
- **Vitest for the frontend's plain functions** (matching, ranking, the registry's store, recent
  commands). It's a dev dependency only; nothing of it ships to the browser. `make test` runs it
  after the Go tests.
- **Object search uses a server endpoint,** `GET /api/clusters/{id}/names` (see below), not 14 list
  calls from the browser.
- **Copy kubectl commands, read-only ones only** (`get`, `describe`, `logs`). Nothing that changes
  the cluster (`delete`, `rollout restart`, `scale`) is offered, even as text to copy.
- **A small toast** confirms copy commands ("Copied kubectl command"), since the palette has closed
  by then. It's a new shared component, usable by any feature later.
- **Built like the rest of the UI.** Plain CSS on the existing tokens, the browser's `<dialog>` (as
  `Dialog` and `Drawer` already use), the ARIA combobox pattern `Combobox` already follows, and the
  component lives on `/kit` before it reaches a page.
- **Derived before declared.** Wherever Opscope already has a list (pages in `sections.js`, clusters,
  namespaces, theme choices, the backend's `Listers`), the palette's commands are built from that
  list. Adding a page or a resource type then adds its commands with no extra code.
- **One source for shortcuts.** A command can carry a keyboard shortcut. The same registry binds the
  key, shows it as a `Kbd` hint in the palette and lists it on the shortcuts help, so they can't drift apart.
- **Opens with ⌘K / Ctrl+K**, and from a search button in the top bar (for touch screens and for
  people who don't know the shortcut). `/` stays the list pages' filter shortcut.

---

## Design

### What a command is

A command is a plain object:

```js
{
  id: "go.pods",                  // unique and stable; used to remember recent commands
  title: "Pods",                  // what the row says
  group: "Go to",                 // heading the row sits under
  icon: PodsIcon,                 // from icons.jsx
  detail: "Workloads",            // optional, quieter text after the title
  keywords: ["po", "pod"],        // optional, extra words that match ("po" finds Pods)
  shortcut: "g p",                // optional, a key or a sequence; bound globally
  // Then one of:
  to: "/c/lab/workloads/pods",    // a link (Cmd/Ctrl+Enter opens a new tab)
  run: (ctx) => { ... },          // an action (copy, switch tab, theme)
  items: (ctx) => [ ...commands ],// a sub-list: "Switch namespace…" opens the namespaces
}
```

`ctx` is what a command may need to know or do, built once by the palette:
`{ cluster, namespace, page, object, clusters, statuses, navigate, location }`. Commands never reach
into React state themselves, which keeps them plain functions that are easy to read and to test.

Commands are built by their source from `ctx`, so a command that doesn't apply (one that needs a
cluster, on the welcome page) is simply not returned, and links are plain strings.

### Where commands come from

Commands are grouped in **sources**. A source is either a list of commands, or a function that
returns them (possibly after loading something, like the object index):

```js
{ id: "pages", commands: (ctx) => [...] }       // built from sections.js
{
  id: "objects",
  searchOnly: true,                             // too many to list; shown once something is typed
                                                // (a single command can be searchOnly too)
  key: (ctx) => ctx.cluster?.id,                // which data; a new key loads again, null loads nothing
  load: (ctx) => api(`/clusters/${ctx.cluster.id}/names`),
  maxAge: 30_000,                               // how long loaded data is used
  label: "objects",                             // for "Loading objects…"
  commands: (ctx, data) => [...],               // called once the data is there
}
```

There are two ways to register them, and they cover every case:

1. **Global sources**, one file each in `src/commands/sources/`, listed in
   `src/commands/index.js`, the same "one list, one line per entry" idea as `sections.js`.
   For commands that make sense on any page: pages, clusters, namespaces, theme, objects.
2. **Page sources**, registered by a component while it's on screen with a hook:

   ```js
   useCommands("pod-logs", [
     { id: "logs.follow", title: following ? "Stop following" : "Follow logs", group: "Logs", run: toggleFollow },
   ], [following]);
   ```

   They disappear when the component unmounts, so the palette always shows what the current page
   can do. The detail page, the list page and the log viewer use this.

### How a feature connects to the palette

| You add...                                        | You do...                                                                      |
| ------------------------------------------------- | ------------------------------------------------------------------------------ |
| a page in `sections.js`                           | nothing: "Go to" and its kind in object search follow (add `aliases` if it has short names) |
| a resource to the backend's `Listers`             | nothing: it's in the object index (it shows in results once it has a page)     |
| a command that works everywhere                   | a file in `src/commands/sources/` and one line in `src/commands/index.js`      |
| a command that belongs to one page or component   | `useCommands(...)` in that component                                           |
| a shortcut                                        | `shortcut: "g x"` on the command                                               |

This table goes into the README's developer section in P6.

### Finding and ranking

- Matching is fuzzy on the title, then keywords and detail: "dply" finds Deployments, "api 1" finds
  `api-1`. Matched letters are shown in bold, so it's clear why a row is there.
- Ranking: exact and prefix matches first, then word starts, then scattered letters; page commands
  above global ones at equal score; recently used commands get a small lift.
- With an empty box the palette shows recent commands, then what the current page offers, then
  "Go to".
- **Kind scoping**, kubectl style: if the first word is a kind or its short name ("pods", "po",
  "deploy", "svc", "cm"), the rest searches only that kind (`po api` → pods matching "api"). The
  short names live in `sections.js` as `aliases`.
- At most 50 rows are drawn, so a cluster with thousands of pods stays fast.

### Object search

The palette needs every object's name in the current cluster without loading 14 tables. A new
endpoint builds that list on the server, from the same `Listers` map the tables use:

```
GET /api/clusters/{id}/names
{ "objects": [{ "resource": "pods", "namespace": "web", "name": "api-1" }, ...],
  "skipped": [{ "resource": "gateways", "code": "not_installed" }] }
```

- Every lister except events runs, in parallel. A kind that's not installed, forbidden or failing is
  skipped and named in `skipped` (with `code` `not_installed`, `forbidden` or `failed`), instead of
  failing the whole answer. Only when no kind at all can be read is it an error.
- Names only. Secrets are listed by name, as their list page already does; no values ever.
- The palette loads it when it opens (and at most every 30 seconds), filters in the browser, and
  ranks objects in the selected namespace a little higher. Picking one opens its detail page.

### Look and feel

- A `<dialog>` near the top of the window, about 640 px wide: a search box, a grouped list, and a
  footer with key hints (↑↓ to move, ↵ to open, esc to close). Full width under 640 px.
- Rows: icon, title (matched letters bold), quieter detail (namespace, kind, group), and a `Kbd`
  shortcut on the right if the command has one.
- Sub-lists show where you are as a chip in the search box ("Namespace ›"); Backspace in an empty box
  goes back up.
- Loading the object index shows a quiet line under the list, not a spinner over everything; a
  failure there is a quiet line too ("Couldn't load objects from multipass"), and the rest keeps working.
- The usual rules from UI-REDESIGN.md: the highlighted row looks like the pickers' (a quiet fill,
  no Signal orange), shadow because it floats, no decoration.

### Files

```
src/commands/
  registry.jsx      CommandsProvider, useCommands, useCommandList, perform; documents the shapes
  context.js        useCommandContext: the `ctx` every command gets, built once by the layout
  store.js          page sources and loaded data, outside React (so only the palette re-renders)
  list.js           collect sources and rank and group the rows (plain functions)
  match.js          fuzzy matching (plain functions)
  recent.js         recently used command ids (localStorage, per browser)
  keys.js           keyboard shortcuts, including sequences like "g p": matching and the listener
  index.js          the list of global sources
  sources/          pages.js, namespaces.js, clusters.jsx, theme.js, help.js, objects.js
  *.test.js         Vitest tests next to the code they test
src/components/
  Highlight.jsx               text with matched letters in bold
  useListNavigation.js        the highlighted row and its keys, shared with Combobox
  CommandPalette.jsx / .css   the dialog: search box, list, footer
  Toast.jsx / .css            where toasts appear (Toaster), and one toast's look (ToastView)
  ShortcutsHelp.jsx / .css    the "?" dialog, generated from the registry
src/pages/detailCommands.js   the detail page's commands (a plain function, tested)
src/platform.js               ⌘ or Ctrl: which modifier this computer uses, and its label
src/toast.js                  toast("Copied …"): one short message at a time
src/clipboard.js              copyText(text, what): copy, then say so in a toast
src/kubectl.js                read-only kubectl lines for one object (get, describe, logs)
```

---

## Phase P0: Command model and registry

Goal: commands can be registered and listed; nothing on screen yet.

- [x] `registry.jsx`: `CommandsProvider` (in `main.jsx`, inside the router), global sources from
      `commands/index.js`, `useCommands(id, commands, deps)` for page sources, and
      `useCommandList(ctx, query)` that turns everything into one ranked, grouped list
- [x] The command and source shapes documented at the top of `registry.jsx`, as `sections.js` does
- [x] `match.js`: fuzzy score and matched-letter positions; ranking rules above
- [x] `recent.js`: the last 10 command ids, in `localStorage`, safe when storage is blocked
      (like `theme.js`)
- [x] `/kit`: a "Command matching" demo, with a box and sample commands showing scores and bold letters
- [x] Vitest, with tests for `match.js`, `list.js`, `store.js` and `recent.js`; `npm test` and `make test`

## Phase P1: The palette component

Goal: a finished palette on `/kit`, with sample commands, before it touches the app.

- [x] `CommandPalette` on `<dialog>`: search box, grouped list, footer with key hints
- [~] Keyboard: ↑↓ (wrapping), Enter, Cmd/Ctrl+Enter for links in a new tab, Escape (clears the
      box, then leaves a sub-list, then closes), Backspace out of a sub-list. No Home/End: in the
      search box they move the text cursor (see notes)
- [x] Mouse: hover moves the active row, click runs it, Cmd/Ctrl-click opens links in a new tab
- [x] Sub-lists (`items`) with the chip in the search box
- [x] Empty box (recent, then the rest), no matches, loading, couldn't load, and "showing the best
      50 of 247"
- [x] ARIA: combobox input with `aria-activedescendant`, `listbox` with groups, results count read
      out with a polite live region
- [x] Shared list navigation with `Combobox` (`useListNavigation`), rather than a second copy of
      the same key handling
- [x] On `/kit` in both themes, keyboard only, and at 375 px

## Phase P2: In the app, with navigation commands

Goal: ⌘K works everywhere and can take you anywhere that isn't a single object.

- [x] ⌘K (Ctrl+K off Apple devices) opens it from anywhere, also while typing in a field
- [x] Top bar: a search button ("Search…" and the ⌘K hint; the hint goes below 1100 px, the words
      below 640 px)
- [x] Sources: pages (from `sections.js`, keeping `?ns`), clusters ("Switch to multipass", with
      status dots; switching keeps the page, like the cluster switcher), namespaces (sub-list from the
      namespaces API, plus "All namespaces", and each namespace found by typing its name), Clusters
      and Add a cluster, theme (System/Light/Dark)
- [x] `aliases` in `sections.js` (`no`, `po`, `deploy`, `sts`, `ds`, `cj`, `cm`, `svc`, `ing`, `gtw`,
      `gc`, and the singulars)
- [x] Commands that need a cluster hide on the welcome and clusters pages
- [x] Checked on both test clusters, light and dark, 375 px and 1440 px

## Phase P3: Object search

Goal: type a name, open that object.

- [x] Backend: `GET /api/clusters/{id}/names`, built from `Listers` (all but events), in parallel,
      skipping `not_installed`, forbidden and failing kinds and naming them in `skipped`
- [x] Go tests with the fake clients: rows from several kinds, events left out, a missing Gateway
      API, a forbidden kind, another failure, nothing readable at all, and every lister's rows
      carrying `Meta`
- [x] README API table: the new endpoint
- [x] `objects` source: loads on open, cached per cluster for 30 seconds, rows with the kind's icon,
      name and namespace; only kinds with a detail page
- [x] Kind scoping (`po api`) and the selected namespace ranked first
- [x] Loading and failure lines inside the palette, and notes for kinds that couldn't be searched
- [x] Checked on multipass (Gateway API objects found) and `opscope-test` (Gateways skipped quietly);
      timing of `/names` noted for both

## Phase P4: Commands from pages

Goal: the palette knows what the current page can do. This is where `useCommands` proves itself on
real features.

- [x] Detail page: open Summary / YAML / Events / Logs, copy the name, copy the YAML, go to the
      namespace's list, go to the owner (and for a pod, its node)
- [x] Copy a kubectl command for the object (`kubectl -n web get pods api-1 -o yaml`,
      `kubectl -n web describe pods api-1`, `kubectl -n web logs api-1`); read-only commands only
- [x] Logs: follow on/off, previous run on/off, wrap lines, reload, pick a container (sub-list), and
      `kubectl logs` for the container and run on screen
- [x] List page: focus the filter, refresh
- [x] Overview: refresh, go to recent warnings
- [x] `Toast`: a short confirmation at the bottom of the window ("Copied the kubectl command", or
      "Couldn't copy" when the browser blocks it), announced politely to screen readers; on `/kit` first

## Phase P5: Keyboard shortcuts

Goal: the most used commands work without opening the palette.

- [x] `keys.js`: binds every command's `shortcut`, single keys and sequences ("g p"), ignored while
      typing in a field or while a dialog is open, as the `/` filter shortcut already is
- [x] Shortcuts: `g o` overview, `g n` nodes, `g p` pods, `g d` deployments, `g s` services,
      `g c` clusters; on a detail page `1`–`4` for its tabs; `?` for the help
- [x] `ShortcutsHelp`: a dialog listing every command that has a shortcut, generated from the registry
- [~] Shortcuts shown as `Kbd` in palette rows, and announced with `aria-keyshortcuts` where there's
      a button for the same thing (single keys only: see notes)
- [x] Move the list filter's `/` onto the registry, so it's in the help too

## Phase P6: Polish and docs

Goal: ready to merge.

- [x] axe-core run with the palette open (empty, results, sub-list, no match), light and dark; and
      the shortcuts help
- [x] Keyboard-only walk; screen reader labels; reduced motion (no open animation)
- [~] 375 px, 768 px and wide; Chrome here, Safari and Firefox for you
- [x] Large-list check: a few thousand objects stay responsive while typing
- [x] Bundle size noted against R8 (JS 353 KB, CSS 61 KB)
- [~] README: the palette in "what it shows", the `/names` endpoint, and "Adding commands" (the table
      above); no screenshot (see notes)
- [x] UI-REDESIGN.md: move the ⌘K item out of "Possible later" with a link here
- [x] Docker image built and checked

---

## Open questions

All four were answered on 2026-10-04 and moved into [Decisions](#decisions): Vitest for frontend
tests, the `/names` endpoint for object search, read-only kubectl commands, and a toast for "Copied".

---

## Possible later

- Status in object results (a crash-looping pod shown in red), which needs the index to carry a status
- Searching labels (`app=web`) and images
- Searching across every cluster at once
- Pinning commands

---

## Phase notes

Things that come up while building, decisions made, and anything that moves between phases.

### Phase P0

- Built as plain modules with a thin React layer on top: `match.js` (matching), `list.js` (collect
  sources, rank, group, the 50-row limit), `store.js` (page sources and loaded data) and `recent.js`
  are plain JavaScript; `registry.jsx` only adds the provider and hooks. That's what made them easy
  to test without a browser.
- 43 tests, about 0.1 seconds. They run in Node, so `recent.js` is tested with a stand-in
  `localStorage`, including one that throws, as a blocked one does.
- The command shape got simpler than planned: `to` is a plain string and there's no `when`. A source
  builds its commands from `ctx`, so it can leave out the ones that don't apply and fill in the link,
  with one way of doing each thing instead of two.
- `store.js` lives outside React and the palette subscribes to it (`useSyncExternalStore`). If page
  sources were React state in the provider, every `useCommands` update (the log viewer's, for
  example) would re-render the whole app below it.
- `useCommands` uses two effects: one adds the source when the component mounts and removes it
  when it goes, the other replaces its commands when `deps` change. With one effect, every change
  would remove the source and add it back at the top, so groups would jump around.
- Loading: a source with `load` is skipped until its data arrives; old data stays in use while new
  data loads and after a failure; a failure is reported in `status` for a quiet line in the
  palette. Data is kept per source and key (per cluster), for 30 seconds by default.
- One broken source can't take the palette down: if a source throws, it's left out and logged.
  If two commands share an id, the first wins, so a page's command can override a global one.
- Matching rules: every query word must match the title, a keyword or the detail. Kinds of match
  are scored in bands so a better kind always wins (exact, prefix, word start, inside a word,
  scattered letters); scattered letters must start at a word start, which keeps random names out of
  short queries like "pod". Word starts include camelCase ("Sets" in StatefulSets) and digits after
  letters ("2" in worker2).
- Checked on `/kit`: `dply` finds Deployments, `sts` puts StatefulSets first (with DaemonSets and
  Secrets just behind it, close; the `sts` alias in P2 makes it exact), `po check` finds the
  crash-looping pod through its `po` keyword and bolds only "check", `kube` finds pods by their
  namespace, and the empty box shows Recent first. Matched letters use weight 700 against the 500
  of the row, through a new `Highlight` component.
- The app shell itself is unchanged: `CommandsProvider` is in place, with no global sources yet.
- A local `.claude/launch.json` starts the dev server for the desktop app's preview. It's kept out
  of git (listed in `.git/info/exclude`).
- Build: JS 354 KB (111 KB gzipped), CSS 61 KB, about the same as after R8. The demo and its
  `Highlight` use are only on `/kit`, which isn't in the bundle.

### Phase P1

- Checked on `/kit` in both themes and at 375 px, mostly with the keyboard: typing, Up/Down with
  wrap-around, Enter on a link, an action and a sub-list, Cmd-click (a new tab, checked with
  `window.open` stubbed), Escape in its three steps (clear, leave the sub-list, close) with focus
  back on the button, Backspace out of a sub-list, hover and click. A short query on 247 sample
  pods showed "Showing the best 50 of 247"; the slow sample source showed "Loading objects…" and,
  with "Loading objects fails" ticked, "Couldn't load objects: the cluster didn't answer" while the
  other commands kept working.
- The `/kit` demo has its own `CommandsProvider` with sample sources, and registers "This page"
  commands with `useCommands`, so it runs the real registry, not a copy. "Follow logs" turning
  into "Stop following logs" after it runs shows `deps` at work.
- The palette's inside is only rendered while it's open. Each opening starts with an empty box at
  the top level, and the registry's loading (`refresh`) runs on opening, not on page load.
- Escape on the search box calls `preventDefault()` while there's something to clear or a sub-list
  to leave, which keeps the `<dialog>` open; only the last Escape reaches the dialog and closes it.
  Checked in Chrome; Safari and Firefox are for P6.
- No Home/End: focus stays in the search box, where they move the text cursor (the namespace
  picker works the same way when it has a search box).
- The highlighted row uses the pickers' quiet fill rather than a Signal orange marker. It's the
  same thing as the highlighted row in a picker, so it looks the same; the plan's "active row's
  marker" is dropped.
- Rows are `div`s with `role="option"` inside `role="group"`s, not links: a listbox can't contain
  links. Clicks are handled on the row (with Cmd/Ctrl for a new tab), and a mouse press on the
  list doesn't take focus out of the search box.
- "Nothing matches" waits while a source is still loading, so it doesn't flash before objects arrive.
- Narrow rows: the title is cut off first; the detail (a namespace, which tells two same-named pods
  apart) keeps its width, up to 40% of the row. Found at 375 px, where a long pod name had pushed
  its namespace out entirely.
- `useListNavigation` (`src/components/`) is the highlighted row and its keys, now shared with
  `Combobox`. Its `active` is always a row that exists, even right after the list got shorter (new
  data, a new query). The namespace picker was checked again on `/kit` after the change.
- `useModal` is now exported from `Dialog.jsx` for the palette; `modKey` in `Tag.jsx` gives "⌘" on
  Apple devices and "Ctrl" elsewhere, for key hints (the top bar's ⌘K hint in P2 uses it too).
- The built-in browser again ran the opening animation slowly while the pane wasn't focused, so the
  first screenshot caught the palette half faded in; the finished state was checked from the page.
- Build unchanged from P0 (JS 354 KB, CSS 61 KB): the palette isn't used by the app until P2.

### Phase P2

- Checked in the browser against a backend on port 8090 with multipass from the environment and
  `opscope-test` added through the API into a scratchpad `DATA_DIR` (so `data/` was left alone):
  ⌘K from a list and a detail page, `kube-sys` + Enter selecting that namespace (the picker followed),
  `svc` going to Services with `?ns` kept, the namespace sub-list, "Switch to opscope-test" keeping
  the Pods page and dropping the namespace, `local-path` finding opscope-test's own namespace, `no`
  going to Nodes (where the namespace commands are gone, as the picker is disabled there), "Dark
  theme", the clusters page (only cluster and theme commands), ⌘K ignored while the remove-cluster
  confirmation is open (cancelled; both clusters still there), the search button at 375 px (an icon
  at the end of the first row), 800 px (no key hint) and 1440 px ("Search… ⌘ K").
- `ctx` is built in one place, `commands/context.js`, used by the layout. A field added there is
  available to every command, so sources never reach for React state or the router themselves.
- The four sources are each a short file in `commands/sources/`, and `commands/index.js` lists them.
  Pages come straight from `sections.js`, so a new page needs no palette code.
- Namespaces: "Switch namespace…" opens them all, and each one can also be found by typing its
  name at the top level (`searchOnly` on the command, so the empty palette isn't flooded on a
  cluster with many). `searchOnly` now works on single commands as well as whole sources. Their ids
  are `namespace:<name>`, which can't clash with `namespace.switch`, whatever a namespace is called.
- Sub-lists have no "Recent" group: it broke the A to Z order of the namespaces. And there a row's
  detail only says "selected"; "Namespace" is only added at the top level, where the row needs to
  say what it is.
- Ctrl+K on a Mac is left alone: it's "delete to the end of the line" in text fields. `platform.js`
  says which modifier this computer uses (`hasModKey`), and `modKey` moved there from `Tag.jsx`.
- ⌘K doesn't open the palette over another dialog (a confirm, the drawer); that one has to be dealt
  with first. Pressed while the palette is open, it closes it.
- Cluster rows use the cluster switcher's status dot as their icon (`StatusDot` and `statusText`
  are now exported from `ClusterSwitcher.jsx`).
- A new `NamespaceIcon` (Lucide's "folder"), since namespaces have no page to borrow an icon from.
- `pageFor(pagePath)` in `sections.js` finds the page a URL belongs to; the top bar and the palette
  both use it.
- `/kit` and the app share recent commands (same browser storage, and the demo's page ids match the
  real ones). Harmless: a recent id only shows while some source offers it.
- The built-in browser's screenshots lagged a step behind twice again; the page state was read
  directly each time.
- Build: JS 367 KB (115 KB gzipped), CSS 65 KB (16 KB gzipped); up 13 KB and 4 KB now that the
  palette ships.

### Phase P3

- Checked against multipass (191 objects in 13 kinds, Gateway API included) and `opscope-test` (39
  objects; gateways, routes and classes skipped as `not_installed`, with no note in the palette):
  `argocd-server` finding the deployment, service and pod, grouped by kind with exact names first;
  `gtw main` finding only the gateway and opening its page; `po argo` the 7 Argo CD pods; `core` on
  `opscope-test` finding CoreDNS's ConfigMap, Deployment and pods; an opened object showing up under
  Recent.
- `ListNames` (`internal/resources/names.go`) runs every lister in `Listers` but events, in
  parallel, and keeps only kind, namespace and name. A kind added to `Listers` is in the index with
  no other change. Listers return typed slices as `any`, so the names are read with a little
  reflection through a `meta()` method every row gets from its embedded `Meta`; a test fails if a
  future lister's rows don't embed `Meta`, since they'd silently be missing from search.
- One kind failing doesn't fail the index: it's named in `skipped`. Only when nothing at all can be
  read (an unreachable cluster, rejected credentials) is it an error, reported like any other
  cluster error.
- **Found while timing `/names`:** three calls in a row took 28 ms, 0.37 s and 1.19 s. client-go's
  default client-side limit (5 requests a second, bursts of 10) was throttling the 15 parallel
  lists, and the limit is shared by everything Opscope does with that cluster, so opening the
  palette a few times would slow the list pages too. The limit is now 50 a second with bursts of
  100 (`clientQPS`, `clientBurst` in `internal/clusters/kubeconfig.go`); the API server still applies
  its own fairness limits. Afterwards: 15–60 ms per call on multipass, 3–56 ms on `opscope-test`,
  however often it's called. Log streams keep client-go's defaults (one request each).
- Answer size: 14.6 KB for multipass's 191 objects, 3 KB for `opscope-test`.
- Kinds come from `sections.js` on the frontend too: an object is offered when its kind has a page,
  grouped and labelled by that page. Namespaces are in the index but have no page, so the palette
  leaves them to the namespace commands.
- Two additions to matching, both general:
  - `scope` words on a command narrow a search but never find anything alone. A pod has
    `["pods", "pod", "po"]`, so `po api` finds pods called api while `pod` alone still finds the
    Pods page first (as an ordinary keyword, "pod" put 50 pods above the page).
  - Strict kind scoping, as with `kubectl get po`: when the first of several words is exactly a scope
    word, only commands with that scope are searched. Without it, `gtw main` also found every
    deployment, service and pod whose name has g, t and w in order.
  - `boost` lifts a command a little; objects in the selected namespace get 3.
- Sources can add `notes` about what they loaded, shown under the results (for a search-only source,
  once something is typed). The objects source uses it for kinds that couldn't be searched: "Secrets
  aren't searched: this user isn't allowed to list them." A missing kind (no Gateway API) gets no
  note. Covered by tests; not seen on a real cluster, since that needs taking RBAC access away.
- Object links keep the selected namespace (`?ns`), like the rest of the app's links.
- Object ids include the cluster (`object:multipass/gateways/topology-test/main-gateway`), so a recent
  object from one cluster never stands in for a same-named one in another.
- 55 frontend tests, all Go tests pass. Build: JS 368 KB (115 KB gzipped), CSS 65 KB, about as after P2.

### Phase P4

- Checked against multipass on a crash-looping Argo CD pod: the "This page" commands (other tabs,
  copy name and YAML, `kubectl get`/`describe`/`logs`, "Pods in argocd", "Go to node kubeworker02");
  "Show logs" opening the Logs tab, where the "Logs" group took over (follow, previous run, wrap,
  reload, `kubectl logs … -c server`, which gained `--previous` once the previous run was shown);
  "Filter pods" on the Pods list leaving focus in the filter (typing "crash" then gave "2 of 50
  pods"); "Go to recent warnings" on the overview. Toasts on `/kit` in both themes.
- **Commands now run after the palette has closed.** Closing a `<dialog>` puts focus back where it
  was before it opened, which undid "Filter pods" (the filter got focus, then lost it again). The
  palette now remembers the chosen command and runs it from an effect after its `<dialog>` has
  closed.
- **The built-in browser doesn't allow clipboard writes** (`clipboard-write` is "denied" for every
  page there, so the code blocks' own copy buttons fail the same way). That showed the failure toast
  for real: "Couldn't copy the kubectl command: the browser didn't allow it". The success path was
  checked with `navigator.clipboard` replaced by a stand-in that records the text: the exact
  `kubectl -n argocd describe pods …` line, and "Copied the kubectl command". Worth one real copy in
  your own browser.
- kubectl lines use the API name (`pods`, `deployments`); the three Gateway API kinds use their full
  name (`gateways.gateway.networking.k8s.io`), from a new optional `kubectl` field in `sections.js`,
  since other projects (Istio) have a kind called Gateway too. Anything unusual in a name would be
  quoted for the shell, though Kubernetes names never need it. The lines use kubectl's current
  context; Opscope can't know what the cluster is called in your kubeconfig.
- Only read-only verbs exist (`kubectlGet`, `kubectlDescribe`, `kubectlLogs`); there's nothing to
  build a changing command from.
- The detail page's commands are a plain function in `pages/detailCommands.js`, tested on its own
  (other tabs, kubectl lines, owners with and without a page, the node, a cluster-wide object).
- On the Logs tab, the detail page leaves out its `kubectl logs` and the log viewer offers one for
  the container and run on screen.
- `DataTable` registers "Filter pods" itself, but only for a page's main table (the one with a
  shortcut), so every list page gets it with no code of its own.
- **Recent is capped at 5 rows in the empty palette.** With 8 recent commands, a pod's own commands
  started below the fold. All 10 are still kept, for the ranking.
- **"Go to recent warnings" first scrolled the whole frame.** `scrollIntoView` also scrolled the
  boxes around the content that hide their overflow, moving the top bar out of view. It now scrolls
  only the nearest box that scrolls (the content area, or the page on phones).
- Toasts: one at a time, 3 seconds, a new one replacing the last; inverted colours like tooltips;
  the region is always in the page (`role="status"`), so screen readers announce each message. The
  icon stays in the text colour, since the status reds don't reach 3:1 on the inverted background;
  the words and the shape (tick or warning sign) carry the meaning. `ToastView` is the look alone,
  used by `/kit` to show both themes side by side.
- New icon: `EventsIcon` (Lucide's "history"). `Card` takes an `id`.
- 64 frontend tests, all Go tests pass. Build: JS 375 KB (117 KB gzipped), CSS 66 KB (16 KB gzipped).

### Phase P5

- Checked against multipass: `g` then `p` from the overview opening Pods; `/` focusing the filter,
  and typing "g p" into it staying in the box (no navigation); `2`, `4` and `1` on a pod's page
  opening YAML, Logs and Summary; `?` opening the help, and the palette's "Keyboard shortcuts" too
  (after the palette had closed); `g p` ignored while the help was open; Escape closing it.
- The test tool can't send `/` or `?` as key presses (it sends an empty key, or inserts the text
  without a keydown), so those two were checked with real `keydown` events sent from the page.
  Worth pressing them once yourself.
- Shortcuts are a field on commands, so the palette's hint, the help and the binding all come from
  one place. Pages get theirs from `sections.js` (`shortcut: "g p"`), "Manage clusters" has `g c`,
  the detail tabs `1`–`4`, the list filter `/`, the help `?` (a small `help` source).
- `useShortcutCommands` collects the commands with a shortcut from every source but the search-only
  ones (building every object on each page change would be wasted work), one per shortcut; the
  layout binds them with `useShortcuts` and passes the same list to the help. When two commands
  share a key, the first wins, so a page's command can take a key from a global one.
- `matchShortcut` (the decision for each key press) is a plain function with tests: single keys,
  sequences, unknown keys, a broken sequence still letting a single-key shortcut through ("g" then
  "/"), exact case, first wins. A sequence waits 1 second for its next key.
- Plain keys are ignored while typing in a field and while any dialog is open (the palette, the
  help, a confirm). Keys with ⌘, Ctrl or Alt are left alone; ⌘K stays the layout's.
- `SearchInput` no longer listens for its own `/`: the table's "Filter pods" command does, through
  the registry, so the key is in the help. The box still shows the hint and `aria-keyshortcuts`.
  On `/kit`, which has no layout, `/` no longer focuses a table's filter there.
- The detail page now lists every tab in the palette, the open one too (marked "open"), so the
  help always shows all four number keys.
- Running a command from a shortcut doesn't add it to Recent: it's already at hand.
- `aria-keyshortcuts` is only used for single keys (the tabs, the filter). It has no way to say "g,
  then p" (a space there separates alternatives), so the sidebar links don't claim their
  sequences; the help lists them instead.
- The help: "Anywhere" (⌘K) first, then each group in registry order, "then" between the keys of a
  sequence. 460 px wide, scrolling inside; a screenshot wasn't possible (the browser pane was
  hidden), so it was checked from its measurements: one line per row, no sideways overflow. A visual
  check is in P6.
- New icon: `KeyboardIcon` (Lucide's "keyboard").
- 70 frontend tests, all Go tests pass. Build: JS 378 KB (118 KB gzipped), CSS 67 KB (17 KB gzipped).

### Phase P6

- **Accessibility:** axe-core 4.13 (WCAG 2.0/2.1 A and AA), served from the scratchpad and run inside
  the page as in R8, on the palette empty, with results, with no match and in a sub-list, and on the
  shortcuts help, in light and dark, on both clusters. One problem, fixed: with no match the
  `listbox` held only the "Nothing matches" text, and a listbox has to contain options. Without rows
  the box is now just a message, and the search box says `aria-expanded="false"` and points at no
  list. Everything then passed.
- **Reduced motion:** the rule in `base.css` covered `*`, `::before` and `::after`, but not a
  dialog's `::backdrop`, so every dialog's backdrop (the palette's, the confirms', the drawer's) still
  faded in. It's covered now.
- **Keyboard:** the walk was done phase by phase (P1 on `/kit`, P2–P5 in the app): ⌘K, typing, the
  arrows, Enter, Cmd/Ctrl+Enter, Escape in its steps, Backspace out of a sub-list, focus back where it
  was on closing (or where a command put it), the shortcuts and the help.
- **Sizes:** 375 px (P2), 768 px (here: the top bar on one 56 px row with "Search…", the palette 634
  px wide, the help 451 px; nothing wider than the window), 800 px and 1440 px (P2).
- **Large clusters:** ranking timed in Node with the real code on generated indexes. 1,000 objects:
  under 3 ms a keystroke; 5,000: under 9 ms; 20,000: 3–9 ms for typical queries, 19 ms for a single
  letter. Building the commands from the index (only when the palette opens or the page changes)
  takes 1, 4 and 19 ms. The palette draws at most 50 rows whatever matches.
- **Sizes of the build:** JS 378 KB (118 KB gzipped) against R8's 353 KB (110 KB), CSS 67 KB (17 KB)
  against 61 KB (16 KB): about 25 KB of JS and 6 KB of CSS for the palette, the shortcuts, the toast
  and their sources. The Docker image is 42.5 MB, as before.
- **Docker:** built as `opscope:palette-check` (so `opscope:dev` was left alone), run with the
  multipass kubeconfig mounted read-only: `/api/health`, `/names` in 25–30 ms, and in the browser
  `po argocd-server` opening the pod and `2` its YAML tab. The container and the test image were
  removed afterwards.
- **README:** the palette in the opening paragraph, a "Command palette and shortcuts" section (what to
  type, the keys), "Adding commands" for developers (the table from this plan), the new files in the
  layout. `/names` went into the API table in P3. No screenshot: the palette is a box over a page,
  and it explains itself better in words than in a fourth picture.
- `PHASES.md` and `UI-REDESIGN.md` link here; the ⌘K item in "Possible later" is ticked off.
- **Not checked here:**
  - Safari and Firefox (Chrome only, as in R8).
  - A real copy to the clipboard: the built-in browser refuses clipboard writes (P4), so only the
    failure toast was seen for real; the success path was checked with a stand-in clipboard.
  - Pressing `/` and `?`: the test tool can't send those keys (P5); real key events sent from the
    page did the same thing.
  - A visual look at the shortcuts help: the browser pane was hidden whenever it was open, so it was
    checked from its measurements and axe only.
