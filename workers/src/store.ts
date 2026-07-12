// D1-backed persistence layer. Ports internal/store/store.go and
// internal/store/auth.go onto Cloudflare D1. Column names/types/indexes match
// the Go SQLite schema (see workers/migrations/0001_init.sql) so the two
// backends store equivalent data.
//
// D1 does not expose interactive multi-statement transactions to Workers code
// the way database/sql's Tx does (batch() runs a fixed list of statements,
// not "read a value, decide in JS, then write" the way Go's CreateDeviceAuthorization
// or PollDeviceAuthorization do). Most methods below perform their guard-check
// SELECT and their write as separate sequential statements rather than a
// single atomic transaction. For a single-owner self-hosted deployment (the
// documented use case) this is an accepted, intentional gap: the practical
// race window (two device-code requests from the same source within the same
// request tick) does not threaten confidentiality or integrity, only (at
// most) an off-by-one on a rate/limit counter.
//
// pollDeviceAuthorization's approved -> consumed transition is the one path
// where a race would mint duplicate valid tokens, so it does not rely on that
// accepted gap: the grant is claimed via a single guarded UPDATE ... WHERE
// status = 'approved' before the token INSERT runs, so only the caller whose
// UPDATE actually changes a row ever mints a token. That single statement is
// atomic on D1/SQLite the same way Go's transaction is, so this path is
// race-safe without needing a multi-statement transaction.

import type {
  APIToken,
  AdminSession,
  CreateGrantOutcome,
  CreateSessionOutcome,
  DecideOutcome,
  DeviceAuthorization,
  D1Like,
  GrantLookupOutcome,
  Page,
  PageMeta,
  PollOutcome,
} from "./types";
import { NotFoundError } from "./types";

export function nowIso(d: Date = new Date()): string {
  return d.toISOString();
}

function toBool(v: unknown): boolean {
  return Number(v) !== 0;
}

// --- pages ------------------------------------------------------------------

export async function createPage(db: D1Like, p: Page): Promise<void> {
  await db
    .prepare(
      `INSERT INTO pages (id, title, slug, html, raw, created_at, updated_at, expires_at)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
    )
    .bind(p.id, p.title, p.slug, p.html, p.raw ? 1 : 0, p.createdAt, p.updatedAt, p.expiresAt)
    .run();
}

export async function getPage(db: D1Like, id: string): Promise<Page> {
  const row = await db
    .prepare(
      `SELECT id, title, slug, html, raw, created_at, updated_at, expires_at FROM pages WHERE id = ?`,
    )
    .bind(id)
    .first<Record<string, unknown>>();
  if (!row) throw new NotFoundError("page not found");
  return {
    id: String(row.id),
    title: String(row.title),
    slug: String(row.slug),
    html: String(row.html),
    raw: toBool(row.raw),
    createdAt: String(row.created_at),
    updatedAt: String(row.updated_at),
    expiresAt: row.expires_at == null ? null : String(row.expires_at),
  };
}

export async function savePage(db: D1Like, p: Page): Promise<void> {
  const result = await db
    .prepare(
      `UPDATE pages SET title = ?, slug = ?, html = ?, raw = ?, updated_at = ?, expires_at = ? WHERE id = ?`,
    )
    .bind(p.title, p.slug, p.html, p.raw ? 1 : 0, p.updatedAt, p.expiresAt, p.id)
    .run();
  if (!(result.meta.changes ?? 0)) throw new NotFoundError("page not found");
}

export async function deletePage(db: D1Like, id: string): Promise<void> {
  const result = await db.prepare(`DELETE FROM pages WHERE id = ?`).bind(id).run();
  if (!(result.meta.changes ?? 0)) throw new NotFoundError("page not found");
}

export async function listPages(db: D1Like, limit: number): Promise<PageMeta[]> {
  let query = `SELECT id, title, slug, raw, created_at, updated_at, expires_at, length(html) AS size
               FROM pages ORDER BY created_at DESC`;
  const stmt = db.prepare(limit > 0 ? `${query} LIMIT ?` : query);
  const bound = limit > 0 ? stmt.bind(limit) : stmt;
  const result = await bound.all<Record<string, unknown>>();
  return (result.results ?? []).map((row) => ({
    id: String(row.id),
    title: String(row.title),
    slug: String(row.slug),
    raw: toBool(row.raw),
    createdAt: String(row.created_at),
    updatedAt: String(row.updated_at),
    expiresAt: row.expires_at == null ? null : String(row.expires_at),
    size: Number(row.size),
  }));
}

export async function deleteExpiredPages(db: D1Like, nowIsoValue: string): Promise<number> {
  const result = await db
    .prepare(`DELETE FROM pages WHERE expires_at IS NOT NULL AND expires_at <= ?`)
    .bind(nowIsoValue)
    .run();
  return result.meta.changes ?? 0;
}

export async function deleteExpiredAuth(db: D1Like, nowIsoValue: string): Promise<number> {
  let total = 0;
  for (const q of [
    `DELETE FROM device_authorizations WHERE expires_at <= ?`,
    `DELETE FROM api_tokens WHERE expires_at <= ?`,
    `DELETE FROM admin_sessions WHERE expires_at <= ?`,
  ]) {
    const result = await db.prepare(q).bind(nowIsoValue).run();
    total += result.meta.changes ?? 0;
  }
  return total;
}

// --- device authorizations ---------------------------------------------------

function rowToGrant(row: Record<string, unknown>): DeviceAuthorization {
  return {
    id: String(row.id),
    deviceCodeHash: String(row.device_code_hash),
    deviceSecretHash: String(row.device_secret_hash),
    userCodeHash: String(row.user_code_hash),
    deviceLabel: String(row.device_label),
    scopes: String(row.scopes),
    sourceKey: String(row.source_key),
    sourceHint: String(row.source_hint),
    status: String(row.status) as DeviceAuthorization["status"],
    createdAt: String(row.created_at),
    expiresAt: String(row.expires_at),
    approvedAt: row.approved_at == null ? null : String(row.approved_at),
    deniedAt: row.denied_at == null ? null : String(row.denied_at),
    lastPollAt: row.last_poll_at == null ? null : String(row.last_poll_at),
    pollIntervalSeconds: Number(row.poll_interval_seconds),
    consumedAt: row.consumed_at == null ? null : String(row.consumed_at),
  };
}

const GRANT_COLUMNS = `id, device_code_hash, device_secret_hash, user_code_hash, device_label, scopes,
  source_key, source_hint, status, created_at, expires_at, approved_at, denied_at, last_poll_at,
  poll_interval_seconds, consumed_at`;

export async function createDeviceAuthorization(
  db: D1Like,
  g: DeviceAuthorization,
  perSource: number,
  perInstance: number,
): Promise<CreateGrantOutcome> {
  const sourceCountRow = await db
    .prepare(
      `SELECT count(*) AS n FROM device_authorizations WHERE source_key = ? AND status = 'pending' AND expires_at > ?`,
    )
    .bind(g.sourceKey, g.createdAt)
    .first<{ n: number }>();
  const totalCountRow = await db
    .prepare(`SELECT count(*) AS n FROM device_authorizations WHERE status = 'pending' AND expires_at > ?`)
    .bind(g.createdAt)
    .first<{ n: number }>();
  const sourceCount = Number(sourceCountRow?.n ?? 0);
  const totalCount = Number(totalCountRow?.n ?? 0);
  if (sourceCount >= perSource || totalCount >= perInstance) {
    return { kind: "limit_reached" };
  }
  await db
    .prepare(
      `INSERT INTO device_authorizations
        (id, device_code_hash, device_secret_hash, user_code_hash, device_label, scopes, source_key, source_hint, status, created_at, expires_at, poll_interval_seconds)
       VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?)`,
    )
    .bind(
      g.id,
      g.deviceCodeHash,
      g.deviceSecretHash,
      g.userCodeHash,
      g.deviceLabel,
      g.scopes,
      g.sourceKey,
      g.sourceHint,
      g.createdAt,
      g.expiresAt,
      g.pollIntervalSeconds,
    )
    .run();
  return { kind: "created" };
}

export async function deviceAuthorizationByUserCode(
  db: D1Like,
  hash: string,
  now: string,
): Promise<GrantLookupOutcome> {
  const row = await db
    .prepare(`SELECT ${GRANT_COLUMNS} FROM device_authorizations WHERE user_code_hash = ?`)
    .bind(hash)
    .first<Record<string, unknown>>();
  if (!row) return { kind: "not_found" };
  const grant = rowToGrant(row);
  if (!(grant.expiresAt > now)) return { kind: "expired" };
  return { kind: "found", grant };
}

export async function deviceAuthorizationByDeviceCode(
  db: D1Like,
  codeHash: string,
  secretHash: string,
  now: string,
): Promise<GrantLookupOutcome> {
  const row = await db
    .prepare(`SELECT ${GRANT_COLUMNS} FROM device_authorizations WHERE device_code_hash = ? AND device_secret_hash = ?`)
    .bind(codeHash, secretHash)
    .first<Record<string, unknown>>();
  if (!row) return { kind: "not_found" };
  const grant = rowToGrant(row);
  if (!(grant.expiresAt > now)) return { kind: "expired" };
  return { kind: "found", grant };
}

export async function decideDeviceAuthorization(
  db: D1Like,
  hash: string,
  decision: "approved" | "denied",
  now: string,
): Promise<DecideOutcome> {
  const column = decision === "denied" ? "denied_at" : "approved_at";
  const result = await db
    .prepare(
      `UPDATE device_authorizations SET status = ?, ${column} = ?
       WHERE user_code_hash = ? AND status = 'pending' AND expires_at > ?`,
    )
    .bind(decision, now, hash, now)
    .run();
  if (!(result.meta.changes ?? 0)) return { kind: "not_found" };
  return { kind: "decided" };
}

/** Advances the persisted polling state. When the grant is approved, it
 * atomically claims the grant (guarded UPDATE ... WHERE status = 'approved')
 * before minting and inserting the token, so concurrent pollers cannot both
 * win. Ports PollDeviceAuthorization from internal/store/auth.go (see the
 * file-level note on how this path stays race-safe without D1's lacking
 * interactive multi-statement transactions). */
export async function pollDeviceAuthorization(
  db: D1Like,
  codeHash: string,
  secretHash: string,
  now: string,
  token: APIToken,
): Promise<PollOutcome> {
  const row = await db
    .prepare(`SELECT ${GRANT_COLUMNS} FROM device_authorizations WHERE device_code_hash = ? AND device_secret_hash = ?`)
    .bind(codeHash, secretHash)
    .first<Record<string, unknown>>();
  if (!row) return { kind: "not_found" };
  const grant = rowToGrant(row);
  if (!(grant.expiresAt > now)) return { kind: "expired" };
  if (grant.status === "denied") return { kind: "denied" };
  if (grant.status === "consumed") return { kind: "consumed" };

  if (grant.status === "approved") {
    // Claim the grant with a single guarded UPDATE before minting anything.
    // Only the request whose UPDATE actually flips a row (changes === 1) is
    // allowed to insert the token; a concurrent loser sees changes === 0 and
    // returns the same "consumed" outcome as a replay against an
    // already-redeemed grant, without ever inserting a token row. This
    // mirrors the guarantee the Go store gets for free from wrapping the
    // INSERT + UPDATE in a single sqlite transaction: here it's a single
    // atomic statement instead, since D1 has no interactive multi-statement
    // transactions.
    const consume = await db
      .prepare(`UPDATE device_authorizations SET status = 'consumed', consumed_at = ? WHERE id = ? AND status = 'approved'`)
      .bind(now, grant.id)
      .run();
    if ((consume.meta.changes ?? 0) !== 1) return { kind: "consumed" };
    await db
      .prepare(
        `INSERT INTO api_tokens (id, token_hash, display_prefix, device_label, scopes, created_at, expires_at)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
      )
      .bind(token.id, token.tokenHash, token.displayPrefix, token.deviceLabel, token.scopes, token.createdAt, token.expiresAt)
      .run();
    return { kind: "issued" };
  }

  if (grant.lastPollAt && now < addSeconds(grant.lastPollAt, grant.pollIntervalSeconds)) {
    const interval = Math.min(grant.pollIntervalSeconds + 5, 30);
    await db
      .prepare(`UPDATE device_authorizations SET last_poll_at = ?, poll_interval_seconds = ? WHERE id = ?`)
      .bind(now, interval, grant.id)
      .run();
    return { kind: "slow_down", interval };
  }

  if (grant.status === "pending") {
    await db
      .prepare(`UPDATE device_authorizations SET last_poll_at = ? WHERE id = ?`)
      .bind(now, grant.id)
      .run();
    return { kind: "pending", interval: grant.pollIntervalSeconds };
  }
  return { kind: "not_found" };
}

function addSeconds(isoTime: string, seconds: number): string {
  return new Date(new Date(isoTime).getTime() + seconds * 1000).toISOString();
}

// --- API tokens ---------------------------------------------------------------

function rowToToken(row: Record<string, unknown>): APIToken {
  return {
    id: String(row.id),
    tokenHash: String(row.token_hash),
    displayPrefix: String(row.display_prefix),
    deviceLabel: String(row.device_label),
    scopes: String(row.scopes),
    createdAt: String(row.created_at),
    expiresAt: String(row.expires_at),
    lastUsedAt: row.last_used_at == null ? null : String(row.last_used_at),
    revokedAt: row.revoked_at == null ? null : String(row.revoked_at),
  };
}

export async function apiTokenByHash(db: D1Like, hash: string, now: string): Promise<APIToken | null> {
  const row = await db
    .prepare(
      `SELECT id, token_hash, display_prefix, device_label, scopes, created_at, expires_at, last_used_at, revoked_at
       FROM api_tokens WHERE token_hash = ? AND revoked_at IS NULL AND expires_at > ?`,
    )
    .bind(hash, now)
    .first<Record<string, unknown>>();
  return row ? rowToToken(row) : null;
}

export async function touchApiToken(db: D1Like, id: string, now: string): Promise<void> {
  const cutoff = new Date(new Date(now).getTime() - 5 * 60 * 1000).toISOString();
  await db
    .prepare(`UPDATE api_tokens SET last_used_at = ? WHERE id = ? AND (last_used_at IS NULL OR last_used_at <= ?)`)
    .bind(now, id, cutoff)
    .run();
}

export async function revokeApiToken(db: D1Like, id: string, now: string): Promise<void> {
  const result = await db
    .prepare(`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`)
    .bind(now, id)
    .run();
  if (!(result.meta.changes ?? 0)) throw new NotFoundError("token not found");
}

export async function listApiTokens(db: D1Like): Promise<APIToken[]> {
  const result = await db
    .prepare(
      `SELECT id, token_hash, display_prefix, device_label, scopes, created_at, expires_at, last_used_at, revoked_at
       FROM api_tokens ORDER BY created_at DESC`,
    )
    .all<Record<string, unknown>>();
  return (result.results ?? []).map(rowToToken);
}

// --- admin sessions -------------------------------------------------------------

const MAX_ADMIN_SESSIONS = 256;

export async function createAdminSession(db: D1Like, session: AdminSession): Promise<CreateSessionOutcome> {
  await db.prepare(`DELETE FROM admin_sessions WHERE expires_at <= ?`).bind(session.createdAt).run();
  const countRow = await db.prepare(`SELECT count(*) AS n FROM admin_sessions`).first<{ n: number }>();
  if (Number(countRow?.n ?? 0) >= MAX_ADMIN_SESSIONS) {
    return { kind: "limit_reached" };
  }
  await db
    .prepare(
      `INSERT INTO admin_sessions (id, session_hash, authenticated, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
    )
    .bind(session.id, session.sessionHash, session.authenticated ? 1 : 0, session.createdAt, session.expiresAt)
    .run();
  return { kind: "created" };
}

export async function adminSessionByHash(db: D1Like, hash: string, now: string): Promise<AdminSession | null> {
  const row = await db
    .prepare(`SELECT id, session_hash, authenticated, created_at, expires_at FROM admin_sessions WHERE session_hash = ? AND expires_at > ?`)
    .bind(hash, now)
    .first<Record<string, unknown>>();
  if (!row) return null;
  return {
    id: String(row.id),
    sessionHash: String(row.session_hash),
    authenticated: toBool(row.authenticated),
    createdAt: String(row.created_at),
    expiresAt: String(row.expires_at),
  };
}

export async function authenticateAdminSession(db: D1Like, id: string, expiresAt: string): Promise<void> {
  await db.prepare(`UPDATE admin_sessions SET authenticated = 1, expires_at = ? WHERE id = ?`).bind(expiresAt, id).run();
}

export async function deleteAdminSession(db: D1Like, id: string): Promise<void> {
  await db.prepare(`DELETE FROM admin_sessions WHERE id = ?`).bind(id).run();
}
