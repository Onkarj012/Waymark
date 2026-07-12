# CLAUDE.md

This project keeps its full agent/contributor guide in **[AGENTS.md](AGENTS.md)** —
read that first. It covers the architecture, repo layout, build/run/test, and the
invariants you must not break (themed-vs-raw contract, pure-Go SQLite, the theme
living only in `theme/theme.css`, the public-but-unguessable page IDs, auth model).

Quick orientation:

- **Waymark** publishes clean HTML pages and returns shareable links. Go
  server + SQLite (HTML inline) + a `waymark` CLI + a house theme + an agent skill.
- To use the tool to *publish* a page, see `.skills/waymark/SKILL.md`.

Most-used commands:

```bash
go test ./... && go vet ./...                  # test + vet
go build -o bin/server ./cmd/server             # build server
go build -o bin/waymark ./cmd/waymark             # build CLI
PUBLIC_BASE_URL=http://pages.localhost:8080 \
CONTROL_BASE_URL=http://control.localhost:8080 \
WAYMARK_ADMIN_PASSCODE=dev-admin-secret ./bin/server
```

Before changing the look, edit `theme/theme.css` (the single source of truth) and
preview with `theme/demo.html`. Before changing storage/API/CLI, skim the
"Conventions & invariants" section of AGENTS.md.
