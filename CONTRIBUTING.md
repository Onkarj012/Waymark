# Contributing

Waymark is intentionally small. Prefer focused changes that preserve the
single-binary server, SQLite storage, raw-only write contract, and agent-
friendly CLI behavior.

## Setup

Install the latest patch release of Go 1.25 or newer, clone the repository, and
run:

```bash
go test ./...
go vet ./...
```

## Before Opening a Change

```bash
gofmt -w .
go test ./...
go vet ./...
```

Update documentation and the bundled skill when CLI behavior changes. When the
theme changes, inspect `theme/demo.html` at desktop and mobile widths.

## Security

Do not include secrets, private page URLs, production databases, or deployment
credentials in issues or pull requests. Follow [SECURITY.md](SECURITY.md) for
vulnerability reports.
