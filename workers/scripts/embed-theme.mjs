#!/usr/bin/env node
// Embeds theme/theme.css (the single source of truth, see AGENTS.md) into a
// generated TS module so both Workers can serve it without a runtime fetch —
// the Workers equivalent of the Go server's `//go:embed theme.css`. Run
// automatically on `npm install` (see package.json "postinstall") and again
// on deploy, so the embedded copy can never drift from the source file.
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const themePath = path.resolve(here, "..", "..", "theme", "theme.css");
const outPath = path.resolve(here, "..", "src", "theme.generated.ts");

const css = readFileSync(themePath, "utf8");

const escaped = css.replace(/\\/g, "\\\\").replace(/`/g, "\\`").replace(/\$\{/g, "\\${");

const banner = `// GENERATED FILE — do not edit by hand.
// Produced by workers/scripts/embed-theme.mjs from theme/theme.css, which
// remains the single source of truth for the house theme. Regenerate with
// \`npm install\` (postinstall hook) or \`node scripts/embed-theme.mjs\`.
`;

writeFileSync(outPath, `${banner}\nexport const THEME_CSS = \`${escaped}\`;\n`);
console.log(`wrote ${path.relative(process.cwd(), outPath)} (${css.length} bytes from theme.css)`);
