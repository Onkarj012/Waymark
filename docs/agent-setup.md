# Agent Setup

Use this guide when a user gives you the Waymark repository URL and asks
you to set it up. First determine which path applies:

1. **Existing instance:** the server is already deployed and this machine needs
   the CLI, skill, and a device token.
2. **New instance:** Waymark must be deployed to Railway before a device
   can connect.

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

## Deploy A New Railway Instance

Railway is the blessed provider. The owner must have a Railway account and
access to the repository.

1. Create a Railway project from this GitHub repository.
2. Add one persistent volume to the service and mount it at `/data`.
3. Generate a Railway service domain for the control origin.
4. Add a separate custom domain for public content. Both domains route to the
   same service, but they must be different origins. If the owner has no custom
   domain, use a second generated domain when Railway makes one available.
5. Have the owner create and retain a high-entropy admin passcode in their
   password manager, then set it directly in Railway as
   `WAYMARK_ADMIN_PASSCODE`. The agent must not receive this value.
6. Set the non-secret variables:

   ```text
   PUBLIC_BASE_URL=https://<public-content-domain>
   CONTROL_BASE_URL=https://<control-domain>
   WAYMARK_TOKEN_TTL_DAYS=90
   ```

7. Do not set `PORT`; Railway supplies it. The container uses
   `/data/waymark.db` automatically.
8. Deploy from the Railway dashboard or link the checkout and upload it:

   ```bash
   railway login
   railway link
   railway up --service waymark
   ```

9. Wait for the deployment to report success and verify both domains reach
   `/healthz`. Confirm that public API routes and control-origin page routes are
   rejected with HTTP 421; this proves host isolation is active.
10. Follow **Add A Device To An Existing Instance** above to authorize the first
    machine.

The volume and distinct origins are required. Do not collapse the control and
content URLs onto one origin: published HTML is active content and must not
share an origin with browser administration or API tokens.

For backups, upgrades, rollback, and domain details, continue with
[Self-Host on Railway](self-hosting/railway.md).

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

- the Railway deployment is healthy, when this was a new deployment;
- `waymark status` reports an authenticated device token;
- the smoke page loads from the public origin; and
- the agent reports the instance URL, device label, scopes, and smoke-page URL
  without printing credentials.
