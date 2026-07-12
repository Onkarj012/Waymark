// Ports the credential/scope/device-authorization helpers from
// internal/web/auth.go that are not pure store access (see store.ts for the
// D1-backed persistence half).

import { hashHighEntropy, hashLowEntropy, normalizeUserCode as normalizeCode } from "./crypto";
import type { Credential, D1Like } from "./types";
import { apiTokenByHash, touchApiToken } from "./store";

export const DEVICE_GRANT_LIFETIME_SECONDS = 10 * 60;
export const INITIAL_POLL_INTERVAL = 5;

export function bearerToken(request: Request): string {
  const header = request.headers.get("Authorization") ?? "";
  if (header.startsWith("Bearer ")) return header.slice("Bearer ".length).trim();
  return "";
}

export function splitScopes(value: string): string[] {
  return value.split(/\s+/).filter(Boolean);
}

export function hasScope(scopes: string[], want: string): boolean {
  return scopes.includes(want);
}

/** Mirrors validateScopes() in auth.go: an empty/absent request defaults to
 * full access; otherwise every scope must be one of the two known scopes and
 * pages:read must be present. Returns null when invalid. */
export function validateScopes(input: string[] | undefined | null): string[] | null {
  if (!input || input.length === 0) return ["pages:read", "pages:write"];
  const seen = new Set<string>();
  for (const scope of input) {
    if (scope !== "pages:read" && scope !== "pages:write") return null;
    seen.add(scope);
  }
  if (!seen.has("pages:read")) return null;
  const out = ["pages:read"];
  if (seen.has("pages:write")) out.push("pages:write");
  return out;
}

export const normalizeUserCode = normalizeCode;

/** Verifies the bearer token on the request and returns its credential, or
 * null if missing/invalid/expired/revoked. Also opportunistically touches
 * last_used_at (throttled to once per 5 minutes, like the Go server). */
export async function authenticate(db: D1Like, request: Request, now: string): Promise<Credential | null> {
  const token = bearerToken(request);
  if (!token) return null;
  const hash = await hashHighEntropy(token);
  const stored = await apiTokenByHash(db, hash, now);
  if (!stored) return null;
  const cutoff = new Date(new Date(now).getTime() - 5 * 60 * 1000).toISOString();
  if (!stored.lastUsedAt || stored.lastUsedAt <= cutoff) {
    await touchApiToken(db, stored.id, now);
  }
  return {
    kind: "device_token",
    tokenId: stored.id,
    label: stored.deviceLabel,
    scopes: splitScopes(stored.scopes),
    expiresAt: stored.expiresAt,
  };
}

export interface RequestSource {
  sourceKey: string;
  sourceHint: string;
}

/** Mirrors requestSource() in auth.go: identifies the caller for abuse-limit
 * bucketing, hashed (keyed by the admin passcode) so raw IPs never land in
 * D1. On Cloudflare, CF-Connecting-IP is the edge-verified client IP (the
 * equivalent of Go's raw TCP RemoteAddr); X-Real-IP is honored only when
 * WAYMARK_TRUST_FORWARDED_IP is set, for deployments that sit behind another
 * proxy in front of Cloudflare. */
export async function requestSource(
  request: Request,
  adminPasscode: string,
  trustForwardedIp: boolean,
): Promise<RequestSource> {
  let host = "";
  if (trustForwardedIp) {
    const forwarded = (request.headers.get("X-Real-IP") ?? "").trim();
    if (isValidIp(forwarded)) host = forwarded;
  }
  if (!host) {
    host = (request.headers.get("CF-Connecting-IP") ?? "").trim();
  }
  if (!host) host = "unknown";

  let hint = host;
  if (host !== "unknown") {
    const v4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
    if (v4 && v4[1] !== "127") {
      hint = `${v4[1]}.${v4[2]}.${v4[3]}.x`;
    } else if (host.includes(":") && host !== "::1") {
      hint = "IPv6 address";
    }
  }
  return { sourceKey: await hashLowEntropy(adminPasscode, host), sourceHint: hint };
}

function isValidIp(value: string): boolean {
  if (!value) return false;
  const v4 = /^(\d{1,3}\.){3}\d{1,3}$/;
  if (v4.test(value)) return value.split(".").every((part) => Number(part) <= 255);
  // Loose IPv6 check — good enough to gate on "looks like an IP" the same way
  // Go's net.ParseIP does for this trusted-header path.
  return /^[0-9a-fA-F:]+$/.test(value) && value.includes(":");
}
