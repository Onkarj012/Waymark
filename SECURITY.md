# Security Policy

## Reporting a Vulnerability

Use GitHub private vulnerability reporting for this repository. Do not open a
public issue containing exploit details, credentials, unlisted page URLs, or
deployment configuration.

Include the affected version, deployment assumptions, reproduction steps,
impact, and any suggested mitigation.

## Supported Versions

Until the first tagged release, only the latest commit on `main` is supported.

## Content Trust Model

Waymark stores and serves publisher-supplied HTML. It does not sanitize
HTML, and raw pages may contain JavaScript. Treat publishing
credentials as trusted-code-authority for the page origin.

Public page URLs are unguessable but are not access control. Anyone with a URL
can view the page. Do not publish secrets or information that requires identity-
based authorization.

The management API uses finite-lived, scoped device tokens. The deployment
admin passcode is used only by the control-origin browser login and is never
returned to the CLI. Use HTTPS outside loopback development and rotate the
admin passcode after suspected exposure.

## Browser Authentication

Browser sessions, approval cookies, administrative UI, and device tokens are
accepted only on `CONTROL_BASE_URL`. Active published HTML is served only from
`PUBLIC_BASE_URL`. The server rejects requests sent to the wrong or an unknown
host with HTTP 421. See [`docs/device-authorization.md`](docs/device-authorization.md).

The free Workers setup uses
`waymark-control.<account>.workers.dev` and
`waymark-content.<account>.workers.dev`. These are different origins but
same-site siblings under the account's subdomain. Sibling custom hosts such
as `control.example.com` and `pages.example.com` are also same-site.
`SameSite` cookies alone do not protect administration from active HTML on a
sibling host. Keep HTTPS host-only `__Host-` admin cookies without a `Domain`
attribute, exact control-origin validation and CSRF tokens on admin posts,
and host gating. Never merge these hosts behind one public origin or forward
control credentials to the content Worker.

See the [Workers setup guide](docs/self-hosting/cloudflare-workers.md#6-understand-the-free-hostnames-and-security-boundary)
for the free hostname model, quota limits, and Cloudflare's production guidance.

## Deployment

- For the Go container, mount persistent storage at `/data`.
- Use a unique, high-entropy `WAYMARK_ADMIN_PASSCODE`.
- Set distinct HTTPS `PUBLIC_BASE_URL` and `CONTROL_BASE_URL` origins.
- For Go, back up the complete SQLite volume consistently, including WAL state.
  For Workers, back up the shared D1 database and check Time Travel retention.
- Keep real `workers/wrangler.control.toml` and `workers/wrangler.content.toml`
  configs ignored and local. Share only sanitized `.example.toml` templates.
  Preserve existing configs before upgrades; do not overwrite custom routes or
  database bindings with free setup defaults. Ignore rules do not erase Git
  history. Database IDs are deployment metadata, not API credentials.
- Do not bake `.env`, databases, credentials, or local build output into images.
- The Go server trusts forwarded client IP metadata only when its Railway
  environment ID is present; otherwise it keys abuse limits from the raw
  network peer. The Cloudflare Workers port (`workers/`) uses Cloudflare's
  edge-verified `CF-Connecting-IP` by default, and only honors an additional
  `X-Real-IP` override when `WAYMARK_TRUST_FORWARDED_IP` is explicitly set
  (for a proxy in front of Cloudflare).
