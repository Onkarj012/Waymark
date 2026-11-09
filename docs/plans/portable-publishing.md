# Portable publishing implementation plan

Status: proposal only, 2026-11-09. No runtime implementation is authorized here.
Baseline: `1bee8a944de4b81e3447d94ebd8c5f8d2ab171f8`, branch `docs/public-release-plan`.
This document owns portable workflows, not the [parent roadmap](public-release-roadmap.md)'s priorities.
All new commands, flags, routes, schemas, and defaults below are **proposed**.
They are not available in the current CLI. Examples are specifications, not instructions to run now.

## Scope and existing behavior

- Waymark is single-owner and self-hosted. Public, unguessable page links are not private ACLs.
- New writes require complete raw HTML. Legacy themed fragments remain readable.
- `waymark get --json <id>` and `GET /api/pages/{id}` return metadata, not HTML.
- `PUT /api/pages/{id}` accepts optional `if_updated_at`. Both stores already enforce atomic timestamp CAS.
  Without that field, existing updates are last-writer-wins. There is no revision history.
- Device login supports `pages:read` and `pages:write`, including read-only approval.
  Owner login, device approval, token listing, and revocation already exist in both implementations.
  Live multi-device authentication was not verified as part of this planning PR.
- CLI server precedence is flag, environment, then config. Token precedence is environment, then config.
  Existing credential storage remains separate from portable files.
- Preserve Go 1.25, pure-Go SQLite, and static no-cgo builds. Use the standard library by default.
  Keep Go and Workers/D1 behavior equivalent without new runtime dependencies.

The foundation plan owns setup, `doctor`, `init`, release binaries, free-domain deployment,
CI, and the platform size cap. Consume those features by name, without guessing PR IDs.
Follow correctness/CI, fork-safe free-host config, doctor/skill install, and releases/support
before the portable workflow sequence. Guided init can proceed independently afterward.
The initial public target is v0.4.0.
Source pull/managed publish and revisions are AFTER that milestone, not launch blockers.
The later owner dashboard and optional starters/check are not initial-release promises either.
Do not redesign them here. Do not add deployment work, public pages, commits, or automatic merging.

## P1: authenticated source export and pull

Use `pull` as the authenticated export command. Never scrape `/p/{id}` to recover source.
Proposed examples, with every flag before the ID:

```text
waymark pull --server https://control.example.test --output ./report.html PAGE_ID
waymark pull --output ./old.fragment.html --legacy-fragment PAGE_ID
```

### Source API contract

Add control-only `GET /api/pages/{id}/source`, requiring bearer `pages:read`.
Keep the existing metadata route and response unchanged, including metadata-only legacy reads.
Choose a JSON source envelope, not a navigable HTML download. Successful response fields are:

```json
{
  "schema": "waymark-source", "version": 1,
  "control_origin": "https://control.example.test",
  "public_origin": "https://pages.example.test",
  "id": "AbCdEf123456", "url": "https://pages.example.test/p/AbCdEf123456",
  "title": "Report", "slug": "report", "raw": true,
  "created_at": "2026-11-09T09:00:00Z", "updated_at": "2026-11-09T09:01:00Z",
  "expires_at": null, "html": "<!DOCTYPE html>...",
  "utf8_bytes": 18, "sha256": "<64 lowercase hex characters>"
}
```

The ellipsis and digest above are illustrative. `utf8_bytes` hashes/counts the decoded `html`
UTF-8 bytes, not JSON transport bytes. All fields are required, with `expires_at` nullable.
`slug` is a string, including empty. Hashing is SHA-256 with no newline or Unicode normalization.
Return `Content-Type: application/json; charset=utf-8`, `Cache-Control: no-store`,
`X-Content-Type-Options: nosniff`, and control-origin security headers on success and errors.
Do not support JSONP, permissive CORS, browser-session source access, or HTML content negotiation.
Use 401 for invalid credentials, 403 for insufficient scope, and 404 for missing or expired pages.
Use 422 for stored source that cannot round-trip as valid UTF-8. Never repair bytes silently.
Do not apply the current new-write size cap to export. Existing oversized pages and legacy
fragments must remain recoverable. Stream transport and staging where possible; use measured
stored byte length for allocation planning, not a rejection threshold. Disk-full/transport failure
must leave no committed sidecar. Test recovery of data larger than the current new-write cap.

Read source and metadata from the same row snapshot. Return the exact authoritative
`updated_at` string used for CAS. CLI source structs must retain that string, not reformat it.
Go currently serializes timestamps through `time.Time`; verify persisted canonical values round-trip.
Workers must count `TextEncoder` bytes, not JavaScript string length or SQLite `length(TEXT)`.
Reject invalid UTF-8 and unpaired surrogate input before storage, without rewriting valid documents.
Legacy invalid stored bytes must fail export rather than pass through lossy JSON encoding.

For `raw:true`, return the stored complete document byte-for-byte after JSON decoding.
For `raw:false`, return the original stored fragment with `raw:false`; never call the hosted renderer.
Normal pull rejects that envelope before writing. `--legacy-fragment` explicitly permits fragment export.
The sidecar marks it non-publishable. No read changes `raw`, timestamps, expiry, or stored HTML.
An owner may later author and inspect a new complete document, then explicitly approve its replacement.
Do not automatically embed the old stylesheet or call that conversion a pull.

### Local files, binding, and recovery

- Require a named `--output` file. No stdout HTML, inferred slug path, browser launch, script execution,
  external-resource fetching, or terminal printing of page source. Treat source and titles as hostile data.
- Write exact bytes plus `<output>.source.json`, both mode `0600`, with no tokens or admin secrets.
  The sidecar contains the envelope minus `html`, plus `source_path` relative to its directory.
  Require both targets absent. Version 1 has no overwrite flag; pull again to a new explicit destination.
- Stage both files in their destination directory, validate hashes, flush, and close before committing.
  Use an atomic no-clobber install such as same-filesystem `os.Link`, then remove staging files.
  Never use check-then-rename that can overwrite a concurrently created target.
  Install source first and sidecar last as the commit record. Two files are not one atomic transaction.
  On interruption, leave no trusted sidecar for incomplete bytes; report orphan files for explicit cleanup.
  If atomic no-clobber or permission protection is unsupported, fail safely rather than weaken guarantees.
- Resolve destinations through a confined directory handle. Reject symlinks, non-regular targets,
  traversal, and source/sidecar collisions. Do not chmod an existing parent or follow remote filenames.
- Normalize and bind the resolved control origin and discovered public origin to the requested ID.
  Preserve HTTPS and loopback-only HTTP rules. Reject userinfo, extra URL paths, query, and fragments.
  Refuse authenticated redirects and origin mismatches before forwarding any token.
  A manifest or sidecar is not authority to send a saved token to another server.
  Explicit server selection must match the binding; changing config must not redirect a managed operation.
- Validate returned ID, origins, URL, byte count, checksum, and timestamp before installation.
  Read sidecar paths only relative to their containing directory, with no absolute paths or `..` escapes.
  Verify regular-file identity and hash from the opened file, not a later reopened pathname.
  A checksum detects drift, not authorship or trust. Never auto-publish because a checksum matches.

Acceptance: two independently approved devices export identical UTF-8 bytes, including CRLF,
emoji, combining characters, script tags, and a trailing newline, without executing them.
Read-only credentials work. Metadata clients remain compatible. Expired pages cannot be exported.
Race-created targets, forged origins, symlinks, bad checksums, and interrupted saves fail closed.

## P2: manifest-managed publish and update

Use explicitly selected `.waymark-page.json` with a new discriminator and independent version.
This filename is distinct from existing `.waymark.json` planning artifacts and `.source.json` exports:

```json
{
  "schema": "waymark-publish", "version": 1,
  "source_path": "report.html", "source_sha256": "<current approved bytes>",
  "destination": {
    "control_origin": "https://control.example.test",
    "public_origin": "https://pages.example.test", "id": "AbCdEf123456"
  },
  "last_known_updated_at": "2026-11-09T09:01:00Z",
  "last_published_sha256": "<last accepted remote bytes>",
  "title": "Report", "slug": "report"
}
```

An unbound draft has `destination:null`, `last_known_updated_at:null`, and
`last_published_sha256:null`. No manifest contains tokens, sessions, approval grants, or passcodes.
Reject unknown versions, duplicate keys, and malformed bindings. Do not auto-discover planning files.
Existing `<slug>.waymark.json` planning metadata uses `version:3` and has different semantics.
Reject it with a migration explanation; never silently reinterpret, overwrite, or migrate it.
Foundation `init` may eventually emit this schema, but this plan does not define that command.

Proposed explicit operations:

```text
waymark publish --server https://control.example.test --manifest ./.waymark-page.json --create --title "Report" --ttl 7
waymark publish --manifest ./.waymark-page.json --if-updated-at "$EXPECTED" --keep-expiry
waymark publish --manifest ./.waymark-page.json --if-updated-at "$EXPECTED" --permanent
```

1. Validate the selected manifest and confined relative source path. Open a regular file once,
   compute its checksum, and validate the exact buffered UTF-8 bytes as a complete raw document.
   A stale `source_sha256` stops the command. Restamping after editing is an explicit local action,
   not permission to publish. Document manual manifest editing until foundation tooling supports it.
2. `--create` requires an unbound draft, explicit server selection, title, and expiry choice.
   Reuse `POST /api/pages` with `raw:true`. Never find or upsert by title or slug.
   Without `--create`, a missing binding is an error. A bound manifest cannot create a replacement ID.
3. Existing destinations require `--if-updated-at` exactly equal to the manifest's recorded string.
   Fetch fresh metadata before mutation. If the destination timestamp differs, stop before writing,
   even when local HTML is unchanged. No automatic timestamp refresh, retry, force, or overwrite.
4. Add `PUT /api/pages/{id}/managed`, requiring `pages:write`, non-null `if_updated_at`, and the
   existing typed update fields. Omit precondition: 428. Invalid precondition: 400. Stale: 409.
   Require `pages:read` too for the CLI preflight, so read-only tokens can pull but cannot publish.
   Old servers lacking this route stop with an upgrade message, never fall back to unconditional PUT.
5. Enforce CAS again in the final atomic store write, including a live-page expiry predicate.
   Keep the legacy update route's optional CAS compatibility; do not claim a CLI read prevents races.
   Preserve opaque timestamp precision across Go and Workers; use monotonic new timestamps.
6. Managed create requires exactly one positive `--ttl N` or `--permanent`.
   Managed updates require exactly one of `--keep-expiry`, positive `--ttl N`, or `--permanent`.
   Keep-expiry omits `ttl_days`; permanent sends zero. Reject negatives and ambiguous combinations.
   Reusing a saved TTL must not silently extend retention. Existing `create` still defaults to zero
   until a separate versioned decision changes it; do not change that legacy default in this work.
7. Server compares bytes and effective title, slug, raw mode, and expiry after checking live CAS.
   Identical state is a no-op: preserve `updated_at`, expiry, and history. Explicit TTL renewal is a
   mutation when its effective expiry changes. Return `{"outcome":"unchanged|updated","page":...}`,
   where `page` uses the existing metadata shape. Do not send that literal outcome union on the wire.
8. After success, atomically replace the manifest with returned authoritative metadata and hashes.
   Serialize local operations with an exclusive lock; compare the original manifest before replacement.
   A local failure after remote success reports remote ID/timestamp and requires explicit reconciliation.
   Never repeat a create after an ambiguous timeout; list/get and owner review resolve possible duplicates.

Binding a pulled raw source is an explicit local adoption of its verified sidecar values into this schema.
It is not publication authorization. Fragment sidecars cannot bind managed publishing.
Source checksum changes require inspection and a new exact approval, including destination and expiry.
The skill retains approval boundaries; command invocation and a write token do not prove human consent.
Acceptance: stale metadata or final-write races produce conflicts with no mutation; same titles/slugs
never select a destination; read-only credentials cannot mutate; no-op preserves every timestamp.

## P3: bounded revisions and safe restore

Depends on P2's live-page CAS contract and foundation's real runtime rollback/concurrency verification.
Add `page_revisions` to SQLite migration logic and a new D1 migration, never rewrite `0001_init.sql`.
Store opaque revision ID, page ID FK with cascade delete, title, slug, HTML, raw flag,
original `created_at`, `updated_at`, `expires_at`, capture time, and measured UTF-8 snapshot bytes.
Index by page ID and capture order with a unique tie-breaker. Preserve original timestamp strings.
Do not backfill invented history. Capture the prior row on the first subsequent successful mutation.

Proposed initial bounds: 20 prior revisions and 8 MiB of retained snapshot bytes per page.
Count HTML and variable metadata bytes, not TEXT character counts. Evict oldest within the same write.
Confirm the foundation platform size cap permits at least one full prior snapshot within this budget.
If it does not, settle limits before landing; never silently skip the required prior snapshot.
An existing oversized row also needs a sufficient history budget before it can be mutated.
Fail that mutation explicitly rather than discard its prior state; P1 export remains available.
These are retention ceilings, not a global database quota or backup replacement.

- Route all actual page mutations, including legacy PUT, through one history-aware store operation.
  In Go, use one transaction for live CAS, snapshot INSERT, current UPDATE, pruning, and commit.
  Failed CAS, validation, snapshot insertion, or pruning leaves current and revisions unchanged.
- D1 requires one transactional batch with SQL-gated snapshot and update predicates, not a JS
  read-check-write chain. A unique operation/revision ID gates UPDATE and pruning on its snapshot INSERT.
  Insert the prior row only when live CAS matches. No stale writer may append history.
  Prove statement-order/change-count behavior under rollback; do not rely on the current FakeD1
  batch, which runs statements sequentially without a transaction and does not prove rollback.
- Proposed routes: `GET /api/pages/{id}/revisions` returns bounded metadata under `pages:read`;
  `GET /api/pages/{id}/revisions/{revision}/source` uses P1's envelope plus `revision_id` and
  `captured_at`. Both require a live parent, control origin, no-store, and no HTML rendering.
- Proposed restore: `waymark restore --if-updated-at "$EXPECTED" PAGE_ID REVISION_ID` calls
  `POST /api/pages/{id}/restore` with `revision_id` and mandatory `if_updated_at` under `pages:write`.
  Apply 428/400/409 as in P2. A revision belonging to another page returns 404.
  Restore needs exact owner approval and creates a NEW update, archiving the current row first.
  Restore title, slug, and valid raw HTML only; retain current page ID, `created_at`, and expiry.
  Generate a new `updated_at`; never replay historical TTL or timestamp, even for identical content.
  Reject legacy fragment restore with 422 rather than convert it or reintroduce themed writes.
- Expired or deleted parents return 404 for source, history, mutation, and restore before the sweeper.
  Check expiry again inside the write. Purge revisions on explicit deletion and expiry cleanup,
  including both Go sweeper and Workers cron. Do not let history pin TTL or resurrect a page.
  No public revision route, historical public URL, or revision ID grants access.

Acceptance: concurrent restore/update yields one CAS winner; injected failures roll everything back;
count and byte pruning agree across stores; expiry races cannot revive pages or retain orphan history.

## P4: owner-only page management

Extend the existing owner UI with `/admin/pages`; keep existing token management, do not rebuild it.
Use authenticated owner sessions for the UI, not device tokens copied into browser storage.
Add metadata-only search on literal title/slug substrings, exact ID lookup, and filters for
raw/legacy mode and active/expired/permanent/expiring-within-days state. Bind all SQL parameters.
Use stable keyset pagination by `created_at DESC, id DESC`, default 50 and maximum 100.
Validate bounded opaque cursors and bind them to the search/filter set; never encode credentials.
Use a separate owner listing query rather than changing legacy `list --limit 0` compatibility.
Expired rows appear only while awaiting cleanup; the UI must not promise an expired-page archive.

Escape titles and slugs; never inject HTML source, previews, `srcdoc`, or raw revision content into
the admin origin. Public links stay on the content origin with safe new-tab attributes.
Keep scope-gated source API access separate from owner session routes. No source-bearing admin cache.
Any owner mutation requires POST, existing CSRF validation, and destination confirmation.
Any future owner restore also requires CAS, not an implicit overwrite. Do not add private sharing ACLs.
Acceptance: unauthenticated users see no metadata; forged CSRF fails; script-bearing titles stay text;
pagination with tied dates has no duplicates on a stable dataset, and filters never bypass ownership.

## P5: optional starters and offline validation

Offer at most three authored, complete raw templates: report, comparison table, and status note.
Proposed `waymark starter --name report --output ./report.html` copies embedded local bytes only.
Keep assets self-contained, use system fonts, and include responsive, accessible, printable structure.
Reuse P1's protected no-clobber file writer. Do not create a manifest or publish automatically.
Proposed `waymark check ./report.html` runs offline, without credentials or network, and reuses
`internal/render.ValidateRawDocument` plus UTF-8 and foundation byte-cap checks.
Use exit 0 for valid, 1 for invalid, 2 for usage; print diagnostics, never source or secrets.
Explain that structural validation is not sanitization or proof against external-resource use.
Keep matching Workers validator fixtures. Never restore `/theme.css` links, `raw:false`, or
`data-plan-profile`; no editor, heavy templating engine, CMS, dependency, or public template gallery.

## Ordered PR-sized delivery and verification

1. P1 source contract: `internal/web/server.go`, `workers/src/control.ts`, `workers/src/pages-api.ts`,
   and focused API tests. Cover scopes, host gating, expiry, UTF-8, no-store, and legacy non-conversion.
2. P1 CLI pull: new helpers/tests under `cmd/waymark`, reusing `config.go` and device credentials.
   Cover origin binding, redirects, exact timestamp strings, filesystem races, and partial-save recovery.
3. P2 managed route/store CAS and no-op tests, then a separate CLI manifest PR and skill examples.
   Scope `internal/store`, `internal/web`, `workers/src`, `cmd/waymark`, and `.skills/waymark/SKILL.md`.
4. P3 schema/store transactions and purge tests, then a separate restore API/CLI PR.
   Include `workers/migrations`, `workers/src/cleanup.ts`, and real runtime failure/concurrency cases.
5. P4 owner queries/UI and auth/CSRF tests in Go admin files and Workers admin/control/store files.
6. P5 local starter assets, offline check, CLI tests, and skill documentation. No foundation command edits.

Parent reports a passing baseline: Go tests/vet/gofmt, Workers typechecks and 80 tests,
and 20 installed planning-helper tests. No live deployment or publishing tests ran.
This document adds no runtime validation claims. Future PRs run focused tests, then these done commands:
`go test ./cmd/waymark ./internal/render ./internal/store ./internal/web`, `go test ./...`,
`go vet ./...`, `CGO_ENABLED=0 go build ./cmd/server ./cmd/waymark`, and from `workers/`,
`npm run typecheck`, `npm run typecheck:test`, `npm test`. Use already provisioned dependencies.
Foundation real Workers/D1 runtime checks must prove rollback and concurrent CAS before P3 ships.
Do not mark a feature done from FakeD1 alone or claim live multi-device approval has happened.

## Open decisions and STOP conditions

Stop if origin binding, byte integrity, atomic no-clobber writes, or timestamp precision cannot be proven.
Settle filesystem platform guarantees and recovery UX before claiming portable saves.
Agree on history budget versus platform size cap and migration rollback with foundation before P3.
Stop on unsupported server capabilities; never downgrade to scraping or unconditional updates.
Source exports increase local disclosure risk; `0600` limits access but is not encryption.
History aids recovery but increases retained sensitive data; purge and finite budgets are mandatory.
There is no cross-device merge engine, automatic retry after conflict, offline queue, or privacy ACL.
This documentation PR is complete without runtime edits, deployment, publication, or any live auth claim.
