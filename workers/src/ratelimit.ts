// D1-backed fixed-window rate limiter. Ports the semantics of the in-memory
// rateLimiter in internal/web/auth.go: a bucket resets once the window has
// fully elapsed since it started, otherwise requests are counted against
// `limit` for that window.
//
// Difference from the Go implementation: Go additionally caps total tracked
// keys at 4096 and evicts stale buckets on overflow, because its limiter
// lives in one process's memory. D1 has no such memory-pressure concern —
// instead, `pruneRateLimits` (called from the scheduled handler alongside the
// expiry sweep) deletes buckets whose window closed more than a day ago, so
// the table doesn't grow without bound.

import type { D1Like } from "./types";

export async function rateLimitAllow(
  db: D1Like,
  key: string,
  limit: number,
  windowSeconds: number,
  nowIso: string,
): Promise<boolean> {
  const nowMs = Date.parse(nowIso);
  const row = await db
    .prepare(`SELECT window_start, count FROM rate_limits WHERE key = ?`)
    .bind(key)
    .first<{ window_start: string; count: number }>();

  if (!row || nowMs - Date.parse(row.window_start) >= windowSeconds * 1000) {
    await db
      .prepare(
        `INSERT INTO rate_limits (key, window_start, count) VALUES (?, ?, 1)
         ON CONFLICT(key) DO UPDATE SET window_start = excluded.window_start, count = 1`,
      )
      .bind(key, nowIso)
      .run();
    return true;
  }

  if (Number(row.count) >= limit) return false;

  await db.prepare(`UPDATE rate_limits SET count = count + 1 WHERE key = ?`).bind(key).run();
  return true;
}

/** Deletes rate-limit buckets whose window closed before `cutoffIso`. Call
 * periodically (e.g. from the control worker's scheduled handler) to bound
 * table growth — see the file-level note above. */
export async function pruneRateLimits(db: D1Like, cutoffIso: string): Promise<number> {
  const result = await db.prepare(`DELETE FROM rate_limits WHERE window_start <= ?`).bind(cutoffIso).run();
  return result.meta.changes ?? 0;
}
