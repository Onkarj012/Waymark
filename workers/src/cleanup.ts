// Ports sweepExpired() from cmd/server/main.go: periodically deletes expired
// pages, expired device/token/session rows, and (Workers-only) stale
// rate-limit buckets. Wired to the control worker's `scheduled` handler
// (Cloudflare Cron Triggers) instead of a goroutine ticker.

import { deleteExpiredAuth, deleteExpiredPages, nowIso } from "./store";
import { pruneRateLimits } from "./ratelimit";
import type { D1Like } from "./types";

const RATE_LIMIT_RETENTION_MS = 24 * 60 * 60 * 1000;

export interface CleanupResult {
  pagesDeleted: number;
  authRowsDeleted: number;
  rateLimitRowsDeleted: number;
}

export async function runCleanup(db: D1Like): Promise<CleanupResult> {
  const now = nowIso();
  const cutoff = new Date(Date.now() - RATE_LIMIT_RETENTION_MS).toISOString();
  const [pagesDeleted, authRowsDeleted, rateLimitRowsDeleted] = await Promise.all([
    deleteExpiredPages(db, now),
    deleteExpiredAuth(db, now),
    pruneRateLimits(db, cutoff),
  ]);
  return { pagesDeleted, authRowsDeleted, rateLimitRowsDeleted };
}
