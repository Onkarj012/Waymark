// A lightweight D1Database stand-in backed by Node's built-in `node:sqlite`,
// used so tests exercise real SQL (including the actual migration file)
// without needing workerd/miniflare. Implements only the subset of the D1
// API our store/ratelimit modules call (see src/types.ts's D1Like).

import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import path from "node:path";
import type { D1Like, D1PreparedLike, D1ResultLike } from "../src/types";

// Imported via createRequire (rather than a static `import ... from
// "node:sqlite"`) so Vite/esbuild's dependency scanner doesn't try to
// resolve/bundle this Node built-in — it's looked up at runtime by Node
// itself, same as it would be in a plain Node script.
type DatabaseSyncCtor = new (path: string) => {
  exec(sql: string): void;
  prepare(sql: string): {
    get(...args: unknown[]): unknown;
    all(...args: unknown[]): unknown[];
    run(...args: unknown[]): { changes: number | bigint; lastInsertRowid: number | bigint };
  };
};
const { DatabaseSync } = createRequire(import.meta.url)("node:sqlite") as { DatabaseSync: DatabaseSyncCtor };
type DatabaseSync = InstanceType<DatabaseSyncCtor>;

class FakeD1Statement implements D1PreparedLike {
  constructor(
    private db: DatabaseSync,
    private sql: string,
    private args: unknown[] = [],
  ) {}

  bind(...values: unknown[]): D1PreparedLike {
    return new FakeD1Statement(this.db, this.sql, values);
  }

  async first<T = Record<string, unknown>>(): Promise<T | null> {
    const stmt = this.db.prepare(this.sql);
    const row = stmt.get(...(this.args as never[]));
    return (row as T | undefined) ?? null;
  }

  async run<T = unknown>(): Promise<D1ResultLike<T>> {
    const stmt = this.db.prepare(this.sql);
    const info = stmt.run(...(this.args as never[]));
    return { success: true, meta: { changes: Number(info.changes), last_row_id: Number(info.lastInsertRowid) } };
  }

  async all<T = unknown>(): Promise<D1ResultLike<T>> {
    const stmt = this.db.prepare(this.sql);
    const rows = stmt.all(...(this.args as never[]));
    return { success: true, results: rows as T[], meta: {} };
  }
}

export class FakeD1 implements D1Like {
  constructor(private db: DatabaseSync) {}

  prepare(query: string): D1PreparedLike {
    return new FakeD1Statement(this.db, query);
  }

  async batch<T = unknown>(statements: D1PreparedLike[]): Promise<D1ResultLike<T>[]> {
    const out: D1ResultLike<T>[] = [];
    for (const s of statements) out.push(await s.run<T>());
    return out;
  }
}

/** Creates an in-memory D1-like database with the real migration applied. */
export function createFakeD1(): FakeD1 {
  const db = new DatabaseSync(":memory:");
  const here = path.dirname(fileURLToPath(import.meta.url));
  const migrationPath = path.resolve(here, "..", "migrations", "0001_init.sql");
  const schema = readFileSync(migrationPath, "utf8");
  db.exec(schema);
  return new FakeD1(db);
}
