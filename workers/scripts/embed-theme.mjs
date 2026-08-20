#!/usr/bin/env node
// Embeds theme/theme.css and the Waymark favicon into a generated TS module
// so both Workers can serve them without a runtime fetch — the Workers
// equivalent of the Go server's `//go:embed`. Run
// automatically on `npm install` (see package.json "postinstall") and again
// on deploy, so the embedded copy can never drift from the source file.
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

const here = path.dirname(fileURLToPath(import.meta.url));
const themePath = path.resolve(here, "..", "..", "theme", "theme.css");
const faviconPath = path.resolve(here, "..", "..", "theme", "logo-dark.svg");
const outPath = path.resolve(here, "..", "src", "theme.generated.ts");

const css = readFileSync(themePath, "utf8");
const favicon = readFileSync(faviconPath, "utf8");

const escapeTemplate = (value) => value.replace(/\\/g, "\\\\").replace(/`/g, "\\`").replace(/\$\{/g, "\\${");
const escaped = escapeTemplate(css);
const escapedFavicon = escapeTemplate(favicon);

const banner = `// GENERATED FILE — do not edit by hand.
// Produced by workers/scripts/embed-theme.mjs from theme/theme.css and
// theme/logo-dark.svg. Regenerate with \`npm install\` (postinstall hook) or
// \`node scripts/embed-theme.mjs\`.
`;

writeFileSync(
  outPath,
  `${banner}\nexport const THEME_CSS = \`${escaped}\`;\nexport const FAVICON_SVG = \`${escapedFavicon}\`;\n`,
);
console.log(`wrote ${path.relative(process.cwd(), outPath)} (${css.length} bytes CSS, ${favicon.length} bytes SVG)`);
