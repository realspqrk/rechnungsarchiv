// Screenshots of every screen and state with sample data (RA_FAKE).
//
// Builds rechnungsarchiv.exe in Wails server mode (`-tags server`: same Go
// code and fake service; `gui` serves it over HTTP instead of a window) and
// drives it with the
// installed Microsoft Edge via playwright-core. Run after `npm run build`:
//
//   npm run screenshots            # writes ../screenshots/*.png
//
// Windows only (uses msedge). Native dialogs and drag & drop cannot be
// driven this way; the archive dialog is opened through the raDebug hook
// that exists only with sample data.

import { spawn, execFileSync } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright-core";

const here = dirname(fileURLToPath(import.meta.url));
const appDir = resolve(here, "..", "..");
const outDir = join(appDir, "screenshots");
const exe = join(appDir, "..", "bin", "rechnungsarchiv-screenshot-server.exe");
const only = process.argv[2] ? new RegExp(process.argv[2]) : null;

const SCAN = "D:\\Scans\\2026-11 Eingangsrechnungen";

/** [file, scenario, steps(page)] */
const shots = [
  ["01-start", "start", async (p) => { await p.fill("#firma", "Musterfirma GmbH"); await p.locator("#firma").blur(); }],
  ["02-start-haftungsausschluss", "start", async (p) => { await p.getByRole("button", { name: "Vollständiger Text" }).click(); await p.waitForSelector("dialog[open]"); }],
  ["03-main-normal", "normal", null],
  ["04-main-kontrolliert", "normal", async (p) => { await checkArchive(p); }],
  ["05-main-alle-ablagen", "normal", async (p) => { await p.getByRole("button", { name: /Weitere .* anzeigen/ }).click(); }],
  ["06-main-leer", "leer", null],
  ["07-main-kein-dienst", "kein-dienst", null],
  ["08-main-schreibgeschuetzt", "schreibgeschuetzt", null],
  ["09-main-luecke", "luecke", null],
  ["10-main-zustaende", "zustaende", async (p) => { await p.getByRole("button", { name: /Weitere .* anzeigen/ }).click(); }],
  ["11-archivieren", "normal", async (p) => { await openArchive(p); }],
  ["12-archivieren-aufbewahrung-aendern", "normal", async (p) => { await openArchive(p); await p.getByRole("button", { name: "Aufbewahrung ändern" }).click(); }],
  ["13-archivieren-zeitgestempelt", "normal", async (p) => { await openArchive(p); await archive(p); }],
  ["14-archivieren-ausstehend-guthaben", "guthaben", async (p) => { await openArchive(p); await archive(p); }],
  ["15-archivieren-ausstehend-kein-dienst", "kein-dienst", async (p) => { await openArchive(p); await archive(p); }],
  ["16-archivieren-abgelehnt", "abgelehnt", async (p) => { await openArchive(p); }],
  ["17-ablage-unveraendert", "zustaende", async (p) => { await gotoDeposit(p, 11); }],
  ["18-ablage-nicht-qualifiziert", "zustaende", async (p) => { await gotoDeposit(p, 10); }],
  ["19-ablage-unbestaetigt", "zustaende", async (p) => { await gotoDeposit(p, 9); }],
  ["20-ablage-veraendert", "zustaende", async (p) => { await gotoDeposit(p, 8); }],
  ["21-ablage-ausstehend-ueber-nachfolger", "zustaende", async (p) => { await gotoDeposit(p, 12); }],
  ["22-ablage-ausstehend-offline", "normal", async (p) => { await gotoDeposit(p, 13); }],
  ["23-ablage-ausstehend-schreibgeschuetzt", "schreibgeschuetzt", async (p) => { await gotoDeposit(p, 13); }],
  ["24-ablage-unbestimmt", "zustaende", async (p) => { await gotoDeposit(p, 7); }],
  ["25-ablage-ungueltig", "zustaende", async (p) => { await gotoDeposit(p, 6); }],
  ["26-ablage-technische-details", "normal", async (p) => { await gotoDeposit(p, 11); await p.click("summary"); }],
  ["27-einstellungen-basic", "normal", async (p) => { await gotoSettings(p); }],
  ["41-archiv-menue", "normal", async (p) => { await openSwitcher(p); }],
  ["42-neues-archiv", "normal", async (p) => { await openSwitcher(p); await p.getByRole("menuitem", { name: "Neues Archiv anlegen …" }).click(); await p.waitForSelector(".back-link--start"); }],
  ["28-einstellungen-clientcert", "disig", async (p) => { await gotoSettings(p); }],
  ["29-einstellungen-kein-dienst", "kein-dienst", async (p) => { await gotoSettings(p); }],
  ["30-export", "normal", async (p) => { await openExport(p); }],
  ["31-export-abgeschlossen", "normal", async (p) => { await openExport(p); await p.getByRole("button", { name: "Exportieren" }).click(); await p.waitForSelector(".export-done:not([hidden])"); }],
  ["32-ueber", "normal", async (p) => { await p.getByRole("button", { name: "Über" }).click(); await p.waitForSelector("dialog[open]"); }],
  ["33-haftungsausschluss", "normal", async (p) => { await p.getByRole("button", { name: "Haftungsausschluss" }).click(); await p.waitForSelector("dialog[open]"); }],
  ["34-fokus-tastatur", "normal", async (p) => { await p.keyboard.press("Tab"); await p.keyboard.press("Tab"); }],
  ["35-main-gesperrt", "gesperrt", null],
  ["36-sperre-uebernehmen", "gesperrt", async (p) => { await p.getByRole("button", { name: "Sperre übernehmen …" }).click(); await p.waitForSelector("dialog[open] #sperre-title"); }],
  ["43-zeitstempel-frage", "nachholen", async (p) => { await p.waitForSelector("dialog[open] #frage-title"); }],
  ["44-zeitstempel-frage-geholt", "nachholen", async (p) => {
    await p.waitForSelector("dialog[open] #frage-title");
    await p.getByRole("button", { name: "Jetzt holen" }).click();
    await p.waitForSelector(".toast");
  }],
  ["38-haftungsausschluss-ende", "normal", async (p) => {
    await p.getByRole("button", { name: "Haftungsausschluss" }).click();
    await p.waitForSelector("dialog[open]");
    await p.evaluate(() => { const c = document.querySelector("dialog[open] .dialog__content--scroll"); c.scrollTop = c.scrollHeight; });
    await p.waitForTimeout(100);
  }],
  ["39-main-leer-kontrolliert", "leer", async (p) => { await checkArchive(p); }],
  ["40-ablage-gesperrt", "gesperrt", async (p) => { await gotoDeposit(p, 13); }],
];

async function checkArchive(p) {
  await p.getByRole("button", { name: "Archiv kontrollieren" }).click();
  await p.waitForSelector('[aria-label="Kontrollergebnis"]');
}

async function openArchive(p) {
  await p.evaluate((f) => window.raDebug.archiveFolder(f), SCAN);
  await p.waitForSelector("dialog[open] #dlg-title");
  await p.waitForFunction(() => /Jahre/.test(document.querySelector(".retention")?.textContent ?? ""));
}
async function archive(p) {
  await p.locator("dialog[open]").getByRole("button", { name: "Archivieren" }).click();
  await p.waitForSelector("dialog[open] .done-head");
}
async function gotoDeposit(p, seq) {
  await p.evaluate((s) => (location.hash = `#/ablage/${s}`), seq);
  await p.waitForSelector(".result");
}
async function gotoSettings(p) {
  await p.evaluate(() => (location.hash = "#/einstellungen"));
  await p.waitForSelector("#tsa-anbieter");
}
async function openSwitcher(p) {
  await gotoSettings(p);
  await p.getByRole("button", { name: "Archiv wechseln" }).click();
  await p.waitForSelector("#archiv-menu:not([hidden])");
}
async function openExport(p) {
  await p.getByRole("button", { name: "Export für Betriebsprüfung" }).click();
  await p.waitForSelector("dialog[open] #exp-title");
  await p.waitForFunction(() => /Inhalt:/.test(document.querySelector(".summary__title")?.textContent ?? ""));
}

function startServer(scenario, port) {
  const child = spawn(exe, ["gui"], {
    env: { ...process.env, RA_FAKE: scenario, RA_FAKE_DELAY_MS: "0", WAILS_SERVER_PORT: String(port) },
    stdio: "ignore",
  });
  return child;
}

async function waitFor(url) {
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(url);
      if (r.ok) return;
    } catch {}
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`server not reachable: ${url}`);
}

async function main() {
  execFileSync("go", ["build", "-tags", "server", "-o", exe, "./cmd/rechnungsarchiv"], { cwd: resolve(appDir, ".."), stdio: "inherit" });
  mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch({ channel: "msedge", headless: true });
  let port = 18700;
  try {
    for (const [name, scenario, steps] of shots) {
      if (only && !only.test(name)) continue;
      port++;
      const server = startServer(scenario, port);
      try {
        const url = `http://localhost:${port}/`;
        await waitFor(url);
        const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, deviceScaleFactor: 1, locale: "de-AT", timezoneId: "Europe/Vienna" });
        const page = await ctx.newPage();
        page.on("pageerror", (e) => console.error(`[${name}] page error:`, e.message));
        await page.goto(url);
        await page.waitForSelector("main");
        if (steps) await steps(page);
        await page.mouse.move(0, 0);
        await page.evaluate(() => document.fonts.ready);
        await page.waitForTimeout(150);
        const file = join(outDir, `${name}.png`);
        rmSync(file, { force: true });
        // Modal dialogs: the backdrop covers the viewport only, so shoot the window.
        const modal = await page.locator("dialog[open]").count();
        await page.screenshot({ path: file, fullPage: modal === 0 });
        console.log("wrote", file);
        await ctx.close();
      } finally {
        server.kill();
      }
    }
  } finally {
    await browser.close();
    rmSync(exe, { force: true });
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
