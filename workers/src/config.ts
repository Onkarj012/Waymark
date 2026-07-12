// Ports NewConfigured()'s validation from internal/web/server.go. The Go
// server refuses to start on misconfiguration (log.Fatalf in cmd/server);
// Workers has no boot phase to fail during; instead each worker validates
// its config on every request and fails that request with 500 if invalid
// (see control.ts / content.ts). Config is cheap to validate, so this has no
// meaningful performance cost.

import { parseConfiguredOrigin } from "./origin";
import type { BaseEnv } from "./types";

export interface WaymarkConfig {
  adminPasscode: string;
  publicUrl: string;
  publicHost: string;
  controlUrl: string;
  controlHost: string;
  tokenTtlDays: number;
  secureCookie: boolean;
  trustForwardedIp: boolean;
}

export function loadConfig(env: BaseEnv): WaymarkConfig {
  const publicOrigin = parseConfiguredOrigin(env.PUBLIC_BASE_URL ?? "");
  if (!publicOrigin) throw new Error("PUBLIC_BASE_URL is required");
  const controlOrigin = parseConfiguredOrigin(env.CONTROL_BASE_URL ?? "");
  if (!controlOrigin) throw new Error("CONTROL_BASE_URL is required");
  if (!env.WAYMARK_ADMIN_PASSCODE) throw new Error("WAYMARK_ADMIN_PASSCODE is required");
  if (publicOrigin.url === controlOrigin.url) {
    throw new Error("PUBLIC_BASE_URL and CONTROL_BASE_URL must use different origins");
  }

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
    tokenTtlDays,
    secureCookie: controlOrigin.secure,
    trustForwardedIp: env.WAYMARK_TRUST_FORWARDED_IP === "true",
  };
}
