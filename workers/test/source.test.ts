import { createHash } from "node:crypto";
import { beforeEach, describe, expect, it, vi } from "vitest";
import controlWorker from "../src/control";
import contentWorker from "../src/content";
import { MAX_BODY_BYTES } from "../src/body";
import { hashHighEntropy } from "../src/crypto";
import { handleGetSource } from "../src/pages-api";
import { createPage, deletePage, getPage, getPageSource } from "../src/store";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import {
  CONTROL_HOST,
  PUBLIC_HOST,
  makeContentEnv,
  makeEnv,
  request,
} from "./test-env";
import type { Page } from "../src/types";

let db: FakeD1;
const token = "waymark_source.secret";
const controlOrigin = `http://${CONTROL_HOST}`;
const publicOrigin = `http://${PUBLIC_HOST}`;

beforeEach(async () => {
  db = createFakeD1();
  await db.prepare(`INSERT INTO api_tokens
    (id, token_hash, display_prefix, device_label, scopes, created_at, expires_at)
    VALUES (?, ?, ?, ?, ?, ?, ?)`).bind(
    "source-token", await hashHighEntropy(token), "waymark_source", "Source test", "pages:read",
    "2026-01-01T00:00:00.000Z", "2100-01-01T00:00:00.000Z",
  ).run();
});

function page(overrides: Partial<Page> = {}): Page {
  return {
    id: "source-page",
    title: "<Report>",
    slug: "",
    html: "\ufeff<p>café 🌍 e\u0301 &amp; <script>alert('source')</script></p>\r\n\0",
    raw: false,
    createdAt: "2026-11-09T09:00:00.120Z",
    updatedAt: "2026-11-09T09:01:00.120Z",
    expiresAt: null,
    ...overrides,
  };
}

function sourceRequest(id: string, credential = token, host = CONTROL_HOST): Request {
  return request(host, `/api/pages/${id}/source`, {
    headers: credential ? { Authorization: `Bearer ${credential}` } : {},
  });
}

function expectSourceHeaders(response: Response): void {
  expect(response.headers.get("Content-Type")).toBe("application/json; charset=utf-8");
  expect(response.headers.get("Cache-Control")).toBe("no-store");
  expect(response.headers.get("X-Content-Type-Options")).toBe("nosniff");
  expect(response.headers.get("Referrer-Policy")).toBe("same-origin");
  expect(response.headers.get("Content-Security-Policy")).toBe(
    "default-src 'none'; style-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'",
  );
  expect(response.headers.get("Access-Control-Allow-Origin")).toBeNull();
}

describe("authenticated source export", () => {
  it.each([true, false])("exports the exact envelope and stored bytes for raw=%s without changing the row", async (raw) => {
    const original = page({ raw });
    if (raw) {
      original.html = `\ufeff<!DOCTYPE html>\r\n<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Report</title><style>body{margin:0}</style></head><body>${original.html}</body></html>\r\n`;
      original.expiresAt = "2100-11-09T09:00:00.120Z";
    }
    await createPage(db, original);

    const response = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
    expect(response.status).toBe(200);
    expectSourceHeaders(response);
    expect(await response.json()).toEqual({
      schema: "waymark-source",
      version: 1,
      control_origin: controlOrigin,
      public_origin: publicOrigin,
      id: original.id,
      url: `${publicOrigin}/p/${original.id}`,
      title: original.title,
      slug: "",
      raw,
      created_at: original.createdAt,
      updated_at: original.updatedAt,
      expires_at: original.expiresAt,
      html: original.html,
      utf8_bytes: Buffer.byteLength(original.html, "utf8"),
      sha256: createHash("sha256").update(original.html, "utf8").digest("hex"),
    });
    expect(await getPage(db, original.id)).toEqual(original);

    const metadata = await controlWorker.fetch(request(CONTROL_HOST, `/api/pages/${original.id}`, {
      headers: { Authorization: `Bearer ${token}` },
    }), makeEnv(db));
    expect(metadata.status).toBe(200);
    const body = await metadata.json();
    expect(body).not.toHaveProperty("html");
    expect(body).not.toHaveProperty("schema");
  });

  it("reads HTML and metadata in one database snapshot", async () => {
    const original = page();
    await createPage(db, original);
    const prepare = vi.spyOn(db, "prepare");
    const response = await handleGetSource(db, controlOrigin, publicOrigin, original.id);
    expect(response.status).toBe(200);
    expect(prepare).toHaveBeenCalledTimes(1);
    expect(prepare.mock.calls[0]?.[0]).toContain("CAST(html AS BLOB)");
    prepare.mockRestore();
    const snapshot = await getPageSource(db, original.id);
    expect(snapshot).toEqual({
      id: original.id, titleBytes: new TextEncoder().encode(original.title), slugBytes: new TextEncoder().encode(original.slug), raw: original.raw,
      createdAt: original.createdAt, updatedAt: original.updatedAt, expiresAt: original.expiresAt,
      htmlBytes: new TextEncoder().encode(original.html),
    });
  });

  it.each(["", "invalid"])("rejects missing or invalid bearer credentials %j before looking up a page", async (credential) => {
    const response = await controlWorker.fetch(sourceRequest("missing", credential), makeEnv(db));
    expect(response.status).toBe(401);
    expectSourceHeaders(response);
    expect(await response.json()).toEqual({ error: "unauthorized" });
  });

  it("rejects an authenticated owner session without a bearer token", async () => {
    const cookie = "owner-session";
    await db.prepare(`INSERT INTO admin_sessions
      (id, session_hash, authenticated, created_at, expires_at) VALUES (?, ?, 1, ?, ?)`)
      .bind("owner", await hashHighEntropy(cookie), "2026-01-01T00:00:00.000Z", "2100-01-01T00:00:00.000Z").run();
    const response = await controlWorker.fetch(request(CONTROL_HOST, "/api/pages/missing/source", {
      cookie: `waymark_admin=${cookie}`,
    }), makeEnv(db));
    expect(response.status).toBe(401);
    expectSourceHeaders(response);
  });

  it("requires pages:read and accepts read-only tokens", async () => {
    const original = page();
    await createPage(db, original);
    await db.prepare("UPDATE api_tokens SET scopes = ?").bind("pages:write").run();
    const denied = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
    expect(denied.status).toBe(403);
    expectSourceHeaders(denied);
    expect(await denied.json()).toEqual({ error: "insufficient_scope" });
    await db.prepare("UPDATE api_tokens SET scopes = ?").bind("pages:read").run();
    const allowed = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
    expect(allowed.status).toBe(200);
    expectSourceHeaders(allowed);
  });

  it("rejects expired and revoked credentials", async () => {
    await db.prepare("UPDATE api_tokens SET expires_at = ?").bind("2000-01-01T00:00:00Z").run();
    const expired = await controlWorker.fetch(sourceRequest("missing"), makeEnv(db));
    expect(expired.status).toBe(401);
    expectSourceHeaders(expired);
    await db.prepare("UPDATE api_tokens SET expires_at = ?, revoked_at = ?")
      .bind("2100-01-01T00:00:00Z", "2026-01-01T00:00:00Z").run();
    const revoked = await controlWorker.fetch(sourceRequest("missing"), makeEnv(db));
    expect(revoked.status).toBe(401);
    expectSourceHeaders(revoked);
  });

  it("rejects the source route on the public host in both workers", async () => {
    const control = await controlWorker.fetch(sourceRequest("missing", token, PUBLIC_HOST), makeEnv(db));
    expect(control.status).toBe(421);
    const content = await contentWorker.fetch(sourceRequest("missing", token, PUBLIC_HOST), makeContentEnv(db));
    expect(content.status).toBe(421);
  });

  it("returns 404 for missing and deleted pages", async () => {
    const original = page();
    await createPage(db, original);
    await deletePage(db, original.id);
    for (const id of ["missing", original.id]) {
      const response = await controlWorker.fetch(sourceRequest(id), makeEnv(db));
      expect(response.status).toBe(404);
      expectSourceHeaders(response);
      expect(await response.json()).toEqual({ error: "page not found" });
    }
  });

  it("returns 404 at and after expiry without waiting for cleanup", async () => {
    const now = new Date("2026-11-09T10:00:00.120Z");
    const clock = vi.spyOn(Date, "now").mockReturnValue(now.getTime());
    try {
      for (const expiresAt of [now.toISOString(), "2026-11-09T09:59:59.120Z"]) {
        const original = page({ expiresAt });
        await createPage(db, original);
        const response = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
        expect(response.status).toBe(404);
        expectSourceHeaders(response);
        expect(await response.json()).toEqual({ error: "page not found" });
        expect(await getPage(db, original.id)).toEqual(original);
        await deletePage(db, original.id);
      }
    } finally {
      clock.mockRestore();
    }
  });

  it.each(["FF", "EDA080", "E282"])("rejects invalid stored UTF-8 bytes %s without replacement", async (hex) => {
    const original = page();
    await createPage(db, original);
    await db.prepare(`UPDATE pages SET html = CAST(X'${hex}' AS TEXT) WHERE id = ?`).bind(original.id).run();
    const response = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
    expect(response.status).toBe(422);
    expectSourceHeaders(response);
    expect(await response.json()).toEqual({ error: "stored source is not valid UTF-8" });
    const bytes = await db.prepare("SELECT hex(CAST(html AS BLOB)) AS bytes FROM pages WHERE id = ?")
      .bind(original.id).first<{ bytes: string }>();
    expect(bytes?.bytes).toBe(hex);
  });

  it.each(["title", "slug"])("rejects invalid persisted %s bytes before any lossy TEXT conversion", async (field) => {
    const original = page();
    await createPage(db, original);
    for (const hex of ["FF", "EDA080", "E282"]) {
      await db.prepare(`UPDATE pages SET ${field} = CAST(X'${hex}' AS TEXT) WHERE id = ?`).bind(original.id).run();
      const response = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
      expect(response.status).toBe(422);
      expectSourceHeaders(response);
      expect(await response.json()).toEqual({ error: "stored source is not valid UTF-8" });
      const persisted = await db.prepare(`SELECT hex(CAST(${field} AS BLOB)) AS bytes FROM pages WHERE id = ?`)
        .bind(original.id).first<{ bytes: string }>();
      expect(persisted?.bytes).toBe(hex);
    }
  });

  it("exports pages larger than the new-write cap without truncation", async () => {
    const original = page({ html: "é".repeat(MAX_BODY_BYTES / 2 + 1), raw: true });
    await createPage(db, original);
    const response = await controlWorker.fetch(sourceRequest(original.id), makeEnv(db));
    expect(response.status).toBe(200);
    expectSourceHeaders(response);
    const body = await response.json();
    expect(body).toMatchObject({
      html: original.html,
      utf8_bytes: Buffer.byteLength(original.html, "utf8"),
      sha256: createHash("sha256").update(original.html, "utf8").digest("hex"),
    });
  });
});
