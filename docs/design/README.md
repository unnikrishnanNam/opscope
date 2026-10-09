# Design records

Larger changes to Opscope are planned in a design record before the work starts, and the record is
kept afterwards as the account of what was decided and why. The current documentation is in the
[README](../../README.md), [deployment.md](../deployment.md) and [api.md](../api.md); these records
describe how Opscope got there.

| Record                                    | Covers                                                        | Released |
| ----------------------------------------- | ------------------------------------------------------------- | -------- |
| [build-phases.md](build-phases.md)        | The first build: clusters, resources, details, logs, usage, packaging | v1.0.0 |
| [ui-redesign.md](ui-redesign.md)          | The interface: brand, light and dark themes, every page rebuilt | v1.1.0   |
| [command-palette.md](command-palette.md)  | The command palette, the command registry, keyboard shortcuts | v1.2.0   |
| [repositioning.md](repositioning.md)      | Opscope as an operations tool: licence, documentation, positioning | Next release |

## When to write one

Write a design record for a change that adds a page or a feature across several pages, changes how
data is loaded, adds a dependency, or affects security or permissions. A fix or a small,
self-contained change needs only an issue and a pull request.

Open an issue first. The record then starts as a pull request, so the plan is reviewed before the
work begins.

## Structure

Name the file after the feature in lowercase with hyphens (`label-search.md`). A record has these
parts; leave out any that don't apply.

```markdown
# Opscope: <Feature>

<One or two paragraphs: what this adds or changes, for whom, and what stays the same.>

**Status (YYYY-MM-DD):** <planned / in progress: which phase / done, released in vX.Y.Z>

**Legend:** `[x]` done · `[ ]` not done yet · `[~]` partly done or changed (see notes)

## Decisions

<What has been decided and why, one bullet each. Include what was considered and rejected when
that's useful later.>

## Design

<How it works: data shapes, endpoints, components, and how it fits what exists.>

## Phase <Xn>: <Name>

Goal: <what works at the end of this phase>.

- [ ] <a concrete, checkable step>

## Open questions

<Anything still to be decided, numbered. Move answers into Decisions.>

## Phase notes

<Added as each phase is done: what changed from the plan, what was found, how it was checked.>
```

Each phase ends with Opscope working, the tests passing and its notes written. Number phases with a
letter not used before (D, P and R are taken), so phase names never collide across records.

Once the work is released, update the status line and add the record to the table above. Its content
then stays as it was: later changes get their own record or are described in the current
documentation.
