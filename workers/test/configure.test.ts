import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const temporaryDirectories: string[] = [];

function outputDirectory(): string {
  const directory = mkdtempSync(join(tmpdir(), "waymark-configure-"));
  temporaryDirectories.push(directory);
  return directory;
}

function configure(directory: string, extra: string[] = []) {
  return spawnSync(process.execPath, [
    resolve("scripts/configure.mjs"),
    "--subdomain", "example-account",
    "--database-id", "00000000-0000-4000-8000-000000000001",
    "--output-dir", directory,
    ...extra,
  ], { encoding: "utf8" });
}

afterEach(() => {
  for (const directory of temporaryDirectories.splice(0)) {
    rmSync(directory, { recursive: true, force: true });
  }
});

describe("fork-safe configuration", () => {
  it("creates distinct free origins without owner identifiers or custom routes", () => {
    const directory = outputDirectory();
    expect(configure(directory).status).toBe(0);
    for (const role of ["control", "content"]) {
      const config = readFileSync(join(directory, `wrangler.${role}.toml`), "utf8");
      expect(config).toContain("https://waymark-control.example-account.workers.dev");
      expect(config).toContain("https://waymark-content.example-account.workers.dev");
      expect(config).not.toMatch(/leo4\.dev|leoeccentric|__D1_|\[\[routes\]\]|WAYMARK_LEGACY_HOSTS/);
    }
  });

  it.each([
    ["--subdomain", "https://other.workers.dev"],
    ["--subdomain", "../other"],
    ["--database-id", "not-a-uuid"],
  ])("rejects invalid %s input", (flag, value) => {
    expect(configure(outputDirectory(), [flag, value]).status).not.toBe(0);
  });

  it("preserves existing regular configs unless explicitly forced", () => {
    const directory = outputDirectory();
    const control = join(directory, "wrangler.control.toml");
    writeFileSync(control, "owner config");
    expect(configure(directory).status).not.toBe(0);
    expect(readFileSync(control, "utf8")).toBe("owner config");
    expect(configure(directory, ["--force"]).status).toBe(0);
    expect(readFileSync(control, "utf8")).not.toBe("owner config");
  });

  it("refuses symlink targets even with force", () => {
    const directory = outputDirectory();
    const target = join(directory, "preserve.toml");
    writeFileSync(target, "original");
    symlinkSync(target, join(directory, "wrangler.control.toml"));
    expect(configure(directory, ["--force"]).status).not.toBe(0);
    expect(readFileSync(target, "utf8")).toBe("original");
  });
});
