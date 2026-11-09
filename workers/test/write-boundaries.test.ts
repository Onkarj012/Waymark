import { beforeEach, describe, expect, it } from "vitest";
import { MAX_BODY_BYTES } from "../src/body";
import { handleCreate, handleUpdate } from "../src/pages-api";
import { createPage, getPage, listPages } from "../src/store";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import type { Page } from "../src/types";

let db: FakeD1;
const publicUrl = "http://pages.localhost";
const maximum = 1_000_000;
const metadataMaximum = 1_024;

beforeEach(() => {
  db = createFakeD1();
});

function document(inner: string): string {
  return `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Report</title><style>body{margin:0}</style></head><body>${inner}</body></html>`;
}

function documentWithBytes(size: number, pattern: string): string {
  const remaining = size - Buffer.byteLength(document(""));
  const patternBytes = Buffer.byteLength(pattern);
  return document(pattern.repeat(Math.floor(remaining / patternBytes)) + "a".repeat(remaining % patternBytes));
}

function request(body: string | Uint8Array): Request {
  return new Request("http://control.localhost/api/pages", {
    method: "POST",
    body,
    headers: { "Content-Type": "application/json" },
  });
}

function legacy(overrides: Partial<Page> = {}): Page {
  return {
    id: "legacy", title: "Original", slug: "", html: "<p>legacy</p>", raw: false,
    createdAt: "2026-11-09T09:00:00.120Z", updatedAt: "2026-11-09T09:01:00.120Z", expiresAt: null,
    ...overrides,
  };
}

describe("page write boundaries", () => {
  it.each(["a", "é", '"\\', "\x01"])("measures decoded HTML UTF-8 bytes for %j at the limit and plus one", async (pattern) => {
    for (const size of [maximum, maximum + 1]) {
      const html = documentWithBytes(size, pattern);
      expect(Buffer.byteLength(html)).toBe(size);
      const body = JSON.stringify({ title: "Report", raw: true, html });
      expect(Buffer.byteLength(body)).toBeLessThan(MAX_BODY_BYTES);
      const countBefore = (await listPages(db, 0)).length;
      const created = await handleCreate(db, publicUrl, request(body));
      expect(created.status).toBe(size === maximum ? 201 : 413);
      if (size > maximum) {
        expect(await created.json()).toEqual({ error: "html exceeds maximum of 1000000 UTF-8 bytes" });
        expect(await listPages(db, 0)).toHaveLength(countBefore);
      } else {
        const pages = await listPages(db, 0);
        expect(await getPage(db, pages[0]!.id)).toMatchObject({ html, raw: true });
      }

      const original = legacy({ id: `legacy-${size}` });
      await createPage(db, original);
      const updated = await handleUpdate(db, publicUrl, original.id, request(body));
      expect(updated.status).toBe(size === maximum ? 200 : 413);
      if (size > maximum) {
        expect(await updated.json()).toEqual({ error: "html exceeds maximum of 1000000 UTF-8 bytes" });
        expect(await getPage(db, original.id)).toEqual(original);
      } else {
        expect(await getPage(db, original.id)).toMatchObject({ html, raw: true });
      }
    }
  });

  it.each(["title", "slug"])("bounds newly supplied %s after trimming by UTF-8 byte count", async (field) => {
    for (const size of [metadataMaximum, metadataMaximum + 1]) {
      const value = "é".repeat(Math.floor(size / 2)) + "a".repeat(size % 2);
      const created = await handleCreate(db, publicUrl, request(JSON.stringify({
        title: "Report", html: document("<p>Hi</p>"), raw: true, [field]: `  ${value}  `,
      })));
      expect(created.status).toBe(size === metadataMaximum ? 201 : 413);
      const original = legacy({ id: `legacy-${size}` });
      await createPage(db, original);
      const updated = await handleUpdate(db, publicUrl, original.id, request(JSON.stringify({ [field]: `  ${value}  ` })));
      expect(updated.status).toBe(size === metadataMaximum ? 200 : 413);
      if (size > metadataMaximum) {
        const error = { error: `${field} exceeds maximum of 1024 UTF-8 bytes` };
        expect(await created.json()).toEqual(error);
        expect(await updated.json()).toEqual(error);
        expect(await getPage(db, original.id)).toEqual(original);
      } else {
        expect(await getPage(db, original.id)).toMatchObject({ [field]: value });
      }
    }
  });

  it("rejects invalid request bytes and escaped unpaired surrogates before storage or legacy conversion", async () => {
    const original = legacy();
    await createPage(db, original);
    for (const invalid of ["\ud800", "\udc00", "\ud800x", "\ud800\ud800"]) {
      for (const field of ["title", "slug", "html"]) {
        const payload = { title: "Report", slug: "report", html: document("<p>Hi</p>"), raw: true, [field]: field === "html" ? document(invalid) : invalid };
        const body = JSON.stringify(payload);
        for (const response of [
          await handleCreate(db, publicUrl, request(body)),
          await handleUpdate(db, publicUrl, original.id, request(body)),
        ]) {
          expect(response.status).toBe(400);
          expect(await response.json()).toEqual({ error: "invalid JSON: invalid UTF-8 or unpaired Unicode surrogate" });
        }
      }
    }
    const body = JSON.stringify({ title: "Report", html: document("UNICODE"), raw: true });
    for (const bytes of [new Uint8Array([0xff]), new Uint8Array([0xed, 0xa0, 0x80])]) {
      const [prefix, suffix] = body.split("UNICODE");
      const invalid = Buffer.concat([Buffer.from(prefix!), bytes, Buffer.from(suffix!)]);
      for (const response of [
        await handleCreate(db, publicUrl, request(invalid)),
        await handleUpdate(db, publicUrl, original.id, request(invalid)),
      ]) {
        expect(response.status).toBe(400);
        expect(await response.json()).toEqual({ error: "invalid JSON: invalid UTF-8 or unpaired Unicode surrogate" });
      }
    }
    expect(await getPage(db, original.id)).toEqual(original);
    expect(await listPages(db, 0)).toHaveLength(1);
  });

  it.each(["🌍", "\\ud800", "�", "é e\u0301"])("preserves valid Unicode and literal escapes %j", async (value) => {
    const html = document(value);
    const created = await handleCreate(db, publicUrl, request(JSON.stringify({ title: "Report", raw: true, html })));
    expect(created.status).toBe(201);
    const pages = await listPages(db, 0);
    expect(await getPage(db, pages[0]!.id)).toMatchObject({ html });
  });

  it("allows metadata-only updates without revalidating oversized legacy HTML or unchanged metadata", async () => {
    const original = legacy({ html: documentWithBytes(maximum + 1, "é"), title: "t".repeat(metadataMaximum + 1), slug: "s".repeat(metadataMaximum + 1) });
    await createPage(db, original);
    const response = await handleUpdate(db, publicUrl, original.id, request('{"ttl_days":0}'));
    expect(response.status).toBe(200);
    expect(await getPage(db, original.id)).toMatchObject({
      html: original.html, title: original.title, slug: original.slug, raw: false,
    });
  });

  it("keeps the independent 8 MiB transport limit for streamed requests without Content-Length", async () => {
    const stream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new Uint8Array(MAX_BODY_BYTES));
        controller.enqueue(new Uint8Array(1));
        controller.close();
      },
    });
    const init = { method: "POST", body: stream, duplex: "half" };
    const streamed = new Request("http://control.localhost/api/pages", init);
    expect(streamed.headers.get("Content-Length")).toBeNull();
    const response = await handleCreate(db, publicUrl, streamed);
    expect(response.status).toBe(413);
    expect(await listPages(db, 0)).toHaveLength(0);
  });
});
