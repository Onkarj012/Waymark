# Agent Setup

Use this guide when a user gives you the Waymark repository URL and asks
you to set it up. First determine which path applies:

1. **Existing instance:** the server is already deployed and this machine needs
   the CLI, skill, and a device token.
2. **New instance:** Waymark must be deployed to Cloudflare Workers + D1
   before a device can connect.

Ask only one setup question at a time. Never ask the user to paste the admin
passcode into an agent prompt. The owner enters it only in the control-origin
browser UI.

## Prepare This Machine

Clone or update the repository, preserving any local work:

```bash
git clone https://github.com/Onkarj012/Waymark.git
cd Waymark
git switch main
git pull --ff-only origin main
```

If the checkout already exists, inspect `git status --short --branch` first and
do not discard local changes. Waymark requires Go 1.25 or newer. Install
Go from [go.dev/dl](https://go.dev/dl/) when it is missing or outdated.

Validate and install the CLI:

```bash
go test -count=1 ./...
mkdir -p ~/.local/bin
go build -o ~/.local/bin/waymark ./cmd/waymark
```

Ensure `~/.local/bin` is on `PATH`, then install the repository skill by linking
`.skills/waymark` into the agent's skill directory. Prefer the universal
location:

```bash
mkdir -p ~/.agents/skills
ln -s "$(pwd)/.skills/waymark" ~/.agents/skills/waymark
```

If that path already exists, inspect it first. Update an existing symlink, but
do not overwrite a real directory. Product-specific alternatives include
`~/.codex/skills` and `~/.claude/skills`.

## Add A Device To An Existing Instance

Obtain the instance's public or control HTTPS URL from the owner. Do not ask for
the admin passcode. Run:

```bash
waymark login --server <instance-url> --device-name "<clear machine name>"
```

The CLI discovers the control origin, prints an activation URL and code, and
waits. Give the activation URL to the owner. The owner signs in in the browser,
checks the device label and requested scopes, and approves it. After approval,
verify:

```bash
waymark status
```

The result must say `authenticated`, identify a `device token`, and show the
expected `pages:read` and `pages:write` scopes. Use `--read-only` during login
when the device should not publish or modify pages.

An existing valid device token normally survives CLI upgrades. If `status`
reports that authentication is missing or rejected, run the browser approval
flow again instead of requesting a shared secret.

## Deploy A New Cloudflare Workers Instance

Cloudflare Workers + D1 is the blessed path. The owner must have a Cloudflare
account and access to the repository. The service is two Workers (control +
content, source in [`workers/`](../workers)) sharing one D1 database. Free
`workers.dev` subdomains are the default, not just a smoke-test option. No
registered domain purchase or DNS zone is required. They are subdomains under
Cloudflare's domain, not a free registered domain owned by the user.

For an existing deployment, retain the current local `wrangler.*.toml` files.
Back them up outside the repository before updating from a version that tracked
them; a Git update can remove those files. Only `.example.toml` templates are
shared now. Do not regenerate custom-domain configs or migrate resources just
to connect a device.

For a new instance, first obtain approval for resource creation and deployment:

1. Use Node.js 24 LTS and npm. The lockfile requires at least Node 22.12;
   Node 18 and 20 are insufficient. From `workers/`, run `npm ci`, then
   `npx wrangler login`. The owner completes the Cloudflare browser flow.
2. In **Workers & Pages**, have the owner check or choose their account's
   `workers.dev` subdomain. For `my-account.workers.dev`, use `my-account`.
   Create the database with `npx wrangler d1 create waymark` and record the
   returned UUID, not a database ID from another deployment.
3. Run `npm run configure`, which prompts for that subdomain label and D1 UUID,
   or pass both explicitly:

   ```bash
   npm run configure -- --subdomain my-account \
     --database-id 00000000-0000-4000-8000-000000000001
   ```

   Replace the example values. This writes ignored `wrangler.control.toml` and
   `wrangler.content.toml` from the tracked templates. It never deploys or calls
   Cloudflare. It refuses overwrites unless `--force` is explicitly passed;
   never use that flag on an existing deployment without a backup and approval.
   Review that both bindings use the new database ID and both configs use:

   ```toml
   PUBLIC_BASE_URL = "https://waymark-content.my-account.workers.dev"
   CONTROL_BASE_URL = "https://waymark-control.my-account.workers.dev"
   ```

   No custom routes or legacy redirects are required. Input validation checks
   syntax, not whether the account owns a hostname or database.
4. Apply the schema with `npm run d1:migrate:remote`.
5. Have the owner create and retain a high-entropy admin passcode in their
   password manager, then set it directly with
   `npx wrangler secret put WAYMARK_ADMIN_PASSCODE -c wrangler.control.toml`.
   The agent must not receive this value.
6. Deploy both: `npm run deploy:control` and `npm run deploy:content`. Each
   re-embeds `theme/theme.css` first, so the deployed theme can't drift from
   the source file.
7. Wait for both Workers to report healthy and verify both hosts reach
   `/healthz`. Confirm that public API routes and control-origin page routes are
   rejected with HTTP 421; this proves host isolation is active. The control
   worker's hourly cron trigger (already declared in `wrangler.control.toml`)
   handles expired-page/token cleanup with no extra setup.
8. Follow **Add A Device To An Existing Instance** above to authorize the first
   machine.

The D1 database and distinct origins are required. Do not collapse the
control and content URLs onto one origin: published HTML is active content
and must not share an origin with browser administration or API tokens.
These free sibling hosts are distinct origins but same-site. `SameSite`
cookies alone are not a security boundary between them; keep host-only
`__Host-` cookies, exact-origin admin checks, CSRF tokens, and host gating.

Free hosting has quotas and is not a business-critical availability guarantee.
Cloudflare recommends custom domains or routes for production. Read the
[verified limits and official Cloudflare references](self-hosting/cloudflare-workers.md#free-hosting-limits)
before choosing the Free plan. Custom domains are optional and do not remove
hosting quotas.

For the D1 schema, backups, upgrades, and production notes, continue with
[Self-Host on Cloudflare Workers](self-hosting/cloudflare-workers.md).

## Verify End To End

After `waymark status` succeeds, write and inspect a complete local raw page:

```bash
cat > /tmp/waymark-smoke.html <<'HTML'
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Waymark smoke test</title>
<style>body { font-family: sans-serif; }</style>
</head>
<body>
<header>
  <h1>Waymark smoke test</h1>
  <p class="dek">CLI, authentication, storage, and public routing are working.</p>
</header>
<section>
  <h2>Result</h2>
  <p>The setup completed successfully.</p>
</section>
</body>
</html>
HTML
```

After the owner approves this file for publication:

```bash
waymark create --title "Waymark smoke test" --ttl 1 /tmp/waymark-smoke.html
```

Open the returned public URL and verify that the page loads as authored. Setup is
complete only when:

- the deployment (Cloudflare Workers, or the container) is healthy, when this
  was a new deployment;
- `waymark status` reports an authenticated device token;
- the smoke page loads from the public origin; and
- the agent reports the instance URL, device label, scopes, and smoke-page URL
  without printing credentials.
