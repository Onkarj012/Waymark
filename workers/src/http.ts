// Small HTTP utilities shared by both workers: JSON responses and a
// minimal path-param router (Workers has no net/http-style ServeMux).

export function json(status: number, body: unknown, headers?: HeadersInit): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json; charset=utf-8", ...headers },
  });
}

export function errJson(status: number, message: string, headers?: HeadersInit): Response {
  return json(status, { error: message }, headers);
}

/** decodeURIComponent that never throws: malformed percent-encoding (e.g. a
 * bare "%") falls back to the raw value instead of raising a URIError that
 * would bubble up as a Workers 1101/500. Page/token IDs are base62/base64url,
 * so a legitimate request never round-trips through the fallback, and a
 * malformed one simply fails its lookup (→ 404), matching how the Go server
 * never serves such a request successfully. */
export function safeDecodeURIComponent(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

export type Handler = (request: Request, params: Record<string, string>) => Promise<Response> | Response;

interface Route {
  method: string;
  matcher: RegExp;
  keys: string[];
  handler: Handler;
}

/** A tiny router matching "METHOD /literal/{param}/segments" patterns,
 * enough to mirror the routes registered in NewConfigured() (server.go). */
export class Router {
  private routes: Route[] = [];

  add(method: string, pattern: string, handler: Handler): void {
    const keys: string[] = [];
    const regexSource = pattern
      .split("/")
      .map((segment) => {
        const match = segment.match(/^\{(\w+)\}$/);
        if (match) {
          keys.push(match[1]!);
          return "([^/]+)";
        }
        return segment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      })
      .join("/");
    this.routes.push({ method, matcher: new RegExp(`^${regexSource}$`), keys, handler });
  }

  async handle(request: Request, pathname: string): Promise<Response | null> {
    for (const route of this.routes) {
      if (route.method !== request.method) continue;
      const match = route.matcher.exec(pathname);
      if (!match) continue;
      const params: Record<string, string> = {};
      route.keys.forEach((key, i) => {
        params[key] = safeDecodeURIComponent(match[i + 1]!);
      });
      return route.handler(request, params);
    }
    return null;
  }
}

/** Redacts unguessable page/token IDs before logging a request path, mirroring
 * logPath() in server.go. */
export function logPath(pathname: string): string {
  if (pathname.startsWith("/p/")) return "/p/[redacted]";
  if (pathname.startsWith("/api/pages/")) return "/api/pages/[redacted]";
  return pathname;
}
