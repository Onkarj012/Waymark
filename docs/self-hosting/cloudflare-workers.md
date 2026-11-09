# Self-host on Cloudflare Workers + D1

Cloudflare Workers + D1 is the blessed deployment path for Waymark. The
service is **two Workers sharing one D1 database**: a control worker for the
API and admin UI, and a content worker for public page views. Published HTML
must never share an origin with admin sessions or API credentials. The default
setup uses distinct HTTPS hosts on your account's free `workers.dev` subdomain.
You do not need to buy a domain or add a DNS zone to Cloudflare.

The TypeScript source lives in [`workers/`](../../workers) and is a
from-scratch port of the Go server (`internal/web`, `internal/store`), with the same
routes, same request/response shapes, same page-ID format, same token
semantics. The `waymark` CLI works against it unchanged.

Agents starting from the repository URL should follow
[Agent Setup](../agent-setup.md) first.

## Prerequisites

- A Cloudflare account
- Node.js 24 LTS and npm, recommended for this lockfile. Node 22.12+ also meets
  the pinned Wrangler and Vite requirements; Node 18 and 20 do not.
- The `wrangler` CLI (installed as a dev dependency of `workers/`; invoke it
  with `npx wrangler` from that directory)
- The `waymark` CLI, or the latest patch release of Go 1.25+ to build it

## Keep deployment configuration local

Only `workers/wrangler.control.example.toml` and
`workers/wrangler.content.example.toml` are shared templates. The real
`wrangler.control.toml` and `wrangler.content.toml` files are gitignored and
contain your deployment's database ID and hostnames. Never copy another
owner's bindings or routes into a new deployment.

Existing operators should **keep their current real configs unchanged**. Before
updating a checkout from a revision that tracked them, back up both files
outside the repository and restore those exact bytes after updating if needed.
Removing tracked configs can remove their working-tree copies during a Git
update. Do not regenerate an existing custom-domain deployment with the free
defaults. Ignoring files does not remove earlier copies from Git history.

## 1. Install and log in

```bash
cd workers
npm ci
npx wrangler login
```

The owner completes Cloudflare's browser login. In **Workers & Pages**, check
or choose the account's `workers.dev` subdomain. If it is
`my-account.workers.dev`, the label used below is `my-account`, not an account
ID, full hostname, email address, or URL. See Cloudflare's
[workers.dev configuration](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/).
Use the account that will own both Workers and the D1 database.

## 2. Create the D1 database

```bash
npx wrangler d1 create waymark
```

Keep the printed `database_id`. Then generate the local configs interactively:

```bash
npm run configure
```

Or supply both values explicitly, replacing the example label and UUID:

```bash
npm run configure -- --subdomain my-account \
  --database-id 00000000-0000-4000-8000-000000000001
```

This dependency-free script validates a lowercase DNS label and UUID syntax,
then fills both templates with the same database ID and these distinct origins:

```text
https://waymark-control.my-account.workers.dev
https://waymark-content.my-account.workers.dev
```

It does not verify remote ownership, create resources, change DNS, migrate, set
secrets, or deploy. Review both generated configs before any remote command.
The examples contain no custom routes or legacy redirects, and set
`workers_dev = true`. The database name is `waymark`; if you intentionally use a
different name, update both generated bindings and migration commands.

The script refuses to overwrite either existing config unless you pass
`--force`. Back up both files before explicitly replacing them. It refuses
symlink or directory targets even with `--force`. Missing values prompt only in
an interactive terminal; automation must pass both arguments. To inspect output
without touching a deployment, use an empty temporary directory:

```bash
npm run configure -- --subdomain my-account \
  --database-id 00000000-0000-4000-8000-000000000001 \
  --output-dir /tmp/waymark-config-review
```

Alternate output is for inspection. Template paths such as `src/control.ts`
are relative to the config file, so use configs in `workers/` for deployment.
Files outside `workers/` are not covered by this repository's ignore rules.

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

## 5. Review non-secret variables and origins

The generated `[vars]` blocks in both configs already contain the two free
HTTPS origins. For the example label above:

```toml
[vars]
PUBLIC_BASE_URL = "https://waymark-content.my-account.workers.dev"
CONTROL_BASE_URL = "https://waymark-control.my-account.workers.dev"
```

`wrangler.control.toml` additionally sets `WAYMARK_TOKEN_TTL_DAYS` (default
`"90"`) and `WAYMARK_TRUST_FORWARDED_IP` (see "Trusting a forwarding proxy"
below). Both origins must match the account label and Worker names actually
deployed. Do not add a `Domain` attribute to admin cookies or combine the hosts.

## 6. Understand the free hostnames and security boundary

Cloudflare assigns `<worker-name>.<account-subdomain>.workers.dev` URLs. These
are free subdomains under Cloudflare's registered domain, **not a free
registered domain** that you own or can transfer. A custom domain is optional
and requires a domain you control; its registration and renewal are separate
from Workers hosting. See [workers.dev](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/)
and [custom domains](https://developers.cloudflare.com/workers/configuration/routing/custom-domains/).

The default hosts are different **origins**, but are **same-site** siblings
under `my-account.workers.dev`. `workers.dev` is a public suffix, not each
Worker's hostname. Sibling custom hosts such as `pages.example.com` and
`control.example.com` are also same-site. See the
[Public Suffix List](https://publicsuffix.org/list/public_suffix_list.dat).

Same-origin policy separates scripts and storage; `SameSite` cookies alone
do not stop requests between same-site sibling hosts. Waymark also uses
host-only `__Host-` admin cookies over HTTPS, exact control-origin checks and
CSRF tokens on admin form posts, and host gating. Keep all those protections
intact. Never put a reverse proxy in front that merges the two origins or
forwards control credentials to published content. Published HTML remains
active, trusted-publisher content, not a sandbox for untrusted users.

### Free hosting limits

Checked against Cloudflare's official documentation on November 9, 2026:

- [Workers Free limits](https://developers.cloudflare.com/workers/platform/limits/)
  include 100,000 requests per account per day, shared by the two Workers and
  other Workers in that account, and 10 ms CPU per HTTP request. Daily request
  allowance resets at midnight UTC; exceeding it returns Error 1027. The
  Free-plan Cron CPU limit is also 10 ms, so monitor expiry-sweep failures as
  data grows.
- [D1 Free pricing](https://developers.cloudflare.com/d1/platform/pricing/)
  includes 5 million rows read and 100,000 rows written per day, and 5 GB total
  storage per account. Queries fail when daily quotas are exhausted. Page
  views and database-backed abuse limits consume this allowance too; it is not
  a quota of 100,000 published pages.
- [D1 limits](https://developers.cloudflare.com/d1/platform/limits/) include
  500 MB per Free database and 7 days of Time Travel recovery on Free, compared
  with 30 days on Paid. Keep independent backups.

Free does not mean unlimited capacity or guaranteed availability. Monitor
usage and failures, and review current limits before relying on it. Cloudflare
[recommends routes or custom domains for production](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/)
and describes `workers.dev` as intended for personal or hobby projects that
are not business-critical. Paid usage and domain registration can add costs;
this setup does not switch your account's billing plan.

### Moving from workers.dev to a custom domain later

This is an optional, separate migration, not part of free setup. Add a
`[[routes]]` block with `pattern = "<your-host>"` and `custom_domain = true` to
each real config, using two distinct hosts in a Cloudflare zone you control.
Point `PUBLIC_BASE_URL`/`CONTROL_BASE_URL` in both configs at the new domains, keep
`workers_dev = true`, and list each worker's old `*.workers.dev` hostname in
its `WAYMARK_LEGACY_HOSTS` var (see `docs/self-hosting/ENV.md`). Requests on
the old host then 308-redirect to the new origin with the path preserved, so
links shared before the move keep resolving. No data migration is involved —
pages live in D1, keyed by ID, not by hostname.

The CLI deliberately refuses redirects and binds saved credentials to their
control origin. After changing the canonical origin, run `waymark login
--server https://<new-control-host>` and approve a new device token. Do not
copy or edit a saved token to bypass the binding. Browser page links can
redirect; credential-bearing CLI requests cannot.

## 7. Deploy both Workers

```bash
npm run deploy:control
npm run deploy:content
```

Each `deploy:*` script re-embeds `theme/theme.css` into the bundle first
(`npm run generate:theme`), so the deployed CSS can never drift from the
source file in `theme/theme.css` — that file remains the single source of
truth, exactly as it is for the Go server's `//go:embed`.

## 8. Verify the expiry sweep

The control worker's `wrangler.control.toml` already declares an hourly cron
trigger (`[triggers] crons = ["0 * * * *"]`). It runs `scheduled()` in
`workers/src/control.ts`, which deletes expired pages, expired device
grants/tokens/admin sessions, and stale rate-limit buckets — the Workers
equivalent of `cmd/server/main.go`'s `sweepExpired` goroutine. No extra setup
is required beyond deploying the control worker; Cloudflare schedules cron
triggers automatically once the Worker is live.

## 9. Connect the CLI

```bash
waymark login --server https://waymark-content.my-account.workers.dev
waymark status
```

Open the printed activation URL, sign in with the admin passcode, and
approve the device. Exactly like the Go deployment: the admin passcode never
reaches the CLI, and `status` reports the token label, scopes, and expiry.

## Smoke test

Write a complete raw document to a local file:

```bash
cat > /tmp/waymark-smoke.html <<'HTML'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Cloudflare smoke test</title>
<style>body { font-family: sans-serif; max-width: 48rem; margin: 2rem auto; padding: 1rem; }</style>
</head>
<body>
<h1>Cloudflare smoke test</h1>
<p>The two Workers, D1, and the public route are working.</p>
</body>
</html>
HTML
```

Inspect that file locally. After the owner approves publishing this exact file:

```bash
waymark create --title "Cloudflare smoke test" --ttl 1 /tmp/waymark-smoke.html
```

Open the printed URL and confirm it loads as authored. The page expires after
one day; delete it earlier with `waymark delete <id>` only if the owner asks.
Check `/healthz` on both origins returns HTTP 200.
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

D1 also supports Time Travel, with 7-day recovery on Free and 30-day recovery
on Paid; `wrangler d1 time-travel` can restore to
a specific timestamp or bookmark without an explicit export.

## Upgrade

```bash
cd workers
git pull
npm ci
npm run d1:migrate:remote   # applies any new migrations; a no-op if there are none
npm run deploy:control
npm run deploy:content
```

Back up D1 and both local configs before upgrading. Review new migrations
before applying them; Wrangler records applied migrations and skips those on
subsequent runs. Do not rerun `configure` over an existing custom deployment.

## Production Checklist

- Both Workers are deployed on two distinct HTTPS hosts, using either the
  account's `workers.dev` URLs or optional custom domains.
- `WAYMARK_ADMIN_PASSCODE` was set with `wrangler secret put` (not in `[vars]`,
  not committed).
- `PUBLIC_BASE_URL` and `CONTROL_BASE_URL` in both `wrangler.*.toml` files
  match the two domains from step 6.
- Both `database_id` fields point at the same D1 database.
- Real configs remain local and ignored; only sanitized examples are tracked.
- Host-only cookies, exact-origin validation, and CSRF checks remain intact.
- Free-plan usage, cron failures, and backup recovery have been reviewed.
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
