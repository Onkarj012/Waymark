// Ports NewConfigured()'s validation from internal/web/server.go. The Go
// server refuses to start on misconfiguration (log.Fatalf in cmd/server);
// Workers has no boot phase to fail during; instead each worker validates
// its config on every request and fails that request with 500 if invalid
// (see control.ts / content.ts). Config is cheap to validate, so this has no
// meaningful performance cost.
//
// The content worker never handles admin credentials (AGENTS.md invariant:
// "published HTML must never share an origin with admin sessions or API
// credentials"), so config loading is split in two: loadConfig() validates
// only what both workers share (the two origins) and is safe to call with no
// admin passcode configured at all; loadControlConfig() additionally
// requires WAYMARK_ADMIN_PASSCODE and the other control-only vars, and is
// the only loader control.ts should ever call.

import { parseConfiguredOrigin, type ParsedOrigin } from "./origin";
import type { BaseEnv, ControlEnv } from "./types";

export interface BaseWaymarkConfig {
  publicUrl: string;
  publicHost: string;
  controlUrl: string;
  legacyHosts: string[];
}

export interface WaymarkConfig extends BaseWaymarkConfig {
  adminPasscode: string;
  controlHost: string;
  tokenTtlDays: number;
  secureCookie: boolean;
  trustForwardedIp: boolean;
}

function parseOrigins(env: BaseEnv): { publicOrigin: ParsedOrigin; controlOrigin: ParsedOrigin } {
  const publicOrigin = parseConfiguredOrigin(env.PUBLIC_BASE_URL ?? "");
  if (!publicOrigin) throw new Error("PUBLIC_BASE_URL is required");
  const controlOrigin = parseConfiguredOrigin(env.CONTROL_BASE_URL ?? "");
  if (!controlOrigin) throw new Error("CONTROL_BASE_URL is required");
  if (publicOrigin.url === controlOrigin.url) {
    throw new Error("PUBLIC_BASE_URL and CONTROL_BASE_URL must use different origins");
  }
  return { publicOrigin, controlOrigin };
}

/** Parses WAYMARK_LEGACY_HOSTS: comma-separated bare hostnames that should
 * 308-redirect to the worker's canonical origin. Rejects entries that are
 * URLs rather than hosts, and entries that collide with either canonical
 * host (a self-redirect loop would take the worker down). */
function parseLegacyHosts(value: string | undefined, canonicalHosts: string[]): string[] {
  const canonical = canonicalHosts.map((h) => h.toLowerCase());
  const hosts: string[] = [];
  for (const entry of (value ?? "").split(",")) {
    const host = entry.trim().toLowerCase();
    if (host === "") continue;
    if (host.includes("/") || host.includes(" ")) {
      throw new Error("WAYMARK_LEGACY_HOSTS entries must be bare hostnames, not URLs");
    }
    if (canonical.includes(host)) {
      throw new Error("WAYMARK_LEGACY_HOSTS must not include a canonical host");
    }
    hosts.push(host);
  }
  return hosts;
}

/** Base config shared by both workers. Used by content.ts, which must never
 * require admin credentials to serve a request. */
export function loadConfig(env: BaseEnv): BaseWaymarkConfig {
  const { publicOrigin, controlOrigin } = parseOrigins(env);
  return {
    publicUrl: publicOrigin.url,
    publicHost: publicOrigin.host,
    controlUrl: controlOrigin.url,
    legacyHosts: parseLegacyHosts(env.WAYMARK_LEGACY_HOSTS, [publicOrigin.host, controlOrigin.host]),
  };
}

/** Full config for the control worker only. Requires WAYMARK_ADMIN_PASSCODE
 * (and validates the other control-only vars) in addition to everything
 * loadConfig() validates. */
export function loadControlConfig(env: ControlEnv): WaymarkConfig {
  const { publicOrigin, controlOrigin } = parseOrigins(env);
  if (!env.WAYMARK_ADMIN_PASSCODE) throw new Error("WAYMARK_ADMIN_PASSCODE is required");

  let tokenTtlDays = 90;
  if (env.WAYMARK_TOKEN_TTL_DAYS) {
    const parsed = Number(env.WAYMARK_TOKEN_TTL_DAYS);
    if (!Number.isInteger(parsed)) throw new Error("WAYMARK_TOKEN_TTL_DAYS must be an integer");
    tokenTtlDays = parsed;
  }
  if (tokenTtlDays < 1 || tokenTtlDays > 365) {
    throw new Error("WAYMARK_TOKEN_TTL_DAYS must be between 1 and 365");
  }

  return {
    adminPasscode: env.WAYMARK_ADMIN_PASSCODE,
    publicUrl: publicOrigin.url,
    publicHost: publicOrigin.host,
    controlUrl: controlOrigin.url,
    controlHost: controlOrigin.host,
    legacyHosts: parseLegacyHosts(env.WAYMARK_LEGACY_HOSTS, [publicOrigin.host, controlOrigin.host]),
    tokenTtlDays,
    secureCookie: controlOrigin.secure,
    trustForwardedIp: env.WAYMARK_TRUST_FORWARDED_IP === "true",
  };
}
