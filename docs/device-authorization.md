# Device Authorization

Waymark implements owner-approved device authorization for the `waymark`
CLI. An agent receives a scoped, revocable token without learning the deployment
admin passcode.

## Security Boundary

Published HTML is active publisher-controlled content. Themed content is not
sanitized, and raw pages may execute JavaScript. Every device-enabled deployment
therefore needs two different origins routed to the same service:

```text
CONTROL_BASE_URL  admin login, activation UI, JSON API
PUBLIC_BASE_URL   public pages and theme
```

The server host-gates both surfaces. Device tokens and the host-only admin
cookie are accepted only on the control origin. Public content is served only
on the content origin. Unknown or misdirected hosts receive HTTP 421. `/healthz`
works on every host for platform health checks.

## CLI Flow

```bash
waymark login --server https://pages.example.com
```

1. The CLI reads `/.well-known/waymark` from either configured origin.
2. It generates a 256-bit device secret and requests a ten-minute grant.
3. It prints the activation URL and an eight-character user code.
4. The owner opens the control URL, signs in, reviews the device label and
   scopes, then approves or denies it.
5. The CLI polls at the server-provided interval and saves the returned token.

Use `--device-name` to choose the approval label and `--read-only` to request
only `pages:read`. The default scopes are `pages:read pages:write`.

Polling uses `authorization_pending`, `slow_down`, `access_denied`, and
`expired_token` responses. Grants are single-use; approval and token issuance
are committed atomically, so concurrent or replayed polls cannot mint a second
token.

## Tokens And Sessions

Device tokens default to 90 days. Configure a value from 1 through 365 with
`WAYMARK_TOKEN_TTL_DAYS`. `waymark status` reports the credential kind,
label, scopes, and expiry. `waymark logout` attempts server-side revocation and
always removes the local config, even when the service is unreachable.

The admin UI lists active and revoked tokens at `/admin/tokens`. Owner sessions
last 30 days and use an opaque database-backed, host-only, `HttpOnly`,
`SameSite=Strict` cookie. Production cookies are `Secure`. Every state-changing
form requires an HMAC CSRF token bound to the session and an exact control-origin
`Origin` header.

The database stores only SHA-256 hashes of high-entropy device and API secrets.
User-code, source, and session lookup values use HMAC-SHA256 keyed by the admin
passcode. Changing the admin passcode invalidates pending codes and sessions.

## Abuse Controls

- Device grants expire after 10 minutes.
- Polling begins at 5 seconds and progressively slows premature clients.
- Creation, polling, code lookup, and admin login are rate limited.
- Pending grants are capped per source and per instance.
- Control responses are non-cacheable and include a restrictive CSP,
  `Referrer-Policy: same-origin`, framing protection, and MIME sniffing
  protection.

## Local Development

Use distinct loopback hostnames without editing `/etc/hosts`:

```text
PUBLIC_BASE_URL=http://pages.localhost:8080
CONTROL_BASE_URL=http://control.localhost:8080
```

Plain HTTP is accepted only for `localhost`, `*.localhost`, and IP loopback
origins. Production origins must use HTTPS.
