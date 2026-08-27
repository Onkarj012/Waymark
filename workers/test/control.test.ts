import { beforeEach, describe, expect, it } from "vitest";
import controlWorker from "../src/control";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import {
  ADMIN_PASSCODE,
  CONTROL_HOST,
  PUBLIC_HOST,
  extractCsrf,
  extractSetCookie,
  formRequest,
  jsonRequest,
  makeEnv,
  request,
} from "./test-env";
import type { ControlEnv } from "../src/types";

function validRawDocument(inner = "<p>Hi</p>", title = "Title"): string {
  return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${title}</title>
<style>body { margin: 0; }</style>
</head>
<body>
${inner}
</body>
</html>
`;
}

let db: FakeD1;
let env: ControlEnv;

beforeEach(() => {
  db = createFakeD1();
  env = makeEnv(db);
});

describe("host gating", () => {
  it("serves /healthz regardless of host", async () => {
    const res = await controlWorker.fetch(request("unknown.localhost", "/healthz"), env);
    expect(res.status).toBe(200);
  });

  it("rejects control routes on the wrong host with 421", async () => {
    const res = await controlWorker.fetch(request(PUBLIC_HOST, "/admin/login"), env);
    expect(res.status).toBe(421);
  });

  it("rejects a route not in the control worker's allow-list even on the right host", async () => {
    const res = await controlWorker.fetch(request(CONTROL_HOST, "/theme.css"), env);
    expect(res.status).toBe(421);
  });

  it("308-redirects a legacy host to the canonical control origin, with security headers", async () => {
    const legacyEnv = { ...env, WAYMARK_LEGACY_HOSTS: "old-control.localhost" };
    const res = await controlWorker.fetch(request("old-control.localhost", "/activate?code=abc"), legacyEnv);
    expect(res.status).toBe(308);
    expect(res.headers.get("Location")).toBe(`http://${CONTROL_HOST}/activate?code=abc`);
    expect(res.headers.get("X-Content-Type-Options")).toBe("nosniff");
  });
});

describe("config validation", () => {
  it("500s clearly when WAYMARK_ADMIN_PASSCODE is missing, unlike the content worker", async () => {
    const { WAYMARK_ADMIN_PASSCODE: _omit, ...rest } = env;
    const misconfigured = rest as ControlEnv;
    const res = await controlWorker.fetch(request(CONTROL_HOST, "/healthz"), misconfigured);
    expect(res.status).toBe(500);
    const body = (await res.json()) as { error: string };
    expect(body.error).toContain("WAYMARK_ADMIN_PASSCODE is required");
  });
});

describe("discovery", () => {
  it("reports both origins and device_authorization support", async () => {
    const res = await controlWorker.fetch(request(CONTROL_HOST, "/.well-known/waymark"), env);
    const body = (await res.json()) as Record<string, unknown>;
    expect(body).toEqual({
      control_url: `http://${CONTROL_HOST}`,
      content_url: `http://${PUBLIC_HOST}`,
      device_authorization: true,
    });
  });
});

describe("bearer auth", () => {
  it("rejects a missing or empty bearer token with 401", async () => {
    const noAuth = await controlWorker.fetch(request(CONTROL_HOST, "/api/auth"), env);
    expect(noAuth.status).toBe(401);
    const emptyBearer = await controlWorker.fetch(
      request(CONTROL_HOST, "/api/auth", { headers: { Authorization: "Bearer " } }),
      env,
    );
    expect(emptyBearer.status).toBe(401);
  });
});

async function adminLogin(): Promise<{ cookie: string; csrf: string }> {
  const loginPage = await controlWorker.fetch(request(CONTROL_HOST, "/admin/login?next=/activate"), env);
  const html = await loginPage.text();
  const cookie = extractSetCookie(loginPage);
  const csrf = extractCsrf(html);
  const submit = await controlWorker.fetch(
    formRequest(
      CONTROL_HOST,
      "/admin/login",
      { csrf, passcode: ADMIN_PASSCODE, next: "/activate" },
      { origin: `http://${CONTROL_HOST}`, cookie },
    ),
    env,
  );
  expect(submit.status).toBe(303);
  return { cookie, csrf };
}

describe("device authorization end-to-end", () => {
  it("code -> admin approval -> token issuance -> scoped API access -> revoke", async () => {
    const secret = "A".repeat(43); // 32 raw bytes, base64url-encoded (43 chars, no padding)
    const codeRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/code", {
        device_secret: secret,
        device_label: "test device",
        scopes: ["pages:read", "pages:write"],
      }),
      env,
    );
    expect(codeRes.status).toBe(201);
    const code = (await codeRes.json()) as { device_code: string; user_code: string };

    const { cookie, csrf } = await adminLogin();

    const activatePage = await controlWorker.fetch(
      request(CONTROL_HOST, `/activate?code=${code.user_code}`, { cookie }),
      env,
    );
    const activateHtml = await activatePage.text();
    expect(activateHtml).toContain("test device");

    const decision = await controlWorker.fetch(
      formRequest(
        CONTROL_HOST,
        "/activate",
        { csrf, code: code.user_code, decision: "approved" },
        { origin: `http://${CONTROL_HOST}`, cookie },
      ),
      env,
    );
    expect(decision.status).toBe(200);

    const tokenRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/token", {
        device_code: code.device_code,
        device_secret: secret,
      }),
      env,
    );
    expect(tokenRes.status).toBe(200);
    const issued = (await tokenRes.json()) as { access_token: string };
    expect(issued.access_token).toBeTruthy();

    // Replaying the same device_code/device_secret must not mint a second token.
    const replay = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/token", {
        device_code: code.device_code,
        device_secret: secret,
      }),
      env,
    );
    expect(replay.status).toBe(400);
    expect((await replay.json())).toEqual({ error: "expired_token" });

    const authed = (headers: Record<string, string> = {}) =>
      request(CONTROL_HOST, "/api/auth", { headers: { Authorization: `Bearer ${issued.access_token}`, ...headers } });

    expect((await controlWorker.fetch(authed(), env)).status).toBe(200);

    const createRes = await controlWorker.fetch(
      request(CONTROL_HOST, "/api/pages", {
        method: "POST",
        headers: { Authorization: `Bearer ${issued.access_token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ title: "Hi", html: validRawDocument("<p>hi</p>", "Hi"), raw: true }),
      }),
      env,
    );
    expect(createRes.status).toBe(201);

    // A device token is a control-origin credential; it must not authenticate on the content host.
    const wrongHost = await controlWorker.fetch(
      request(PUBLIC_HOST, "/api/auth", { headers: { Authorization: `Bearer ${issued.access_token}` } }),
      env,
    );
    expect(wrongHost.status).toBe(421);

    const revoke = await controlWorker.fetch(
      request(CONTROL_HOST, "/api/auth/revoke", {
        method: "POST",
        headers: { Authorization: `Bearer ${issued.access_token}`, "Content-Type": "application/json" },
        body: "{}",
      }),
      env,
    );
    expect(revoke.status).toBe(200);

    expect((await controlWorker.fetch(authed(), env)).status).toBe(401);
  });

  it("read-only scope cannot create pages", async () => {
    const secret = "B".repeat(43);
    const codeRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/code", {
        device_secret: secret,
        device_label: "read-only device",
        scopes: ["pages:read"],
      }),
      env,
    );
    const code = (await codeRes.json()) as { device_code: string; user_code: string };
    const { cookie, csrf } = await adminLogin();
    await controlWorker.fetch(
      formRequest(
        CONTROL_HOST,
        "/activate",
        { csrf, code: code.user_code, decision: "approved" },
        { origin: `http://${CONTROL_HOST}`, cookie },
      ),
      env,
    );
    const tokenRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/token", {
        device_code: code.device_code,
        device_secret: secret,
      }),
      env,
    );
    const issued = (await tokenRes.json()) as { access_token: string };

    const createRes = await controlWorker.fetch(
      request(CONTROL_HOST, "/api/pages", {
        method: "POST",
        headers: { Authorization: `Bearer ${issued.access_token}`, "Content-Type": "application/json" },
        body: JSON.stringify({ title: "Hi", html: validRawDocument("<p>hi</p>", "Hi"), raw: true }),
      }),
      env,
    );
    expect(createRes.status).toBe(403);
  });
});

describe("admin token management", () => {
  it("lists issued tokens without leaking the raw secret, and revocation works from the admin UI", async () => {
    const secret = "C".repeat(43);
    const codeRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/code", {
        device_secret: secret,
        device_label: "listed device",
        scopes: ["pages:read"],
      }),
      env,
    );
    const code = (await codeRes.json()) as { device_code: string; user_code: string };
    const { cookie, csrf } = await adminLogin();
    await controlWorker.fetch(
      formRequest(
        CONTROL_HOST,
        "/activate",
        { csrf, code: code.user_code, decision: "approved" },
        { origin: `http://${CONTROL_HOST}`, cookie },
      ),
      env,
    );
    const tokenRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/token", {
        device_code: code.device_code,
        device_secret: secret,
      }),
      env,
    );
    const issued = (await tokenRes.json()) as { access_token: string };

    const tokensPage = await controlWorker.fetch(request(CONTROL_HOST, "/admin/tokens", { cookie }), env);
    const html = await tokensPage.text();
    expect(html).toContain("waymark_");
    expect(html).not.toContain(issued.access_token);

    const tokenIdMatch = html.match(/\/admin\/tokens\/([^/]+)\/revoke/);
    expect(tokenIdMatch).not.toBeNull();
    const revokeCsrf = extractCsrf(html);
    const revokeRes = await controlWorker.fetch(
      formRequest(
        CONTROL_HOST,
        `/admin/tokens/${tokenIdMatch![1]}/revoke`,
        { csrf: revokeCsrf },
        { origin: `http://${CONTROL_HOST}`, cookie },
      ),
      env,
    );
    expect(revokeRes.status).toBe(303);

    const authCheck = await controlWorker.fetch(
      request(CONTROL_HOST, "/api/auth", { headers: { Authorization: `Bearer ${issued.access_token}` } }),
      env,
    );
    expect(authCheck.status).toBe(401);
  });
});

describe("control security headers", () => {
  // The exact set setControlHeaders() applies in internal/web/admin.go; Go's
  // ServeHTTP stamps it on every control-host response, so the worker must too.
  const EXPECTED_HEADERS: Record<string, string> = {
    "Cache-Control": "no-store",
    "Referrer-Policy": "same-origin",
    "X-Content-Type-Options": "nosniff",
    "Content-Security-Policy":
      "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
  };

  function expectSecurityHeaders(res: Response): void {
    for (const [name, value] of Object.entries(EXPECTED_HEADERS)) {
      expect(res.headers.get(name), name).toBe(value);
    }
  }

  it("stamps the device-token poll success response that carries the access token", async () => {
    const secret = "D".repeat(43);
    const codeRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/code", {
        device_secret: secret,
        device_label: "headers device",
        scopes: ["pages:read"],
      }),
      env,
    );
    expect(codeRes.status).toBe(201);
    expectSecurityHeaders(codeRes);
    const code = (await codeRes.json()) as { device_code: string; user_code: string };

    const { cookie, csrf } = await adminLogin();
    await controlWorker.fetch(
      formRequest(
        CONTROL_HOST,
        "/activate",
        { csrf, code: code.user_code, decision: "approved" },
        { origin: `http://${CONTROL_HOST}`, cookie },
      ),
      env,
    );

    const tokenRes = await controlWorker.fetch(
      jsonRequest(CONTROL_HOST, "POST", "/api/auth/device/token", {
        device_code: code.device_code,
        device_secret: secret,
      }),
      env,
    );
    expect(tokenRes.status).toBe(200);
    expect(((await tokenRes.json()) as { access_token: string }).access_token).toBeTruthy();
    expectSecurityHeaders(tokenRes);
  });

  it("stamps every other control response: discovery, 401 JSON errors, 404s, and 421s", async () => {
    expectSecurityHeaders(await controlWorker.fetch(request(CONTROL_HOST, "/.well-known/waymark"), env));

    const unauthorized = await controlWorker.fetch(request(CONTROL_HOST, "/api/auth"), env);
    expect(unauthorized.status).toBe(401);
    expectSecurityHeaders(unauthorized);

    const notFound = await controlWorker.fetch(request(CONTROL_HOST, "/api/no-such-route"), env);
    expect(notFound.status).toBe(404);
    expectSecurityHeaders(notFound);

    const misdirected = await controlWorker.fetch(request(PUBLIC_HOST, "/admin/login"), env);
    expect(misdirected.status).toBe(421);
    expectSecurityHeaders(misdirected);
  });
});

describe("malformed cookies", () => {
  it("treats an admin cookie with malformed percent-encoding as logged out instead of 500ing", async () => {
    const res = await controlWorker.fetch(
      request(CONTROL_HOST, "/admin/tokens", { cookie: "waymark_admin=%" }),
      env,
    );
    expect(res.status).toBe(303);
    expect(res.headers.get("Location")).toContain("/admin/login");
  });

  it("does not 500 the pages API when the request path has malformed percent-encoding", async () => {
    const res = await controlWorker.fetch(request(CONTROL_HOST, "/api/pages/%", { method: "DELETE" }), env);
    expect(res.status).toBe(401); // unauthenticated, but routed — not an uncaught URIError
  });
});

describe("CSRF / origin enforcement on admin forms", () => {
  it("rejects logout with the wrong origin or a bad csrf token", async () => {
    const { cookie, csrf } = await adminLogin();
    const badOrigin = await controlWorker.fetch(
      formRequest(CONTROL_HOST, "/admin/logout", { csrf }, { origin: "http://evil.localhost", cookie }),
      env,
    );
    expect(badOrigin.status).toBe(403);

    const badCsrf = await controlWorker.fetch(
      formRequest(CONTROL_HOST, "/admin/logout", { csrf: "bad" }, { origin: `http://${CONTROL_HOST}`, cookie }),
      env,
    );
    expect(badCsrf.status).toBe(403);
  });
});
