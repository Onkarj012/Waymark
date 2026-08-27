---
name: waymark
description: Publish and safely maintain polished HTML artifacts with the waymark CLI. Use when reports, comparisons, tables, status pages, or approved visual plans would be clearer as a hosted page than inline chat.
---

# Waymark

Prepare a complete, self-contained HTML document, inspect it locally, and
publish only with explicit authorization. The composing agent owns layout,
CSS, typography, responsiveness, and print rules. Waymark stores and serves
the document verbatim.

## Resolve The CLI

Resolve the executable with `command -v waymark`. On Onkar's machine, fall back
to `/Users/onkarj012/Projects/Alternatives/Waymark/bin/waymark` when it is not on
`PATH`; elsewhere, ask where it is installed rather than guessing. Use the
resolved executable for every command below.

Before any remote operation, run `waymark status` and continue only when it
succeeds and reports `Auth: authenticated`. If needed, run:

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

## Inspect The Local Document

Every artifact intended for publication must be a complete HTML document:

- `<!DOCTYPE html>`
- `<html>`, `<head>`, and `<body>`
- viewport meta, `<title>`, and at least one `<style>` in `<head>`
- no house-theme `<link>` to `/theme.css`
- no `data-plan-profile` attribute

Inspect the exact local file in a browser or with the file URL before
requesting publication or update approval. Confirm the title, section order,
navigation targets, textual equivalents, and that nothing is clipped or
missing. There is no `waymark preview` command and no `--raw` flag.

Visual plans follow the planning skill's three-file contract: canonical
`<slug>.md`, self-contained `<slug>.html`, and `<slug>.waymark.json`. Do not
create `<slug>.waymark.html` body-only sources.

## Approval Boundary

Creating or reviewing local files does not authorize a remote mutation. After
the latest local inspection, request explicit approval that names the exact
next action:

- create a new page from a named source, with the title and TTL/permanent choice;
- update a named page ID from a named source, with the expected remote
  `updated_at` value and any TTL change;
- any separate downstream action, such as implementation handoff, delegation,
  issue creation, branch work, commits, or pull requests.

Approval authorizes only the listed actions. Plan approval alone does not imply
publication or implementation. Publication approval does not imply later
updates. Never create or update from a draft the user has not reviewed in its
latest local file.

## Publish An Approved Artifact

```bash
waymark create --json --title "Quarterly review" --ttl 7 quarterly-review.html
```

Use `--ttl 0` only when the user explicitly wants a permanent page. Verify
success and return the URL. For a maintained page, record its page ID, URL, TTL,
and the returned JSON `updated_at` beside the local canonical source.

A one-off report may use stdin only after the exact complete document has been
inspected locally:

```bash
waymark create --title "Quarterly review" - < quarterly-review.html
```

## Update Without Overwriting Others

Waymark updates replace the entire stored document, retain no revision history,
and are last-writer-wins. `get --json` and `list --json` return metadata, not
stored HTML, so preserve the canonical source locally.

Before requesting update approval, fetch `waymark get --json <id>` and compare
its exact `updated_at` value with the last known remote timestamp. If the local
record has no known timestamp, establish and record the current value before
seeking approval. Immediately before the approved update, fetch it again. If it
differs from the approved expected value, stop and reconcile instead of
overwriting. The JSON response from `waymark get --json`, `waymark update
--json`, or the approved create command is the only source to record as
`updated_at`; do not infer it from human-readable output.

```bash
waymark update --json --if-updated-at "$EXPECTED_UPDATED_AT" <id> quarterly-review.html
```

Replacement HTML is always sent as a complete document. Metadata-only updates
omit HTML. The server atomically checks `--if-updated-at` before writing. If the
command returns a 409 conflict, stop and reconcile with a fresh
`waymark get --json` before attempting another update; never overwrite an
intervening change. After a successful JSON update, record its authoritative
`updated_at`. Never delete a page automatically.

## Hard Contract

- Put every flag before positional arguments.
- Publish complete, self-contained HTML only. Do not send body-only fragments,
  house-theme links, or `data-plan-profile`.
- Escape external or user-provided text before inserting it. Waymark trusts
  publisher HTML and does not sanitize it.
- Do not invent authorship, dates, confidentiality labels, status, or other
  metadata.
