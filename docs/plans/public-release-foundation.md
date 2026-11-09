# Public-release foundation implementation plan

Status: proposed slices, not implementation or deployment authorization.
Evidence: 2026-11-09, base `1bee8a944de4b81e3447d94ebd8c5f8d2ab171f8`.
Branch: `docs/public-release-plan`. Parent roadmap: `public-release-roadmap.md`.

## Scope and release boundary

This Markdown plan covers CLI, hosting, CI, distribution, onboarding, and operations.
No HTML or publication record is required.
This change owns only `docs/plans/public-release-foundation.md`. It authorizes no
runtime edits, installations, accounts, publication, commits, branches, remote PR
operations, deployments, or production adoption.

Roadmap alignment: PRs 1-2 support R1 correctness and CI. PRs 3-4 support R2
portable limits and fork-safe hosting. PR5 supports R3 diagnostics and skill installation.
PRs 6-7 support R4 and public `v0.4.0`. PR8 supports R5, after `v0.4.0`.
Source pull and managed update, history, dashboard, and starters/check remain later work.
The backup restore drill is operational verification, not a history feature.

## Observed repository evidence

Observations at the base commit follow, with starting lines. Proposals do not exist yet.

- `AGENTS.md:1` defines scoped tokens, raw-only writes, and separate origins.
- `cmd/waymark/main.go:114` defaults create TTL to zero. Updates track flag presence at `cmd/waymark/main.go:152`.
- `cmd/waymark/main.go:549` delegates to Go flag parsing, which stops at a positional.
  Parent reproduction: `--ttl` after the source was ignored and sent `ttl_days=0`.
- `cmd/waymark/config_test.go:89` already rejects trailing flags for `get`.
- `cmd/waymark/main.go:434` and `cmd/waymark/main.go:456` construct API requests.
  Discovery/device calls at `cmd/waymark/device_login.go:115` lack a shared UA policy.
- `cmd/waymark/config.go:19` isolates credentials. `cmd/waymark/config.go:57` secures their storage.
- `workers/wrangler.control.toml:9` and `workers/wrangler.content.toml:9`
  contain deployment-specific bindings and canonical custom-domain settings.
  Their enabled workers.dev hosts currently provide legacy redirects only.
- `workers/src/config.ts:34` requires distinct origins and validates legacy hosts.
- `workers/src/content.ts:33` redirects legacy hosts and gates canonical routes.
- `workers/src/admin.ts:59` selects `__Host-` cookies. `workers/src/admin.ts:91` enforces Origin and CSRF.
- `workers/src/body.ts:5` and `internal/web/server.go:23` cap JSON requests at 8 MiB.
  `workers/migrations/0001_init.sql:9` stores HTML and metadata in one page row.
- `workers/src/store.ts:93` conditionally updates pages. `workers/src/store.ts:284` batches grant claim and token insertion.
- `workers/test/fake-d1.ts:64` runs statements sequentially without a transaction.
  It cannot establish D1 rollback semantics or concurrent request correctness.
- `workers/package.json:7` exposes both typechecks. `workers/vitest.config.ts:3` runs Node unit tests.
- `.depot/workflows/ci.yml:14` defines Go formatting, tests, and vet only.
  Parent reports no active GitHub Actions workflows via the main CI API.
  The presence of Depot YAML does not prove checks actually run.
- `README.md:23` installs from Go source. `cmd/waymark/main.go:38` fixes a version.
- `docs/agent-setup.md:38` protects real skill directories when linking.
- `.skills/waymark/SKILL.md:55` requires action-specific approval. `.skills/waymark/SKILL.md:115` requires flags first.
- `docs/agent-setup.md:77` describes manual deployment and owner secret entry.
- `docs/self-hosting/cloudflare-workers.md:87` prioritizes custom domains.
  Its smoke example at `docs/self-hosting/cloudflare-workers.md:150` is body-only.
  Backup guidance begins at `docs/self-hosting/cloudflare-workers.md:194`.

Parent reports passing `go test -count=1 ./...`, `go vet ./...`, and gofmt checks.
After ignored `npm ci`, both Workers typechecks, 80 tests in six files, and 20 installed
planning-artifact tests passed. This baseline proves no proposed feature or live deployment.

## Decisions that all slices must preserve

1. Create keeps its existing `ttl_days=0` default. Changing expiry defaults needs
   separate product approval. Update omission preserves expiry, explicit zero clears it.
2. New installations default to two HTTPS workers.dev origins using
   `<worker>.<account>.workers.dev`. Custom domains are an optional upgrade.
   Existing production domains and database bindings require explicit migration.
3. Different origins are not necessarily different sites. The PSL workers.dev
   entry is non-wildcard, so sibling Workers under an account are same-site.
   Preserve host gating, host-only `__Host-` cookies, exact-Origin checks, and CSRF.
   HTTPS cookies retain Secure, HttpOnly, Path=/, no Domain, and current SameSite.
4. Every CLI request sends a descriptive `Waymark/<version>` application User-Agent,
   including discovery, device code, polling, auth status, CRUD, revoke, and diagnostics.
   Preserve timeouts and credential isolation across redirects. Never mimic a browser.
   BIC is forced on workers.dev. Some default UAs receive pre-Worker 1010 blocks.
   Application identification is required, not a universal reachability guarantee.
5. The proposed `v0.4.0` new-write document ceiling is 1,000,000 UTF-8 bytes.
   Bound each newly supplied title and slug to 1,024 UTF-8 bytes after normalization.
   Include fixed fields and SQLite record overhead in the tested row budget.
   New rows must stay below D1's 2,000,000-byte string, BLOB, and row limits.
6. Keep the independent 8,388,608-byte JSON request cap in both backends.
   JSON escaping increases wire size. Do not treat JSON bytes, document bytes,
   JavaScript string length, Go rune count, and serialized row size as interchangeable.
   Worst-case escaped supported documents and bounded metadata must fit the body cap.
7. Reject invalid UTF-8 before new HTML storage. Test invalid request bytes and
   escaped unpaired surrogates explicitly. Never promise byte-exact preservation
   where decoders replace invalid input. Existing legacy reads remain available.
8. Tightening Go writes is an announced `v0.4.0` constraint, not an incidental fix.
   Keep existing oversized pages readable and operationally exportable.
   Metadata-only changes must not revalidate unchanged oversized legacy HTML.
   Validate newly supplied fields and replacement HTML before mutation.
   Replacement HTML above the ceiling fails without truncation or partial conversion.
9. Discovery may add `max_document_bytes`, `max_title_bytes`, and `max_slug_bytes`.
   These are additive hints. Old clients ignore unknown fields. Missing limits mean
   unknown, not unlimited. Servers remain final authority and return documented 413s.
10. Setup never accepts a secret through argv, logs, chat, or checkpoint files.
    Owner browser approval and private terminal secret entry remain explicit steps.

## Ordered implementation slices

Commands below are future gates, not authorized actions. Record results and baseline failures separately.

### PR1: Reject ambiguous CLI arguments and identify HTTP clients

Depends on none. In scope: `cmd/waymark/main.go`, `cmd/waymark/device_login.go`, CLI tests.
Out of scope: expiry defaults, new deployment commands, API or storage changes.
Use `cmd/waymark/config_test.go:89` and device-login httptest cases as exemplars.
- Retain flags-before-positionals. Enforce exact arity before files, credentials, or HTTP.
  Respect `--` for literal dash-prefixed filenames and `-` for stdin.
- Reject trailing flags, unknown flags, missing flag values, extra source files,
  and missing IDs across commands. Errors must state accepted argument ordering.
- Test omitted TTL, explicit zero, positive TTL, metadata-only update, and preconditions.
  Test `create --title T file.html --ttl 7` fails with zero outgoing requests.
  Test the correctly ordered form sends seven and omitted TTL still sends zero.
- Introduce one stdlib request policy used by every existing and future HTTP path.
  Capture headers for discovery, polling, status, CRUD, logout/revoke, and redirects.
  Ensure no bearer token follows an unapproved cross-origin redirect.
- Run `go test -count=1 ./cmd/waymark` and `go vet ./cmd/waymark`.
  Expect passing regressions and no request for any rejected argument vector.
- STOP if preserving existing valid CLI forms requires silently ignoring arguments,
  if a request bypasses the User-Agent policy, or if credentials cross origins.

### PR2: Make Workers checks executable and prove D1 transactions

Depends on PR1 for the R1 gate only. In scope: `.depot/workflows/ci.yml`,
`workers/package.json`, lockfile, test configuration, runtime tests, optional portable CI.
Out of scope: deployment credentials, release publication, unrelated store rewrites.
Use `workers/test/store.test.ts:1` and existing migrations as fixture exemplars.
- Verify Depot produces required PR checks on an actual test PR.
  If it does not, add a portable GitHub Actions workflow with equivalent checks.
  Keep checks read-only and secretless. Do not claim YAML existence proves execution.
- Pin a supported Node runtime that handles the existing Node SQLite test adapter
  and the locked Wrangler version. Reconcile engine declarations and setup docs.
- Run `cd workers && npm ci && npm run typecheck && npm run typecheck:test && npm test`.
  Expect source and test types plus unit tests to pass from a clean checkout.
- Add `npm run test:runtime` using local workerd with actual D1 bindings and migrations.
  Keep FakeD1 for unit speed, not transaction evidence. No cloud account required.
- Force token insertion failure after a grant claim. Expect the grant still approved,
  no token inserted, and a subsequent valid poll able to issue exactly one token.
- Start competing polls for one approved grant through independent requests.
  Expect one issued token and no token row for the loser. Repeat with fresh fixtures.
- Race updates with the same expected timestamp. Expect one success and one 409.
  Assert final HTML and timestamp belong to the winner, not just response counts.
- Run `go test ./... && go vet ./...` alongside Workers checks in required CI.
- STOP if rollback is tested only through FakeD1, races are only sequential replays,
  runtime bindings differ from migrated schema, or no provider executes required checks.

### PR3: Define and enforce the supported document-size contract

Depends on PR2. Supports R2. In scope: CLI validation, Go and Workers handlers,
discovery responses, shared fixtures, API tests, upgrade notes, export instructions.
Out of scope: raising D1 limits, splitting HTML across rows, blob storage, TTL changes.
Use `workers/src/body.ts:13`, `internal/web/server.go:485`, and raw validation tests.
- Implement the byte limits and legacy policy in decisions 5-9 on both backends.
  Bound CLI file and stdin reads. Keep JSON stream limits independent of Content-Length.
- Test exact ceiling and ceiling plus one with ASCII, multibyte text, quotes,
  backslashes, and JSON-escaped characters. Measure decoded UTF-8 bytes, not characters.
- Reject invalid UTF-8 and invalid Unicode escapes consistently without mutation.
  Test metadata limits, escaped-body expansion, streamed bodies, and absent length.
- Exercise actual D1 at the maximum supported row. Assert writes and subsequent reads.
  Bound record overhead explicitly and show the full row stays below 2,000,000 bytes.
- Run Go and Workers suites plus `npm run test:runtime` against the same fixtures.
  Expect matching acceptance and 413 behavior, with no truncation or partial writes.
- Seed an oversized legacy Go page. Verify public read, metadata update, and export.
- Publish upgrade notes with the first enforcing server and CLI versions.
  Test old CLI/new server and new CLI/old server, including missing discovery limits.
- STOP if one backend silently accepts a different new-write contract, old data
  becomes unreadable, or the supported ceiling exceeds a measured D1 row budget.

### PR4: Ship fork-safe configs with free-host defaults and migration

Depends on PR1-3. Supports R2. In scope: Worker examples, package scripts, ignore rules,
config validation, hosting/migration docs, routing/auth tests, generic skill setup wording.
Out of scope: account secrets, production resource edits, custom-domain removal.
Use existing origin validation and legacy-host redirects, not a second routing scheme.
- Move public examples to placeholder-only files and generate ignored, account-specific
  control/content config pairs outside tracked production defaults.
  Require explicit `--config` paths for deploy, migration, export, and secret commands.
- First preserve the owner's current config in a private deployment directory.
  Supply an explicit migration checklist for both bindings, names, origins, cron,
  routes, and legacy hosts. Compare normalized effective configs before any deploy.
  Only then replace tracked deployment-specific values with generic examples.
- Default fresh configs to separate canonical HTTPS workers.dev hosts, enabled routing,
  one shared fresh D1 binding, and empty legacy hosts. Discover the account subdomain.
  Do not invent it from an account ID or reuse an existing production database.
- Keep custom-domain examples optional. A later migration preserves old public links
  through explicit legacy redirects. Test methods, query preservation, and auth safety.
- Run runtime route tests for discovery, public pages, wrong-host 421, redirects,
  forged sibling-Origin POSTs, missing CSRF, and HTTPS cookie attributes.
  Smoke-test two Workers only in an explicitly approved disposable account context.
- Expect both canonical free hosts to serve directly, not redirect to owner domains.
  Document BIC/1010 and proxy blocks as possible pre-Worker failures despite the UA.
- STOP if generated defaults target existing resources, copied production config
  cannot be proven equivalent, or sibling same-site cookies bypass Origin/CSRF checks.

### PR5: Add read-only doctor and safe skill installation

Depends on PR4. Supports R3. In scope: CLI dispatch, doctor, skill install, tests, docs.
Out of scope: remote repair, deployment, login approval, artifact publication policy.
Use existing `status`, config precedence, permissions tests, and setup symlink guidance.
- Define `waymark doctor --json` with `schema_version`, overall status, and checks.
  Each check has a stable code, `ok|warn|fail|unknown`, and redacted remediation text.
  Exit 0 for healthy/warnings, 1 for failed checks, 2 for invalid invocation.
- Inspect version, config source and permissions, binary location, skill target,
  origins, discovery, and auth through bounded read-only requests.
  Report unavailable account data as unknown. Never create tokens or repair state.
  Existing authentication may perform normal bookkeeping such as updating token
  `last_used_at`; this is permitted and must be disclosed. Doctor must not change
  page data, token lifecycle, deployment configuration, or account resources.
- Redact secrets from all output, including wrapped errors and server response bodies.
  Exclude token prefixes, cookies, secret environment values, and raw config dumps.
  Test sentinels in stdout, stderr, JSON, URLs, and network failure messages.
- Propose `waymark skill install --source <directory> --target <directory>`.
  Same destination and source is a no-op. A different symlink requires confirmation.
  Refuse files and real directories, including stale copied skills. Never delete them.
  Windows must not require symlinks. Offer an explicit copy into a new destination.
  Identical managed copies are no-ops. Conflicting copies stop, without directory overwrite.
  Preserve the source skill's explicit-only workflow. Description-only drift is not a policy change.
- Run `go test -count=1 ./cmd/waymark` with temporary homes, symlink conflicts,
  unavailable network, expired tokens, and differing installed description text.
  Expect idempotent installs and unchanged filesystem snapshots after doctor.
- Test authentication with bookkeeping both due and not due. Apart from that
  normal bookkeeping, assert no remote application or token-lifecycle mutation.
- STOP if doctor changes remote state beyond normal authentication bookkeeping,
  diagnostics leak a sentinel, or installation overwrites a real directory or
  silently replaces a conflicting link.

### PR6: Test setup docs, operating limits, and support policy

Depends on PR1-5. Supports R4. In scope: README, setup/hosting guides, CLI reference,
security/support policy, complete HTML fixtures, disposable local smoke scripts.
Out of scope: roadmap-owned content workflow changes and page-history implementation.
- Replace body-only examples with tested complete documents and flags-first commands.
  Separate source install, verified binary install, existing-instance login,
  new free-host deployment, and optional custom domains. Mark guided init as future.
- Document CLI/API combinations, Go 1.25 minimum, tested Node/OS versions, latest-tag
  stable support, security reporting, and breaking-change notices.
  Only the latest released stable tag receives routine fixes. Older tags require upgrade.
  Identify a maintainer and response expectations without promising response SLAs.
- Record quota boundaries with dates: free D1 500 MB per database, 5 GB total,
  2,000,000-byte value/row limits, and seven-day recovery. Link request and query quotas.
  Explain quota exhaustion, upstream blockers, and optional paid upgrades.
  Do not promise free forever, zero costs, or universally reachable free hostnames.
- Test export then restore into a different disposable database, never production.
  For D1, use explicit config-bound export/import and validate rows and HTML hashes.
  Run `wrangler d1 export <db> --remote --config <source-config> --output <backup.sql>`.
  Restore with `wrangler d1 execute <restore-db> --remote --config <restore-config> --file <backup.sql>`.
  For Go, pause writes or use a consistent SQLite backup that includes WAL state.
  Verify expiry, auth-table handling, host isolation, and authorized page reads after restore.
  Treat backups as secrets. Define retention, encryption, access, and safe disposal.
- Run local documented smoke commands with isolated credentials and approved fixtures.
  Expect login/status, raw publish/read, guarded update/conflict, and explicit cleanup.
- Add troubleshooting for parser errors, expiry, wrong origins, 421, 413, D1 failures,
  BIC/1010 before Worker logs, proxy blocks, and config/account mismatches.
- STOP if documented commands need undisclosed owner values, examples publish without
  consent, backup restore is untested, or quota/support claims lack dated references.

### PR7: Publish verifiable cross-platform CLI releases

Depends on PR6 and R1-R3 gates. Completes R4 for `v0.4.0`. In scope: release CI,
build scripts, versions, installers, PowerShell docs, notes, checksums, provenance.
Out of scope: automatic deployment, automatic upgrades, init, server binary promises.
- Build Linux, macOS, and Windows CLI archives for amd64 and arm64 with CGO disabled.
  Set tag/commit build metadata consistently. State which targets receive native
  execution tests versus cross-compilation only. Do not claim untested native support.
- Generate SHA-256 manifests and signed provenance binding artifacts to tag, commit,
  workflow identity, and builder. Sign or attest the checksum manifest too.
- Gate publication on protected tag/manual approval, passing tests, and changelog review.
  Use least-privilege release permissions and pinned action identities.
- Installers select explicit versions and verify trusted signatures/attestations and
  checksums before extraction. Reject traversal, unexpected binaries, and mismatches.
  Use a temporary directory and atomic replacement. No `curl | sh`, sudo, or token prompt.
- Build all targets using `CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build ./cmd/waymark`.
  Test version/help/doctor and local login/CRUD natively. Reject tampered archives and wrong signer identities.
- Rehearse unsigned-publication refusal and interrupted-install rollback locally.
  Keep source install supported and document verification tooling prerequisites.
- STOP release if checks are not required, an artifact lacks trusted provenance,
  any installer executes before verification, or setup/restore gates remain unproven.

### PR8: Add resumable, explicitly confirmed init after the first release

Depends on PR7. Supports R5. In scope: `waymark init`, typed provider operations,
private state/configs, setup docs, fake-provider tests, approved account integration tests.
Out of scope: production adoption, arbitrary hooks, shell templates, paid-plan changes.
- Separate connect-existing from create-new. Existing mode only selects the instance,
  offers skill installation, and requests owner device approval. It never deploys.
- New mode records verified account identity and a unique deployment namespace.
  Use private account-specific Wrangler configs/state and an isolated Waymark config.
  Never fall back to repository production config or ambient account selection.
- Present a plan, then confirm each remote mutation with account, resource, and effect.
  Steps cover new D1 creation, migration, owner secret entry, two Worker deployments,
  health/isolation verification, device approval, and separately approved smoke publish.
- Persist only non-secret step IDs, resource references, config hashes, and outcomes.
  Resume revalidates account, observed resources, and config hashes before continuing.
  Interrupts must not create duplicates. Drift, ambiguous ownership, or account changes stop.
- Accept secrets only through owner-controlled private terminal input or browser flows.
  Use fixed executable allowlists and typed argument arrays, never a shell or arbitrary
  commands from config, remote responses, templates, or checkpoint files.
- Test cancellation at every boundary, retries, partial deployment, secret redaction,
  denied permissions, and pre-existing name collisions. No automatic destructive rollback.
  Print redacted manual recovery steps and require separate cleanup confirmation.
- Run `go test -count=1 ./cmd/waymark`, then an explicitly approved disposable-account drill.
  Expect resume without duplicates and no unrelated account, credential, or resource changes.
- STOP on secret-bearing argv/checkpoints, unclear confirmation, unavailable private input,
  required production adoption, or inability to isolate the selected account.

## Review, babysit, and completion evidence

For each authorized future PR, review its scope and record redacted test evidence.
Watch required checks, address findings, and rerun affected gates. Review approval
never authorizes deployment. Merge, release, and account changes need explicit authority.
Release requires PR1-7, running CI, compatible limits, restored backups, verified artifacts, and tested setup docs.
Account restrictions and external host blocking remain operator risks. PR8 needs its own approval.

## Dated external references

Parent verified these sources on 2026-11-09. This edit used no network. Recheck before release.

- Workers free hostname and custom-domain guidance: https://developers.cloudflare.com/workers/configuration/routing/workers-dev/
- Browser Integrity Check: https://developers.cloudflare.com/waf/tools/browser-integrity-check/
- Error 1010: https://developers.cloudflare.com/support/troubleshooting/http-status-codes/cloudflare-1xxx-errors/error-1010/
- D1 limits: https://developers.cloudflare.com/d1/platform/limits/
- D1 pricing and query quotas: https://developers.cloudflare.com/d1/platform/pricing/
- Workers limits: https://developers.cloudflare.com/workers/platform/limits/
- D1 recovery: https://developers.cloudflare.com/d1/reference/time-travel/
- D1 import/export: https://developers.cloudflare.com/d1/best-practices/import-export-data/
- Public Suffix List source, workers.dev entry: https://github.com/publicsuffix/list/blob/master/public_suffix_list.dat
