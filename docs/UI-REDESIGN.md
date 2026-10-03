# Opscope: UI redesign

The app works (see [PHASES.md](PHASES.md)); this plan gives it a look that matches the new brand kit
and is good enough to publish. It changes the frontend only. The backend, the API and what each page
shows stay the same.

We work the same way as before: one small phase at a time, each ending with a working app, a review,
ticked boxes and notes at the bottom of this file. Phases are numbered **R0–R8** so they don't mix
with the build phases 0–8.

**Status (2026-10-03):** R0–R2 done; R3 next.

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
- One icon set, one line weight (1.6 px at any size), used for meaning, not decoration.
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

- [x] `icons.jsx`: the Lucide subset (nav sections, refresh, copy, search, chevrons, check, x, eye,
      external link, alert, info, sun/moon/monitor, menu, ...)
- [x] `Logo`: the mark inlined with `currentColor` (follows the theme, pupil stays orange), plus the
      wordmark SVG; the tile icon below 24 px, as the brand rules ask
- [x] `Button`: primary, secondary, quiet, danger; small and normal size; icon-only (with a label for
      screen readers); loading; disabled; works as a link too
- [x] `TextInput`, `SearchInput` (with clear button), `Textarea`
- [x] `Select` (styled native select, for short fixed lists like "last 500 lines")
- [x] `Checkbox` and `Switch`
- [x] `SegmentedControl` (used for the theme switch)
- [x] `Tag` and `StatusBadge` (dot + word, tones ok / warn / bad / neutral, small icon for warn and bad)
- [x] `Spinner` and `Skeleton` (loading placeholders shaped like the content)
- [x] `Tooltip` for icon buttons and cut-off text (instead of only the browser's `title`)
- [x] `Kbd` for keyboard hints
- [~] All of the above on `/kit`, both themes; disabled and loading shown side by side, hover and
      focus tried for real (see notes)

## Phase R2: Data display components

Goal: everything that shows cluster data, tested on realistic sample data before it touches a page.

- [x] Sample data in `src/kit/samples.js`, shaped like real API responses (long names, zero rows,
      errors, crash loops; no real secrets)
- [x] `Card` and `Section` (title, optional action on the right, body)
- [x] `StatTile` (label, big number, health line; clickable)
- [x] `DataTable`: the current `ResourceTable` rebuilt. Header stays visible while scrolling, sort
      icons, row hover, a whole-row link (not only the name), skeleton rows while loading, empty and
      "no match" states, comfortable and compact density
- [x] `Tabs` (with counts, keyboard arrows between tabs)
- [x] `Callout`: info, warning and error, replacing `ErrorBox`, `.notice`, `.empty` and the
      metrics-server help box; technical details still folded away
- [x] `EmptyState` (icon, one sentence, optional action)
- [x] `CodeBlock` for YAML and config data: line numbers, copy button, wrap on/off, light YAML
      colouring (keys, values, comments) that works in both themes
- [x] `LogView` surface: mono, line wrap, "live" indicator while following, jump to newest
- [x] `UsageBar`, `Sparkline` and `StackBar` with legend, restyled on the new palette
- [x] `FactGrid` (label/value pairs) and `KeyValueList` (labels, annotations)
- [x] `EventList` (warnings and events, newest first, object links)

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

### Phase R1

- Checked on `/kit` in both themes and at 375 px, and in the app against multipass: pods (filter,
  refresh, statuses), a pod's logs toolbar (selects, Previous run checkbox, Follow), Add a cluster
  (including the error state, with the message linked to the text box for screen readers) and Manage clusters.
- Components live next to their own CSS in `src/components/`: `Button`, `Field` (with `TextInput`,
  `SearchInput`, `Textarea`, `Select`), `Toggle` (`Checkbox`, `Switch`, `SegmentedControl`), `Tag`
  (with `Kbd`), `Loading` (`Spinner`, `Skeleton`, `SkeletonText`), `Tooltip`, `Logo` and `icons`.
- The new components reuse the old class names (`.button`, `.input`, `.select`, `.field-*`, `.tag`,
  `.status`, `.checkbox`), and the matching rules were deleted from `legacy.css`. A component's CSS
  only ships when something imports the component. So every place that used those classes as raw
  markup now uses the component instead: the filter box, refresh, copy, reveal and log buttons, the
  Add a cluster form, tags and the top bar's selects. Page layouts are unchanged; that work is R5–R7.
- `StatusBadge` was rebuilt in place with the same props, so every table picked it up. Warn and bad
  statuses now show an icon instead of the dot, and their text is weight 600.
- Icons are generated from Lucide's SVGs by a script, so the shapes are exact. Lucide's 2-unit line
  is only 1.33 px at 16 px and looked thin next to Manrope 500. The icons now keep a 1.6 px line at
  every size; the plan's "2 px" turned out heavy in the small details.
- The logo is drawn inline from the brand kit's paths: the body uses `currentColor`, the pupil
  `--signal`. The full logo defaults to 26 px high, which keeps the brand's 96 px minimum width.
- Hover and focus states aren't faked side by side on `/kit`: that would mean extra state classes
  in every component just for the demo. They're checked for real instead (hover with the mouse, Tab
  through). Disabled and loading are shown side by side.
- Tooltips are drawn in a layer on top (so scrolling tables can't clip them), in inverted colours.
  They appear after 350 ms on hover, at once on keyboard focus, and close on Escape, scroll or click.
  Screen readers skip them, so whatever they wrap must carry its own words. On `/kit` a tooltip goes
  into the nearest themed panel, so a light panel on a dark page gets light-theme colours.
- The `<select>` arrow is a theme token (`--chevron`, an SVG in each theme's colour), so plain
  native selects need no wrapper element.
- Fixed while checking: the disabled select lost its arrow, because the `background` shorthand also
  resets the image. It now uses `background-color`.
- The existing code isn't strictly Prettier-formatted (13 files differ), so no formatter was run;
  new code follows the same style by hand.
- Build: JS 315 KB (98 KB gzipped), CSS 36 KB. `/kit` is still left out of the bundle.

### Phase R2

- Checked on `/kit` in both themes and at 375 px. Table: sorting, filtering with its "no match"
  state and Escape, row clicks, the header staying put while the box scrolls, and the loading and
  empty states. Tabs with the keyboard. Following logs, scrolling up and "Jump to newest". Folded
  annotations. In the app against multipass: the overview's pods-by-status bar, the Nodes usage
  bars and sparklines, and a pod's detail page.
- The new metrics-server notice was only seen on `/kit`. The `opscope-test` kind cluster is down
  (its container exited, code 137, a few hours before this phase), and it wasn't restarted without asking.
- Split of work: components that keep their props were rebuilt in place, so pages already use them:
  `UsageBar`, `Sparkline` and `MetricsUnavailable` (now a `Callout` with a `CodeBlock`), and
  `PodStatusBar` (now a thin wrapper around the general `StackBar`). Components that replace a
  different structure are new and get wired in with their pages in R5–R7: `DataTable`, `Card`,
  `Section`, `StatTile`, `Tabs`, `Callout`, `EmptyState`, `CodeBlock`, `LogView`, `FactGrid`,
  `KeyValueList` and `EventList`.
- Class name clashes: the old `.card`, `.card-title`, `.tabs`, `.tab` and `.tab-active` rules would
  have leaked into the new Card and Tabs, so they were renamed to `old-*` in `legacy.css` and in the
  overview and detail pages. The other new components use names the old CSS doesn't have
  (`data-table`, `callout`, `code-block`, ...).
- `DataTable` keeps `ResourceTable`'s column format, so `columns.jsx` works unchanged; `/kit` uses
  the real pod columns. Header cells take only the column's alignment, so a `mono` column doesn't
  set its header in monospace. Clicking anywhere on a row opens it (Cmd/Ctrl-click opens a new tab),
  except on links and buttons inside it or after selecting text. The name stays a real link for the
  keyboard and screen readers.
- The table header sticks inside the table's own box, because a box that scrolls sideways can't
  also let its header stick to the page. So the header only stays put when the box has a height:
  `maxHeight`. R5 should decide whether list pages give the table the rest of the window, so it
  scrolls inside and the header stays visible, or let the page scroll as now.
- YAML colouring is a line-by-line splitter, not a parser (`yamlParts` in `CodeBlock.jsx`). It
  colours keys, quoted strings, numbers/true/false/null and comments, and leaves plain values in the
  normal text colour so the YAML stays calm. Tested on URLs with `#`, quoted `#`, list items, colons
  inside values and quoted keys. Code colours are tokens (`--code-*`), all at least 5:1 on code blocks.
- Bugs found on `/kit` and fixed: with line numbers on, each coloured piece of a line became its own
  grid cell and wrapped (each line's content is now one element); and a code block without a title
  had an empty header bar (its buttons now sit beside the code instead).
- `LogView` only keeps the newest line in view while you're at the bottom, so reading older lines
  while following is no longer interrupted. "Live" is marked in Signal orange (it's what's being watched).
- `EventList` is a list instead of a table: long messages wrap under the reason and object instead
  of being cut to two lines in a narrow column.
- For R5: completed pods show "0/1" ready in the warning colour (the `Fraction` in the pods
  columns); that's expected for a finished pod and shouldn't look like a problem. A `/` shortcut to
  focus the filter would also fit the list pages.
- Build: JS 318 KB (99 KB gzipped), CSS 40 KB. `/kit` and its samples are still left out of the bundle.

