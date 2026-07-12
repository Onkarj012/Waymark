// Hashing / token primitives. Ports internal/web/auth.go's crypto helpers
// onto Web Crypto (available in both the Workers runtime and Node >= 19,
// which is what lets these run under plain vitest without workerd).

const textEncoder = new TextEncoder();

function toHex(bytes: ArrayBuffer): string {
  return Array.from(new Uint8Array(bytes))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

function base64Url(bytes: Uint8Array): string {
  let binary = "";
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function base64UrlDecode(value: string): Uint8Array | null {
  try {
    const padded = value.replace(/-/g, "+").replace(/_/g, "/");
    const pad = padded.length % 4 === 0 ? "" : "=".repeat(4 - (padded.length % 4));
    const binary = atob(padded + pad);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
  } catch {
    return null;
  }
}

/** sha256(value) as lowercase hex — the "high entropy" hash used for device
 * codes, device secrets, and API tokens (values with enough entropy that a
 * plain hash is an adequate deterrent against offline guessing). */
export async function hashHighEntropy(value: string): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", textEncoder.encode(value));
  return toHex(digest);
}

/** HMAC-SHA256(adminPasscode, value) as lowercase hex — the "low entropy"
 * hash used for user codes and admin session cookies, where keying the hash
 * with the admin passcode blocks offline brute force of the short values. */
export async function hashLowEntropy(adminPasscode: string, value: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    "raw",
    textEncoder.encode(adminPasscode),
    { name: "HMAC", hash: "SHA-256" },
    false,
    ["sign"],
  );
  const sig = await crypto.subtle.sign("HMAC", key, textEncoder.encode(value));
  return toHex(sig);
}

/** Compares two secrets by hashing both sides first, then comparing every
 * byte (no early return), mirroring the Go server's constantTimeSecretEqual. */
export async function constantTimeSecretEqual(left: string, right: string): Promise<boolean> {
  const [a, b] = await Promise.all([
    crypto.subtle.digest("SHA-256", textEncoder.encode(left)),
    crypto.subtle.digest("SHA-256", textEncoder.encode(right)),
  ]);
  const av = new Uint8Array(a);
  const bv = new Uint8Array(b);
  let diff = av.length ^ bv.length;
  for (let i = 0; i < Math.max(av.length, bv.length); i++) {
    diff |= (av[i] ?? 0) ^ (bv[i] ?? 0);
  }
  return diff === 0;
}

/** Constant-shape (not early-exiting) string compare for already-hashed hex
 * values, used for CSRF token comparison. */
export function constantTimeHexEqual(want: string, got: string): boolean {
  if (want.length !== got.length) return false;
  let diff = 0;
  for (let i = 0; i < want.length; i++) diff |= want.charCodeAt(i) ^ got.charCodeAt(i);
  return diff === 0;
}

export function randomBase64Url(size: number): string {
  const bytes = new Uint8Array(size);
  crypto.getRandomValues(bytes);
  return base64Url(bytes);
}

/** Returns the byte length of a base64url string without allocating, or -1 if invalid. */
export function base64UrlByteLength(value: string): number {
  const decoded = base64UrlDecode(value);
  return decoded ? decoded.length : -1;
}

const USER_CODE_ALPHABET = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"; // 32 chars — 256/32 divides evenly, no modulo bias

export function randomUserCode(): string {
  const random = new Uint8Array(8);
  crypto.getRandomValues(random);
  let out = "";
  for (let i = 0; i < 8; i++) {
    if (i === 4) out += "-";
    out += USER_CODE_ALPHABET[random[i]! % USER_CODE_ALPHABET.length];
  }
  return out;
}

/** Returns { raw, id, displayPrefix } for a new API token, mirroring
 * newAPIToken() in auth.go: id = 9 random bytes b64url, secret = 32 random
 * bytes b64url, prefix = "waymark_" + id, raw = prefix + "." + secret. */
export function newApiToken(): { raw: string; id: string; displayPrefix: string } {
  const id = randomBase64Url(9);
  const secret = randomBase64Url(32);
  const displayPrefix = "waymark_" + id;
  return { raw: `${displayPrefix}.${secret}`, id, displayPrefix };
}

export function normalizeUserCode(value: string): string {
  return value.trim().toUpperCase().replaceAll("-", "");
}
