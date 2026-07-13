// Shared types for both the control and content Workers.

/** Minimal D1-compatible surface our store/ratelimit layers depend on. Kept
 * narrow so a lightweight fake (see test/fake-d1.ts) can stand in for tests
 * without needing workerd/miniflare. */
export interface D1Like {
  prepare(query: string): D1PreparedLike;
  batch<T = unknown>(statements: D1PreparedLike[]): Promise<D1ResultLike<T>[]>;
}

export interface D1PreparedLike {
  bind(...values: unknown[]): D1PreparedLike;
  first<T = Record<string, unknown>>(colName?: string): Promise<T | null>;
  run<T = unknown>(): Promise<D1ResultLike<T>>;
  all<T = unknown>(): Promise<D1ResultLike<T>>;
}

export interface D1ResultLike<T = unknown> {
  results?: T[];
  success: boolean;
  meta: { changes?: number; last_row_id?: number; [key: string]: unknown };
}

/** Bindings/environment shared by both workers. The content worker never
 * handles admin credentials (AGENTS.md invariant), so this interface only
 * carries what both workers actually need. */
export interface BaseEnv {
  DB: D1Like;
  PUBLIC_BASE_URL: string;
  CONTROL_BASE_URL: string;
  /** Comma-separated hostnames this worker previously lived on (e.g. its
   * *.workers.dev host after moving to a custom domain). Requests arriving on
   * a legacy host are 308-redirected to the canonical origin so old shared
   * links keep working. */
  WAYMARK_LEGACY_HOSTS?: string;
}

/** Bindings/environment for the control worker only — adds the admin
 * passcode (required) and the auth-tuning vars that only control-side code
 * (device-api.ts, admin.ts) reads. */
export interface ControlEnv extends BaseEnv {
  WAYMARK_ADMIN_PASSCODE: string;
  WAYMARK_TOKEN_TTL_DAYS?: string;
  WAYMARK_TRUST_FORWARDED_IP?: string;
}

export interface Page {
  id: string;
  title: string;
  slug: string;
  html: string;
  raw: boolean;
  createdAt: string; // RFC3339Nano, UTC
  updatedAt: string;
  expiresAt: string | null;
}

export interface PageMeta {
  id: string;
  title: string;
  slug: string;
  raw: boolean;
  createdAt: string;
  updatedAt: string;
  expiresAt: string | null;
  size: number;
}

export interface DeviceAuthorization {
  id: string;
  deviceCodeHash: string;
  deviceSecretHash: string;
  userCodeHash: string;
  deviceLabel: string;
  scopes: string;
  sourceKey: string;
  sourceHint: string;
  status: "pending" | "approved" | "denied" | "consumed";
  createdAt: string;
  expiresAt: string;
  approvedAt: string | null;
  deniedAt: string | null;
  lastPollAt: string | null;
  pollIntervalSeconds: number;
  consumedAt: string | null;
}

export interface APIToken {
  id: string;
  tokenHash: string;
  displayPrefix: string;
  deviceLabel: string;
  scopes: string;
  createdAt: string;
  expiresAt: string;
  lastUsedAt: string | null;
  revokedAt: string | null;
}

export interface AdminSession {
  id: string;
  sessionHash: string;
  authenticated: boolean;
  createdAt: string;
  expiresAt: string;
}

export class NotFoundError extends Error {
  constructor(message = "not found") {
    super(message);
    this.name = "NotFoundError";
  }
}

/** Discriminated result of looking up a device authorization grant by user
 * code or device code — mirrors the Go store's ErrGrantNotFound/ErrGrantExpired
 * sentinel errors. */
export type GrantLookupOutcome =
  | { kind: "found"; grant: DeviceAuthorization }
  | { kind: "not_found" }
  | { kind: "expired" };

/** Discriminated result of polling a device authorization grant — mirrors the
 * Go store's (interval int, err error) return without abusing exceptions for
 * expected control flow. */
export type PollOutcome =
  | { kind: "issued" }
  | { kind: "pending"; interval: number }
  | { kind: "slow_down"; interval: number }
  | { kind: "denied" }
  | { kind: "consumed" }
  | { kind: "expired" }
  | { kind: "not_found" };

export type CreateGrantOutcome = { kind: "created" } | { kind: "limit_reached" };

export type DecideOutcome = { kind: "decided" } | { kind: "not_found" };

export type CreateSessionOutcome = { kind: "created" } | { kind: "limit_reached" };

export interface Credential {
  kind: "device_token";
  tokenId: string;
  label: string;
  scopes: string[];
  expiresAt: string;
}
