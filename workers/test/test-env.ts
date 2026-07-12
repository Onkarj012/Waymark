import { createFakeD1, type FakeD1 } from "./fake-d1";
import type { BaseEnv } from "../src/types";

export const CONTROL_HOST = "control.localhost";
export const PUBLIC_HOST = "pages.localhost";
export const ADMIN_PASSCODE = "admin-secret";

export function makeEnv(db: FakeD1 = createFakeD1()): BaseEnv {
  return {
    DB: db,
    WAYMARK_ADMIN_PASSCODE: ADMIN_PASSCODE,
    PUBLIC_BASE_URL: `http://${PUBLIC_HOST}`,
    CONTROL_BASE_URL: `http://${CONTROL_HOST}`,
    WAYMARK_TOKEN_TTL_DAYS: "90",
  };
}

export function request(
  host: string,
  path: string,
  init: RequestInit & { origin?: string; cookie?: string } = {},
): Request {
  const headers = new Headers(init.headers);
  headers.set("Host", host);
  if (init.origin) headers.set("Origin", init.origin);
  if (init.cookie) headers.set("Cookie", init.cookie);
  return new Request(`http://${host}${path}`, { ...init, headers });
}

export function jsonRequest(host: string, method: string, path: string, body?: unknown): Request {
  return request(host, path, {
    method,
    headers: body !== undefined ? { "Content-Type": "application/json" } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
}

export function formRequest(
  host: string,
  path: string,
  fields: Record<string, string>,
  opts: { origin?: string; cookie?: string } = {},
): Request {
  return request(host, path, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    body: new URLSearchParams(fields).toString(),
    origin: opts.origin,
    cookie: opts.cookie,
  });
}

export function extractCsrf(html: string): string {
  const match = html.match(/name="csrf" value="([a-f0-9]+)"/);
  if (!match) throw new Error(`csrf token not found in: ${html}`);
  return match[1]!;
}

export function extractSetCookie(res: Response): string {
  const setCookie = res.headers.get("Set-Cookie");
  if (!setCookie) throw new Error("no Set-Cookie header in response");
  return setCookie.split(";")[0]!; // "name=value"
}
