# Theme Components

Use these components as a vocabulary, not a mandatory page template.

## Section Navigation

Use for longer reports with several major sections. Keep links and section IDs
in the same order.

```html
<div class="page-layout">
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
</div>
```

## Header

The title is useful; the subtitle and metadata are optional. Include only facts
known from the task.

```html
<header>
  <h1>Program review</h1>
  <p class="dek">Performance, open questions, and next actions.</p>
</header>
```

## Callouts

Variants are `note`, `ok`, and `warn`. Use the structure and class names below
exactly; do not substitute aliases such as `success`. Treat the icon as
decorative.

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

Use a caption and column scopes. Wrap wide tables so they remain usable on
small screens. Add `num` to numeric cells.

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

Badge variants are `ok`, `warn`, `bad`, `accent`, and `plain`.

## Facts

```html
<dl class="facts">
  <dt>Owner</dt><dd>Platform team</dd>
  <dt>Review date</dt><dd>June 30</dd>
</dl>
```

## Content Resilience

- Give images meaningful `alt` text unless they are decorative.
- Use descriptive link text rather than raw URLs.
- Prefer SVG or HTML tables for charts that must remain legible when printed.
- Avoid status communicated by color alone; keep a visible text label.
