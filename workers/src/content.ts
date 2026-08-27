// Content worker entrypoint. Ports the public-origin half of
// internal/web/server.go: GET /theme.css, GET /p/{id}, GET /, and the
// discovery document. Deploy with wrangler.content.toml on the content
// domain — this worker never sees a bearer token or admin cookie, and must
// stay a distinct origin from the control worker (AGENTS.md invariant:
// "Published HTML is active content").

import { loadConfig, type BaseWaymarkConfig } from "./config";
import { errJson, logPath, safeDecodeURIComponent } from "./http";
import { handleDiscovery } from "./device-api";
import { getPage, nowIso } from "./store";
import { renderThemed } from "./render";
import { NotFoundError, type BaseEnv } from "./types";
import { FAVICON_SVG, THEME_CSS } from "./theme.generated";

function publicRouteAllowed(pathname: string): boolean {
  return pathname === "/" || pathname === "/theme.css" || pathname === "/favicon.svg" || pathname === "/.well-known/waymark" || pathname.startsWith("/p/");
}

export default {
  async fetch(request: Request, env: BaseEnv): Promise<Response> {
    const start = Date.now();
    const url = new URL(request.url);
    const pathname = url.pathname;

    let config: BaseWaymarkConfig;
    try {
      config = loadConfig(env);
    } catch (err) {
      return errJson(500, `server misconfigured: ${(err as Error).message}`);
    }

    const host = request.headers.get("Host") ?? "";
    if (config.legacyHosts.includes(host.toLowerCase())) {
      return Response.redirect(`${config.publicUrl}${pathname}${url.search}`, 308);
    }
    const hostMatches = host.toLowerCase() === config.publicHost.toLowerCase();
    if (pathname !== "/healthz" && (!hostMatches || !publicRouteAllowed(pathname))) {
      return new Response("misdirected request", { status: 421 });
    }

    const response = await route(request, env, config, pathname);
    const durationMs = Date.now() - start;
    console.log(`${request.method} ${logPath(pathname)} -> ${response.status} (${durationMs}ms)`);
    return response;
  },
};

async function route(request: Request, env: BaseEnv, config: BaseWaymarkConfig, pathname: string): Promise<Response> {
  const method = request.method;

  if (method === "GET" && pathname === "/healthz") return new Response("ok");

  if (method === "GET" && pathname === "/") {
    return new Response("Waymark\n", { headers: { "Content-Type": "text/plain; charset=utf-8" } });
  }

  if (method === "GET" && pathname === "/.well-known/waymark") {
    return handleDiscovery(config.controlUrl, config.publicUrl);
  }

  if (method === "GET" && pathname === "/theme.css") {
    return new Response(THEME_CSS, {
      headers: { "Content-Type": "text/css; charset=utf-8", "Cache-Control": "public, max-age=3600" },
    });
  }

  if (method === "GET" && pathname === "/favicon.svg") {
    return new Response(FAVICON_SVG, {
      headers: { "Content-Type": "image/svg+xml", "Cache-Control": "public, max-age=86400" },
    });
  }

  if (method === "GET" && pathname.startsWith("/p/")) {
    // safeDecodeURIComponent: malformed percent-encoding (GET /p/%) must fall
    // through to the page lookup and 404, never throw an uncaught URIError.
    const id = safeDecodeURIComponent(pathname.slice("/p/".length));
    return servePage(env, id);
  }

  return new Response("not found", { status: 404 });
}

async function servePage(env: BaseEnv, id: string): Promise<Response> {
  let page;
  try {
    page = await getPage(env.DB, id);
  } catch (err) {
    if (err instanceof NotFoundError) return new Response("404 page not found\n", { status: 404 });
    return new Response("internal error", { status: 500 });
  }
  if (page.expiresAt && !(page.expiresAt > nowIso())) {
    return new Response("404 page not found\n", { status: 404 }); // expired
  }

  const headers: Record<string, string> = {
    "Content-Type": "text/html; charset=utf-8",
    "Referrer-Policy": "no-referrer",
    "X-Content-Type-Options": "nosniff",
    "Content-Security-Policy": "frame-ancestors 'none'",
    "X-Frame-Options": "DENY",
  };
  if (page.raw) return new Response(page.html, { headers });
  return new Response(renderThemed(page.title, page.html), { headers });
}
