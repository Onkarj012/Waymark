// Ports the owner-facing admin UI from internal/web/admin.go: sign-in,
// device activation/approval, and the token list. Templates are manual
// string interpolation (Workers has no html/template) — every dynamic value
// that can contain publisher/attacker-controlled text (device labels, the
// echoed activation code, form fields) is passed through escapeHtml(), the
// equivalent of html/template's automatic contextual escaping.

import {
  constantTimeHexEqual,
  constantTimeSecretEqual,
  hashLowEntropy,
  normalizeUserCode,
  randomBase64Url,
} from "./crypto";
import { escapeHtml } from "./render";
import { getCookie, serializeCookie } from "./cookies";
import { formatAdminDate, formatAdminDateTime } from "./dates";
import { requestSource } from "./auth";
import { rateLimitAllow } from "./ratelimit";
import {
  adminSessionByHash,
  authenticateAdminSession,
  createAdminSession,
  deleteAdminSession,
  deviceAuthorizationByUserCode,
  decideDeviceAuthorization,
  listApiTokens,
  nowIso,
  revokeApiToken,
} from "./store";
import { NotFoundError, type AdminSession, type APIToken, type D1Like, type DeviceAuthorization } from "./types";

export const PRE_AUTH_SESSION_LIFETIME_SECONDS = 15 * 60;
export const ADMIN_SESSION_LIFETIME_SECONDS = 30 * 24 * 60 * 60;

export interface AdminCtx {
  db: D1Like;
  adminPasscode: string;
  controlUrl: string;
  secureCookie: boolean;
  trustForwardedIp: boolean;
}

// --- headers / cookies -------------------------------------------------------

export function controlSecurityHeaders(): Record<string, string> {
  return {
    "Cache-Control": "no-store",
    // Chrome serializes same-origin form submissions with Origin: null under
    // no-referrer, which makes exact-origin CSRF enforcement reject valid forms.
    "Referrer-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "Content-Security-Policy":
      "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
  };
}

function adminCookieName(secureCookie: boolean): string {
  return secureCookie ? "__Host-waymark_admin" : "waymark_admin";
}

function setAdminCookieHeader(ctx: AdminCtx, raw: string, lifetimeSeconds: number): string {
  return serializeCookie(adminCookieName(ctx.secureCookie), raw, {
    path: "/",
    httpOnly: true,
    secure: ctx.secureCookie,
    sameSite: "Strict",
    maxAgeSeconds: lifetimeSeconds,
  });
}

function clearAdminCookieHeader(ctx: AdminCtx): string {
  return serializeCookie(adminCookieName(ctx.secureCookie), "", {
    path: "/",
    httpOnly: true,
    secure: ctx.secureCookie,
    sameSite: "Strict",
    maxAgeSeconds: 0,
  });
}

async function csrfToken(ctx: AdminCtx, raw: string): Promise<string> {
  return hashLowEntropy(ctx.adminPasscode, `csrf:${raw}`);
}

async function validCsrf(ctx: AdminCtx, raw: string, supplied: string): Promise<boolean> {
  return constantTimeHexEqual(await csrfToken(ctx, raw), supplied);
}

function validOrigin(ctx: AdminCtx, request: Request): boolean {
  return request.headers.get("Origin") === ctx.controlUrl;
}

// --- session lookup -----------------------------------------------------------

interface SessionLookup {
  session: AdminSession;
  raw: string;
}

async function currentAdminSession(ctx: AdminCtx, request: Request, now: string): Promise<SessionLookup | null> {
  const raw = getCookie(request, adminCookieName(ctx.secureCookie));
  if (!raw) return null;
  const session = await adminSessionByHash(ctx.db, await hashLowEntropy(ctx.adminPasscode, raw), now);
  return session ? { session, raw } : null;
}

/** Returns the existing valid session, or creates a fresh pre-auth one and
 * reports the Set-Cookie header that must be attached to the response.
 * Mirrors ensureAdminSession() in admin.go. */
async function ensureAdminSession(
  ctx: AdminCtx,
  request: Request,
  now: string,
): Promise<{ raw: string; setCookie?: string } | { limitReached: true }> {
  const existing = await currentAdminSession(ctx, request, now);
  if (existing) return { raw: existing.raw };

  const raw = randomBase64Url(32);
  const id = randomBase64Url(12);
  const session: AdminSession = {
    id,
    sessionHash: await hashLowEntropy(ctx.adminPasscode, raw),
    authenticated: false,
    createdAt: now,
    expiresAt: new Date(new Date(now).getTime() + PRE_AUTH_SESSION_LIFETIME_SECONDS * 1000).toISOString(),
  };
  const outcome = await createAdminSession(ctx.db, session);
  if (outcome.kind === "limit_reached") return { limitReached: true };
  return { raw, setCookie: setAdminCookieHeader(ctx, raw, PRE_AUTH_SESSION_LIFETIME_SECONDS) };
}

/** Session + origin + CSRF validation for state-changing admin form posts.
 * Mirrors validatedAdminSession() in admin.go. */
async function validatedAdminSession(
  ctx: AdminCtx,
  request: Request,
  form: URLSearchParams,
  now: string,
): Promise<AdminSession | null> {
  const lookup = await currentAdminSession(ctx, request, now);
  if (!lookup) return null;
  if (!validOrigin(ctx, request)) return null;
  if (!(await validCsrf(ctx, lookup.raw, form.get("csrf") ?? ""))) return null;
  return lookup.session;
}

// --- form parsing ---------------------------------------------------------------

const MAX_FORM_BYTES = 64 << 10;

export async function parseAdminForm(request: Request): Promise<URLSearchParams | null> {
  const buf = await request.arrayBuffer();
  if (buf.byteLength > MAX_FORM_BYTES) return null;
  try {
    return new URLSearchParams(new TextDecoder().decode(buf));
  } catch {
    return null;
  }
}

export function sanitizeNext(value: string): string {
  if (!value || !value.startsWith("/") || value.startsWith("//") || value.includes("\\")) return "/activate";
  try {
    const u = new URL(value, "http://placeholder.invalid");
    if (u.origin !== "http://placeholder.invalid") return "/activate";
    // Go's url.RequestURI() (used by sanitizeNext in admin.go) omits the
    // fragment, so match that here.
    return u.pathname + u.search;
  } catch {
    return "/activate";
  }
}

// --- templates ------------------------------------------------------------------

function pageShell(title: string, content: string): string {
  return (
    `<!doctype html>\n` +
    `<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">\n` +
    `<title>${escapeHtml(title)} - Waymark</title><link rel="stylesheet" href="/admin/style.css"></head>\n` +
    `<body><main><header><a href="/admin/tokens">Waymark</a><span>Control</span></header>${content}</main></body></html>`
  );
}

export const ADMIN_CSS =
  `:root{color-scheme:light dark;font:16px/1.5 system-ui,sans-serif}body{margin:0;background:#f5f6f7;color:#17191c}main{max-width:42rem;margin:3rem auto;padding:0 1rem}header{display:flex;justify-content:space-between;margin-bottom:2rem}section{background:#fff;border:1px solid #dfe2e5;border-radius:6px;padding:1.5rem;margin-bottom:1rem}label{display:block;font-weight:600;margin:.75rem 0 .25rem}input{box-sizing:border-box;width:100%;padding:.65rem;border:1px solid #a9afb5;border-radius:4px}button{padding:.65rem 1rem;border:0;border-radius:4px;background:#1769aa;color:#fff;font-weight:600;cursor:pointer}.danger{background:#a62b2b}.actions{display:flex;gap:.75rem;margin-top:1rem}.muted{color:#687078;font-size:.9rem}code{font-family:ui-monospace,monospace}@media(prefers-color-scheme:dark){body{background:#111315;color:#e8eaed}section{background:#191c1f;border-color:#34393e}input{background:#111315;color:#fff;border-color:#596169}a{color:#77bdf2}.muted{color:#aab0b6}}`;

function loginContent(next: string, csrf: string, error?: string): string {
  return (
    `<section><h1>Owner sign in</h1>` +
    (error ? `<p role="alert">${escapeHtml(error)}</p>` : "") +
    `<form method="post" action="/admin/login">` +
    `<input type="hidden" name="csrf" value="${escapeHtml(csrf)}">` +
    `<input type="hidden" name="next" value="${escapeHtml(next)}">` +
    `<label for="passcode">Admin passcode</label>` +
    `<input id="passcode" name="passcode" type="password" required autofocus autocomplete="current-password">` +
    `<div class="actions"><button type="submit">Sign in</button></div></form></section>`
  );
}

function activateContent(opts: { code: string; csrf: string; error?: string; grant?: DeviceAuthorization }): string {
  const header =
    `<h1>Activate a device</h1>` + (opts.error ? `<p>${escapeHtml(opts.error)}</p>` : "");
  if (opts.grant) {
    const g = opts.grant;
    return (
      `<section>${header}` +
      `<p><strong>${escapeHtml(g.deviceLabel)}</strong></p>` +
      `<p class="muted">Scopes: <code>${escapeHtml(g.scopes)}</code><br>` +
      `Requested ${formatAdminDateTime(g.createdAt)}<br>` +
      `Source: ${escapeHtml(g.sourceHint)}</p>` +
      `<form method="post" action="/activate">` +
      `<input type="hidden" name="csrf" value="${escapeHtml(opts.csrf)}">` +
      `<input type="hidden" name="code" value="${escapeHtml(opts.code)}">` +
      `<div class="actions">` +
      `<button name="decision" value="approved" type="submit">Approve</button>` +
      `<button class="danger" name="decision" value="denied" type="submit">Deny</button>` +
      `</div></form></section>`
    );
  }
  return (
    `<section>${header}` +
    `<form method="get" action="/activate">` +
    `<label for="code">Device code</label>` +
    `<input id="code" name="code" value="${escapeHtml(opts.code)}" placeholder="ABCD-EFGH" required autofocus>` +
    `<div class="actions"><button type="submit">Continue</button></div></form></section>`
  );
}

function decisionContent(decision: string): string {
  return `<section><h1>Device ${escapeHtml(decision)}</h1><p>You can return to the terminal.</p></section>`;
}

function tokensContent(tokens: APIToken[], csrf: string): string {
  const items = tokens.length
    ? tokens
        .map((t) => {
          const revoked = t.revokedAt
            ? ` - revoked`
            : "";
          const form = t.revokedAt
            ? ""
            : `<form method="post" action="/admin/tokens/${escapeHtml(t.id)}/revoke">` +
              `<input type="hidden" name="csrf" value="${escapeHtml(csrf)}">` +
              `<button class="danger" type="submit">Revoke</button></form>`;
          return (
            `<article><p><strong>${escapeHtml(t.deviceLabel)}</strong> <code>${escapeHtml(t.displayPrefix)}</code></p>` +
            `<p class="muted">${escapeHtml(t.scopes)}<br>Expires ${formatAdminDate(t.expiresAt)}${revoked}</p>${form}</article>`
          );
        })
        .join("")
    : `<p>No device tokens have been issued.</p>`;
  return (
    `<section><h1>Device tokens</h1>${items}</section>` +
    `<form method="post" action="/admin/logout"><input type="hidden" name="csrf" value="${escapeHtml(csrf)}">` +
    `<button type="submit">Sign out</button></form>`
  );
}

function htmlResponse(status: number, body: string, headers?: HeadersInit): Response {
  return new Response(body, {
    status,
    headers: { "Content-Type": "text/html; charset=utf-8", ...headers },
  });
}

// --- handlers ---------------------------------------------------------------

export async function handleAdminCSS(): Promise<Response> {
  return new Response(ADMIN_CSS, {
    headers: { ...controlSecurityHeaders(), "Content-Type": "text/css; charset=utf-8", "Cache-Control": "no-store" },
  });
}

export async function handleAdminLogin(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const url = new URL(request.url);
  const next = sanitizeNext(url.searchParams.get("next") ?? "");

  const existing = await currentAdminSession(ctx, request, now);
  if (!existing) {
    const { sourceKey } = await requestSource(request, ctx.adminPasscode, ctx.trustForwardedIp);
    if (!(await rateLimitAllow(ctx.db, `session:${sourceKey}`, 10, 15 * 60, now))) {
      return new Response("too many session requests", { status: 429, headers: controlSecurityHeaders() });
    }
  }

  const ensured = await ensureAdminSession(ctx, request, now);
  if ("limitReached" in ensured) {
    return new Response("too many active sessions", { status: 429, headers: controlSecurityHeaders() });
  }
  const csrf = await csrfToken(ctx, ensured.raw);
  const headers: Record<string, string> = { ...controlSecurityHeaders() };
  if (ensured.setCookie) headers["Set-Cookie"] = ensured.setCookie;
  return htmlResponse(200, pageShell("Sign in", loginContent(next, csrf)), headers);
}

export async function handleAdminLoginPost(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const form = await parseAdminForm(request);
  if (!form) return new Response("invalid form", { status: 400, headers: controlSecurityHeaders() });
  if (!validOrigin(ctx, request)) {
    return new Response("invalid origin", { status: 403, headers: controlSecurityHeaders() });
  }
  const lookup = await currentAdminSession(ctx, request, now);
  if (!lookup || !(await validCsrf(ctx, lookup.raw, form.get("csrf") ?? ""))) {
    return new Response("invalid session", { status: 403, headers: controlSecurityHeaders() });
  }
  const { sourceKey } = await requestSource(request, ctx.adminPasscode, ctx.trustForwardedIp);
  if (!(await rateLimitAllow(ctx.db, `login:${sourceKey}`, 5, 15 * 60, now))) {
    return new Response("too many attempts", { status: 429, headers: controlSecurityHeaders() });
  }
  const next = sanitizeNext(form.get("next") ?? "");
  if (!(await constantTimeSecretEqual(form.get("passcode") ?? "", ctx.adminPasscode))) {
    const csrf = await csrfToken(ctx, lookup.raw);
    return htmlResponse(
      401,
      pageShell("Sign in", loginContent(next, csrf, "Incorrect admin passcode. Try again.")),
      controlSecurityHeaders(),
    );
  }
  await authenticateAdminSession(
    ctx.db,
    lookup.session.id,
    new Date(new Date(now).getTime() + ADMIN_SESSION_LIFETIME_SECONDS * 1000).toISOString(),
  );
  const headers: Record<string, string> = {
    ...controlSecurityHeaders(),
    Location: next,
    "Set-Cookie": setAdminCookieHeader(ctx, lookup.raw, ADMIN_SESSION_LIFETIME_SECONDS),
  };
  return new Response(null, { status: 303, headers });
}

/** Wraps a handler so it is only reachable with an authenticated admin
 * session, redirecting to /admin/login otherwise — mirrors requireAdmin() in
 * admin.go. */
export function requireAdmin(
  ctx: AdminCtx,
  request: Request,
  handler: () => Promise<Response>,
): Promise<Response> | Response {
  return (async () => {
    const now = nowIso();
    const lookup = await currentAdminSession(ctx, request, now);
    if (!lookup || !lookup.session.authenticated) {
      const url = new URL(request.url);
      const nextPath = sanitizeNext(url.pathname + url.search);
      const headers: Record<string, string> = {
        ...controlSecurityHeaders(),
        Location: `/admin/login?next=${encodeURIComponent(nextPath)}`,
      };
      return new Response(null, { status: 303, headers });
    }
    return handler();
  })();
}

export async function handleActivate(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const lookup = await currentAdminSession(ctx, request, now);
  const raw = lookup?.raw ?? "";
  const url = new URL(request.url);
  const code = (url.searchParams.get("code") ?? "").trim().toUpperCase();
  const csrf = await csrfToken(ctx, raw);

  let error: string | undefined;
  let grant: DeviceAuthorization | undefined;
  if (code !== "") {
    const { sourceKey } = await requestSource(request, ctx.adminPasscode, ctx.trustForwardedIp);
    if (!(await rateLimitAllow(ctx.db, `lookup:${sourceKey}`, 20, 10 * 60, now))) {
      error = "Too many code lookups. Try again later.";
    } else {
      const hash = await hashLowEntropy(ctx.adminPasscode, normalizeUserCode(code));
      const outcome = await deviceAuthorizationByUserCode(ctx.db, hash, now);
      if (outcome.kind !== "found" || outcome.grant.status !== "pending") {
        error = "That code is invalid or expired.";
      } else {
        grant = outcome.grant;
      }
    }
  }
  return htmlResponse(
    200,
    pageShell("Activate device", activateContent({ code, csrf, error, grant })),
    controlSecurityHeaders(),
  );
}

export async function handleActivateDecision(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const form = await parseAdminForm(request);
  if (!form) return new Response("invalid form", { status: 400, headers: controlSecurityHeaders() });
  if (!validOrigin(ctx, request)) {
    return new Response("invalid origin", { status: 403, headers: controlSecurityHeaders() });
  }
  const lookup = await currentAdminSession(ctx, request, now);
  if (!lookup || !(await validCsrf(ctx, lookup.raw, form.get("csrf") ?? ""))) {
    return new Response("invalid csrf token", { status: 403, headers: controlSecurityHeaders() });
  }
  const decision = form.get("decision") ?? "";
  if (decision !== "approved" && decision !== "denied") {
    return new Response("invalid decision", { status: 400, headers: controlSecurityHeaders() });
  }
  const code = (form.get("code") ?? "").trim().toUpperCase();
  const hash = await hashLowEntropy(ctx.adminPasscode, normalizeUserCode(code));
  const outcome = await decideDeviceAuthorization(ctx.db, hash, decision, now);
  if (outcome.kind !== "decided") {
    return new Response("code is invalid or expired", { status: 400, headers: controlSecurityHeaders() });
  }
  return htmlResponse(200, pageShell(`Device ${decision}`, decisionContent(decision)), controlSecurityHeaders());
}

export async function handleAdminTokens(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const lookup = await currentAdminSession(ctx, request, now);
  const csrf = await csrfToken(ctx, lookup?.raw ?? "");
  const tokens = await listApiTokens(ctx.db);
  return htmlResponse(200, pageShell("Device tokens", tokensContent(tokens, csrf)), controlSecurityHeaders());
}

export async function handleAdminTokenRevoke(ctx: AdminCtx, request: Request, tokenId: string): Promise<Response> {
  const now = nowIso();
  const form = await parseAdminForm(request);
  if (!form) return new Response("invalid form", { status: 400, headers: controlSecurityHeaders() });
  const session = await validatedAdminSession(ctx, request, form, now);
  if (!session) return new Response("invalid form", { status: 403, headers: controlSecurityHeaders() });
  try {
    await revokeApiToken(ctx.db, tokenId, now);
  } catch (err) {
    if (!(err instanceof NotFoundError)) {
      return new Response("could not revoke token", { status: 500, headers: controlSecurityHeaders() });
    }
    // NotFound is treated as a no-op success, matching Go's errors.Is(err, store.ErrNotFound) guard.
  }
  return new Response(null, {
    status: 303,
    headers: { ...controlSecurityHeaders(), Location: "/admin/tokens" },
  });
}

export async function handleAdminLogout(ctx: AdminCtx, request: Request): Promise<Response> {
  const now = nowIso();
  const form = await parseAdminForm(request);
  if (!form) return new Response("invalid form", { status: 400, headers: controlSecurityHeaders() });
  const session = await validatedAdminSession(ctx, request, form, now);
  if (!session) return new Response("invalid form", { status: 403, headers: controlSecurityHeaders() });
  await deleteAdminSession(ctx.db, session.id);
  return new Response(null, {
    status: 303,
    headers: {
      ...controlSecurityHeaders(),
      Location: "/admin/login",
      "Set-Cookie": clearAdminCookieHeader(ctx),
    },
  });
}
