# Theme Components

Use this reference when composing themed HTML. Components are an optional
vocabulary, not a page template: choose the smallest structure that improves
comprehension. Semantic source order, visible labels, and textual equivalents
carry meaning without layout or color. For visual-plan root, profile, paths,
metadata, and body safety, read the
[planning artifact contract](../../planning/references/artifact-contract.md).

Themed pages contain body HTML only. Per-plan scripts and styles stay outside
the component grammar; use the documented classes and structures.

## Section navigation

Use for longer reports with several major sections. Keep links and section IDs
in reading order. For a visual plan, keep the plan article as the single body
root:

```html
<article class="plan page-layout" data-plan-profile="delivery">
  <aside class="section-nav" aria-label="On this page">
    <p class="label">On this page</p>
    <nav>
      <ul>
        <li><a href="#overview">Overview</a></li>
        <li><a href="#details">Details</a></li>
        <li><a href="#decision">Decision</a></li>
      </ul>
    </nav>
  </aside>
  <div class="page-content">
    <section id="overview"><h2>Overview</h2></section>
    <section id="details"><h2>Details</h2></section>
    <section id="decision"><h2>Decision</h2></section>
  </div>
</article>
```

## Identity header

Use at the start of a visual plan. Include only facts known from the task; a
two-to-five-entry register is usually enough:

```html
<header class="plan-identity">
  <p class="identity-kicker">Migration plan</p>
  <h1>Move page delivery to the edge</h1>
  <p class="dek identity-summary">Preserve public links while separating control and content releases.</p>
  <dl class="identity-register">
    <div><dt>Owner</dt><dd>Platform systems</dd></div>
    <div><dt>State</dt><dd>Ready for implementation</dd></div>
    <div><dt>Constraint</dt><dd>No public URL changes</dd></div>
  </dl>
</header>
```

The conventional report header remains valid:

```html
<header>
  <h1>Program review</h1>
  <p class="dek">Performance, open questions, and next actions.</p>
</header>
```

## Before / after change map

Use for a genuine state transition. Keep `Before`, `Changes to`, and `After` as
visible text so direction survives small screens and print:

```html
<div class="change-map" aria-label="Current and target delivery model">
  <section class="change-state before">
    <span class="state-label">Before</span>
    <h3>Shared release</h3>
    <p>Reads and writes move together.</p>
  </section>
  <div class="change-direction" aria-label="Changes to">Changes to</div>
  <section class="change-state after">
    <span class="state-label">After</span>
    <h3>Bounded releases</h3>
    <p>Control and content deploy independently.</p>
  </section>
</div>
```

## Architecture / data-flow figure

Use an ordered list when sequence matters. Keep node names and notes in the
HTML; the connecting line reinforces the relationship. State what the figure
represents in a caption:

```html
<figure class="system-flow">
  <ol class="flow-track">
    <li class="flow-node"><span class="flow-label">Client</span><span class="flow-note">Scoped request</span></li>
    <li class="flow-node"><span class="flow-label">Control service</span><span class="flow-note">Validate and persist</span></li>
    <li class="flow-node"><span class="flow-label">Page store</span><span class="flow-note">HTML inline</span></li>
  </ol>
  <figcaption>Authenticated write flow.</figcaption>
</figure>
```

## Phase roadmap and detail

Use the roadmap for a short ordered program view. Pair it with ordinary
sections or a `phase-detail` block when a phase needs implementation detail.
Supported status hooks are `complete`, `active`, and `planned`; repeat each
status in visible `.status-text`:

```html
<ol class="phase-roadmap" aria-label="Delivery phases">
  <li data-status="complete"><span class="phase-index">Phase 01</span><span class="phase-name">Establish parity</span><span class="status-text">Complete</span></li>
  <li data-status="active"><span class="phase-index">Phase 02</span><span class="phase-name">Mirror traffic</span><span class="status-text">In progress</span></li>
  <li data-status="planned"><span class="phase-index">Phase 03</span><span class="phase-name">Cut over</span><span class="status-text">Planned</span></li>
</ol>

<div class="phase-detail">
  <h3>Phase 02 — mirror traffic</h3>
  <p class="phase-outcome"><strong>Outcome:</strong> responses match before user traffic moves.</p>
  <ul><li>Compare status, headers, and normalized bodies.</li></ul>
</div>
```

## Dependency flow

Use for ordered prerequisites or hand-offs. The short key locates an item; pair
it with a plain-language name and note:

```html
<ol class="dependency-flow">
  <li><span class="dependency-key">DB</span><div class="dependency-body"><span class="dependency-name">Schema parity</span><span class="dependency-note">Required before mirrored reads.</span></div></li>
  <li><span class="dependency-key">DNS</span><div class="dependency-body"><span class="dependency-name">Origin routes</span><span class="dependency-note">Activated during cutover.</span></div></li>
</ol>
```

## Decision comparison

Use for a small set of real alternatives. Set `data-recommendation` to
`recommended` or `rejected` and include the verdict in
`.decision-verdict`. Position, border, and color reinforce the verdict; visible
text carries it:

```html
<div class="decision-grid">
  <article class="decision-option" data-recommendation="rejected">
    <span class="decision-verdict">Not selected</span>
    <h3>Single cutover</h3>
    <p>Fast, but couples two rollback boundaries.</p>
  </article>
  <article class="decision-option" data-recommendation="recommended">
    <span class="decision-verdict">Recommended</span>
    <h3>Reads, then writes</h3>
    <p>Proves the public path first.</p>
  </article>
</div>
```

## File-impact map

Use for a bounded implementation footprint. Keep each path selectable text or
`code`, give it a visible action such as `Modify` or `No change`, and explain
why the file is affected:

```html
<ul class="file-impact">
  <li><code class="file-path">workers/src/content.ts</code><span class="impact-kind">Modify</span><span class="impact-note">Serve pages from the shared record.</span></li>
  <li><code class="file-path">cmd/waymark</code><span class="impact-kind">No change</span><span class="impact-note">The CLI remains the compatibility boundary.</span></li>
</ul>
```

## Risk display

Use `data-severity="high|medium|low"` as a styling hook. Repeat the complete
severity in `.status-text`, and give every risk a mitigation, owner, or next
action in the note:

```html
<ul class="risk-list">
  <li class="risk-item" data-severity="high">
    <span class="status-text">High risk</span>
    <div class="risk-body"><span class="risk-title">Wrong-origin routing</span><span class="risk-note">Mitigate with host-gating tests and a cutover probe.</span></div>
  </li>
</ul>
```

## Verification board

Use `data-status="passed|pending|failed"` as a styling hook and repeat the same
meaning in visible `.status-text`. Describe evidence or the completion
condition rather than using a bare checkbox:

```html
<ul class="verification-board">
  <li class="verification-item" data-status="passed">
    <span class="status-text">Passed</span>
    <div class="verification-body"><span class="verification-title">Contract suite</span><span class="verification-note">Headers, wrapping, passthrough, and host gating.</span></div>
  </li>
  <li class="verification-item" data-status="pending">
    <span class="status-text">Pending</span>
    <div class="verification-body"><span class="verification-title">Rollback drill</span><span class="verification-note">Restore the previous route inside the release budget.</span></div>
  </li>
</ul>
```

## Open-question panel

Use an `aside` for unresolved material adjacent to the implementation plan. Give
the heading an ID and connect it with `aria-labelledby`; the count is optional:

```html
<aside class="open-questions" aria-labelledby="open-questions-title">
  <span class="question-count">2 open questions</span>
  <h3 id="open-questions-title">Resolve before cutover</h3>
  <ol>
    <li>What mismatch threshold pauses the mirror window?</li>
    <li>Who owns the final rollback call?</li>
  </ol>
</aside>
```

## Callouts

Use exactly the variants `note`, `ok`, and `warn`. The icon is decorative; the
title and text carry the meaning:

```html
<div class="callout ok">
  <span class="ico" aria-hidden="true">&#10003;</span>
  <div class="body">
    <div class="title">Recommendation</div>
    <p>Proceed with the renewal.</p>
  </div>
</div>
```

## Stats

Use for a small set of meaningful metrics:

```html
<section class="stats" aria-label="Key metrics">
  <div class="stat">
    <div class="label">Availability</div>
    <div class="value">99.98%</div>
    <div class="sub">Last 30 days</div>
  </div>
  <div class="stat">
    <div class="label">Open actions</div>
    <div class="value">4</div>
    <div class="sub">Two due this week</div>
  </div>
</section>
```

## Tables

Use a caption, column scopes, and `num` on numeric cells. Wrap wide tables in a
focusable region for small screens. Badge variants are `ok`, `warn`, `bad`,
`accent`, and `plain`:

```html
<div class="table-wrap" role="region" aria-label="Results by period" tabindex="0">
  <table>
    <caption>Results by period</caption>
    <thead>
      <tr><th scope="col">Period</th><th scope="col" class="num">Total</th><th scope="col">Status</th></tr>
    </thead>
    <tbody>
      <tr><th scope="row">May</th><td class="num">148</td><td><span class="badge ok">On track</span></td></tr>
      <tr><th scope="row">June</th><td class="num">132</td><td><span class="badge warn">Watch</span></td></tr>
    </tbody>
  </table>
</div>
```

## Facts

Use a definition list for compact labeled facts:

```html
<dl class="facts">
  <dt>Owner</dt><dd>Platform team</dd>
  <dt>Review date</dt><dd>June 30</dd>
</dl>
```

## Content resilience

- Give meaningful images meaningful `alt` text; mark decorative images
  decorative.
- Use descriptive link text.
- Use ordered lists for flows that must remain meaningful without CSS.
- Use SVG or HTML tables for charts that must remain legible in print.
- Pair every status, severity, recommendation, direction, and phase signal with
  a visible text label; color, shape, position, and icon never carry it alone.
- Use the documented grammar for per-plan presentation; scripts and styles are
  not component content.
