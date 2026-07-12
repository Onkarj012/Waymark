// Ports parseConfiguredOrigin / isLoopbackHostname from internal/web/server.go.
// Used to validate PUBLIC_BASE_URL / CONTROL_BASE_URL and to compute the
// canonical `host` string used for host-gating comparisons.

export interface ParsedOrigin {
  url: string; // canonical scheme://host (default ports stripped)
  host: string; // host used for comparisons (bracketed for IPv6)
  secure: boolean; // true if https
}

export function isLoopbackHostname(host: string): boolean {
  const h = host.toLowerCase();
  if (h === "localhost" || h.endsWith(".localhost")) return true;
  return isIpLoopback(h);
}

function isIpLoopback(host: string): boolean {
  // IPv4 loopback range 127.0.0.0/8.
  const v4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (v4) return v4[1] === "127";
  // IPv6 loopback ::1, optionally bracketed.
  const stripped = host.replace(/^\[/, "").replace(/\]$/, "");
  return stripped === "::1";
}

/** Parses and validates a configured origin string. Returns null for an
 * empty value (mirrors Go returning ("", "", false, nil) for ""), so callers
 * can distinguish "not set" from "invalid". Throws for anything malformed. */
export function parseConfiguredOrigin(value: string): ParsedOrigin | null {
  const trimmed = value.trim().replace(/\/+$/, "");
  if (trimmed === "") return null;

  let u: URL;
  try {
    u = new URL(trimmed);
  } catch {
    throw new Error("must be an absolute HTTP or HTTPS origin");
  }
  if (!u.hostname || (u.protocol !== "http:" && u.protocol !== "https:")) {
    throw new Error("must be an absolute HTTP or HTTPS origin");
  }
  if (u.username || u.password || (u.pathname !== "" && u.pathname !== "/") || u.search || u.hash) {
    throw new Error("must not contain credentials, a path, query, or fragment");
  }
  const scheme = u.protocol.replace(":", "");
  if (scheme === "http" && !isLoopbackHostname(u.hostname)) {
    throw new Error("must use HTTPS outside loopback development");
  }
  const hostname = u.hostname.toLowerCase();
  let port = u.port;
  if ((scheme === "https" && port === "443") || (scheme === "http" && port === "80")) {
    port = "";
  }
  let host = hostname;
  if (port !== "") {
    host = hostname.includes(":") ? `[${hostname}]:${port}` : `${hostname}:${port}`;
  } else if (hostname.includes(":")) {
    host = `[${hostname}]`;
  }
  return { url: `${scheme}://${host}`, host, secure: scheme === "https" };
}
