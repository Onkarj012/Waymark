---
name: waymark
description: Preview, publish, and safely maintain polished HTML artifacts with the waymark CLI. Use when reports, comparisons, tables, status pages, or approved visual plans would be clearer as a hosted page than inline chat.
---

# Waymark

Prepare the artifact, inspect it offline, and publish only with explicit
authorization. Visual plans use themed body-only HTML and the house component
grammar. Generic reports use raw, complete, self-contained HTML so their
composing agent owns the layout, CSS, typography, responsiveness, and print
rules.

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

For every local themed body-only source intended for publication, generate a
standalone preview before requesting publication or update approval:

```bash
waymark preview --title "Release plan" \
  --output release-plan.html release-plan.waymark.html
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
absence of clipped or missing content. `waymark preview` accepts themed
body-only input; it has no `--raw` mode. For an approved raw artifact, inspect
the complete local file separately in a browser or with the file URL, then use
`waymark create --raw` or `waymark update --raw` only after that inspection.

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

Use the approved source and its required rendering mode. Generic reports are
raw, complete HTML documents; visual plans are themed body-only sources:

```bash
# Raw report
waymark create --raw --json --title "Quarterly review" --ttl 7 quarterly-review.html

# Themed visual plan
waymark create --json --title "Release plan" --ttl 0 release-plan.waymark.html
```

Use `--ttl 0` only when the user explicitly wants a permanent page. Verify
success and return the URL. For a maintained page, record its page ID, URL, TTL,
and the returned JSON `updated_at` beside the local canonical source.

A one-off raw report may use stdin only after the exact complete document has
been inspected locally:

```bash
waymark create --raw --title "Quarterly review" - < quarterly-review.html
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
overwriting. The JSON response from `waymark get --json`, `waymark update
--json`, or the approved create command is the only source to record as
`updated_at`; do not infer it from human-readable output.

```bash
waymark update --raw --json --if-updated-at "$EXPECTED_UPDATED_AT" <id> quarterly-review.html
```

Use `--raw` for reports and omit it for themed visual plans. The server
atomically checks `--if-updated-at` before writing. If the command
returns a 409 conflict, stop and reconcile with a fresh `waymark get --json`
before attempting another update; never overwrite an intervening change.
After a successful JSON update, record its authoritative `updated_at`. Never
delete a page automatically.

## Hard Contract

- Put every flag before positional arguments.
- Visual plans use themed body-only HTML: no `doctype`, `html`, `head`, `body`,
  `style`, or `script` elements, and no raw mode.
- Generic reports use raw, complete, self-contained HTML and own their design,
  responsive behavior, and print rules. Inspect the exact document separately;
  preview accepts themed body-only input only.
- Escape external or user-provided text before inserting it. Waymark trusts
  publisher HTML and does not sanitize it.
- Do not invent authorship, dates, confidentiality labels, status, or other
  metadata.
- Use themed components only for visual plans and only when they improve
  comprehension. Plain semantic HTML remains valid.
