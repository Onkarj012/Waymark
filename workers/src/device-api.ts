// Ports the device-authorization JSON handlers from internal/web/auth.go:
// handleDiscovery, handleDeviceCode, handleDeviceToken, handleSelfRevoke, and
// handleAuthCheck. The admin-facing side of the same flow (activate/decide)
// lives in admin.ts, matching the Go split between auth.go and admin.go.

import { decodeStrict } from "./body";
import {
  base64UrlByteLength,
  hashHighEntropy,
  hashLowEntropy,
  newApiToken,
  normalizeUserCode,
  randomBase64Url,
  randomUserCode,
} from "./crypto";
import { errJson, json } from "./http";
import { rateLimitAllow } from "./ratelimit";
import { requestSource, validateScopes } from "./auth";
import {
  createDeviceAuthorization,
  deviceAuthorizationByDeviceCode,
  nowIso,
  pollDeviceAuthorization,
  revokeApiToken,
} from "./store";
import type { APIToken, Credential, D1Like, DeviceAuthorization } from "./types";

export const DEVICE_GRANT_LIFETIME_MS = 10 * 60 * 1000;
export const INITIAL_POLL_INTERVAL = 5;

export function handleDiscovery(controlUrl: string, publicUrl: string): Response {
  return json(200, { control_url: controlUrl, content_url: publicUrl, device_authorization: true });
}

export function handleAuthCheck(credential: Credential): Response {
  return json(200, {
    ok: true,
    credential_type: credential.kind,
    scopes: credential.scopes,
    label: credential.label,
    expires_at: credential.expiresAt,
  });
}

export async function handleDeviceCode(
  db: D1Like,
  request: Request,
  adminPasscode: string,
  controlUrl: string,
  trustForwardedIp: boolean,
): Promise<Response> {
  const now = nowIso();
  const { sourceKey, sourceHint } = await requestSource(request, adminPasscode, trustForwardedIp);
  if (!(await rateLimitAllow(db, `create:${sourceKey}`, 5, 10 * 60, now))) {
    return errJson(429, "rate_limited");
  }

  const text = await request.text();
  const decoded = decodeStrict<{ device_secret?: unknown; device_label?: unknown; scopes?: unknown }>(text, [
    "device_secret",
    "device_label",
    "scopes",
  ]);
  if (!decoded.ok) return errJson(400, decoded.message);
  const data = decoded.value;

  const deviceSecret = typeof data.device_secret === "string" ? data.device_secret : "";
  if (base64UrlByteLength(deviceSecret) !== 32) {
    return errJson(400, "device_secret must be 32 random bytes encoded as base64url");
  }
  const label = typeof data.device_label === "string" ? data.device_label.trim() : "";
  if (!label || label.length > 120) {
    return errJson(400, "device_label must be between 1 and 120 characters");
  }
  const scopesInput = Array.isArray(data.scopes) ? data.scopes.filter((s): s is string => typeof s === "string") : undefined;
  const scopes = validateScopes(scopesInput);
  if (!scopes) return errJson(400, "scopes must contain pages:read and optionally pages:write");

  const deviceCode = randomBase64Url(32);
  const userCode = randomUserCode();
  const id = randomBase64Url(12);
  const expiresAt = new Date(Date.now() + DEVICE_GRANT_LIFETIME_MS).toISOString();

  const grant: DeviceAuthorization = {
    id,
    deviceCodeHash: await hashHighEntropy(deviceCode),
    deviceSecretHash: await hashHighEntropy(deviceSecret),
    userCodeHash: await hashLowEntropy(adminPasscode, normalizeUserCode(userCode)),
    deviceLabel: label,
    scopes: scopes.join(" "),
    sourceKey,
    sourceHint,
    status: "pending",
    createdAt: now,
    expiresAt,
    approvedAt: null,
    deniedAt: null,
    lastPollAt: null,
    pollIntervalSeconds: INITIAL_POLL_INTERVAL,
    consumedAt: null,
  };

  const outcome = await createDeviceAuthorization(db, grant, 5, 50);
  if (outcome.kind === "limit_reached") return errJson(429, "too_many_pending_authorizations");

  const verification = `${controlUrl}/activate`;
  return json(201, {
    device_code: deviceCode,
    user_code: userCode,
    verification_uri: verification,
    verification_uri_complete: `${verification}?code=${userCode}`,
    expires_in: DEVICE_GRANT_LIFETIME_MS / 1000,
    interval: INITIAL_POLL_INTERVAL,
  });
}

function deviceErr(status: number, code: string, retryAfter?: number): Response {
  const headers: HeadersInit = retryAfter && retryAfter > 0 ? { "Retry-After": String(retryAfter) } : {};
  return errJson(status, code, headers);
}

export async function handleDeviceToken(
  db: D1Like,
  request: Request,
  adminPasscode: string,
  tokenTtlDays: number,
  trustForwardedIp: boolean,
): Promise<Response> {
  const now = nowIso();
  const { sourceKey } = await requestSource(request, adminPasscode, trustForwardedIp);
  if (!(await rateLimitAllow(db, `poll:${sourceKey}`, 120, 10 * 60, now))) {
    return errJson(429, "rate_limited");
  }

  const text = await request.text();
  const decoded = decodeStrict<{ device_code?: unknown; device_secret?: unknown }>(text, [
    "device_code",
    "device_secret",
  ]);
  if (!decoded.ok) return errJson(400, decoded.message);
  const deviceCode = typeof decoded.value.device_code === "string" ? decoded.value.device_code : "";
  const deviceSecret = typeof decoded.value.device_secret === "string" ? decoded.value.device_secret : "";

  const { id: tokenId, displayPrefix, raw: rawToken } = newApiToken();
  const codeHash = await hashHighEntropy(deviceCode);
  const secretHash = await hashHighEntropy(deviceSecret);

  const lookup = await deviceAuthorizationByDeviceCode(db, codeHash, secretHash, now);
  if (lookup.kind !== "found") {
    return deviceErr(400, "expired_token");
  }
  const grant = lookup.grant;

  const token: APIToken = {
    id: tokenId,
    tokenHash: await hashHighEntropy(rawToken),
    displayPrefix,
    deviceLabel: grant.deviceLabel,
    scopes: grant.scopes,
    createdAt: now,
    expiresAt: new Date(Date.now() + tokenTtlDays * 24 * 60 * 60 * 1000).toISOString(),
    lastUsedAt: null,
    revokedAt: null,
  };

  const outcome = await pollDeviceAuthorization(db, codeHash, secretHash, now, token);
  switch (outcome.kind) {
    case "issued":
      return json(200, {
        access_token: rawToken,
        token_type: "Bearer",
        expires_in: tokenTtlDays * 86400,
        scope: grant.scopes,
      });
    case "pending":
      return deviceErr(400, "authorization_pending", outcome.interval);
    case "slow_down":
      return deviceErr(400, "slow_down", outcome.interval);
    case "denied":
      return deviceErr(400, "access_denied");
    case "consumed":
      // A consumed grant is terminal; replay must never reveal or mint a token.
      return deviceErr(400, "expired_token");
    case "expired":
    case "not_found":
      return deviceErr(400, "expired_token");
    default:
      return errJson(500, "could not complete device authorization");
  }
}

export async function handleSelfRevoke(db: D1Like, credential: Credential): Promise<Response> {
  if (credential.kind !== "device_token") {
    return errJson(400, "only device tokens can revoke themselves");
  }
  try {
    await revokeApiToken(db, credential.tokenId, nowIso());
  } catch {
    return errJson(500, "could not revoke token");
  }
  return json(200, { revoked: true });
}
