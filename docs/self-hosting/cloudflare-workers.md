# Self-Host on Cloudflare Workers + D1

Cloudflare Workers + D1 is the blessed deployment path for Waymark. The
service is **two Workers sharing one D1 database**: a control worker (API +
admin UI) and a content worker (public page views). Keeping them as separate
Workers on separate domains is what gives you the two required origins —
published HTML must never share an origin with admin sessions or API
credentials (see [AGENTS.md](../../AGENTS.md) "Conventions & invariants").

The TypeScript source lives in [`workers/`](../../workers) and is a
from-scratch port of the Go server (`internal/web`, `internal/store`) — same
routes, same request/response shapes, same page-ID format, same token
semantics. The `waymark` CLI works against it unchanged.

Agents starting from the repository URL should follow
[Agent Setup](../agent-setup.md) first.

## Prerequisites

- A Cloudflare account
- Node.js 18+ and npm
- The `wrangler` CLI (installed as a dev dependency of `workers/`; invoke it
  with `npx wrangler` from that directory)
- The `waymark` CLI, or the latest patch release of Go 1.25+ to build it

## 1. Install and log in

```bash
cd workers
npm install
npx wrangler login
```

## 2. Create the D1 database

```bash
npx wrangler d1 create waymark
```

Copy the printed `database_id` into **both** `wrangler.control.toml` and
`wrangler.content.toml` (the `database_id` under `[[d1_databases]]`) — both
Workers must bind the same database.

## 3. Run migrations

```bash
npm run d1:migrate:remote
```

This applies `workers/migrations/0001_init.sql`, which creates the `pages`,
`device_authorizations`, `api_tokens`, `admin_sessions`, and `rate_limits`
tables (schema notes below). For local development, use
`npm run d1:migrate:local` instead and `npx wrangler dev` against the local
D1 simulator.

## 4. Set the admin passcode

```bash
npx wrangler secret put WAYMARK_ADMIN_PASSCODE -c wrangler.control.toml
```

Generate a high-entropy value first:

```bash
openssl rand -base64 48
```

Never commit this value or paste it into an agent prompt. It is used only by
the control-origin browser sign-in; the CLI never receives it.

## 5. Set the non-secret variables

Edit the `[vars]` block in **both** `wrangler.control.toml` and
`wrangler.content.toml` (both Workers validate the same two origins):

```toml
[vars]
PUBLIC_BASE_URL = "https://pages.example.com"
CONTROL_BASE_URL = "https://control.example.com"
```

`wrangler.control.toml` additionally sets `WAYMARK_TOKEN_TTL_DAYS` (default
`"90"`) and `WAYMARK_TRUST_FORWARDED_IP` (see "Trusting a forwarding proxy"
below). `PUBLIC_BASE_URL` and `CONTROL_BASE_URL` must resolve to two
different custom domains — see step 6.

## 6. Attach custom domains

Each Worker needs its own domain in a Cloudflare zone. Add both domains to
your account, then either:

- add a `[[routes]]` block to each `wrangler.*.toml` (examples are commented
  in the files), or
- run `npx wrangler deploy` once and attach the custom domain from the
  Cloudflare dashboard (Workers & Pages → your worker → Settings → Domains &
  Routes).

`workers.dev` subdomains work for a quick smoke test but two different
`*.workers.dev` subdomains are still two different origins, which is exactly
what's required — you don't strictly need a custom zone to satisfy the
origin-separation invariant, only to get stable hostnames for
`PUBLIC_BASE_URL`/`CONTROL_BASE_URL`.

## 7. Deploy both Workers

```bash
npm run deploy:control
npm run deploy:content
```

Each `deploy:*` script re-embeds `theme/theme.css` into the bundle first
(`npm run generate:theme`), so the deployed CSS can never drift from the
source file in `theme/theme.css` — that file remains the single source of
truth, exactly as it is for the Go server's `//go:embed`.

## 8. Enable the expiry sweep

The control worker's `wrangler.control.toml` already declares an hourly cron
trigger (`[triggers] crons = ["0 * * * *"]`). It runs `scheduled()` in
`workers/src/control.ts`, which deletes expired pages, expired device
grants/tokens/admin sessions, and stale rate-limit buckets — the Workers
equivalent of `cmd/server/main.go`'s `sweepExpired` goroutine. No extra setup
is required beyond deploying the control worker; Cloudflare schedules cron
triggers automatically once the Worker is live.

## 9. Connect the CLI

```bash
waymark login --server https://pages.example.com
waymark status
```

Open the printed activation URL, sign in with the admin passcode, and
approve the device. Exactly like the Go deployment: the admin passcode never
reaches the CLI, and `status` reports the token label, scopes, and expiry.

## Smoke Test

```bash
waymark create --title "Cloudflare smoke test" - <<'HTML'
<header><h1>Cloudflare smoke test</h1></header>
<p>The two Workers, D1, and the public route are working.</p>
HTML
```

Open the printed URL, then remove the page with `waymark delete <id>`.
Confirm host isolation is active: a request for an API/admin path on the
content domain, or `/theme.css`/`/p/*` on the control domain, must return
HTTP 421.

## Schema Notes (D1 vs. the Go SQLite store)

`workers/migrations/0001_init.sql` mirrors `internal/store/store.go`'s
`migrate()` column-for-column and index-for-index: `pages`,
`device_authorizations`, `api_tokens`, and `admin_sessions` have identical
names, types, and indexes in both backends. One addition exists only in the
Workers schema:

- **`rate_limits`** — the Go server keeps its abuse-rate limiter in an
  in-process map (`internal/web/auth.go`'s `rateLimiter`). Workers isolates
  are ephemeral and not memory-shared across Cloudflare's edge, so the
  Workers port keeps the same fixed-window algorithm but persists counters in
  D1 instead. The hourly cron prunes buckets whose window closed more than a
  day ago, the same way the Go limiter evicts stale entries once its
  in-memory map grows past a size cap.

Timestamps are ISO 8601 strings (millisecond precision) rather than Go's
`RFC3339Nano`; both sort and compare correctly as text, so behavior is
unaffected.

## Trusting A Forwarding Proxy

By default, both Workers use Cloudflare's edge-verified `CF-Connecting-IP`
header to key abuse-rate limits — the Workers equivalent of the Go server
reading the raw TCP peer address. If you additionally run something in front
of Cloudflare that overwrites the true client IP, set
`WAYMARK_TRUST_FORWARDED_IP = "true"` in `wrangler.control.toml`'s `[vars]`
and ensure that upstream proxy sets `X-Real-IP` to the genuine client address.
Leave it `"false"` (the default) for a standard Cloudflare-fronted deployment.

## Back Up The Database

```bash
npx wrangler d1 export waymark --remote --output backup.sql -c wrangler.control.toml
```

D1 also takes automatic time-travel snapshots (30-day point-in-time
recovery on Cloudflare's paid plan); `wrangler d1 time-travel` can restore to
a specific timestamp or bookmark without an explicit export.

## Upgrade

```bash
cd workers
git pull
npm install
npm run d1:migrate:remote   # applies any new migrations; a no-op if there are none
npm run deploy:control
npm run deploy:content
```

Migrations are additive (new tables/columns only) and idempotent — re-running
`d1:migrate:remote` after every deploy is always safe.

## Production Checklist

- Both Workers are deployed on two distinct HTTPS custom domains.
- `WAYMARK_ADMIN_PASSCODE` was set with `wrangler secret put` (not in `[vars]`,
  not committed).
- `PUBLIC_BASE_URL` and `CONTROL_BASE_URL` in both `wrangler.*.toml` files
  match the two domains from step 6.
- Both `database_id` fields point at the same D1 database.
- The control worker's cron trigger is active (check **Workers & Pages →
  waymark-control → Triggers** in the dashboard).
- A request for an admin/API path on the content domain, or a page/theme path
  on the control domain, returns HTTP 421.
- Published content contains no secrets.

Official references: [Workers](https://developers.cloudflare.com/workers/),
[D1](https://developers.cloudflare.com/d1/),
[D1 migrations](https://developers.cloudflare.com/d1/reference/migrations/),
[D1 backups & time travel](https://developers.cloudflare.com/d1/reference/time-travel/),
[Cron Triggers](https://developers.cloudflare.com/workers/configuration/cron-triggers/),
[Wrangler secrets](https://developers.cloudflare.com/workers/wrangler/commands/#secret),
and [custom domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).
