# Environment Variables & Secrets Reference

Complete reference for every environment variable, secret, and binding read
by Waymark, across all four runtime surfaces: the Go server, the Cloudflare
Workers control worker, the Cloudflare Workers content worker, and the
`waymark` CLI.

For a copy-pasteable starting point for the Go server and CLI, see
[`.env.example`](../../.env.example) at the repo root. For the full
Cloudflare Workers setup walkthrough, see
[`cloudflare-workers.md`](cloudflare-workers.md).

## Go server (`cmd/server`)

Source: `cmd/server/main.go`.

| Variable | Required? | Default | How to set | Notes |
|---|---|---|---|---|
| `WAYMARK_ADMIN_PASSCODE` | **Required** | none — server exits with `WAYMARK_ADMIN_PASSCODE is required` if empty | env | Owner secret for the control-origin browser sign-in (device authorization approval, admin UI). Never reaches the CLI. Generate with `openssl rand -base64 48`. |
| `PUBLIC_BASE_URL` | **Required** | none | env | Origin that serves published pages, e.g. `https://pages.example.com`. Must differ from `CONTROL_BASE_URL` (enforced at startup). |
| `CONTROL_BASE_URL` | **Required** | none | env | Origin that serves the API and admin UI, e.g. `https://control.example.com`. Must differ from `PUBLIC_BASE_URL`. |
| `WAYMARK_TOKEN_TTL_DAYS` | Optional | `90` | env | Device API token lifetime in days. Must parse as an integer in `1..365` or the server exits at startup. |
| `DB_PATH` | Optional | `./waymark.db` | env | Path to the SQLite database file (pure-Go SQLite, no CGo). On Railway or similar, point this at a mounted persistent volume. |
| `PORT` | Optional | `8080` | env | TCP port the HTTP server listens on. |
| `RAILWAY_ENVIRONMENT_ID` | Optional | unset | env | Not a config value you set intentionally — Railway injects it automatically. Its mere *presence* (non-empty) flips `TrustForwardedIP` to `true`, so the server trusts `X-Forwarded-For`/`X-Real-IP` for rate-limit keying instead of the raw TCP peer address. Leave unset on a bare VM/container with no trusted reverse proxy in front. |

## Cloudflare Workers — control worker

Source: `workers/wrangler.control.toml`, `workers/src/types.ts` (`ControlEnv`), `workers/src/config.ts`.
Config file: `workers/wrangler.control.toml`.

| Variable/Binding | Required? | Default | How to set | Notes |
|---|---|---|---|---|
| `WAYMARK_ADMIN_PASSCODE` | **Required** | none — `loadControlConfig` throws `WAYMARK_ADMIN_PASSCODE is required` | **secret** (`wrangler secret put`) | Same semantics as the Go server. Must never go in `[vars]` or be committed. |
| `PUBLIC_BASE_URL` | **Required** | none | `[vars]` in `wrangler.control.toml` | Must match the content worker's `PUBLIC_BASE_URL` and differ from `CONTROL_BASE_URL`. |
| `CONTROL_BASE_URL` | **Required** | none | `[vars]` in `wrangler.control.toml` | Must match the content worker's `CONTROL_BASE_URL` and differ from `PUBLIC_BASE_URL`. |
| `WAYMARK_TOKEN_TTL_DAYS` | Optional | `90` | `[vars]` in `wrangler.control.toml` | String in the TOML (e.g. `"90"`); parsed to an integer and validated to `1..365`. |
| `WAYMARK_TRUST_FORWARDED_IP` | Optional | `"false"` | `[vars]` in `wrangler.control.toml` | Only the literal string `"true"` enables it. Set `"true"` only if you run something in front of Cloudflare that overwrites the true client IP; that upstream must set `X-Real-IP` to the genuine client address. Unlike the Go server's `RAILWAY_ENVIRONMENT_ID` auto-detection, this is an explicit opt-in var on Workers. |
| `WAYMARK_LEGACY_HOSTS` | Optional | none | `[vars]` in `wrangler.control.toml` | Comma-separated bare hostnames this worker used to live on (e.g. its `*.workers.dev` host after moving to a custom domain). Requests on a legacy host get a 308 redirect to `CONTROL_BASE_URL` with the path and query preserved. Entries must not be URLs and must not equal either canonical host. Workers-only — the Go server has no equivalent. |
| `DB` (D1 binding) | **Required** | none | `[[d1_databases]]` block, `database_id` field | Both workers must bind the **same** D1 database. See "Create the D1 database" below. |

## Cloudflare Workers — content worker

Source: `workers/wrangler.content.toml`, `workers/src/types.ts` (`BaseEnv`).
Config file: `workers/wrangler.content.toml`.

The content worker never handles admin credentials by design (it must not
share trust with the control worker), so it only needs the two shared
origins and the D1 binding — it does **not** read
`WAYMARK_ADMIN_PASSCODE`, `WAYMARK_TOKEN_TTL_DAYS`, or
`WAYMARK_TRUST_FORWARDED_IP`.

| Variable/Binding | Required? | Default | How to set | Notes |
|---|---|---|---|---|
| `PUBLIC_BASE_URL` | **Required** | none | `[vars]` in `wrangler.content.toml` | Must match the control worker's value. |
| `CONTROL_BASE_URL` | **Required** | none | `[vars]` in `wrangler.content.toml` | Must match the control worker's value. |
| `WAYMARK_LEGACY_HOSTS` | Optional | none | `[vars]` in `wrangler.content.toml` | Same semantics as the control worker's entry, but redirects to `PUBLIC_BASE_URL`. Keeps previously shared page links working after a domain move. |
| `DB` (D1 binding) | **Required** | none | `[[d1_databases]]` block, `database_id` field | Same database as the control worker. |

## `waymark` CLI (`cmd/waymark`)

Source: `cmd/waymark/config.go`, `cmd/waymark/device_login.go`.

| Variable | Required? | Default | How to set | Notes |
|---|---|---|---|---|
| `WAYMARK_URL` | Optional | none — falls back to the server URL saved by `waymark login` in `config.json` | env | Overrides the saved server URL for a single invocation. Precedence: `--server` flag > `WAYMARK_URL` env > saved config. |
| `WAYMARK_TOKEN` | Optional | none — falls back to the device token saved by `waymark login` | env | Overrides the saved device API token. Precedence: `WAYMARK_TOKEN` env > saved config. |
| `WAYMARK_CONFIG_DIR` | Optional | `$XDG_CONFIG_HOME/waymark`, else `~/.config/waymark` | env | Directory holding `config.json` (server URL + device token, written with `0600`/`0700` permissions). |
| `XDG_CONFIG_HOME` | Optional | unset | env | Standard XDG base-directory var. Only consulted if `WAYMARK_CONFIG_DIR` is unset. |

## Setting Cloudflare Workers secrets and D1 bindings

Run these from the `workers/` directory.

### Create the D1 database (one-time)

```bash
npx wrangler d1 create waymark
```

Copy the printed `database_id` into the `[[d1_databases]]` block's
`database_id` field in **both** `wrangler.control.toml` and
`wrangler.content.toml` — both workers must bind the identical database.

### Apply migrations

```bash
npm run d1:migrate:local    # local dev (miniflare/wrangler dev simulator)
npm run d1:migrate:remote   # production D1
```

### Set the admin passcode secret (control worker only)

```bash
npx wrangler secret put WAYMARK_ADMIN_PASSCODE -c wrangler.control.toml
```

Generate a high-entropy value first and paste it at the prompt — never as a
CLI argument, never committed, never in `[vars]`:

```bash
openssl rand -base64 48
```

To list, or later delete, a secret:

```bash
npx wrangler secret list -c wrangler.control.toml
npx wrangler secret delete WAYMARK_ADMIN_PASSCODE -c wrangler.control.toml
```

### Set the non-secret `[vars]`

Edit the `[vars]` block directly in `wrangler.control.toml` and
`wrangler.content.toml` (plain TOML, safe to commit since they hold no
secrets):

```toml
[vars]
PUBLIC_BASE_URL = "https://pages.example.com"
CONTROL_BASE_URL = "https://control.example.com"
WAYMARK_TOKEN_TTL_DAYS = "90"            # control worker only
WAYMARK_TRUST_FORWARDED_IP = "false"     # control worker only
```

### Deploy

```bash
npm run deploy:control
npm run deploy:content
```

Each `deploy:*` script re-embeds `theme/theme.css` first
(`npm run generate:theme`) so the deployed CSS never drifts from the source
file.

## Summary counts

- Go server: 7 variables (3 required, 4 optional).
- Control worker: 6 variables/bindings (4 required — including the D1
  binding and the `WAYMARK_ADMIN_PASSCODE` secret — 2 optional).
- Content worker: 3 variables/bindings (all required).
- CLI: 4 variables (all optional; all have config-file or flag fallbacks).
