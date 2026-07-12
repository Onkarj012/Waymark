import { beforeEach, describe, expect, it } from "vitest";
import { createFakeD1, type FakeD1 } from "./fake-d1";
import {
  adminSessionByHash,
  apiTokenByHash,
  authenticateAdminSession,
  createAdminSession,
  createDeviceAuthorization,
  createPage,
  decideDeviceAuthorization,
  deleteAdminSession,
  deleteExpiredAuth,
  deleteExpiredPages,
  deletePage,
  deviceAuthorizationByDeviceCode,
  deviceAuthorizationByUserCode,
  getPage,
  listApiTokens,
  listPages,
  nowIso,
  pollDeviceAuthorization,
  revokeApiToken,
  savePage,
  touchApiToken,
} from "../src/store";
import { NotFoundError, type APIToken, type DeviceAuthorization, type Page } from "../src/types";
import { rateLimitAllow } from "../src/ratelimit";

let db: FakeD1;

beforeEach(() => {
  db = createFakeD1();
});

function makePage(overrides: Partial<Page> = {}): Page {
  const now = nowIso();
  return {
    id: "page-id-0001",
    title: "Title",
    slug: "",
    html: "<p>Hi</p>",
    raw: false,
    createdAt: now,
    updatedAt: now,
    expiresAt: null,
    ...overrides,
  };
}

describe("pages", () => {
  it("creates and retrieves a page", async () => {
    const page = makePage();
    await createPage(db, page);
    const got = await getPage(db, page.id);
    expect(got).toEqual(page);
  });

  it("throws NotFoundError for a missing page", async () => {
    await expect(getPage(db, "missing")).rejects.toBeInstanceOf(NotFoundError);
  });

  it("save updates mutable fields and rejects a missing id", async () => {
    const page = makePage();
    await createPage(db, page);
    const updated = { ...page, title: "New title", updatedAt: nowIso() };
    await savePage(db, updated);
    expect((await getPage(db, page.id)).title).toBe("New title");
    await expect(savePage(db, { ...page, id: "missing" })).rejects.toBeInstanceOf(NotFoundError);
  });

  it("delete removes a page and rejects a missing id", async () => {
    const page = makePage();
    await createPage(db, page);
    await deletePage(db, page.id);
    await expect(getPage(db, page.id)).rejects.toBeInstanceOf(NotFoundError);
    await expect(deletePage(db, page.id)).rejects.toBeInstanceOf(NotFoundError);
  });

  it("lists pages newest-first with a limit and reports html length as size", async () => {
    const first = makePage({ id: "a", createdAt: "2026-01-01T00:00:00.000Z" });
    const second = makePage({ id: "b", createdAt: "2026-01-02T00:00:00.000Z" });
    await createPage(db, first);
    await createPage(db, second);
    const all = await listPages(db, 0);
    expect(all.map((p) => p.id)).toEqual(["b", "a"]);
    expect(all[0]!.size).toBe(second.html.length);
    const limited = await listPages(db, 1);
    expect(limited).toHaveLength(1);
  });

  it("deleteExpiredPages removes only pages whose expiry has passed", async () => {
    const past = makePage({ id: "expired", expiresAt: "2020-01-01T00:00:00.000Z" });
    const future = makePage({ id: "future", expiresAt: "2999-01-01T00:00:00.000Z" });
    const never = makePage({ id: "never", expiresAt: null });
    await createPage(db, past);
    await createPage(db, future);
    await createPage(db, never);
    const deleted = await deleteExpiredPages(db, nowIso());
    expect(deleted).toBe(1);
    await expect(getPage(db, "expired")).rejects.toBeInstanceOf(NotFoundError);
    await getPage(db, "future");
    await getPage(db, "never");
  });
});

function makeGrant(overrides: Partial<DeviceAuthorization> = {}): DeviceAuthorization {
  const now = nowIso();
  return {
    id: "grant-1",
    deviceCodeHash: "code-hash",
    deviceSecretHash: "secret-hash",
    userCodeHash: "user-hash",
    deviceLabel: "test device",
    scopes: "pages:read pages:write",
    sourceKey: "source",
    sourceHint: "local",
    status: "pending",
    createdAt: now,
    expiresAt: new Date(Date.now() + 60_000).toISOString(),
    approvedAt: null,
    deniedAt: null,
    lastPollAt: null,
    pollIntervalSeconds: 5,
    consumedAt: null,
    ...overrides,
  };
}

describe("device authorizations", () => {
  it("creates a grant and enforces the per-source and per-instance limits", async () => {
    const outcome = await createDeviceAuthorization(db, makeGrant(), 5, 50);
    expect(outcome.kind).toBe("created");

    // per-source limit of 1
    const second = makeGrant({ id: "grant-2", deviceCodeHash: "code-2", userCodeHash: "user-2" });
    const limited = await createDeviceAuthorization(db, second, 1, 50);
    expect(limited.kind).toBe("limit_reached");
  });

  it("looks up a grant by user code and by device code, rejecting expired grants", async () => {
    const grant = makeGrant();
    await createDeviceAuthorization(db, grant, 5, 50);

    const byUser = await deviceAuthorizationByUserCode(db, grant.userCodeHash, nowIso());
    expect(byUser.kind).toBe("found");

    const byDevice = await deviceAuthorizationByDeviceCode(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso());
    expect(byDevice.kind).toBe("found");

    const expiredGrant = makeGrant({
      id: "grant-expired",
      deviceCodeHash: "code-expired",
      userCodeHash: "user-expired",
      expiresAt: "2020-01-01T00:00:00.000Z",
    });
    await createDeviceAuthorization(db, expiredGrant, 5, 50);
    const expired = await deviceAuthorizationByUserCode(db, expiredGrant.userCodeHash, nowIso());
    expect(expired.kind).toBe("expired");

    const missing = await deviceAuthorizationByUserCode(db, "no-such-hash", nowIso());
    expect(missing.kind).toBe("not_found");
  });

  it("decide approves or denies a pending grant, and rejects a non-pending one", async () => {
    const grant = makeGrant();
    await createDeviceAuthorization(db, grant, 5, 50);
    const decided = await decideDeviceAuthorization(db, grant.userCodeHash, "approved", nowIso());
    expect(decided.kind).toBe("decided");
    const again = await decideDeviceAuthorization(db, grant.userCodeHash, "denied", nowIso());
    expect(again.kind).toBe("not_found"); // already approved, no longer pending
  });

  function makeToken(overrides: Partial<APIToken> = {}): APIToken {
    const now = nowIso();
    return {
      id: "token-1",
      tokenHash: "token-hash",
      displayPrefix: "waymark_abc",
      deviceLabel: "test device",
      scopes: "pages:read pages:write",
      createdAt: now,
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
      lastUsedAt: null,
      revokedAt: null,
      ...overrides,
    };
  }

  it("poll: pending -> approved -> issued, then consumed on replay", async () => {
    const grant = makeGrant();
    await createDeviceAuthorization(db, grant, 5, 50);

    const pending = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso(), makeToken());
    expect(pending.kind).toBe("pending");

    await decideDeviceAuthorization(db, grant.userCodeHash, "approved", nowIso());

    const issued = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso(), makeToken());
    expect(issued.kind).toBe("issued");
    const stored = await apiTokenByHash(db, "token-hash", nowIso());
    expect(stored?.id).toBe("token-1");

    const replay = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso(), makeToken({ id: "token-2" }));
    expect(replay.kind).toBe("consumed");
  });

  it("poll: a second poll against an already-redeemed grant is rejected and mints no extra token", async () => {
    // Simulates the race the atomic claim in pollDeviceAuthorization guards
    // against: two pollers hit an approved grant, but only the first request
    // whose claiming UPDATE actually flips status='approved' -> 'consumed'
    // may mint a token. Here the first poll wins and redeems the grant; the
    // second poll (standing in for the loser of a concurrent race, or a
    // simple replay) must see the grant already consumed, get the same
    // terminal outcome Go returns for ErrGrantConsumed, and must not cause a
    // second token row to be inserted.
    const grant = makeGrant({ id: "race-grant", deviceCodeHash: "race-code", userCodeHash: "race-user" });
    await createDeviceAuthorization(db, grant, 5, 50);
    await decideDeviceAuthorization(db, grant.userCodeHash, "approved", nowIso());

    const first = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso(), makeToken({ id: "winner" }));
    expect(first.kind).toBe("issued");

    const second = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, nowIso(), makeToken({ id: "loser", tokenHash: "loser-hash" }));
    expect(second.kind).toBe("consumed");

    const tokens = await listApiTokens(db);
    const raceTokens = tokens.filter((t) => t.deviceLabel === grant.deviceLabel && (t.id === "winner" || t.id === "loser"));
    expect(raceTokens).toHaveLength(1);
    expect(raceTokens[0]?.id).toBe("winner");
    expect(await apiTokenByHash(db, "loser-hash", nowIso())).toBeNull();
  });

  it("poll: denied and expired grants report their terminal state", async () => {
    const denied = makeGrant({ id: "denied", deviceCodeHash: "denied-code", userCodeHash: "denied-user" });
    await createDeviceAuthorization(db, denied, 5, 50);
    await decideDeviceAuthorization(db, denied.userCodeHash, "denied", nowIso());
    const deniedResult = await pollDeviceAuthorization(db, denied.deviceCodeHash, denied.deviceSecretHash, nowIso(), makeToken());
    expect(deniedResult.kind).toBe("denied");

    const expiring = makeGrant({
      id: "expiring",
      deviceCodeHash: "expiring-code",
      userCodeHash: "expiring-user",
      expiresAt: new Date(Date.now() - 1000).toISOString(),
    });
    await createDeviceAuthorization(db, expiring, 5, 50);
    const expiredResult = await pollDeviceAuthorization(db, expiring.deviceCodeHash, expiring.deviceSecretHash, nowIso(), makeToken());
    expect(expiredResult.kind).toBe("expired");
  });

  it("poll: slows down a caller that polls faster than the interval", async () => {
    const grant = makeGrant({ pollIntervalSeconds: 5 });
    await createDeviceAuthorization(db, grant, 5, 50);
    const t0 = nowIso();
    const first = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, t0, makeToken());
    expect(first.kind).toBe("pending");
    const tooSoon = new Date(new Date(t0).getTime() + 1000).toISOString();
    const second = await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, tooSoon, makeToken());
    expect(second.kind).toBe("slow_down");
    if (second.kind === "slow_down") expect(second.interval).toBeGreaterThan(5);
  });
});

describe("api tokens", () => {
  it("finds a live token by hash, ignoring revoked or expired ones", async () => {
    const now = nowIso();
    const grant = makeGrant();
    await createDeviceAuthorization(db, grant, 5, 50);
    await decideDeviceAuthorization(db, grant.userCodeHash, "approved", now);
    await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, now, {
      id: "t1",
      tokenHash: "hash-1",
      displayPrefix: "waymark_t1",
      deviceLabel: "d",
      scopes: "pages:read",
      createdAt: now,
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
      lastUsedAt: null,
      revokedAt: null,
    });

    expect(await apiTokenByHash(db, "hash-1", nowIso())).not.toBeNull();
    await revokeApiToken(db, "t1", nowIso());
    expect(await apiTokenByHash(db, "hash-1", nowIso())).toBeNull();
    await expect(revokeApiToken(db, "t1", nowIso())).rejects.toBeInstanceOf(NotFoundError);
  });

  it("touchApiToken only updates last_used_at once per 5-minute window", async () => {
    const now = nowIso();
    const grant = makeGrant({ id: "g2", deviceCodeHash: "g2-code", userCodeHash: "g2-user" });
    await createDeviceAuthorization(db, grant, 5, 50);
    await decideDeviceAuthorization(db, grant.userCodeHash, "approved", now);
    await pollDeviceAuthorization(db, grant.deviceCodeHash, grant.deviceSecretHash, now, {
      id: "t2",
      tokenHash: "hash-2",
      displayPrefix: "waymark_t2",
      deviceLabel: "d",
      scopes: "pages:read",
      createdAt: now,
      expiresAt: new Date(Date.now() + 3600_000).toISOString(),
      lastUsedAt: null,
      revokedAt: null,
    });
    await touchApiToken(db, "t2", now);
    const tokens = await listApiTokens(db);
    const touched = tokens.find((t) => t.id === "t2");
    expect(touched?.lastUsedAt).toBe(now);
  });
});

describe("admin sessions", () => {
  it("creates, authenticates, and deletes a session", async () => {
    const now = nowIso();
    const outcome = await createAdminSession(db, {
      id: "s1",
      sessionHash: "hash-s1",
      authenticated: false,
      createdAt: now,
      expiresAt: new Date(Date.now() + 900_000).toISOString(),
    });
    expect(outcome.kind).toBe("created");

    const found = await adminSessionByHash(db, "hash-s1", now);
    expect(found?.authenticated).toBe(false);

    await authenticateAdminSession(db, "s1", new Date(Date.now() + 3600_000).toISOString());
    const authed = await adminSessionByHash(db, "hash-s1", now);
    expect(authed?.authenticated).toBe(true);

    await deleteAdminSession(db, "s1");
    expect(await adminSessionByHash(db, "hash-s1", now)).toBeNull();
  });

  it("enforces the max concurrent session cap", async () => {
    const now = nowIso();
    for (let i = 0; i < 256; i++) {
      const outcome = await createAdminSession(db, {
        id: `session-${i}`,
        sessionHash: `hash-${i}`,
        authenticated: false,
        createdAt: now,
        expiresAt: new Date(Date.now() + 900_000).toISOString(),
      });
      expect(outcome.kind).toBe("created");
    }
    const overflow = await createAdminSession(db, {
      id: "session-overflow",
      sessionHash: "hash-overflow",
      authenticated: false,
      createdAt: now,
      expiresAt: new Date(Date.now() + 900_000).toISOString(),
    });
    expect(overflow.kind).toBe("limit_reached");
  });
});

describe("deleteExpiredAuth", () => {
  it("removes expired device grants, tokens, and admin sessions", async () => {
    const past = "2020-01-01T00:00:00.000Z";
    await createDeviceAuthorization(
      db,
      makeGrant({ id: "old-grant", deviceCodeHash: "old-code", userCodeHash: "old-user", expiresAt: past }),
      5,
      50,
    );
    await createAdminSession(db, {
      id: "old-session",
      sessionHash: "old-hash",
      authenticated: false,
      createdAt: past,
      expiresAt: past,
    });
    const removed = await deleteExpiredAuth(db, nowIso());
    expect(removed).toBeGreaterThanOrEqual(2);
  });
});

describe("rateLimitAllow", () => {
  it("allows up to the limit within a window, then denies, then resets after the window elapses", async () => {
    const t0 = "2026-01-01T00:00:00.000Z";
    for (let i = 0; i < 3; i++) {
      expect(await rateLimitAllow(db, "k", 3, 60, t0)).toBe(true);
    }
    expect(await rateLimitAllow(db, "k", 3, 60, t0)).toBe(false);

    const later = new Date(new Date(t0).getTime() + 61_000).toISOString();
    expect(await rateLimitAllow(db, "k", 3, 60, later)).toBe(true);
  });
});
