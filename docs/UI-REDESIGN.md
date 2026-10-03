# Opscope: UI redesign

The app works (see [PHASES.md](PHASES.md)); this plan gives it a look that matches the new brand kit
and is good enough to publish. It changes the frontend only. The backend, the API and what each page
shows stay the same.

We work the same way as before: one small phase at a time, each ending with a working app, a review,
ticked boxes and notes at the bottom of this file. Phases are numbered **R0–R8** so they don't mix
with the build phases 0–8.

**Status (2026-10-03):** R0 done; R1 next.

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

---

## Decisions already made

- **Light and dark themes.** Follows the OS setting by default; a System / Light / Dark switch
  overrides it and is remembered in the browser.
- **Fonts:** Manrope for all interface text, JetBrains Mono for code, YAML, logs, IPs and image names.
  Both bundled locally with Fontsource, as now, so Opscope keeps working offline.
- **Name:** "Opscope" everywhere in the UI, page title, manifest, README and docs (it was "OpScope").
  The wordmark is lowercase `opscope` and is only ever used as the SVG from the brand kit, never typed.
- **Still plain CSS**, no framework. The one 1,286-line `styles.css` is split: tokens and base styles
  in `src/styles/`, and each component imports its own small CSS file next to it.
- **No new runtime dependencies.** Icons are about 25 hand-picked [Lucide](https://lucide.dev) icons
  copied into one `icons.jsx` (ISC licence, noted in the file), not the whole package.
- **Components are built before pages.** A development-only page at `/kit` shows every component in
  every state, in both themes, with realistic sample data. It needs no cluster, and it's left out of
  the production build.

---

## Design direction

### Principles

1. **Calm by default, loud only when something is wrong.** Most of a cluster is fine most of the
   time. Neutral surfaces, and colour only for status and for "you are here".
2. **Dense but readable.** This is a tool people scan. Tight tables, generous line height, nothing
   decorative taking up room.
3. **Same thing, same look.** One status colour scale, one table, one card, one empty state, used everywhere.
4. **The pupil rule.** In the logo, the orange pupil marks what's being watched. In the UI, Signal
   orange marks the same idea and nothing else: the current page in the sidebar, the open tab, keyboard
   focus, and "live" (following logs). It's never used for body text or for big filled areas.

### Colour roles

Starting values come from the brand kit. Exact shades are tuned in R0 and every text colour is
checked for contrast (at least 4.5:1 for text, 3:1 for marks, in both themes).

| Role                        | Light                    | Dark                      |
| --------------------------- | ------------------------ | ------------------------- |
| Page background             | Paper `#F6F3EE`          | Night `#101418`           |
| Surface (cards, tables)     | white, slightly warm     | a step lighter than Night |
| Text                        | Ink `#17140F`            | Paper `#F6F3EE`           |
| Secondary text              | warm grey, ≥ 4.5:1       | warm grey, ≥ 4.5:1        |
| Primary button              | Ink fill, Paper text     | Paper fill, Ink text      |
| "You are here", focus, live | Signal `#E8590C`         | Signal `#E8590C`          |
| Usage bars, sparklines      | Ink / Paper (neutral)    | Ink / Paper (neutral)     |
| Status: ok / warn / bad     | green / amber / red      | lighter versions of these |

Signal orange sits close to both amber (warn) and red (bad). To keep them apart, warn moves towards
yellow, bad moves towards crimson, both are checked with the same ΔE test used in Phase 3, and status
always comes with a word (and an icon where there's room), so colour is never the only cue.

### Typography

- Sizes: 12, 13, 14 (body), 16, 20, 28 px. Nothing smaller than 12 px.
- Weights: **500 for body text**, 600 for labels, table headers and buttons, 700 for page titles,
  800 only for large numbers on the overview. Manrope 400 and lighter is not used anywhere.
- Numbers use tabular figures so columns line up.
- Mono text is JetBrains Mono at 500 inline and 400–450 in large code and log blocks.

### What "no AI slop" means here

These rules are checked at every review:

- No thin or light font weights, and no grey-on-grey text that fails contrast.
- No tiny uppercase, letter-spaced labels as decoration (the current sidebar group titles are one).
  Sentence case everywhere.
- No gradients, glows, blurred blobs, glass effects, emoji or sparkle icons.
- No purple or indigo "default" accent; the palette is the brand's.
- Borders separate things; shadows are only for things that float (menus, dialogs). Corner radius
  6–8 px, not pill-shaped cards.
- One icon set, one stroke width (2 px at 16 px), used for meaning, not decoration.
- Text stays plain and specific, as it is now ("Can't reach multipass", not "Oops! Something went wrong").

---

## Workflow for each phase

1. Build or change the pieces listed for the phase.
2. Check them on `/kit` in light and dark, with the keyboard only, and at 375 px wide.
3. From R4 on, also check against both test clusters: multipass (Gateway API and metrics) and
   `opscope-test` (neither). Local test servers use ports 8090/8091.
4. `npm run build` passes; note the bundle size if it changed noticeably.
5. Tick the boxes here, write the phase notes, then you review it before it's committed.

---

## Phase R0: Foundations

Goal: new tokens, fonts, themes and brand assets are in place, and the existing app still works on top of them.

- [x] Brand assets in `frontend/public/`: `favicon.ico`, `favicon.svg`, Apple and Android icons,
      maskable icon, `site.webmanifest`
- [x] `index.html`: icon links and manifest from `head-snippet.html`, `theme-color` for both themes,
      title "Opscope". The `og:` tags are left out: Opscope is self-hosted with no fixed address, and
      the GitHub social preview is uploaded by hand (see R8)
- [x] Check that the Go server serves the new root files (`/favicon.ico`, `/site.webmanifest`) rather
      than the `index.html` fallback
- [x] Fonts: `@fontsource-variable/manrope` and `@fontsource-variable/jetbrains-mono` replace IBM Plex
- [x] `src/styles/tokens.css`: colour, type, spacing, radius, shadow and motion tokens, for light and dark
- [x] Theme: a `data-theme` attribute on `<html>`, set by a tiny inline script before the page draws
      (so there's no white flash in dark mode), plus a `useTheme` hook
- [x] `src/styles/base.css`: reset, body text, links, focus ring, selection colour, scrollbars,
      `prefers-reduced-motion`
- [x] Old class names keep working on the new tokens for now, so every page still renders
      (temporary; removed in R8)
- [x] `/kit` route, development only, with sections for colours (with contrast ratios) and type
- [x] "OpScope" → "Opscope" in UI text

## Phase R1: Basic components

Goal: the small building blocks, each with all its states.

- [ ] `icons.jsx`: the Lucide subset (nav sections, refresh, copy, search, chevrons, check, x, eye,
      external link, alert, info, sun/moon/monitor, menu, ...)
- [ ] `Logo`: the mark inlined with `currentColor` (follows the theme, pupil stays orange), plus the
      wordmark SVG; the tile icon below 24 px, as the brand rules ask
- [ ] `Button`: primary, secondary, quiet, danger; small and normal size; icon-only (with a label for
      screen readers); loading; disabled; works as a link too
- [ ] `TextInput`, `SearchInput` (with clear button), `Textarea`
- [ ] `Select` (styled native select, for short fixed lists like "last 500 lines")
- [ ] `Checkbox` and `Switch`
- [ ] `SegmentedControl` (used for the theme switch)
- [ ] `Tag` and `StatusBadge` (dot + word, tones ok / warn / bad / neutral, small icon for warn and bad)
- [ ] `Spinner` and `Skeleton` (loading placeholders shaped like the content)
- [ ] `Tooltip` for icon buttons and cut-off text (instead of only the browser's `title`)
- [ ] `Kbd` for keyboard hints
- [ ] All of the above on `/kit`, hover / focus / disabled / loading shown side by side, both themes

## Phase R2: Data display components

Goal: everything that shows cluster data, tested on realistic sample data before it touches a page.

- [ ] Sample data in `src/kit/samples.js`, shaped like real API responses (long names, zero rows,
      errors, crash loops; no real secrets)
- [ ] `Card` and `Section` (title, optional action on the right, body)
- [ ] `StatTile` (label, big number, health line; clickable)
- [ ] `DataTable`: the current `ResourceTable` rebuilt. Header stays visible while scrolling, sort
      icons, row hover, a whole-row link (not only the name), skeleton rows while loading, empty and
      "no match" states, comfortable and compact density
- [ ] `Tabs` (with counts, keyboard arrows between tabs)
- [ ] `Callout`: info, warning and error, replacing `ErrorBox`, `.notice`, `.empty` and the
      metrics-server help box; technical details still folded away
- [ ] `EmptyState` (icon, one sentence, optional action)
- [ ] `CodeBlock` for YAML and config data: line numbers, copy button, wrap on/off, light YAML
      colouring (keys, values, comments) that works in both themes
- [ ] `LogView` surface: mono, line wrap, "live" indicator while following, jump to newest
- [ ] `UsageBar`, `Sparkline` and `StackBar` with legend, restyled on the new palette
- [ ] `FactGrid` (label/value pairs) and `KeyValueList` (labels, annotations)
- [ ] `EventList` (warnings and events, newest first, object links)

## Phase R3: Navigation and overlay components

Goal: the interactive pieces of the frame, fully usable with the keyboard.

- [ ] `Popover` / `Menu`: opens under its button, closes on Escape and outside click, arrow-key navigation
- [ ] `Combobox` for the namespace picker: type to filter, arrow keys, Enter, "All namespaces" on top
- [ ] `ClusterSwitcher`: each cluster with its status dot and version, and "Manage clusters" at the bottom
- [ ] `Breadcrumbs`
- [ ] `PageHeader`: title, short description, actions and "updated 10:42" on the right
- [ ] `ThemeSwitch` (System / Light / Dark)
- [ ] `Drawer` for the sidebar on narrow screens
- [ ] `Dialog`, so removing a cluster asks in-app instead of with `window.confirm`

## Phase R4: App shell

Goal: the new frame around the old pages, working on real clusters.

- [ ] Sidebar: logo, icon + label per page, group titles in sentence case, orange marker on the
      current page, Clusters link, server status and theme switch in the footer
- [ ] Top bar: breadcrumbs on the left; cluster switcher, namespace picker and connection status on the right
- [ ] Below about 900 px: sidebar becomes a drawer behind a menu button; pickers stay usable
- [ ] Shell-level states: loading the cluster list, unknown cluster, unreachable cluster
- [ ] Every existing page still works inside the new shell (pages themselves are restyled in R5–R7)
- [ ] Checked on multipass and `opscope-test`, light and dark, wide and narrow

## Phase R5: Overview and list pages

Goal: the pages people open most.

- [ ] Overview: header with version, namespace count and API server; stat tiles; cluster usage with
      sparklines; pods by status; nodes; recent warnings. Missing metrics-server and missing Gateway
      API look intentional, not broken
- [ ] All 16 list pages on `DataTable` with `PageHeader`, filter, count, refresh and "updated" time
- [ ] Nodes and Pods: usage columns on the new `UsageBar` and `Sparkline`
- [ ] "Not installed" (Gateway API) and "forbidden" (Secrets without RBAC) as `Callout`s
- [ ] Checked on both clusters

## Phase R6: Detail pages

Goal: one object's page, including logs and secrets.

- [ ] Header: kind tag, name, status, namespace and age, with tabs underneath
- [ ] Summary: `FactGrid`, owner links, containers, data, tables, conditions, labels, annotations
- [ ] YAML tab on `CodeBlock`
- [ ] Events tab on `EventList`
- [ ] Logs tab: container, lines, previous run and follow controls in one toolbar; `LogView`
- [ ] Secret keys: masked values, reveal / copy / hide per key, the base64 note
- [ ] Checked on a crash-looping pod, a node, a Gateway, an HTTPRoute, a Secret and a ConfigMap

## Phase R7: Clusters and first run

Goal: the pages around the clusters, and a good first impression.

- [ ] First run (no clusters): a welcome screen with the logo, one sentence on what Opscope does,
      and the two ways to connect
- [ ] Add a cluster: drop a kubeconfig file or paste it, pick a context, name it, test and save; clear
      progress and error states
- [ ] Manage clusters: source, context and server per cluster, remove with the new `Dialog`
- [ ] Not found page
- [ ] Remove `Placeholder.jsx` (every page exists since Phase 7)

## Phase R8: Polish and publish

Goal: ready to show people.

- [ ] Accessibility pass: contrast in both themes, keyboard-only walk through every page, focus
      order, screen-reader labels, reduced motion
- [ ] Safari, Firefox and Chrome; 375 px, 768 px and wide screens
- [ ] Remove the old class names kept in R0 and any CSS no longer used
- [ ] Bundle and font size check (only the font weights we use are shipped)
- [ ] Screenshots in both themes for the README; README and PHASES.md updated for the new look and name
- [ ] Upload `github-social-preview.png` as the repo's social preview (you, in GitHub settings)
- [ ] Docker image built and checked, and the in-cluster deployment on `opscope-test` checked once more

---

## Possible later

Considered and left out to keep the redesign focused:

- A ⌘K command palette to jump to any resource
- Syntax colouring beyond simple YAML
- Remembering column widths or hidden columns per table

---

## Phase notes

Things that come up while building, decisions made, and anything that moves between phases.

### Phase R0

- Checked on `/kit` (both themes, and at 375 px) and in the app against multipass, in light and
  dark: overview, pods, a pod's summary and logs, nodes, and Add a cluster. Every page still works
  on the new tokens. `opscope-test` waits until R4, when the shell changes.
- Colours were picked with a small script first (WCAG contrast and CIEDE2000 distance), then checked
  again in the browser: `/kit` reads each token's real computed value and shows its ratios. All 52
  checks pass in both themes.
- Two starting values failed and were changed. Amber (warn marks) on white was 2.3:1, so it's now
  `#AD8200` (3.5:1). Red was ΔE 17 from Signal orange in dark mode, so the reds moved towards
  crimson: `#C42847` light, `#E8506A` dark. Both now sit at least ΔE 23 from orange and 25 from each other.
- Status colours come in pairs: a text colour for words and a separate, slightly brighter mark
  colour for dots and bar segments (`--ok` / `--ok-mark`, and so on).
- `data-theme` on `<html>` is always the resolved theme, `light` or `dark`. The choice
  (`system`, `light` or `dark`) is kept in `localStorage`, and the script in `index.html` and
  `theme.js` both read it. Any element can carry `data-theme`, which is how `/kit` shows both themes
  at once. A choice made in one tab is picked up by the others.
- The fonts are variable fonts, one file per script. Browsers download only the Latin subset
  (25 KB for Manrope, 40 KB for JetBrains Mono).
- `styles.css` became `src/styles/legacy.css` (moved with `git mv`, so its history is kept). Its old
  token names point at the new tokens. Its ten or so hard-coded colours became tokens so dark mode
  works. Every font weight went up one step, since body text is now 500. The old navy accent now maps
  to Ink (or Paper in dark), and the current sidebar item and open tab use Signal orange.
- One backend change: Go's list of file types didn't include `.webmanifest`, so `site.webmanifest`
  was sent as `text/plain`. It's now `application/manifest+json`, with a test.
- `vite.config.js` takes an optional `OPSCOPE_API` variable, so the dev server can talk to a Go
  server on another port (`OPSCOPE_API=http://localhost:8090`). The default is still `:8080`.
- `/kit` is checked to be absent from the production bundle. Build size: JS 308 KB (96 KB gzipped),
  CSS 28 KB.

