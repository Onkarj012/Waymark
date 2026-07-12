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
cd waymark
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
content, source in [`workers/`](../workers)) sharing one D1 database.

1. From the `workers/` directory: `npm install`, then `npx wrangler login`.
2. Create the database: `npx wrangler d1 create waymark`, and copy the
   printed `database_id` into the `[[d1_databases]]` block of **both**
   `wrangler.control.toml` and `wrangler.content.toml`.
3. Apply the schema: `npm run d1:migrate:remote`.
4. Have the owner create and retain a high-entropy admin passcode in their
   password manager, then set it directly with
   `npx wrangler secret put WAYMARK_ADMIN_PASSCODE -c wrangler.control.toml`.
   The agent must not receive this value.
5. Set the non-secret `[vars]` in **both** `wrangler.*.toml` files:

   ```toml
   [vars]
   PUBLIC_BASE_URL = "https://<public-content-domain>"
   CONTROL_BASE_URL = "https://<control-domain>"
   ```

   These must be two different domains — see step 6.
6. Attach a distinct custom domain to each Worker (Workers & Pages → each
   worker → Settings → Domains & Routes), or use two different
   `*.workers.dev` subdomains for a quick smoke test.
7. Deploy both: `npm run deploy:control` and `npm run deploy:content`. Each
   re-embeds `theme/theme.css` first, so the deployed theme can't drift from
   the source file.
8. Wait for both Workers to report healthy and verify both domains reach
   `/healthz`. Confirm that public API routes and control-origin page routes are
   rejected with HTTP 421; this proves host isolation is active. The control
   worker's hourly cron trigger (already declared in `wrangler.control.toml`)
   handles expired-page/token cleanup with no extra setup.
9. Follow **Add A Device To An Existing Instance** above to authorize the first
   machine.

The D1 database and distinct origins are required. Do not collapse the
control and content URLs onto one origin: published HTML is active content
and must not share an origin with browser administration or API tokens.

For the D1 schema, backups, upgrades, and production notes, continue with
[Self-Host on Cloudflare Workers](self-hosting/cloudflare-workers.md).

## Verify End To End

After `waymark status` succeeds, publish a small themed page:

```bash
waymark create --title "Waymark smoke test" - <<'HTML'
<header>
  <h1>Waymark smoke test</h1>
  <p class="dek">CLI, authentication, storage, theme, and public routing are working.</p>
</header>
<section>
  <h2>Result</h2>
  <p>The setup completed successfully.</p>
</section>
HTML
```

Open the returned public URL and verify that the themed page loads. Setup is
complete only when:

- the deployment (Cloudflare Workers, or the container) is healthy, when this
  was a new deployment;
- `waymark status` reports an authenticated device token;
- the smoke page loads from the public origin; and
- the agent reports the instance URL, device label, scopes, and smoke-page URL
  without printing credentials.
