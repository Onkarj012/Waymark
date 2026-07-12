// Bounded body reading + strict JSON decoding, mirroring server.go's
// `http.MaxBytesReader` + `json.Decoder.DisallowUnknownFields()` combo used
// by Server.decode().

export const MAX_BODY_BYTES = 8 << 20; // 8 MiB, same cap as the Go server

export class BodyTooLargeError extends Error {
  constructor() {
    super("request body too large");
  }
}

export async function readBodyText(request: Request, maxBytes = MAX_BODY_BYTES): Promise<string> {
  if (!request.body) return "";
  const reader = request.body.getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    if (value) {
      total += value.byteLength;
      if (total > maxBytes) {
        await reader.cancel();
        throw new BodyTooLargeError();
      }
      chunks.push(value);
    }
  }
  const merged = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    merged.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(merged);
}

/** Parses JSON and rejects any key not in `allowedKeys` — the equivalent of
 * Go's dec.DisallowUnknownFields(). Returns { ok: false } with a message on
 * any failure so callers can respond 400 the way Server.decode() does. */
export function decodeStrict<T extends Record<string, unknown>>(
  text: string,
  allowedKeys: readonly string[],
): { ok: true; value: T } | { ok: false; message: string } {
  let parsed: unknown;
  try {
    parsed = text.trim() === "" ? {} : JSON.parse(text);
  } catch (err) {
    return { ok: false, message: `invalid JSON: ${(err as Error).message}` };
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { ok: false, message: "invalid JSON: expected an object" };
  }
  const allowed = new Set(allowedKeys);
  for (const key of Object.keys(parsed)) {
    if (!allowed.has(key)) {
      return { ok: false, message: `invalid JSON: unknown field "${key}"` };
    }
  }
  return { ok: true, value: parsed as T };
}

/** True when `key` is present in `data` and not explicitly `null` — mirrors
 * how a Go `*T` struct field distinguishes "absent" from "set" (JSON `null`
 * unmarshals a pointer field to nil, same as omission). */
export function isSet(data: Record<string, unknown>, key: string): boolean {
  return key in data && data[key] !== null;
}

export type FieldType = "string" | "boolean" | "integer";

/** Checks that every present, non-null field matches its declared JSON type —
 * the equivalent of encoding/json returning an *UnmarshalTypeError* (which
 * Server.decode() turns into a 400) when a request field carries the wrong
 * type. JSON `null` passes: Go leaves a non-pointer field at its zero value
 * and a pointer field nil, both without error. Returns the 400 error message
 * for the first mismatch, or null when everything checks out. */
export function checkFieldTypes(
  data: Record<string, unknown>,
  types: Readonly<Record<string, FieldType>>,
): string | null {
  for (const [key, expected] of Object.entries(types)) {
    if (!isSet(data, key)) continue;
    const value = data[key];
    const ok =
      expected === "string"
        ? typeof value === "string"
        : expected === "boolean"
          ? typeof value === "boolean"
          : typeof value === "number" && Number.isInteger(value);
    if (!ok) {
      const article = expected === "integer" ? "an integer" : `a ${expected}`;
      return `invalid JSON: field "${key}" must be ${article}`;
    }
  }
  return null;
}
