---
name: waymark
description: Preview, publish, and safely maintain polished HTML artifacts with the waymark CLI. Use when reports, comparisons, tables, status pages, or approved visual plans would be clearer as a hosted page than inline chat.
---

# Waymark

Create body-only themed HTML, inspect it offline, and publish only with explicit
authorization. The house theme supplies the visual system; use raw mode only for
generic non-plan artifacts that genuinely require a complete custom document.

## Resolve The CLI

Resolve the executable with `command -v waymark`. On Onkar's machine, fall back
to `/Users/onkarj012/Projects/Alternatives/Waymark/bin/waymark` when it is not on
`PATH`; elsewhere, ask where it is installed rather than guessing. Use the
resolved executable for every command below.

Offline preview needs no credentials. Before any remote operation, run
`waymark status` and continue only when it succeeds and reports
`Auth: authenticated`. If needed, run:

```bash
waymark login --server https://waymark-api.leo4.dev \
  --device-name "<clear device/agent name>"
```

The owner approves the scoped device in a browser. Never request, print, or
store the admin passcode in chat.

Published pages are public to anyone with the unguessable URL. Never publish
credentials or secrets. Include private source material, personal data, or
other non-public information only when the user explicitly intends it to become
public.

## Required Offline Preview

For every local body-only source intended for publication, generate a standalone
preview before requesting publication or update approval:

```bash
waymark preview --title "Quarterly review" \
  --output quarterly-review.html quarterly-review.waymark.html
```

The exact interface is:

```text
waymark preview --title TITLE --output FILE [--force] <body-file|->
```

Preview is local and deterministic: it does not authenticate, use the network,
open a browser, or publish. It refuses to overwrite an existing output unless
`--force` is supplied. The output is a standalone inspection artifact; never
pass it to `create` or `update`. If the body source changes, regenerate and
reinspect the preview before approval.

A successful preview is the required themed-body structural check: full-document
tags, `<style>`, and `<script>` are rejected. Also inspect the generated file for
the intended title, section order, navigation targets, textual equivalents, and
absence of clipped or missing content.

## Visual Plan Contract

Visual plans use these local names:

- canonical specification: `<slug>.md`;
- body-only Waymark source: `<slug>.waymark.html`;
- standalone preview: `<slug>.html`.

The body source must have one root
`<article class="plan" data-plan-profile="...">` using a documented profile:
`systems`, `journey`, `migration`, `decision`, or `delivery`. Verify that root,
profile, headings, anchors, and textual equivalents before approval.

Plans must remain themed body-only HTML. Plan-supplied scripts, styles,
full-document tags, and raw mode are prohibited. Use semantic HTML or inline SVG
with captions or adjacent textual equivalents. Follow
[references/components.md](references/components.md) exactly for component
markup and accessibility requirements.

## Approval Boundary

Creating or reviewing local files does not authorize a remote mutation. After
the latest preview and structural verification, request explicit approval that
names the exact next action:

- create a new page from a named source, with the title and TTL/permanent choice;
- update a named page ID from a named source, with the expected remote
  `updated_at` value and any TTL change;
- any separate downstream action, such as implementation handoff, delegation,
  issue creation, branch work, commits, or pull requests.

Approval authorizes only the listed actions. Plan approval alone does not imply
publication or implementation. Publication approval does not imply later
updates. Never create or update from a draft the user has not reviewed in its
latest preview.

## Publish An Approved Artifact

Generic non-plan reports remain supported. Prefer themed body-only HTML and keep
a local source when the page may be maintained:

```bash
waymark create --title "Quarterly review" --ttl 7 quarterly-review.waymark.html
```

Use `--ttl 0` only when the user explicitly wants a permanent page. The command
prints the public URL; verify success and return that URL. For a maintained page,
record its page ID, URL, TTL, and returned `updated_at` metadata beside the local
canonical source.

One-off generic reports may use stdin, but must still be previewed from the same
body before approval:

```bash
waymark preview --title "Quarterly review" --output /tmp/quarterly-review.html - < body.html
waymark create --title "Quarterly review" - < body.html
```

## Update Without Overwriting Others

Waymark updates replace the entire stored body, retain no revision history, and
are last-writer-wins. `get --json` and `list --json` return metadata, not stored
HTML, so preserve the canonical source locally.

Before requesting update approval, fetch `waymark get --json <id>` and compare
its exact `updated_at` value with the last known remote timestamp. If the local
record has no known timestamp, establish and record the current value before
seeking approval. Immediately before the approved update, fetch it again. If it
differs from the approved expected value, stop and reconcile instead of
overwriting.

```bash
waymark update <id> quarterly-review.waymark.html
```

After an update, verify success and record the new `updated_at`. Never delete a
page automatically.

## Hard Contract

- Put every flag before positional arguments.
- Default to themed mode. Supply body content only: no `doctype`, `html`, `head`,
  `body`, `style`, or `script` elements.
- Escape external or user-provided text before inserting it. Waymark trusts
  publisher HTML and does not sanitize it.
- Do not invent authorship, dates, confidentiality labels, status, or other
  metadata.
- Use raw mode only for an approved generic non-plan artifact that genuinely
  needs a complete document, custom CSS, or JavaScript. Preview's body-only
  validation does not apply to raw input, so inspect its full-document structure
  separately before requesting publication approval.
- Use themed components only when they improve comprehension. Plain semantic
  headings, paragraphs, lists, links, quotes, code, and tables are valid.
