// Ports the page CRUD handlers from internal/web/server.go
// (handleCreate/handleList/handleGetMeta/handleUpdate/handleDelete) plus
// their request/response shapes (createReq/updateReq/pageResp) and
// ttlToExpiry(). This is the surface the `waymark` CLI drives directly.

import { BodyTooLargeError, checkFieldTypes, decodeStrict, isSet, readBodyText, type FieldType } from "./body";
import { errJson, json } from "./http";
import { newId } from "./id";
import { ConflictError, NotFoundError, type D1Like, type Page, type PageMeta } from "./types";
import { createPage, deletePage, getPage, listPages, nowIso, savePage, savePageIfUpdatedAt } from "./store";

export interface PageResp {
  id: string;
  url: string;
  title: string;
  slug?: string;
  raw: boolean;
  created_at: string;
  updated_at: string;
  expires_at?: string | null;
  size?: number;
}

function pageUrl(publicUrl: string, id: string): string {
  return `${publicUrl}/p/${id}`;
}

function toResp(publicUrl: string, p: Page, size: number): PageResp {
  return {
    id: p.id,
    url: pageUrl(publicUrl, p.id),
    title: p.title,
    slug: p.slug || undefined,
    raw: p.raw,
    created_at: p.createdAt,
    updated_at: p.updatedAt,
    expires_at: p.expiresAt ?? undefined,
    size,
  };
}

function metaToResp(publicUrl: string, m: PageMeta): PageResp {
  return {
    id: m.id,
    url: pageUrl(publicUrl, m.id),
    title: m.title,
    slug: m.slug || undefined,
    raw: m.raw,
    created_at: m.createdAt,
    updated_at: m.updatedAt,
    expires_at: m.expiresAt ?? undefined,
    size: m.size,
  };
}

function nextUpdatedAt(now: string, current: string): string {
  if (Date.parse(now) > Date.parse(current)) return now;
  return new Date(Date.parse(current) + 1).toISOString();
}

/** ttlToExpiry: converts a TTL in days to an absolute ISO expiry, or null if
 * days <= 0 (never expires). Mirrors ttlToExpiry() in server.go. */
export function ttlToExpiry(now: string, days: number | undefined): string | null {
  if (!days || days <= 0) return null;
  return new Date(new Date(now).getTime() + days * 24 * 60 * 60 * 1000).toISOString();
}

/** Field types shared by createReq and updateReq in server.go: encoding/json
 * rejects a wrong-typed field with an error (→ 400), so type mismatches must
 * never be silently coerced. */
const PAGE_FIELD_TYPES: Readonly<Record<string, FieldType>> = {
  title: "string",
  slug: "string",
  html: "string",
  raw: "boolean",
  ttl_days: "integer",
  if_updated_at: "string",
};

const PAGE_KEYS = ["title", "slug", "html", "raw", "ttl_days"] as const;

async function readPageReq(request: Request, allowPrecondition = false) {
  let text: string;
  try {
    text = await readBodyText(request);
  } catch (err) {
    if (err instanceof BodyTooLargeError) return { ok: false as const, status: 413, message: "request body too large" };
    throw err;
  }
  const allowedKeys = allowPrecondition ? [...PAGE_KEYS, "if_updated_at"] : PAGE_KEYS;
  const decoded = decodeStrict<Record<string, unknown>>(text, allowedKeys);
  if (!decoded.ok) return { ok: false as const, status: 400, message: decoded.message };
  const typeError = checkFieldTypes(decoded.value, PAGE_FIELD_TYPES);
  if (typeError) return { ok: false as const, status: 400, message: typeError };
  return { ok: true as const, value: decoded.value };
}

export async function handleCreate(db: D1Like, publicUrl: string, request: Request): Promise<Response> {
  const decoded = await readPageReq(request);
  if (!decoded.ok) return errJson(decoded.status, decoded.message);
  const data = decoded.value;

  // readPageReq guarantees types; a JSON null falls back to the Go zero value.
  const title = (typeof data.title === "string" ? data.title : "").trim();
  if (!title) return errJson(400, "title is required");
  const html = typeof data.html === "string" ? data.html : "";
  if (!html.trim()) return errJson(400, "html is required");

  const now = nowIso();
  const page: Page = {
    id: "",
    title,
    slug: (typeof data.slug === "string" ? data.slug : "").trim(),
    html,
    raw: data.raw === true,
    createdAt: now,
    updatedAt: now,
    expiresAt: ttlToExpiry(now, typeof data.ttl_days === "number" ? data.ttl_days : undefined),
  };

  // Generate a unique id (collisions are astronomically unlikely; retry anyway).
  let created = false;
  for (let attempt = 0; attempt < 5 && !created; attempt++) {
    page.id = newId();
    try {
      await createPage(db, page);
      created = true;
    } catch {
      // retry with a new id (likely a rare PK collision)
    }
  }
  if (!created) return errJson(500, "could not save page");

  return json(201, toResp(publicUrl, page, page.html.length));
}

export async function handleList(db: D1Like, publicUrl: string, request: Request): Promise<Response> {
  const url = new URL(request.url);
  let limit = 50;
  const raw = url.searchParams.get("limit");
  if (raw !== null && raw !== "") {
    const parsed = Number(raw);
    if (Number.isInteger(parsed) && parsed >= 0) limit = parsed;
  }
  const metas = await listPages(db, limit);
  return json(200, { pages: metas.map((m) => metaToResp(publicUrl, m)) });
}

export async function handleGetMeta(
  db: D1Like,
  publicUrl: string,
  id: string,
): Promise<Response> {
  try {
    const page = await getPage(db, id);
    return json(200, toResp(publicUrl, page, page.html.length));
  } catch (err) {
    if (err instanceof NotFoundError) return errJson(404, "page not found");
    return errJson(500, "could not load page");
  }
}

export async function handleUpdate(
  db: D1Like,
  publicUrl: string,
  id: string,
  request: Request,
): Promise<Response> {
  const decoded = await readPageReq(request, true);
  if (!decoded.ok) return errJson(decoded.status, decoded.message);
  const data = decoded.value;

  let expectedUpdatedAt: string | undefined;
  if (isSet(data, "if_updated_at")) {
    const value = data.if_updated_at as string;
    if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) || Number.isNaN(Date.parse(value))) {
      return errJson(400, 'invalid JSON: field "if_updated_at" must be an RFC3339 timestamp');
    }
    expectedUpdatedAt = new Date(value).toISOString();
  }

  let page: Page;
  try {
    page = await getPage(db, id);
  } catch (err) {
    if (err instanceof NotFoundError) return errJson(404, "page not found");
    return errJson(500, "could not load page");
  }

  // readPageReq guarantees every set field carries the right type, mirroring
  // Go's updateReq pointer fields: present means typed value, null/absent
  // means "leave unchanged".
  if (isSet(data, "title")) {
    const t = (data.title as string).trim();
    if (!t) return errJson(400, "title cannot be empty");
    page.title = t;
  }
  if (isSet(data, "slug")) {
    page.slug = (data.slug as string).trim();
  }
  if (isSet(data, "html")) {
    const h = data.html as string;
    if (!h.trim()) return errJson(400, "html cannot be empty");
    page.html = h;
  }
  if (isSet(data, "raw")) {
    page.raw = data.raw as boolean;
  }
  const now = nextUpdatedAt(nowIso(), page.updatedAt);
  if (isSet(data, "ttl_days")) {
    page.expiresAt = ttlToExpiry(now, data.ttl_days as number);
  }
  page.updatedAt = now;

  try {
    if (expectedUpdatedAt === undefined) await savePage(db, page);
    else await savePageIfUpdatedAt(db, page, expectedUpdatedAt);
  } catch (err) {
    if (err instanceof ConflictError) return errJson(409, "page was updated by someone else");
    if (err instanceof NotFoundError) return errJson(404, "page not found");
    return errJson(500, "could not save page");
  }
  return json(200, toResp(publicUrl, page, page.html.length));
}

export async function handleDelete(db: D1Like, id: string): Promise<Response> {
  try {
    await deletePage(db, id);
  } catch (err) {
    if (err instanceof NotFoundError) return errJson(404, "page not found");
    return errJson(500, "could not delete page");
  }
  return json(200, { id, deleted: true });
}
