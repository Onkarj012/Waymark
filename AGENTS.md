# AGENTS.md

Guidance for AI agents and human contributors working **on** this codebase.
(For how an agent *uses* the tool to publish pages, see
`.skills/waymark/SKILL.md`.)

## Setup and onboarding

If the user gave you this repository URL to install Waymark, connect a
new device, or deploy a new instance, read
[`docs/agent-setup.md`](docs/agent-setup.md) before exploring the implementation.
It separates the existing-instance and first-deployment paths, identifies the
owner-only browser steps, and defines the verification required before setup is
complete.

## What this is

**Waymark** is a small self-hosted service for publishing clean, shareable
HTML pages — sponsor analyses, deal breakdowns, data tables, reports — and
getting back a public link. It's designed to be driven by an AI agent through
the `waymark` CLI.

Pieces:
- a **Go HTTP server** (scoped-token JSON API + public page views),
- **SQLite** storage with the page HTML stored inline (one file, no blob store),
- a **`waymark` CLI** the agent calls,
- a **house theme** (`theme/theme.css`) the server applies to every page,
- an **agent skill** that teaches agents when and how to publish.

## Data flow

```
owner browser ──▶ control origin /activate ──▶ scoped device token
                                                     │
agent writes body HTML ──▶ waymark create ──▶ control origin /api/pages ──▶ server
                                                                           │
                                                       SQLite (HTML inline) │
browser ◀── content origin /p/{id} (public) ◀── linked to /theme.css ◀──────┘
```

A **themed** page stores only the *body content*; the server wraps it in a full
document (`renderThemed` in `internal/web/server.go`) inside `<main class="page">`
and links `/theme.css`. A **raw** page stores a complete document and is served
verbatim.

## Repo layout

| Path | What |
|---|---|
| `cmd/server/main.go` | server entrypoint: config, expiry sweeper, graceful shutdown |
| `cmd/waymark/main.go` | CLI commands (`create`/`list`/`get`/`update`/`delete`/`login`/…) |
| `cmd/waymark/config.go` | CLI token storage and server/token resolution |
| `internal/store/store.go` | SQLite persistence; `Page`/`Meta` models; CRUD + expiry sweep |
| `internal/web/server.go` | host gating, scoped auth, themed rendering, JSON handlers |
| `internal/web/id.go` | unguessable base62 page IDs (crypto/rand) |
| `theme/theme.css` | **the** house stylesheet (source of truth) |
| `theme/theme.go` | `//go:embed theme.css` → `theme.CSS` |
| `theme/demo.html` | standalone design preview linked to the source CSS |
| `.skills/waymark/SKILL.md` | how the agent uses the tool (source of truth) |
| `.claude/skills/waymark` | symlink → `../../.skills/waymark` so Claude Code loads the skill in-repo |
| `Dockerfile` | container build (self-host anywhere that runs a container) |
| `workers/` | Cloudflare Workers + D1 port of the server (TypeScript); a from-scratch, drop-in replacement the `waymark` CLI also talks to unchanged — see `workers/` and [`docs/self-hosting/cloudflare-workers.md`](docs/self-hosting/cloudflare-workers.md) |

## Build, run, test

```bash
go mod tidy                       # resolve deps + go.sum
go test ./... && go vet ./...     # test + vet everything
go build -o bin/server ./cmd/server
go build -o bin/waymark ./cmd/waymark
```

Run locally:

```bash
PUBLIC_BASE_URL=http://pages.localhost:8080 \
CONTROL_BASE_URL=http://control.localhost:8080 \
WAYMARK_ADMIN_PASSCODE=dev-admin-secret \
WAYMARK_TOKEN_TTL_DAYS=90 \
DB_PATH=/tmp/cp.db PORT=8080 ./bin/server
```

Device tokens default to 90 days; the explicit TTL above makes that local
development behavior visible.

End-to-end smoke test (server must be running on :8080):

```bash
export WAYMARK_CONFIG_DIR=/tmp/cp-cfg     # isolate from your real login
./bin/waymark login --server http://pages.localhost:8080
# Open the printed URL, sign in with dev-admin-secret, and approve the device.
printf '<h1>Hi</h1><p>It works.</p>' > /tmp/body.html
./bin/waymark create --title "Smoke" /tmp/body.html   # prints the URL
./bin/waymark list
```

Tests cover CLI credential handling, storage lifecycle and permissions, API
authentication, rendering headers, and page-path log redaction. Add focused
regression tests alongside behavior changes.

## Conventions & invariants — read before changing things

- **Standard library first.** The server uses only stdlib + `modernc.org/sqlite`.
  The CLI uses only the standard library.
- **`modernc.org/sqlite` is pure Go on purpose.** Do **not** swap in
  `mattn/go-sqlite3` — it needs cgo and would break the `CGO_ENABLED=0` static
  Docker build.
- **The `go` directive is `1.25`.** The Dockerfile build image must be at least
  that version (`golang:1.25-alpine`). If a dependency bumps it, bump the image
  too.
- **Themed vs raw is a hard contract.** Themed content must be *body only* — no
  `<!doctype>`, `<html>`, `<head>`, or `<style>`; the server adds those. Raw
  content must be a complete document. Don't blur the two.
- **The theme lives in exactly one place: `theme/theme.css`.** It's embedded into
  the binary and served at `/theme.css`. `theme/demo.html` links the source file
  directly, so visual previews cannot drift from the embedded stylesheet.
- **Page IDs are public and unguessable** (12 base62 chars, crypto/rand). The
  scoped credential protects the *API*, not viewing — anyone with a link can
  view a page.
- **Auth uses scoped device tokens.** The control origin hosts APIs, owner
  sessions, approval, and revocation; the content origin hosts `/p/{id}` and
  `/theme.css`. Both origins and the admin passcode are required at startup.
- **CLI credentials** are saved by device login to
  `~/.config/waymark/config.json` (mode `0600`). The server URL resolves
  by **flag → env → config**; tokens resolve by **environment token → saved
  token**. Never log or print credentials. The CLI refuses non-loopback plain
  HTTP.
- **Published HTML is active content.** Never collapse `CONTROL_BASE_URL` and
  `PUBLIC_BASE_URL` into one origin. Host gating and host-only admin cookies are
  security boundaries, not deployment conveniences.

## Common tasks

- **Change the look** → edit `theme/theme.css`, reopen `theme/demo.html` to
  inspect, rebuild the server (it re-embeds). Everything is driven by the CSS
  variables at the top of the file.
- **Add an API endpoint** → register the route in `NewConfigured()` (`internal/web/server.go`),
  write the handler, and wrap it with `s.auth(...)` and the narrowest scope it
  requires. Return JSON via `s.writeJSON` / `s.writeErr`.
- **Add a CLI command** → add a `case` in the `main()` switch, a `cmdX` function,
  and a line in `usage()` (`cmd/waymark/main.go`).
- **Add a stored field** → update the schema in `migrate()` and the `Page`/`Meta`
  structs + `Create`/`Get`/`Save`/`List` in `internal/store/store.go`, then the
  API request/response structs in `internal/web/server.go`, then the CLI.

## Gotchas

- Don't commit secrets. `.env` and the SQLite files (`*.db`, `-wal`, `-shm`) are
  gitignored; the CLI's `config.json` lives outside the repo.
- SQLite uses WAL mode. Back up the complete database state during a write
  pause instead of copying only the main `.db` file.

## Deploy

Cloudflare Workers + D1 is the blessed path: two Workers (control + content)
sharing one D1 database, each on its own domain — see `workers/` and
[`docs/self-hosting/cloudflare-workers.md`](docs/self-hosting/cloudflare-workers.md)
for setup, migrations, secrets, and the expiry-sweep cron trigger.

The Go server also still runs as a container via the `Dockerfile` for anyone
self-hosting outside Cloudflare: mount a volume at `/data` (the image sets
`DB_PATH=/data/waymark.db`), attach distinct content and control domains, and
set the auth variables.
