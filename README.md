# Waymark

Waymark is a small self-hosted service for publishing clean HTML reports
and getting back a shareable URL. It includes a Go server, SQLite storage, the
`waymark` CLI, a built-in report theme, and an agent skill.

```text
agent -> waymark CLI -> authenticated API -> SQLite
                                      |
browser <- public unguessable URL <---+
```

Published pages are public to anyone who has their URL. Waymark is best
for reports you intend to share, not for storing secrets.

> **Giving this repository URL to an agent?** Start with
> [Agent Setup](docs/agent-setup.md). It has separate, end-to-end checklists for
> connecting a new device to an existing instance and deploying a new Railway
> instance from scratch.

## Quick Start

### 1. Install the CLI

Install with the latest patch release of Go 1.25 or newer:

```bash
go install github.com/Onkarj012/Waymark/cmd/waymark@latest
```

Make sure `$(go env GOPATH)/bin` is on `PATH`, then confirm the install:

```bash
waymark version
```

### 2. Connect to an existing instance

If someone has already deployed Waymark, ask for its public HTTPS URL,
then run:

```bash
waymark login --server https://your-service.up.railway.app
waymark status
```

`login` prints an activation URL and short code. Open the URL, sign in with the
deployment's admin passcode, review the requested scopes, and approve the
device. The CLI receives a revocable 90-day token and saves it in
`~/.config/waymark/config.json` with mode `0600`. A successful status
check identifies the token, scopes, label, and expiry. Run login again to switch
instances or replace a token.

### 3. Or deploy and connect a new Railway instance

Create a Railway project from this repository, attach a volume at `/data`, and
give the same service two domains: one for public content and one for the
control plane. Set:

```text
WAYMARK_ADMIN_PASSCODE=<a long random owner secret>
PUBLIC_BASE_URL=https://pages.example.com
CONTROL_BASE_URL=https://your-service.up.railway.app
```

Railway supplies `PORT`; the container stores SQLite at
`/data/waymark.db` and exposes `/healthz`. Published HTML is active, so
the two origins are a security requirement even though both route to the same
container.

After Railway reports the deployment healthy, connect exactly as you would to
an existing instance:

```bash
waymark login --server https://your-service.up.railway.app
waymark status
```

See [the complete Railway guide](docs/self-hosting/railway.md) for agent-friendly
deployment steps, backups, custom domains, upgrades, and production notes.

### 4. Publish

```bash
waymark create --title "First report" - <<'HTML'
<header>
  <h1>First report</h1>
  <p class="dek">A small report published from the command line.</p>
</header>
<section>
  <h2>Summary</h2>
  <p>Waymark is ready.</p>
</section>
HTML
```

The command prints the public URL. Body-only HTML receives the house theme
automatically.

## Agent Skill

The source skill is [`.skills/waymark`](.skills/waymark). From a
clone, link it into the skill directory used by your agent:

```bash
git clone https://github.com/Onkarj012/Waymark.git
cd waymark
mkdir -p ~/.agents/skills
ln -s "$(pwd)/.skills/waymark" ~/.agents/skills/waymark
```

For a product-specific location, replace `~/.agents/skills` in both commands
with `~/.codex/skills` or `~/.claude/skills`. The committed
`.claude/skills/waymark` symlink also makes the skill available to Claude
Code while working in this repository.

The skill treats the theme as a flexible component vocabulary. Semantic HTML
works without a fixed report template.

## How It Works

- `cmd/server` runs the HTTP service.
- `cmd/waymark` manages login and pages.
- `internal/store` persists page HTML and metadata in one SQLite database.
- `internal/web` serves the authenticated API and public page URLs.
- `theme/theme.css` is embedded into the server binary.
- `.skills/waymark` teaches agents how to publish accessible reports.

Themed pages store body HTML and are wrapped by the server. Raw pages store and
serve a complete document verbatim.

## CLI

```text
waymark login   [--server URL] [--device-name NAME] [--read-only]
waymark logout
waymark status

waymark create  --title "Title" [--slug s] [--raw] [--ttl N] <file|->
waymark list    [--limit N] [--json]
waymark get     [--json] <id>
waymark update  [--title T] [--slug s] [--raw] [--ttl N] <id> [<file|->]
waymark delete  <id>
```

Put flags before positional arguments. Use `-` to read page HTML from stdin.

## Security Model

- The management API accepts scoped, revocable device tokens on the control
  origin.
- Public page IDs contain roughly 71 bits of randomness.
- Page HTML is trusted publisher content and is not sanitized.
- Raw pages may execute JavaScript.
- CLI credentials are stored in `~/.config/waymark/config.json` with
  mode `0600`.

Read [SECURITY.md](SECURITY.md) before exposing an instance publicly. Browser
authentication never shares an origin with published page HTML. The protocol
and token lifecycle are documented in
[docs/device-authorization.md](docs/device-authorization.md).

## Development

```bash
go test ./...
go vet ./...
gofmt -w .
```

Run the local service with an isolated database and config directory. Plain
HTTP is accepted only for loopback development:

**Terminal 1:**
```bash
export WAYMARK_ADMIN_PASSCODE=dev-admin-secret
export PUBLIC_BASE_URL=http://pages.localhost:8080
export CONTROL_BASE_URL=http://control.localhost:8080
export DB_PATH=/tmp/waymark.db
go run ./cmd/server
```

In another terminal:

```bash
export WAYMARK_CONFIG_DIR=/tmp/waymark-config
waymark login --server http://pages.localhost:8080
```

See [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md) for repository
guidance.

## License

Waymark is available under the [MIT License](LICENSE).
