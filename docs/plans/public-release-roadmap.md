# Public release and portable publishing roadmap

- Status: proposed implementation roadmap, not implemented features
- Updated: November 9, 2026
- Baseline: `main` at `1bee8a944de4b81e3447d94ebd8c5f8d2ab171f8`
- Target: a useful single-owner, self-hosted public release

Implementation progress: `feat/portable-publishing-foundation` implements source
download, diagnostics, local skill installation, fork-safe free-host setup, CLI
correctness fixes, and executable Go/Workers CI. Review, merge, and deployment
are separate from implementation. Managed publishing, history/restore, guided
deployment, and release distribution remain later work.

## Purpose and authorization

Make Waymark easy for someone other than its author to install, diagnose, and
use from multiple machines and agent clients. Keep the service small. Its job
is to publish complete HTML documents and return stable public links, not to
become a general CMS or an anonymous hosting platform.

This PR changes documentation only. The user authorized creating this plan,
subagent review, a pull request, and monitoring that PR. Merging the roadmap
does not implement the proposed commands, authorize deployments or domain
migrations, publish reports, approve device grants, or authorize automatic
merging of future implementation PRs. Each implementation slice gets its own
reviewable diff and validation evidence.

This is a repository implementation roadmap, not a published Waymark planning
artifact. It intentionally does not create HTML, publication metadata, or a
public page, and does not change the existing three-file planning contract.

## Product boundary

Waymark is a self-hosted publishing service for developers and small trusted
groups using AI agents. One owner manages an instance and approves independent,
scoped device tokens. Trusted publishers can run active HTML on the content
origin; pages are public to anyone with their unguessable link.

The public project should offer a clear README, installable binaries, working
free-host instructions, tested examples, troubleshooting, upgrade notes, and
a small synthetic read-only demonstration. A demo is not permission for the
public to publish on the owner's instance. Never expose an index of an
owner's unlisted pages as a product showcase.

The first release milestone is provisionally `v0.4.0`, subject to confirming
the repository's tags before implementation. It contains launch essentials,
not every later feature in this roadmap. Preserve the Go server and the
Workers port; neither is silently deprecated.

## Verified baseline and evidence

These observations apply to the baseline commit, not to proposed behavior:

- The CLI creates raw documents and validates them locally. Go and Workers
  independently validate the API contract. See
  [CLI commands](../../cmd/waymark/main.go),
  [Go API](../../internal/web/server.go), and
  [Workers API](../../workers/src/pages-api.ts).
- `get` returns metadata, not stored HTML. Updates support optional atomic
  `if_updated_at` comparisons; there is no revision history. See
  [Go persistence](../../internal/store/store.go) and
  [D1 persistence](../../workers/src/store.ts).
- A localhost-only probe confirmed that `create --title T - --ttl 7`
  succeeds with `ttl_days=0`: trailing flags are ignored. Correctly positioned
  flags remain the documented syntax. Fix the silent acceptance, not the
  documented permanent-page default without a compatibility decision.
- The repository's Workers configuration includes owner-specific deployment
  values. The reusable skill includes an owner-specific server and Mac path.
  Reference the locations, not the values, when preparing public examples:
  [control configuration](../../workers/wrangler.control.toml),
  [content configuration](../../workers/wrangler.content.toml), and
  [skill](../../.skills/waymark/SKILL.md).
- [Checked-in CI](../../.depot/workflows/ci.yml) covers Go, not Workers. Its
  presence alone does not prove an active required check. Verify the actual
  integration and branch-protection requirements before changing CI.
- [Fake D1 batch](../../workers/test/fake-d1.ts) runs statements sequentially
  without transaction rollback. It is not proof of deployed D1 atomicity.
- [Deployment instructions](../self-hosting/cloudflare-workers.md) contain
  a body-only smoke document incompatible with current writes and a Node
  prerequisite older than the version required by the lockfile. Clone-path
  casing also needs a Linux check.
- Device authorization already provides labels, read-only scopes, expiry,
  and revocation. Do not present those as missing features or copy one
  machine's token to another. No live device onboarding was completed as part
  of this planning PR.

## Chosen direction and compatibility

1. Make two `workers.dev` HTTPS origins the documented no-domain-purchase path
   for new instances. Custom domains remain optional. Keep the author's
   current instance unchanged unless a separate migration is approved.
2. Preserve raw-only writes, legacy themed reads, 12-character random page
   IDs, distinct control/content origins, scoped tokens, host-only admin
   cookies, exact-Origin checks, and CSRF protection.
3. Keep the stdlib Go CLI and pure-Go SQLite server. Use the existing Workers
   toolchain rather than adding application frameworks for setup commands.
4. Preserve existing `create`, `get`, and API response semantics. New managed
   publishing can require explicit lifetime and compare-and-swap without
   silently changing existing scripts.
5. Retain local canonical sources and exportable server copies. Do not claim
   the local source is synchronized without comparing its checksum and the
   server's authoritative timestamp.
6. Separate new-write size limits from read/export compatibility. Existing
   data must remain recoverable even when it exceeds the new portable limit.

## Implementation order

The two detailed plans are authoritative for feature contracts, test cases,
file scope, and STOP conditions:

- [Public-release foundation](public-release-foundation.md)
- [Portable publishing](portable-publishing.md)

Each row is a reviewable slice, not a promise to put all changes in one PR.
An executor may split a row further, but must not mix unrelated schema,
installer, and deployment changes to satisfy a deadline.

| Slice | User outcome | Prerequisites | Milestone | State |
| --- | --- | --- | --- | --- |
| R1 | No ignored arguments; Go and Workers verification is reproducible | Baseline | Initial release | Planned |
| R2 | Fork-safe configuration and correct free-host deployment, including client identity and portable document limits | R1 | Initial release | Planned |
| R3 | `doctor --json` and safe, versioned skill installation | R2 | Initial release | Planned |
| R4 | Downloadable binaries, tested documentation, and an honest support policy | R1–R3 | Initial release | Planned |
| R5 | Guided, resumable `init` without hidden account or production mutations | R2–R4 | After initial release | Planned |
| R6 | Pull sources on another machine and update through a bound, conflict-safe manifest | R1–R4 | After initial release | Planned |
| R7 | Inspect history and restore without losing intervening work | R6 and real transaction verification | After initial release | Planned |
| R8 | Search and manage pages in the owner UI without executing publisher HTML there | R6; R7 for restore controls | After initial release | Planned |
| R9 | A few offline starters and a reusable document check | R2 and established CLI contracts | Optional follow-up | Planned |

R5 and R6 can proceed independently after their shared prerequisites. R8 can
ship list/search controls before R7, but restore controls must wait. R9 must
not delay launch. Prioritize recovery and diagnostics over decorative
templates. Promote a feature only when its proposed contract is implemented
and its tests run on the advertised platforms.

## Free domains and deployment correctness

Proposed new-instance defaults, with the user's actual account subdomain:

```text
PUBLIC_BASE_URL=https://waymark-content.<account>.workers.dev
CONTROL_BASE_URL=https://waymark-control.<account>.workers.dev
```

These are provider-issued subdomains, not independently owned registered
domains. Do not promise a particular account subdomain is available, change
an account-wide subdomain, or rename existing Workers automatically. Derive
URLs from the selected account and actual deployment results.

Official guidance checked November 9, 2026:

- [Workers routing](https://developers.cloudflare.com/workers/configuration/routing/workers-dev/)
  documents the hostname format, recommends custom domains for business-critical
  deployments, and applies Browser Integrity Check on `workers.dev`. A
  descriptive application User-Agent is required for our clients; it is not
  a guarantee every network or link-preview service will permit the host.
- [Workers pricing](https://developers.cloudflare.com/workers/platform/pricing/)
  and [D1 pricing](https://developers.cloudflare.com/d1/platform/pricing/)
  describe bounded free usage, not unlimited hosting. Check the current plan
  and account usage before an actual deployment.
- [D1 limits](https://developers.cloudflare.com/d1/platform/limits/) specify
  a 2,000,000-byte string/BLOB/row limit, a 500 MB free database limit, and
  a seven-day free recovery window. Reserve row space for metadata rather
  than treating the HTML limit as the row limit.
- The [Public Suffix List](https://publicsuffix.org/list/public_suffix_list.dat)
  lists `workers.dev`. Sibling Workers within an account remain same-site
  despite being different origins. Retain the `__Host-` production cookie,
  no `Domain` attribute, and exact-Origin CSRF checks; `SameSite=Strict`
  alone is insufficient isolation.

Existing legacy-host redirects are not an independent free-domain deployment.
R2 must prove the complete free-host path without relying on a purchased
domain. A production domain move needs a separate reviewed migration with
redirect and credential-discovery tests, a backup, and a rollback plan.

## Definition of better support

- **Platforms:** publish and test macOS arm64/amd64, Linux arm64/amd64, and
  Windows amd64 first. Do not claim Windows arm64 or package-manager support
  until binaries, config paths, permissions, and login are tested there.
- **Agents:** test the universal skill path and Codex/Claude Code installation
  explicitly. Other clients can use the CLI but are best-effort until their
  installation and approval workflow is verified. Existing real skill
  directories are never replaced silently; Windows need not require symlinks.
- **Diagnostics:** commands report actionable failures, stable JSON schemas
  and exit categories, and sanitized output. Never ask users to attach a
  credential file or active private report URL to an issue.
- **Maintenance:** initially support the latest tagged stable release, with
  upgrade instructions from its predecessor. Publish a changelog and security
  contact. Do not promise response-time SLAs the maintainer cannot sustain.
- **Public presence:** update repository description and homepage only in
  the launch slice, with owner approval. Use synthetic, read-only examples;
  an attractive demo must not become an anonymous publishing endpoint.

## Launch and validation gates

An implementation PR records its actual commands and results. Baseline tests
do not prove future commands or production deployability.

The planning PR's baseline verification on November 9, 2026 passed Go tests,
vet, and the formatting check; Workers source and test typechecks; all 80
Workers tests across six files; and all 20 installed planning-artifact helper
tests. The environment used Go 1.27.3 and Node 26.13.0. This is not evidence
that the advertised minimum toolchain or other operating systems were tested.
Dependency installation generated ignored files only. No production
deployment, publication, or successful device approval was performed.

```bash
go test -count=1 ./...
go vet ./...
test -z "$(gofmt -l cmd internal theme)"
npm ci --prefix workers
npm run typecheck --prefix workers
npm run typecheck:test --prefix workers
npm test --prefix workers
git diff --check
```

R1 must add runtime-backed D1 tests with an explicit script and documented
prerequisites before those tests can become a required check. Cover atomic
grant issuance and rollback, then reuse the same harness for revisions.

Before calling the initial release supported, provide evidence from a new
isolated instance and two machines: fresh install, free-host discovery,
owner-approved write token, approved short-lived synthetic publish, public
read, metadata update, read-only scope rejection, revocation, and denied
wrong-origin access. Inspect exact HTML locally first. Never perform this
test against the owner's instance without explicit authorization. Cleanup
of test pages also needs authorization; a short TTL is not permission to
delete unrelated data. Pull/managed updates and restore are separate later
milestone gates, not implied by the first smoke test.

Documentation-only validation checks repository-relative links, source
references, consistent prerequisite names, command status labels, and the
absence of credentials, personal deployment identifiers, or private page
URLs in the diff. This roadmap does not claim the proposed release has
already passed those launch gates.

## Deferred work and risks

Do not add anonymous or multi-tenant publishing, teams/accounts, private-page
ACLs, a WYSIWYG editor, billing, analytics, or public galleries in this
roadmap. They change the trust model or ongoing support burden. An optional
read-only demo remains compatible with this decision.

Main risks are destructive config adoption, missing free-tier limits,
same-site cookie assumptions, source overwrites, incompatible manifests,
non-atomic history, and unsupported platform claims. The detailed plans
turn these into regression tests and STOP conditions. Never solve a free
hosting limit by collapsing origins, silently moving HTML to another store,
or weakening authentication.

## Decisions deliberately left open

- A future major or explicitly announced breaking release may require
  lifetime flags for legacy `create`. The initial release fixes ignored
  arguments and keeps its existing default; managed publishing is explicit.
- Final revision count and per-page byte budgets need storage measurements
  before R7. Bounds are mandatory, not an unlimited-history promise.
- A public docs site and package-manager distribution can follow a tagged
  release; neither is required to buy or promise a new domain.

No open item blocks the documentation PR. Each relevant implementation slice
must settle its remaining details before it can be called implementation-ready.
