// Runs src/unit.test.ts: bundles it with rolldown (part of Vite, no extra
// dependency) into a temporary ESM file and imports it in Node. The file
// may only import pure modules (no DOM, no Wails runtime).
import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL, fileURLToPath } from "node:url";
import { build } from "rolldown";

const root = fileURLToPath(new URL("..", import.meta.url));
const dir = mkdtempSync(join(tmpdir(), "ra-unit-"));
const out = join(dir, "unit.mjs");
try {
  await build({ cwd: root, input: join(root, "src", "unit.test.ts"), platform: "node", output: { file: out, format: "esm" }, logLevel: "warn" });
  await import(pathToFileURL(out).href);
} finally {
  rmSync(dir, { recursive: true, force: true });
}
