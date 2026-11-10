import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterEach, describe, expect, it } from "vitest";

const temporaryDirectories: string[] = [];

function outputDirectory(): string {
  const directory = mkdtempSync(join(tmpdir(), "waymark-configure-"));
  temporaryDirectories.push(directory);
  return directory;
}

function configure(directory: string, extra: string[] = [], script = resolve("scripts/configure.mjs")) {
  return spawnSync(process.execPath, [
    script,
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
  it("rejects unresolved placeholders containing digits before writing either config", () => {
    const fixture = outputDirectory();
    const scripts = join(fixture, "scripts");
    mkdirSync(scripts);
    const script = join(scripts, "configure.mjs");
    writeFileSync(script, readFileSync(resolve("scripts/configure.mjs")));
    for (const role of ["control", "content"]) {
      const name = `wrangler.${role}.example.toml`;
      writeFileSync(join(fixture, name), readFileSync(resolve(name), "utf8") + "\n__D1_UNKNOWN__\n");
    }
    const directory = outputDirectory();
    const result = configure(directory, [], script);
    expect(result.status).not.toBe(0);
    expect(result.stderr).toContain("Unresolved placeholder");
    expect(existsSync(join(directory, "wrangler.control.toml"))).toBe(false);
    expect(existsSync(join(directory, "wrangler.content.toml"))).toBe(false);
  });

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
