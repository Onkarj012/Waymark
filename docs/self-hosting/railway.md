# Self-Host on Railway

Railway is the blessed deployment path for Waymark. The service is one
container, one persistent volume, and two domains routed to that container.

Agents starting from the repository URL should follow
[Agent Setup](../agent-setup.md) first. It identifies the owner-only steps and
continues here for Railway operations and maintenance.

## Prerequisites

- A Railway account
- A GitHub repository containing Waymark
- The `waymark` CLI, or the latest patch release of Go 1.25+ to install it

## Deploy

1. Create a Railway project from this repository.
2. Add a volume to the service and mount it at `/data`.
3. Generate a Railway domain under service networking for the control plane.
4. Add a custom domain for published pages, or attach a second generated domain
   if you do not have one.
5. Add the variables below and wait for `/healthz`.

The volume is required. Without it, pages and device tokens disappear on the
next deployment. The two domains are also required: published HTML is active
content and cannot safely share an origin with an authenticated admin UI.

## Variables

```text
WAYMARK_ADMIN_PASSCODE=<at least 32 random bytes>
WAYMARK_TOKEN_TTL_DAYS=90
PUBLIC_BASE_URL=https://pages.example.com
CONTROL_BASE_URL=https://${{RAILWAY_PUBLIC_DOMAIN}}
```

Do not set `PORT`; Railway supplies it. The container defaults `DB_PATH` to
`/data/waymark.db`. Generate the admin passcode with:

```bash
openssl rand -base64 48
```

Never commit the passcode or paste it into an agent prompt.

Railway injects the environment metadata that lets Waymark trust the
edge-supplied client IP for abuse limits. Direct and other self-hosted
deployments ignore forwarded IP headers by default.

## Connect The CLI

```bash
go install github.com/Onkarj012/Waymark/cmd/waymark@latest
waymark login --server https://pages.example.com
waymark status
```

Open the printed activation URL, sign in with the admin passcode, and approve
the device. The admin passcode stays in the browser flow and is never saved by
the CLI. `status` reports the token label, scopes, and expiry.

## Smoke Test

```bash
waymark create --title "Railway smoke test" - <<'HTML'
<header><h1>Railway smoke test</h1></header>
<p>The service, CLI, database, and public route are working.</p>
HTML
```

Open the printed URL, then remove the page with `waymark delete <id>`.

## Domain Layout

Add both domains in Railway service networking. Configure the content domain's
DNS record, set it as `PUBLIC_BASE_URL`, and keep Railway's generated domain as
`CONTROL_BASE_URL`. Both route to the same service but must be different
origins. The server returns HTTP 421 when a route arrives on the wrong host.

## Back Up The Database

SQLite runs in WAL mode. Do not copy only `waymark.db` while the service
is actively writing; committed data may still be in sidecar files.

Use Railway's volume backup feature:

1. Open the service's **Backups** tab.
2. Create a manual backup before an upgrade.
3. Configure a daily, weekly, or monthly schedule.
4. Test a restore before relying on it.

Restoring a Railway backup stages a replacement volume at the same mount path.
Review the staged change, deploy it, and verify `/healthz` and `waymark status`.

## Upgrade

For a GitHub-connected service, merge the new commit to its connected branch.
With autodeploy enabled, Railway builds it automatically. Otherwise choose
**Deploy Latest Commit** from Railway's command palette.

For a service deployed from the Railway CLI, upload the current checkout:

```bash
railway status
railway up --service waymark
```

If the checkout is not linked yet:

```bash
railway link --project "Waymark" --environment production --service waymark
railway up --service waymark
```

Do not use `railway redeploy` for an upgrade; it deploys the previously uploaded
source. The SQLite volume remains attached across image deployments. Device
grant, token, and admin-session tables are added automatically without changing
existing page rows.

If the deployment fails, roll back to the previous successful deployment in
Railway. The persistent volume remains the service's data source.

## Upgrade An Existing Deployment

For an existing deployment with a custom page domain and a Railway-generated
domain, keep the custom domain exactly where it is and use the generated domain
for control:

1. Take a volume backup.
2. Keep `PUBLIC_BASE_URL=https://<your-custom-domain>`.
3. Add `CONTROL_BASE_URL=https://<service>.up.railway.app`.
4. Add a new high-entropy `WAYMARK_ADMIN_PASSCODE`.
5. Remove the obsolete `WAYMARK_PASSCODE` and
   `WAYMARK_ALLOW_LEGACY_AUTH` variables if they exist.
6. Run `railway up --service waymark` from the repository root.
7. Wait for `/healthz`, install the updated CLI on each machine, and run:

   ```bash
   waymark login --server https://<your-custom-domain>
   waymark status
   ```

8. Approve each device at the generated Railway control domain. Existing pages
   remain unchanged; old passcode-only CLI configurations stop working and must
   be replaced with this login flow.

## Production Checklist

- Both origins use HTTPS.
- A persistent volume is mounted at `/data` and backups are tested.
- The admin passcode is long and unique.
- `PUBLIC_BASE_URL` is the canonical content origin.
- `CONTROL_BASE_URL` is a different canonical control origin.
- Both domains reach `/healthz`; content and admin routes are host-gated.
- Published content contains no secrets.

## Railway Template

After the repository is public, publish a Railway Template containing:

- One service sourced from this repository
- A generated control domain and a separately configured content domain
- A volume mounted at `/data`
- `WAYMARK_ADMIN_PASSCODE=${{secret(64)}}`
- `CONTROL_BASE_URL=https://${{RAILWAY_PUBLIC_DOMAIN}}`
- A prompted `PUBLIC_BASE_URL` for the distinct content domain

Project-level resources such as volumes and domains cannot be fully declared by
`railway.json` alone.

Official references: [Railway templates](https://docs.railway.com/templates/create),
[variables](https://docs.railway.com/variables),
[volumes](https://docs.railway.com/volumes),
[volume backups](https://docs.railway.com/volumes/backups),
[deployment actions](https://docs.railway.com/deployments/deployment-actions),
[GitHub autodeploys](https://docs.railway.com/guides/github-autodeploys),
[Railway CLI deployments](https://docs.railway.com/cli/up), and
[public networking](https://docs.railway.com/public-networking).
