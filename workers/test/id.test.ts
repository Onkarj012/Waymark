import { describe, expect, it } from "vitest";
import { newId } from "../src/id";

const ID_ALPHABET = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";

describe("newId", () => {
  it("returns 12 characters drawn from the base62 alphabet (matches internal/web/id.go)", () => {
    for (let i = 0; i < 200; i++) {
      const id = newId();
      expect(id).toHaveLength(12);
      for (const ch of id) {
        expect(ID_ALPHABET.includes(ch)).toBe(true);
      }
    }
  });

  it("generates unique ids across many calls", () => {
    const seen = new Set<string>();
    for (let i = 0; i < 2000; i++) seen.add(newId());
    expect(seen.size).toBe(2000);
  });

  it("draws roughly uniformly from the alphabet (rejection sampling, no modulo bias)", () => {
    const counts = new Map<string, number>();
    const samples = 20000;
    for (let i = 0; i < samples; i++) {
      const id = newId();
      for (const ch of id) counts.set(ch, (counts.get(ch) ?? 0) + 1);
    }
    // 12 chars * 20000 samples / 62 symbols ~= 3871 expected per symbol.
    const expected = (12 * samples) / 62;
    for (const ch of ID_ALPHABET) {
      const count = counts.get(ch) ?? 0;
      expect(count).toBeGreaterThan(expected * 0.7);
      expect(count).toBeLessThan(expected * 1.3);
    }
  });
});
