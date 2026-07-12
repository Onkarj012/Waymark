import { beforeEach, describe, expect, it } from "vitest";
import contentWorker from "../src/content";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import { CONTROL_HOST, PUBLIC_HOST, makeEnv, request } from "./test-env";
import type { BaseEnv } from "../src/types";

let db: FakeD1;
let env: BaseEnv;

beforeEach(() => {
  db = createFakeD1();
  env = makeEnv(db);
});

describe("host gating", () => {
  it("serves /healthz regardless of host", async () => {
    const res = await contentWorker.fetch(request("unknown.localhost", "/healthz"), env);
    expect(res.status).toBe(200);
  });

  it("rejects content routes on the wrong host with 421", async () => {
    const res = await contentWorker.fetch(request(CONTROL_HOST, "/theme.css"), env);
    expect(res.status).toBe(421);
  });

  it("rejects a route not in the content worker's allow-list even on the right host", async () => {
    const res = await contentWorker.fetch(request(PUBLIC_HOST, "/admin/login"), env);
    expect(res.status).toBe(421);
  });
});

describe("GET /theme.css", () => {
  it("serves the embedded house theme verbatim, byte-for-byte the same as theme/theme.css", async () => {
    const res = await contentWorker.fetch(request(PUBLIC_HOST, "/theme.css"), env);
    expect(res.status).toBe(200);
    expect(res.headers.get("Content-Type")).toBe("text/css; charset=utf-8");
    const css = await res.text();
    expect(css).toContain(".theme-toggle");
    expect(css).toContain(":root[data-theme=\"dark\"]");
  });
});

describe("GET /", () => {
  it("returns the plain-text index", async () => {
    const res = await contentWorker.fetch(request(PUBLIC_HOST, "/"), env);
    expect(res.status).toBe(200);
    expect(await res.text()).toBe("Waymark\n");
  });
});

async function seedPage(overrides: Record<string, unknown> = {}): Promise<{ id: string; url: string }> {
  // Seeds a page directly through the store layer (shared with the control
  // worker's own persistence) since this suite is about content-worker
  // *serving* behavior, not the auth/create flow already covered by
  // control.test.ts and store.test.ts.
  const { createPage, nowIso } = await import("../src/store");
  const { newId } = await import("../src/id");
  const now = nowIso();
  const id = newId();
  await createPage(db, {
    id,
    title: "Status <check>",
    slug: "",
    html: "<script>window.demo=true</script><p>Hello</p>",
    raw: false,
    createdAt: now,
    updatedAt: now,
    expiresAt: null,
    ...overrides,
  });
  return { id, url: `http://${PUBLIC_HOST}/p/${id}` };
}

describe("GET /p/{id}", () => {
  it("wraps themed page HTML, escapes the title, and sets the safe-serving headers", async () => {
    const page = await seedPage();
    const res = await contentWorker.fetch(request(PUBLIC_HOST, `/p/${page.id}`), env);
    expect(res.status).toBe(200);
    const html = await res.text();
    expect(html).toContain("<title>Status &lt;check&gt;</title>");
    expect(html).toContain("<script>window.demo=true</script>");
    expect(res.headers.get("Referrer-Policy")).toBe("no-referrer");
    expect(res.headers.get("X-Content-Type-Options")).toBe("nosniff");
  });

  it("serves a raw page verbatim with no theme wrapping", async () => {
    const page = await seedPage({ raw: true, html: "<!doctype html><html><body>raw</body></html>" });
    const res = await contentWorker.fetch(request(PUBLIC_HOST, `/p/${page.id}`), env);
    const html = await res.text();
    expect(html).toBe("<!doctype html><html><body>raw</body></html>");
  });

  it("404s for a missing page", async () => {
    const res = await contentWorker.fetch(request(PUBLIC_HOST, "/p/does-not-exist"), env);
    expect(res.status).toBe(404);
  });

  it("404s for an expired page instead of serving it", async () => {
    const page = await seedPage({ expiresAt: "2020-01-01T00:00:00.000Z" });
    const res = await contentWorker.fetch(request(PUBLIC_HOST, `/p/${page.id}`), env);
    expect(res.status).toBe(404);
  });
});

describe("discovery", () => {
  it("is also reachable from the content host", async () => {
    const res = await contentWorker.fetch(request(PUBLIC_HOST, "/.well-known/waymark"), env);
    expect(res.status).toBe(200);
  });
});
