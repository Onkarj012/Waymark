# Visual-first agent planning system

**Status:** Draft — awaiting user approval<br>
**Last updated:** 2026-08-13<br>
**Priority:** P1<br>
**Effort:** L<br>
**Risk:** Medium<br>
**Waymark base:** `feat/enhanced-dark-and-live-nav` at `2d61439`<br>
**Shared skills base:** `$SKILLS_ROOT` at `d19384b` (set `SKILLS_ROOT` to the
shared skills checkout before running the commands below)

> **Approval boundary:** Creating and reviewing this plan does not authorize
> publication, implementation, delegation to an executor, branch creation,
> commits, issues, or pull requests. After the local HTML document is accepted,
> the approval request must name the exact next actions it authorizes. Until
> that explicit approval, stop at the plan.

## Executive summary

Replace text-heavy agent plans with visual, self-contained HTML plans that help a human
understand the destination, change, sequence, dependencies, risks, and proof of
completion before reading executor detail. Every plan will retain Waymark's
shared visual language while receiving a controlled identity based on its
shape: system map, journey, migration, decision, or delivery.

The work spans two repositories:

1. **Waymark** supplies the dark-first theme, reusable visual components,
   shared renderer, and deterministic local browser inspection.
2. **SKILLS** teaches planning agents to create synchronized Markdown and
   complete self-contained HTML, validate it once, show one rendered preview, and stop for
   approval before publishing or handing work to an executor.

The workflow is intentionally bounded: one composition pass, automated
validation, one desktop/mobile/print smoke inspection, and at most one repair
pass. If an ambitious diagram is costly or unreliable, the agent falls back to
a documented component and moves on.

## Destination

An explicit `$planning` run ends at a locally reviewable draft containing:

- `docs/plans/<slug>.md` — canonical specification;
- `docs/plans/<slug>.html` — complete, self-contained HTML document;
- `docs/plans/<slug>.waymark.json` — status, checksums, and publication
  metadata;
- one local inspection of the complete HTML document and one representative
  screenshot returned with a short orientation summary.

The first viewport answers six questions without requiring a long read:

1. Where are we going?
2. What changes from the current state?
3. Which decisions are settled?
4. In what order will the work happen?
5. What is the principal risk?
6. How will completion be proven?

The plan remains a draft until the user explicitly approves it. Publication and
handoff are downstream actions, never implicit consequences of plan creation.

## Current state and verified evidence

### Waymark repository

- The working tree is clean on `feat/enhanced-dark-and-live-nav` at `2d61439`.
  The branch is 257 changed lines ahead of `origin/main` across the Go renderer,
  Worker renderer, theme, demo, and tests. There is no GitHub pull request for
  it. The visual-plan implementation must therefore be stacked on `2d61439` or
  wait for that branch to merge; starting from `main` would discard required
  dark-register and live-navigation work.
- `theme/theme.css` is the single stylesheet source. It already provides a
  responsive section navigation, reading progress, headers, stats, tables,
  badges, callouts, facts, print rules, reduced-motion behavior, and a tested
  light/dark color register.
- The current component vocabulary does **not** include plan identity, change
  maps, architecture diagrams, roadmaps, phase cards, dependency flows,
  decision comparisons, file-impact maps, risk displays, verification boards,
  or open-question panels.
- First-time theme selection currently follows the visitor's OS preference in
  both `internal/web/server.go` and `workers/src/render.ts`. The user-selected
  light/dark choice is already persisted in `localStorage`.
- Go and Workers independently mirror the themed document wrapper and its
  scripts. The Go CLI cannot reuse the current renderer because
  `renderThemed` is private to `internal/web`.
- `cmd/waymark/main.go` has create/list/get/update/delete and authentication
  commands, but no complete-document validator.
- The repo-local `.skills/waymark/SKILL.md` is older than the global shared
  `$SKILLS_ROOT/skills/waymark/SKILL.md`; their component reference
  files currently match. This drift must be reconciled before adding another
  contract.
- Baseline verification on 2026-08-13 passed:
  - `go test ./... && go vet ./...`
  - Workers: 6 files and 69 tests passed
  - `npm run typecheck`
  - `npm run typecheck:test`

### Shared skills repository

- `$SKILLS_ROOT` is at `d19384b` and is one commit ahead of
  `origin/main`.
- Its working tree contains unrelated user edits in `bespoke-ui` and
  `design-directions`. Implementation must use an isolated worktree or another
  non-overlapping branch and must not stage, rewrite, or clean those files.
- `skills/planning/SKILL.md` keeps Markdown canonical and approval-gates
  Waymark publication, but it does not require a local HTML source, visual
  identity, checksum freshness, preview screenshot, bounded repair count, or a
  separate no-handoff-before-approval state.
- `skills/planning/references/artifact-contract.md` defines a twelve-section
  specification and older Waymark metadata, but no complete-document rendering
  contract beyond generic section navigation.
- `skills/waymark/references/components.md` documents the generic Waymark
  components, not the plan-specific visual grammar agreed here.

## Settled decisions

These are user decisions, not implementation assumptions:

1. The behavior applies to every explicit `$planning` run system-wide.
2. Plans serve both humans and executors through progressive disclosure.
3. Agents receive constrained creativity, not a fixed template; each raw
   document owns its visual styling.
4. Markdown remains canonical; complete HTML and metadata remain local, and an
   approved Waymark page is the public view.
5. Waymark gains reusable plan-specific visual components.
6. The handoff includes an orientation summary, one rendered preview, and the
   eventual URL; it is not a naked Markdown link.
7. Publication and executor handoff are approval-gated.
8. The first screen is an orientation layer rather than a wall of prose.
9. Every plan has a common visual backbone with optional plan-shaped modules.
10. Diagrams use semantic HTML or native SVG with textual equivalents; plan
    documents own their CSS in a `<style>` element and do not supply JavaScript.
11. Dark is the first-visit default; a persistent light-mode toggle remains.
12. Each plan receives a document-owned visual treatment while remaining recognizably
    Waymark.
13. Relationships should be visualized when doing so improves understanding;
    there is no arbitrary visual-count quota.
14. Decorative generated imagery is not used by default.
15. Plan generation is bounded to one composition, one validation, one visual
    smoke pass, and at most one repair.
16. A hard-to-render visual falls back to a clear component rather than
    blocking the plan.
17. After approval, the plan is handed off; it is not continually polished or
    updated after every implementation step.
18. Before approval, nothing is published, handed off, or implemented.

## Scope

### In scope: Waymark

- Factor the Go themed-document wrapper into a small standard-library-only
  package reusable by the server and CLI.
- Make dark the first-visit default while preserving a persisted light choice.
- Keep plan-component CSS inside each complete raw document.
- Document accessible markup for every new component.
- Expand the theme demo into a realistic visual-plan example.
- Keep local browser inspection offline and independent of Waymark
  authentication or network access.
- Preserve Go/Workers rendering parity and add focused regression tests.
- Update repo documentation and the repo-local Waymark skill.

### In scope: shared skills

- Extend the planning artifact contract to include complete raw HTML, status,
  format, and source/render checksums.
- Add a small Node-standard-library artifact helper that stamps and verifies
  metadata without introducing package dependencies.
- Teach the planning skill the visual-first composition rules, bounded quality
  loop, local document inspection, and explicit approval state machine.
- Mirror the repo-local Waymark component contract into the shared Waymark
  skill and synchronize the installed `.agents` and `.codex` copies.

### Non-goals

- A drag-and-drop page builder or general design application.
- Plan-supplied JavaScript, external stylesheets, or third-party diagram
  runtimes such as Mermaid.
- AI-generated decorative hero art by default.
- Private Waymark pages, authentication changes, database migrations, or new
  API endpoints.
- Automatic implementation, automatic subagent handoff, issue creation, PR
  creation, or publication before approval.
- Continuous plan progress updates after every commit or phase.
- A validator that judges prose quality, demands a fixed number of visuals, or
  repeatedly asks the agent to polish subjective details.

## Requirements

### R1 — Human orientation

Every visual plan begins with a concise identity header and orientation layer
covering destination, change, sequence, primary risk, and completion proof.
Technical evidence follows rather than competing with the first screen.

### R2 — Document-owned identity

Each plan chooses semantic components that match its subject. Its complete HTML
document owns its visual treatment in `<style>`, while Waymark's legacy theme
remains available only when reading older themed records. Layout must work in
dark, light, responsive, reduced-motion, and print modes. Status is always
communicated by text as well as color.

### R3 — Visual vocabulary

The plan documents reusable semantic markup for:

- identity header;
- before/after change map;
- architecture/data-flow figure;
- phase roadmap and phase detail;
- dependency flow;
- decision comparison;
- file-impact map;
- risk display;
- verification board;
- open-question panel.

The components are a vocabulary, not a checklist. The agent selects only those
that clarify real relationships in the source material. Every diagram has a
caption or adjacent textual equivalent.

### R4 — Progressive disclosure

The visual story remains scannable. Exact paths, symbols, commands, expected
results, and STOP conditions live in phase details below the overview. Native
`<details>` may be used, but core information must remain available without
scripts.

### R5 — Local inspection of a complete document

The inspectable HTML file is the publication source:

- it is a complete, self-contained document with doctype, html, head, viewport
  meta, title, style, and body;
- it does not link `/theme.css` and does not use `data-plan-profile`;
- inspection happens against the local file URL;
- it never publishes anything and never opens a browser automatically as a
  side effect of stamping or verification.

### R6 — Synchronized artifacts

Version-3 plan metadata records:

```json
{
  "version": 3,
  "status": "draft",
  "title": "Example plan",
  "slug": "example-plan",
  "source_path": "docs/plans/example-plan.md",
  "source_sha256": "<sha256>",
  "html_path": "docs/plans/example-plan.html",
  "html_sha256": "<sha256>",
  "page_id": null,
  "url": null,
  "ttl_days": 0,
  "last_known_updated_at": null
}
```

Allowed statuses are `draft`, `approved`, `completed`, and `superseded`.
Execution progress does not turn the plan into a task tracker. The artifact
helper updates checksums deliberately and verifies them read-only before
inspection or publication.

### R7 — Bounded planning loop

The planning skill performs exactly this quality loop:

1. Compose Markdown and complete HTML once.
2. Stamp metadata and run structural validation once.
3. Inspect the complete HTML document on desktop, mobile, and print in one
   browser session.
4. If a concrete defect exists, make one repair and re-run only the failed
   check.
5. If the repair still fails, stop and report the limitation. Do not continue
   polishing.

Subjective preference, lack of decorative novelty, or the possibility of a
more elaborate diagram is not a failed gate.

### R8 — Approval state machine

The planning agent may inspect the repository and create local plan artifacts.
It then shows the title, destination, canonical paths, visual treatment,
permanent/TTL choice, remaining questions, one preview, and the exact actions
that approval would authorize. It stops there.

Before explicit approval it must not publish, delegate to an executor, begin
implementation, create a branch or commit, file an issue or PR, or mutate any
external system. Following approval it performs only the named actions.

## Proposed design

### Shared Go renderer

Create `internal/render` as a deep, standard-library-first module containing
the themed document wrapper and progressive-enhancement scripts. It exposes:

- a hosted renderer that links `/theme.css` for `internal/web`;
- a standalone renderer that embeds `theme.CSS` for offline CLI previews;
- a complete-document validator shared by API and CLI paths.

`internal/web/server.go` becomes a consumer instead of owning private renderer
constants. `cmd/waymark` imports only `internal/render` and `theme`, preserving
the CLI's no-SQLite/no-cgo dependency boundary. Workers continue to mirror the
wrapper in TypeScript, with parity assertions in both test suites.

### Dark-first selection

On first visit, the wrapper applies dark mode before paint. A stored value of
`light` removes the dark attribute; a stored `dark` value retains it. The
toggle continues to persist the user's explicit choice. The OS media-query
listener is removed because it conflicts with a deterministic dark default.
Print continues to use the existing high-contrast light register.

### Document-owned visual components

The inspectable HTML is a complete document whose body may begin with
`<article class="plan">`. Its `<style>` element owns component-level variables
without changing semantic status colors. Use CSS Grid/Flexbox for ordinary
relationships and inline SVG inside a documented `.plan-diagram` figure for
graphs that genuinely require edges. Components collapse to one column on
small screens and avoid connector-dependent meaning in print.

### Artifact helper

`skills/planning/scripts/plan-artifact.mjs` uses only Node built-ins. Its
`stamp` operation computes SHA-256 values and updates only the version-3
artifact fields while preserving Waymark page metadata. Its `verify` operation
checks schema, status, canonical paths, hashes, complete-document structure,
and navigation targets. It does not score prose or require a visual count.

### Approval-oriented agent workflow

The planning skill becomes a small state machine:

```text
settled decisions
      │
      ▼
local Markdown + complete HTML
      │
      ▼
stamp → verify → preview → one smoke inspection
      │
      ▼
AWAITING USER APPROVAL
      │
      ├── changes requested ──▶ one bounded revision
      │
      └── explicit approval ──▶ publish + named handoff actions
```

The visual treatment and component selection are agent decisions constrained by
the verified plan shape. A fallback component is always preferred over another
open-ended design iteration.

## Ordered implementation phases

### Phase 0 — Establish safe integration boundaries

**Outcome:** Both repositories have isolated, non-destructive implementation
branches with their existing work preserved.

1. Treat Waymark commit `2d61439` as the required base. Create
   `feat/visual-plan-system` from it. If the predecessor branch remains
   unmerged, make the new PR explicitly target
   `feat/enhanced-dark-and-live-nav`; retarget to `main` only after the
   predecessor merges.
2. Create an isolated SKILLS worktree from `d19384b`. Do not use or clean the
   dirty primary checkout.
3. Reconcile the global additions in
   `$SKILLS_ROOT/skills/waymark/SKILL.md` into the repo-local
   `.skills/waymark/SKILL.md` before making visual-plan edits. Preserve the
   repo-local component reference as the code-adjacent contract, then mirror
   it into the shared distribution in Phase 5.

**Verify:**

```bash
git status --short --branch
git -C "$SKILLS_ROOT" status --short --branch
```

Expected: the Waymark implementation branch is clean before edits; unrelated
SKILLS edits remain only in the original checkout and are absent from the
isolated worktree.

**STOP if:** `2d61439` is not an acceptable dependency base, either planned
worktree path already contains work, or reconciling the skill would discard a
newer instruction.

### Phase 1 — Share rendering and make dark the first-visit default

**Files:**

- Create `internal/render/render.go`
- Create `internal/render/render_test.go`
- Update `internal/web/server.go`
- Update `internal/web/server_test.go`
- Update `workers/src/render.ts`
- Update `workers/test/render.test.ts`
- Update `theme/theme.css`
- Update `theme/demo.html`
- Update `theme/theme_test.go`

**Work:**

1. Move the Go document wrapper, toggle, progress, and section-navigation
   markup/scripts into `internal/render`.
2. Provide hosted and standalone render entry points without importing store or
   SQLite code.
3. Change first-visit behavior to dark; retain and test persisted light.
4. Remove OS-preference switching from Go, Workers, and the demo.
5. Keep Worker output behaviorally equivalent and retain comments identifying
   the mirrored contract.

**Verify:**

```bash
go test ./internal/render ./internal/web ./theme ./cmd/waymark
go vet ./...
! go list -deps ./cmd/waymark | rg -q 'modernc.org/sqlite'
cd workers && npm test && npm run typecheck && npm run typecheck:test
```

Expected: all checks exit 0; CLI dependencies contain no SQLite package; Go
and Worker tests prove dark-first initialization and persisted light behavior.

### Phase 2 — Add the visual-plan component grammar

**Files:**

- Update `theme/theme.css`
- Update `theme/demo.html`
- Update `theme/theme_test.go`
- Update `.skills/waymark/SKILL.md`

**Work:**

1. Keep the ten documented visual component families from R3 in the
   document-owned `<style>` block.
2. Make every layout responsive, printable, reduced-motion safe, and usable
   without color.
4. Use a realistic plan in `theme/demo.html` to demonstrate orientation,
   roadmap, dependencies, risk, and verification without turning the demo into
   an exhaustive component catalogue.
5. Add markup examples and accessibility rules to the component reference.
6. Extend contract and contrast tests rather than snapshotting every CSS byte.

**Verify:**

```bash
go test ./theme ./internal/render ./internal/web
git diff --check
```

Expected: tests cover every public component selector, responsive collapse,
print fallback, and visible text status.

### Phase 3 — Enforce raw-only writes

**Files:**

- Update `internal/render/render.go` and `internal/render/render_test.go`
- Update `internal/web/server.go` and `internal/web/server_test.go`
- Update `workers/src/render.ts` and Worker tests
- Update `cmd/waymark/main.go` and CLI tests

**Work:**

1. Require complete self-contained HTML for every new page.
2. Validate doctype, document roots, viewport metadata, title, style, and body.
3. Reject house-theme links and `data-plan-profile` in submitted markup.
4. Preserve legacy themed reads and metadata-only updates.
5. Convert legacy pages to raw mode when complete replacement HTML is supplied.

**Verify:**

```bash
go test ./... && go vet ./...
cd workers && npm test && npm run typecheck && npm run typecheck:test
```

Expected: Go and Worker APIs reject themed writes, serve raw bytes verbatim, and
retain legacy read compatibility.

### Phase 4 — Version and verify planning artifacts

**Files in the isolated SKILLS worktree:**

- Update `skills/planning/references/artifact-contract.md`
- Create `skills/planning/scripts/plan-artifact.mjs`
- Create `skills/planning/scripts/plan-artifact.test.mjs`
- Update `skills/planning/SKILL.md`

**Work:**

1. Use `docs/plans/<slug>.md`, `docs/plans/<slug>.html`, and
   `docs/plans/<slug>.waymark.json` as the canonical three-file artifact set.
2. Stamp version-3 metadata with `format: "raw-self-contained"`.
3. Implement bounded `stamp` and read-only `verify` operations.
4. Validate synchronization and complete-document structure only; keep
   subjective design judgment out of the script.
5. Preserve page ID, URL, TTL, and last-known update time when restamping an
   existing plan.
6. Treat an unexpected remote update timestamp as a hard publication STOP.

**Verify:**

```bash
node --test skills/planning/scripts/plan-artifact.test.mjs
node skills/planning/scripts/plan-artifact.mjs --help
```

Expected: tests cover stamping, stale Markdown, stale HTML, forbidden legacy
format markers, missing document elements, missing anchors, preservation of
remote metadata, and a clean valid artifact.

### Phase 5 — Teach agents the visual and approval workflow

**Files:**

- Update `skills/planning/SKILL.md`
- Update `skills/planning/references/artifact-contract.md`
- Update `skills/waymark/SKILL.md`
- Mirror `.skills/waymark/SKILL.md` to
  `skills/waymark/references/components.md`
- Update `$SKILLS_ROOT/README.md`
- Synchronize installed copies under the configured agent skill destinations

**Work:**

1. Encode the human-first orientation layer and plan-shape-to-component mapping.
2. Require visuals for meaningful relationships without a numeric quota.
3. Encode the one-compose/one-check/one-repair ceiling and fallback rule.
4. Encode the draft → approval → publication/handoff state machine literally.
5. Require the approval prompt to enumerate the exact actions approval unlocks.
6. Keep legacy themed writes, plan-supplied scripts, external stylesheets,
   secrets, and invented facts prohibited.
7. Install from the validated shared sources and compare all copies byte for
   byte.

**Verify:**

```bash
diff -ru "$SKILLS_ROOT/skills/planning" "$AGENTS_SKILLS_ROOT/planning"
diff -ru "$SKILLS_ROOT/skills/planning" "$CODEX_SKILLS_ROOT/planning"
diff -ru "$SKILLS_ROOT/skills/waymark" "$AGENTS_SKILLS_ROOT/waymark"
diff -ru "$SKILLS_ROOT/skills/waymark" "$CODEX_SKILLS_ROOT/waymark"
```

Expected: every diff is empty. The original dirty SKILLS checkout still
contains exactly the unrelated user edits it had before this work.

### Phase 6 — Run one bounded end-to-end proof

**Work:**

1. Create one representative plan fixture containing verified synthetic
   content, a controlled identity, change map, roadmap, dependency view, risk,
   and verification board.
2. Stamp and verify it with the planning helper.
3. Render it using the built CLI.
4. Inspect desktop, mobile, and print in one browser session.
5. Make at most one repair for a concrete defect.
6. Confirm that the workflow stops at `draft` and emits the approval prompt;
   do not publish or hand off the fixture.

**Verify:**

```bash
go test ./... && go vet ./...
go build -o bin/waymark ./cmd/waymark
cd workers && npm run generate:theme && npm test && npm run typecheck && npm run typecheck:test
node --test "$SKILLS_ROOT/skills/planning/scripts/plan-artifact.test.mjs"
git diff --check
```

Expected: all commands exit 0; visual evidence shows a readable first screen,
usable mobile collapse, and legible print output; no page, issue, PR, branch
handoff, or executor run is created by the proof.

## Acceptance and verification criteria

### Product acceptance

- [ ] A first-time visitor sees dark mode; a user-selected light mode persists.
- [ ] Every plan uses Waymark's readable structure while its document-owned
      components give it a clear identity.
- [ ] The first screen communicates destination, change, sequence, primary
      risk, and completion proof.
- [ ] Plan-shaped relationships use visual components when useful; prose is not
      merely placed into decorated boxes.
- [ ] All visuals retain a caption or textual equivalent and work without
      color.
- [ ] Exact implementation files, commands, tests, and STOP conditions remain
      available below the human overview.
- [ ] A difficult visualization falls back cleanly without blocking the plan.
- [ ] Planning stops after the bounded quality loop.
- [ ] No publication or handoff occurs before explicit user approval.

### Technical acceptance

- [ ] `theme/theme.css` remains the only stylesheet source.
- [ ] Hosted Go and hosted Workers preserve legacy themed-read behavior, while
      raw documents deliver exact submitted bytes.
- [ ] New and replacement writes require complete raw HTML, while legacy themed
      pages remain readable and metadata-only updates remain safe.
- [ ] The CLI remains standard-library-first and does not acquire SQLite/cgo.
- [ ] Version-3 metadata detects stale Markdown or HTML before publication.
- [ ] The artifact verifier is structural and bounded, not a subjective quality
      loop.
- [ ] Repo-local, shared, and installed component/skill contracts match.
- [ ] All Go and Worker baseline checks remain green.

### Approval acceptance

- [ ] The draft handoff shows title, destination, canonical paths, visual
      format, TTL/permanent choice, remaining questions, preview, and proposed
      post-approval actions.
- [ ] The agent stops after showing the draft.
- [ ] Approval authorizes only the actions listed in that approval prompt.

## Risks and mitigations

| Risk | Consequence | Mitigation |
| --- | --- | --- |
| The predecessor theme branch is unmerged | New work silently loses or duplicates dark/nav changes | Stack on `2d61439`; use an explicit PR dependency and retarget only after merge |
| Go and Worker wrappers drift | Local preview differs from production | Centralize Go rendering; retain mirrored Worker tests for every wrapper behavior |
| Component library becomes a mini design system project | Planning slows down and implementation scope balloons | Limit the public vocabulary to the ten agreed families; reuse generic primitives internally |
| Agents optimize for visual count | Decorative diagrams replace useful explanation | Validate structure, not quantity; teach relationship-to-visual mapping and fallback behavior |
| Validator becomes an endless quality gate | Agents plan indefinitely instead of executing | One structural pass, one browser smoke, one repair maximum; stop and report after that |
| Raw HTML introduces active-content risk | Published plan can execute unsafe code | Keep JavaScript out of plan documents, validate structure, and escape inserted external text |
| Dark-first harms readers who need light mode | Reduced usability | Keep persistent light toggle and print-specific light register |
| Shared skill installs drift | Different agents create incompatible plans | Reconcile repo/global sources, sync installed copies, require empty recursive diffs |
| Dirty SKILLS checkout is contaminated | Unrelated user work is overwritten or committed | Use an isolated worktree and verify the original checkout before and after |
| Metadata checksums become stale after legitimate edits | Valid plans cannot publish | Provide an explicit `stamp` operation; never auto-restamp during read-only verification |

## STOP conditions

Stop implementation and report rather than improvising if:

- the current Waymark base no longer contains `2d61439` or its dark/live-nav
  behavior;
- the implementation would require collapsing public and control origins;
- local inspection would import the store/SQLite dependency;
- a component requires plan-supplied JavaScript or an external stylesheet;
- the SKILLS isolation step risks modifying the unrelated dirty files;
- published metadata changed unexpectedly since the last local record;
- satisfying a visual layout requires more than the single allowed repair pass;
- a material decision in this plan proves false.

## Non-blocking open questions

None. Exact CSS measurements, internal helper names, and the choice of a
specific representative fixture are implementation details constrained by the
requirements above; they do not change scope, behavior, risk, or approval.

## Approval request

This plan is ready for review, not execution.

- **Canonical source:** `docs/plans/visual-plan-system.md`
- **Self-contained HTML:** `docs/plans/visual-plan-system.html`
- **Waymark metadata:** `docs/plans/visual-plan-system.waymark.json`
- **Publication lifetime:** permanent (`ttl_days: 0`)
- **Material open questions:** none
- **Not yet authorized:** publication, implementation, delegation/handoff,
  branches, commits, issues, or pull requests

When requesting approval, state separately whether approval authorizes only
publication, or publication plus implementation handoff. Do nothing downstream
until the user explicitly chooses.
