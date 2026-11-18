// Screenshots of the REAL service (not sample data) against the demo
// archive that app/realshots_testtsa_test.go builds with the test TSA.
// That test starts the GUI in server mode and calls this script:
//
//   node scripts/screenshots-real.mjs <url> <out-dir> <folder-to-archive>
//
// Writes <out-dir>/real-*.png. Uses the installed Microsoft Edge.

import { mkdirSync, rmSync } from "node:fs";
import { join } from "node:path";
import { chromium } from "playwright-core";

const [url, outDir, folder] = process.argv.slice(2);
if (!url || !outDir || !folder) {
  console.error("usage: screenshots-real.mjs <url> <out-dir> <folder>");
  process.exit(2);
}

async function shoot(page, name) {
  await page.mouse.move(0, 0);
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(200);
  const file = join(outDir, `${name}.png`);
  rmSync(file, { force: true });
  const modal = await page.locator("dialog[open]").count();
  await page.screenshot({ path: file, fullPage: modal === 0 });
  console.log("wrote", file);
}

/** Waits until Go's background work (stamping, list refresh) is done. */
async function idle(page) {
  await page.waitForFunction(async () => {
    const st = await window.raDebug.status();
    return !st.busy;
  }, null, { timeout: 60000, polling: 250 });
  await page.evaluate(() => window.raDebug.rerender());
  await page.waitForSelector("main");
}

async function main() {
  mkdirSync(outDir, { recursive: true });
  const browser = await chromium.launch({ channel: "msedge", headless: true });
  try {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, deviceScaleFactor: 1, locale: "de-AT", timezoneId: "Europe/Vienna" });
    const page = await ctx.newPage();
    page.on("pageerror", (e) => console.error("page error:", e.message));
    await page.goto(url);
    await page.waitForSelector(".archive-head");
    await idle(page);

    // Ordner archivieren: commit, then a real timestamp from the test TSA.
    await page.evaluate((f) => window.raDebug.archiveFolder(f), folder);
    await page.waitForSelector("dialog[open] #dlg-title");
    await page.waitForFunction(() => /Jahre/.test(document.querySelector(".retention")?.textContent ?? ""));
    await shoot(page, "real-01-archivieren");
    await page.locator("dialog[open]").getByRole("button", { name: "Archivieren" }).click();
    await page.waitForSelector("dialog[open] .done-head", { timeout: 60000 });
    await shoot(page, "real-02-archivieren-zeitgestempelt");
    await page.locator("dialog[open]").getByRole("button", { name: "Fertig" }).click();
    await page.waitForSelector("dialog[open]", { state: "detached" });
    await page.waitForFunction(() => document.querySelectorAll("tbody tr").length === 2);
    await shoot(page, "real-03-uebersicht-nach-archivieren");

    await page.getByRole("button", { name: "Archiv kontrollieren" }).click();
    await page.waitForSelector('[aria-label="Kontrollergebnis"]', { timeout: 60000 });
    await shoot(page, "real-04-uebersicht-kontrolliert");

    await page.evaluate(() => (location.hash = "#/ablage/1"));
    await page.waitForSelector(".result");
    await shoot(page, "real-05-ablage-zeitgestempelt");
    await page.click("summary");
    await shoot(page, "real-06-ablage-technische-details");

    await page.evaluate(() => (location.hash = "#/"));
    await page.waitForSelector(".archive-head");
    await page.getByRole("button", { name: "Export für Betriebsprüfung" }).click();
    await page.waitForSelector("dialog[open] #exp-title");
    await page.waitForFunction(() => /Inhalt: \d/.test(document.querySelector(".summary__title")?.textContent ?? ""));
    await shoot(page, "real-07-export");
    await page.locator("dialog[open]").getByRole("button", { name: "Exportieren" }).click();
    await page.waitForSelector(".export-done:not([hidden])", { timeout: 120000 });
    await shoot(page, "real-08-export-abgeschlossen");
    await page.locator("dialog[open]").getByRole("button", { name: "Fertig" }).click();

    await page.evaluate(() => (location.hash = "#/einstellungen"));
    await page.waitForSelector("#tsa-anbieter");
    await shoot(page, "real-09-einstellungen");
    await ctx.close();
  } finally {
    await browser.close();
  }
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
