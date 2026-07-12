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
themed body content, and raw pages may contain JavaScript. Treat publishing
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

## Deployment

- Mount persistent storage at `/data`.
- Use a unique, high-entropy `WAYMARK_ADMIN_PASSCODE`.
- Set distinct HTTPS `PUBLIC_BASE_URL` and `CONTROL_BASE_URL` origins.
- Back up the complete SQLite volume consistently, including WAL state.
- Do not bake `.env`, databases, credentials, or local build output into images.
- Railway edge client IP metadata is trusted only when Railway's injected
  environment ID is present. Other deployments ignore forwarded IP headers and
  key abuse limits from the network peer.
