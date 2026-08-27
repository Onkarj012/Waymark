import { beforeEach, describe, expect, it, vi } from "vitest";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import { handleCreate, handleDelete, handleGetMeta, handleList, handleUpdate, ttlToExpiry } from "../src/pages-api";
import { nowIso, createPage } from "../src/store";
import { newId } from "../src/id";

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
const publicUrl = "http://pages.localhost";

beforeEach(() => {
  db = createFakeD1();
});

function req(body: unknown): Request {
  return new Request("http://control.localhost/api/pages", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

describe("ttlToExpiry", () => {
  it("returns null for zero/absent/negative TTL, and an absolute expiry otherwise", () => {
    const now = "2026-01-01T00:00:00.000Z";
    expect(ttlToExpiry(now, undefined)).toBeNull();
    expect(ttlToExpiry(now, 0)).toBeNull();
    expect(ttlToExpiry(now, -5)).toBeNull();
    expect(ttlToExpiry(now, 1)).toBe("2026-01-02T00:00:00.000Z");
  });
});

describe("handleCreate", () => {
  it("creates a raw page and returns its public URL", async () => {
    const html = validRawDocument("<p>Hi</p>", "Hello");
    const res = await handleCreate(db, publicUrl, req({ title: "Hello", html, raw: true }));
    expect(res.status).toBe(201);
    const body = (await res.json()) as { id: string; url: string; raw: boolean };
    expect(body.url).toBe(`${publicUrl}/p/${body.id}`);
    expect(body.raw).toBe(true);
  });

  it("rejects omitted, false, or null raw and incomplete HTML", async () => {
    const html = validRawDocument();
    const omitted = await handleCreate(db, publicUrl, req({ title: "Hello", html }));
    expect(omitted.status).toBe(400);
    expect(((await omitted.json()) as { error: string }).error).toBe("raw:true is required");
    const falsy = await handleCreate(db, publicUrl, req({ title: "Hello", html, raw: false }));
    expect(falsy.status).toBe(400);
    expect(((await falsy.json()) as { error: string }).error).toBe("raw:true is required");
    const rawNull = await handleCreate(db, publicUrl, req({ title: "Hello", html, raw: null }));
    expect(rawNull.status).toBe(400);
    expect(((await rawNull.json()) as { error: string }).error).toBe("raw:true is required");
    const invalid = await handleCreate(db, publicUrl, req({ title: "Hello", html: "<p>Hi</p>", raw: true }));
    expect(invalid.status).toBe(400);
    expect(((await invalid.json()) as { error: string }).error).toContain("invalid raw HTML");
  });

  it("rejects a missing title or html", async () => {
    expect((await handleCreate(db, publicUrl, req({ title: "", html: "x", raw: true }))).status).toBe(400);
    expect((await handleCreate(db, publicUrl, req({ title: "x", html: "  ", raw: true }))).status).toBe(400);
  });

  it("rejects unknown fields (mirrors DisallowUnknownFields in server.go)", async () => {
    const res = await handleCreate(db, publicUrl, req({ title: "x", html: "y", nope: true }));
    expect(res.status).toBe(400);
  });

  it("rejects wrong-typed fields with 400 instead of coercing them (mirrors encoding/json type errors)", async () => {
    const html = validRawDocument();
    const wrongTyped: unknown[] = [
      { title: "x", html, raw: true, ttl_days: "7" }, // string ttl_days must not be silently dropped
      { title: "x", html, raw: "false" }, // Boolean("false") === true must never happen
      { title: 123, html, raw: true }, // numeric title must not become "123"
      { title: "x", html: ["y"], raw: true },
      { title: "x", html, raw: true, slug: 1 },
    ];
    for (const body of wrongTyped) {
      const res = await handleCreate(db, publicUrl, req(body));
      expect(res.status).toBe(400);
      const err = (await res.json()) as { error: string };
      expect(err.error).toContain("invalid JSON");
    }
    // Nothing above may have created a page.
    const list = await handleList(db, publicUrl, new Request("http://control.localhost/api/pages"));
    expect(((await list.json()) as { pages: unknown[] }).pages).toHaveLength(0);
  });

  it("still treats JSON null ttl/slug as absent, like Go's zero values", async () => {
    const res = await handleCreate(db, publicUrl, req({ title: "x", html: validRawDocument(), raw: true, ttl_days: null, slug: null }));
    expect(res.status).toBe(201);
    const body = (await res.json()) as { raw: boolean; expires_at?: string };
    expect(body.raw).toBe(true);
    expect(body.expires_at).toBeUndefined();
  });

  it("sets an absolute expiry from ttl_days", async () => {
    const res = await handleCreate(db, publicUrl, req({ title: "x", html: validRawDocument(), raw: true, ttl_days: 7 }));
    const body = (await res.json()) as { expires_at?: string; created_at: string };
    expect(body.expires_at).toBeDefined();
    expect(new Date(body.expires_at!).getTime()).toBeGreaterThan(new Date(body.created_at).getTime());
  });
});

describe("handleList / handleGetMeta / handleUpdate / handleDelete", () => {
  async function createOne(title = "Item") {
    const res = await handleCreate(db, publicUrl, req({ title, html: validRawDocument("<p>x</p>", title), raw: true }));
    return (await res.json()) as { id: string; updated_at: string };
  }

  async function seedThemed(title = "Legacy") {
    const now = nowIso();
    const id = newId();
    await createPage(db, {
      id,
      title,
      slug: "",
      html: "<p>old</p>",
      raw: false,
      createdAt: now,
      updatedAt: now,
      expiresAt: null,
    });
    return { id, updated_at: now };
  }

  it("lists created pages newest first", async () => {
    await createOne("first");
    await createOne("second");
    const res = await handleList(db, publicUrl, new Request("http://control.localhost/api/pages"));
    const body = (await res.json()) as { pages: { title: string }[] };
    expect(body.pages.map((p) => p.title)).toEqual(["second", "first"]);
  });

  it("gets metadata for an existing page and 404s for a missing one", async () => {
    const created = await createOne();
    const ok = await handleGetMeta(db, publicUrl, created.id);
    expect(ok.status).toBe(200);
    const missing = await handleGetMeta(db, publicUrl, "does-not-exist");
    expect(missing.status).toBe(404);
  });

  it("updates only the fields present in the request body", async () => {
    const created = await createOne("Original");
    const updateReq = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Renamed" }),
    });
    const res = await handleUpdate(db, publicUrl, created.id, updateReq);
    expect(res.status).toBe(200);
    const body = (await res.json()) as { title: string; raw: boolean };
    expect(body.title).toBe("Renamed");
    expect(body.raw).toBe(true);
  });

  it("rejects raw:false, conversion without html, and invalid replacement HTML", async () => {
    const created = await createOne("Original");
    const rawFalse = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ raw: false }),
    });
    expect((await handleUpdate(db, publicUrl, created.id, rawFalse)).status).toBe(400);
    const convert = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ raw: true }),
    });
    const convertRes = await handleUpdate(db, publicUrl, created.id, convert);
    expect(convertRes.status).toBe(400);
    expect(((await convertRes.json()) as { error: string }).error).toBe("raw:true requires html");
    const invalid = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ html: "<p>nope</p>" }),
    });
    expect((await handleUpdate(db, publicUrl, created.id, invalid)).status).toBe(400);
  });

  it("keeps legacy themed metadata-only updates themed and converts on html replacement", async () => {
    const legacy = await seedThemed();
    const meta = new Request(`http://control.localhost/api/pages/${legacy.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Still themed" }),
    });
    const metaRes = await handleUpdate(db, publicUrl, legacy.id, meta);
    expect(metaRes.status).toBe(200);
    expect(((await metaRes.json()) as { raw: boolean; title: string }).raw).toBe(false);

    const next = validRawDocument("<p>new</p>", "Converted");
    const replace = new Request(`http://control.localhost/api/pages/${legacy.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ html: next }),
    });
    const replaceRes = await handleUpdate(db, publicUrl, legacy.id, replace);
    expect(replaceRes.status).toBe(200);
    const body = (await replaceRes.json()) as { raw: boolean };
    expect(body.raw).toBe(true);
  });

  it("keeps an already-raw page raw when replacing html without sending raw", async () => {
    const created = await createOne("Original");
    const next = validRawDocument("<p>replaced</p>", "Original");
    const replace = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ html: next }),
    });
    const res = await handleUpdate(db, publicUrl, created.id, replace);
    expect(res.status).toBe(200);
    expect(((await res.json()) as { raw: boolean }).raw).toBe(true);
  });

  it("conditionally updates with authoritative metadata and rejects a stale timestamp", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00.000Z"));
    const created = await createOne("Original");
    const first = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Fresh", if_updated_at: created.updated_at }),
    });
    const updated = await handleUpdate(db, publicUrl, created.id, first);
    expect(updated.status).toBe(200);
    const updatedBody = (await updated.json()) as { title: string; updated_at: string };
    expect(updatedBody.title).toBe("Fresh");
    expect(updatedBody.updated_at).toBe("2026-01-01T00:00:00.001Z");

    const stale = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "Stale", if_updated_at: created.updated_at }),
    });
    expect((await handleUpdate(db, publicUrl, created.id, stale)).status).toBe(409);
    const meta = (await (await handleGetMeta(db, publicUrl, created.id)).json()) as { title: string };
    expect(meta.title).toBe("Fresh");
    vi.useRealTimers();
  });

  it("rejects wrong-typed fields on update with 400 and leaves the page untouched", async () => {
    const created = await createOne("Original");
    const wrongTyped: unknown[] = [{ ttl_days: "7" }, { raw: "false" }, { title: 123 }];
    for (const body of wrongTyped) {
      const putReq = new Request(`http://control.localhost/api/pages/${created.id}`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });
      const res = await handleUpdate(db, publicUrl, created.id, putReq);
      expect(res.status).toBe(400);
      const err = (await res.json()) as { error: string };
      expect(err.error).toContain("invalid JSON");
    }
    const meta = (await (await handleGetMeta(db, publicUrl, created.id)).json()) as {
      title: string;
      raw: boolean;
      expires_at?: string;
    };
    expect(meta.title).toBe("Original");
    expect(meta.raw).toBe(true);
    expect(meta.expires_at).toBeUndefined(); // ttl_days: "7" must not clear/set expiry via NaN
  });

  it("rejects clearing the title or html to empty", async () => {
    const created = await createOne();
    const blankTitle = new Request(`http://control.localhost/api/pages/${created.id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title: "  " }),
    });
    expect((await handleUpdate(db, publicUrl, created.id, blankTitle)).status).toBe(400);
  });

  it("deletes a page and reports not-found afterward", async () => {
    const created = await createOne();
    const res = await handleDelete(db, created.id);
    expect(res.status).toBe(200);
    expect((await handleGetMeta(db, publicUrl, created.id)).status).toBe(404);
    expect((await handleDelete(db, created.id)).status).toBe(404);
  });
});
