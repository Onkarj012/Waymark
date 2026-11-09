import { lstat, mkdir, readFile, rename, unlink, writeFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { dirname, resolve } from "node:path";
import { createInterface } from "node:readline/promises";
import { stdin, stdout } from "node:process";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";

const workersDir = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const usage = `Usage: npm run configure -- [options]

  --subdomain <label>    Your account's workers.dev label, not a URL or hostname
  --database-id <uuid>   D1 database ID returned by wrangler d1 create waymark
  --output-dir <path>    Write configs elsewhere for inspection; default: workers/
  --force               Replace existing regular config files explicitly
  --help                Show this help

Missing values are prompted for in an interactive terminal.
This command only writes local files. It never creates resources or deploys.
`;

async function main() {
  const { values } = parseArgs({
    options: {
      subdomain: { type: "string" },
      "database-id": { type: "string" },
      "output-dir": { type: "string" },
      force: { type: "boolean", default: false },
      help: { type: "boolean", default: false },
    },
    allowPositionals: false,
  });
  if (values.help) {
    stdout.write(usage);
    return;
  }

  let subdomain = values.subdomain;
  let databaseId = values["database-id"];
  if (subdomain === undefined || databaseId === undefined) {
    if (!stdin.isTTY || !stdout.isTTY) {
      throw new Error("Non-interactive setup requires --subdomain and --database-id.");
    }
    const prompt = createInterface({ input: stdin, output: stdout });
    try {
      subdomain ??= await prompt.question("Account workers.dev subdomain label: ");
      databaseId ??= await prompt.question("D1 database ID: ");
    } finally {
      prompt.close();
    }
  }

  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(subdomain)) {
    throw new Error("Subdomain must be one lowercase DNS label, 1-63 letters/digits/hyphens, with no leading or trailing hyphen.");
  }
  if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(databaseId)) {
    throw new Error("Database ID must be a UUID, not a database name.");
  }
  if (values["output-dir"] !== undefined && !values["output-dir"].trim()) {
    throw new Error("Output directory must not be empty.");
  }

  const outputDir = values["output-dir"] === undefined
    ? workersDir
    : resolve(values["output-dir"]);
  const configs = await Promise.all(["control", "content"].map(async (role) => {
    const template = await readFile(resolve(workersDir, `wrangler.${role}.example.toml`), "utf8");
    const content = template
      .replaceAll("__WORKERS_SUBDOMAIN__", subdomain)
      .replaceAll("__D1_DATABASE_ID__", databaseId);
    if (/__[A-Z0-9_]+__/.test(content)) {
      throw new Error(`Unresolved placeholder in ${role} template.`);
    }
    return { path: resolve(outputDir, `wrangler.${role}.toml`), content };
  }));

  for (const config of configs) {
    let existing;
    try {
      existing = await lstat(config.path);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    if (existing && (!values.force || !existing.isFile())) {
      throw new Error(`Refusing to overwrite ${config.path}. Back up existing configs; use --force only to replace regular files intentionally.`);
    }
  }

  await mkdir(outputDir, { recursive: true });
  for (const config of configs) {
    if (values.force) {
      const staged = `${config.path}.${randomUUID()}.tmp`;
      try {
        await writeFile(staged, config.content, { flag: "wx" });
        await rename(staged, config.path);
      } finally {
        await unlink(staged).catch((error) => {
          if (error.code !== "ENOENT") throw error;
        });
      }
    } else {
      await writeFile(config.path, config.content, { flag: "wx" });
    }
    stdout.write(`Wrote ${config.path}\n`);
  }
  stdout.write(`Control: https://waymark-control.${subdomain}.workers.dev\n`);
  stdout.write(`Content: https://waymark-content.${subdomain}.workers.dev\n`);
  stdout.write("Review both configs before any migrations, secrets, or deployments.\n");
}

main().catch((error) => {
  console.error(`Configure failed: ${error.message}`);
  process.exitCode = 1;
});
