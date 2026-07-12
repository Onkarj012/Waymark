// Control worker entrypoint. Ports the control-origin half of
// internal/web/server.go's NewConfigured()/ServeHTTP(): the authenticated
// JSON API, device authorization, and the admin UI. Deploy with
// wrangler.control.toml on the control domain (see AGENTS.md — this must
// stay a different origin from the content worker; published HTML is active
// content and must never share an origin with admin sessions/credentials).

import { loadControlConfig, type WaymarkConfig } from "./config";
import { errJson, logPath } from "./http";
import { authenticate, hasScope } from "./auth";
import {
  handleAuthCheck,
  handleDeviceCode,
  handleDeviceToken,
  handleDiscovery,
  handleSelfRevoke,
} from "./device-api";
import { handleCreate, handleDelete, handleGetMeta, handleList, handleUpdate } from "./pages-api";
import {
  controlSecurityHeaders,
  handleActivate,
  handleActivateDecision,
  handleAdminCSS,
  handleAdminLogin,
  handleAdminLoginPost,
  handleAdminLogout,
  handleAdminTokenRevoke,
  handleAdminTokens,
  requireAdmin,
  type AdminCtx,
} from "./admin";
import { runCleanup } from "./cleanup";
import { nowIso } from "./store";
import type { ControlEnv, Credential } from "./types";

function controlRouteAllowed(pathname: string): boolean {
  return (
    pathname === "/.well-known/waymark" ||
    pathname.startsWith("/api/") ||
    pathname === "/activate" ||
    pathname.startsWith("/admin/")
  );
}

export default {
  async fetch(request: Request, env: ControlEnv): Promise<Response> {
    const start = Date.now();
    const url = new URL(request.url);
    const pathname = url.pathname;

    let config: WaymarkConfig;
    try {
      config = loadControlConfig(env);
    } catch (err) {
      return errJson(500, `server misconfigured: ${(err as Error).message}`);
    }

    const host = request.headers.get("Host") ?? "";
    const hostMatches = host.toLowerCase() === config.controlHost.toLowerCase();
    if (pathname !== "/healthz" && (!hostMatches || !controlRouteAllowed(pathname))) {
      return new Response("misdirected request", { status: 421 });
    }

    const adminCtx: AdminCtx = {
      db: env.DB,
      adminPasscode: config.adminPasscode,
      controlUrl: config.controlUrl,
      secureCookie: config.secureCookie,
      trustForwardedIp: config.trustForwardedIp,
    };

    const response = await route(request, env, config, adminCtx, pathname);
    const durationMs = Date.now() - start;
    console.log(`${request.method} ${logPath(pathname)} -> ${response.status} (${durationMs}ms)`);
    return response;
  },

  async scheduled(_event: ScheduledEvent, env: ControlEnv): Promise<void> {
    const result = await runCleanup(env.DB);
    if (result.pagesDeleted > 0) console.log(`expiry sweep: removed ${result.pagesDeleted} page(s)`);
    if (result.authRowsDeleted > 0) console.log(`auth expiry sweep: removed ${result.authRowsDeleted} record(s)`);
    if (result.rateLimitRowsDeleted > 0) {
      console.log(`rate-limit sweep: removed ${result.rateLimitRowsDeleted} bucket(s)`);
    }
  },
};

async function requireScope(
  env: ControlEnv,
  request: Request,
  scope: "pages:read" | "pages:write",
): Promise<Response | Credential> {
  const credential = await authenticate(env.DB, request, nowIso());
  if (!credential) return errJson(401, "unauthorized");
  if (!hasScope(credential.scopes, scope)) return errJson(403, "insufficient_scope");
  return credential;
}

async function route(
  request: Request,
  env: ControlEnv,
  config: WaymarkConfig,
  adminCtx: AdminCtx,
  pathname: string,
): Promise<Response> {
  const method = request.method;

  if (method === "GET" && pathname === "/healthz") return new Response("ok");
  if (method === "GET" && pathname === "/.well-known/waymark") {
    return handleDiscovery(config.controlUrl, config.publicUrl);
  }

  // --- authenticated JSON API ---
  if (pathname === "/api/auth" && method === "GET") {
    const credential = await requireScope(env, request, "pages:read");
    if (credential instanceof Response) return credential;
    return handleAuthCheck(credential);
  }
  if (pathname === "/api/pages" && method === "POST") {
    const credential = await requireScope(env, request, "pages:write");
    if (credential instanceof Response) return credential;
    return handleCreate(env.DB, config.publicUrl, request);
  }
  if (pathname === "/api/pages" && method === "GET") {
    const credential = await requireScope(env, request, "pages:read");
    if (credential instanceof Response) return credential;
    return handleList(env.DB, config.publicUrl, request);
  }
  const pageIdMatch = pathname.match(/^\/api\/pages\/([^/]+)$/);
  if (pageIdMatch && method === "GET") {
    const credential = await requireScope(env, request, "pages:read");
    if (credential instanceof Response) return credential;
    return handleGetMeta(env.DB, config.publicUrl, decodeURIComponent(pageIdMatch[1]!));
  }
  if (pageIdMatch && method === "PUT") {
    const credential = await requireScope(env, request, "pages:write");
    if (credential instanceof Response) return credential;
    return handleUpdate(env.DB, config.publicUrl, decodeURIComponent(pageIdMatch[1]!), request);
  }
  if (pageIdMatch && method === "DELETE") {
    const credential = await requireScope(env, request, "pages:write");
    if (credential instanceof Response) return credential;
    return handleDelete(env.DB, decodeURIComponent(pageIdMatch[1]!));
  }

  if (pathname === "/api/auth/device/code" && method === "POST") {
    return handleDeviceCode(env.DB, request, config.adminPasscode, config.controlUrl, config.trustForwardedIp);
  }
  if (pathname === "/api/auth/device/token" && method === "POST") {
    return handleDeviceToken(env.DB, request, config.adminPasscode, config.tokenTtlDays, config.trustForwardedIp);
  }
  if (pathname === "/api/auth/revoke" && method === "POST") {
    const credential = await requireScope(env, request, "pages:read");
    if (credential instanceof Response) return credential;
    return handleSelfRevoke(env.DB, credential);
  }

  // --- admin UI ---
  if (pathname === "/activate" && method === "GET") {
    return requireAdmin(adminCtx, request, () => handleActivate(adminCtx, request));
  }
  if (pathname === "/activate" && method === "POST") {
    return requireAdmin(adminCtx, request, () => handleActivateDecision(adminCtx, request));
  }
  if (pathname === "/admin/login" && method === "GET") {
    return handleAdminLogin(adminCtx, request);
  }
  if (pathname === "/admin/login" && method === "POST") {
    return handleAdminLoginPost(adminCtx, request);
  }
  if (pathname === "/admin/logout" && method === "POST") {
    return requireAdmin(adminCtx, request, () => handleAdminLogout(adminCtx, request));
  }
  if (pathname === "/admin/tokens" && method === "GET") {
    return requireAdmin(adminCtx, request, () => handleAdminTokens(adminCtx, request));
  }
  const tokenRevokeMatch = pathname.match(/^\/admin\/tokens\/([^/]+)\/revoke$/);
  if (tokenRevokeMatch && method === "POST") {
    const tokenId = decodeURIComponent(tokenRevokeMatch[1]!);
    return requireAdmin(adminCtx, request, () => handleAdminTokenRevoke(adminCtx, request, tokenId));
  }
  if (pathname === "/admin/style.css" && method === "GET") {
    return handleAdminCSS();
  }

  return new Response("not found", { status: 404, headers: controlSecurityHeaders() });
}
