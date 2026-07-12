---
name: waymark
description: Publish polished, shareable HTML reports with the waymark CLI. Use when analysis, comparisons, tables, status reports, or other structured information would be clearer as a hosted page than as inline chat.
---

# Waymark

Publish useful HTML and return the shareable URL. Let the content determine the
composition; the house theme supplies a restrained visual system without
requiring every page to look the same.

## Preflight

Run `waymark status` before writing the page. Continue only when it exits
successfully and the `Auth` line says `authenticated`. If it is not configured,
ask the user to run `waymark login --server <url>`. The command prints a browser
activation URL and waits for the deployment owner to approve a scoped device
token. Never ask the user to paste the admin passcode into an agent prompt.

Published pages are public to anyone with the unguessable URL. Do not publish
secrets or private source material unless the user explicitly intends to share
it that way.

## Publish

Prefer stdin for a page created once:

```bash
waymark create --title "Quarterly review" - <<'HTML'
<header>
  <h1>Quarterly review</h1>
  <p class="dek">Performance, open decisions, and the next set of actions.</p>
</header>

<section>
  <h2>Summary</h2>
  <p>The program is on track, with one decision needed this week.</p>
</section>
HTML
```

Use a temporary file when you expect to inspect or revise the page before
publishing:

```bash
f="$(mktemp)"
# Write body HTML to "$f".
waymark create --title "Quarterly review" "$f"
rm -f "$f"
```

The command prints the page URL. Return that URL to the user.

## Hard Contract

- Default to themed mode. Supply body content only: no `doctype`, `html`,
  `head`, `body`, or `style` elements.
- Use `--raw` only for a complete HTML document that genuinely needs custom CSS
  or JavaScript.
- Put every flag before positional arguments.
- Use themed component class names and structure exactly as documented in
  `references/components.md`; do not invent modifier aliases.
- Escape external or user-provided text before inserting it into HTML. The
  server trusts publisher HTML and does not sanitize it.
- Do not invent authorship, dates, confidentiality labels, or status metadata.

## Compose The Page

Start with semantic HTML. Use themed components only when they improve the
content:

- Add a `.page-layout` section navigation for longer reports that benefit from
  scanning, usually four or more major sections.
- Add a callout when there is a real conclusion, recommendation, or risk.
- Use stat blocks for a small set of meaningful comparable metrics.
- Use tables for comparison, with captions and numeric alignment where useful.
- Keep metadata and footers optional.

Read [references/components.md](references/components.md) when you need the
component markup or accessibility details. Plain headings, paragraphs, lists,
links, quotes, code, and tables are all valid without additional decoration.

## Raw Mode

Write a complete document and pass `--raw`. Link the same-origin house theme
when useful:

```html
<link rel="stylesheet" href="/theme.css">
```

Raw pages may run JavaScript. Use them only for trusted content and keep all
external text escaped or safely rendered.

## Manage Pages

```bash
waymark list
waymark get <id>
waymark update <id> "$f"
waymark update --ttl 0 <id>
waymark delete <id>
```

Use `--json` on read commands when structured output is helpful. After creating
or updating a page, verify the command succeeded and return its printed URL.
