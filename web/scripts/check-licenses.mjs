// Fails if any installed npm package (direct or transitive) is not under an
// allowlisted OSI-approved, commercially usable license. No dependencies.
// Usage: node scripts/check-licenses.mjs   (run after `npm ci`)
import { readFileSync } from "node:fs";

// Keep in sync with CONTRIBUTING.md ("Dependency licenses").
const ALLOWED = new Set([
  "MIT", "Apache-2.0", "BSD-2-Clause", "BSD-3-Clause", "ISC", "0BSD",
  "MPL-2.0", "PostgreSQL", "BlueOak-1.0.0", "Zlib",
]);

// Fonts only: the SIL Open Font License (OSI-approved, allows commercial use)
// is accepted for font packages, not for code.
const FONT_ONLY = new Set(["OFL-1.1"]);
const isFontPackage = (name) => name.startsWith("@fontsource/");

const lock = JSON.parse(readFileSync(new URL("../package-lock.json", import.meta.url), "utf8"));
const bad = [];
let count = 0;
for (const [path, meta] of Object.entries(lock.packages ?? {})) {
  if (path === "" || meta.link) continue;
  count++;
  const name = path.replace(/^.*node_modules\//, "");
  const expr = String(meta.license ?? "UNKNOWN").replace(/[()]/g, "");
  // "A OR B" is acceptable if any alternative is allowed; "A AND B" needs all.
  const allowed = (l) => ALLOWED.has(l) || (FONT_ONLY.has(l) && isFontPackage(name));
  const ok = expr.split(/\s+OR\s+/).some((alt) => alt.split(/\s+AND\s+/).every((l) => allowed(l.trim())));
  if (!ok) bad.push(`${name}@${meta.version}: ${meta.license ?? "no license field"}`);
}
if (bad.length) {
  console.error(`Disallowed or unknown licenses (${bad.length}):\n  ${bad.join("\n  ")}`);
  process.exit(1);
}
console.log(`All ${count} npm packages use allowlisted licenses.`);
