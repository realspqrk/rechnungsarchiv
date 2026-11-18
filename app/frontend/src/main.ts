import "./fonts.css";
import "./style.css";

import { api, errMsg, Events, OrdnerAbgelegt } from "./api";
import { clear, h } from "./dom";
import { app, rerender, setRenderer } from "./state";
import { toast } from "./ui";
import { renderStart } from "./views/start";
import { renderMain } from "./views/overview";
import { renderDeposit } from "./views/deposit";
import { renderSettings } from "./views/settings";
import { openArchiveDialog } from "./dialogs/archive";
import { openExportDialog } from "./dialogs/export";
import { openAboutDialog, openDisclaimerDialog } from "./dialogs/about";

const root = document.getElementById("app") as HTMLElement;

// No remote navigation: links inside the app are hash routes only. External
// pages open through Go (fixed URLs) in the default browser.
document.addEventListener("click", (ev) => {
  const a = (ev.target as Element | null)?.closest?.("a");
  if (a && !(a.getAttribute("href") ?? "").startsWith("#")) ev.preventDefault();
});
window.open = () => null;

async function route(): Promise<void> {
  const status = app.status;
  const view = root.firstElementChild;
  let next: HTMLElement;
  try {
    if (!status || !status.hasArchive) {
      next = renderStart();
    } else {
      const hash = location.hash || "#/";
      const m = hash.match(/^#\/ablage\/(\d+)$/);
      if (m) next = await renderDeposit(Number(m[1]));
      else if (hash === "#/einstellungen" && !status.archive.readOnly) next = await renderSettings();
      else next = renderMain();
    }
  } catch (e) {
    toast(errMsg(e), "error");
    next = renderMain();
  }
  if (view) clear(root);
  root.appendChild(next);
  window.scrollTo(0, 0);
}

async function boot(): Promise<void> {
  try {
    [app.about, app.disclaimer, app.status] = await Promise.all([api.About(), api.Disclaimer(), api.Status()]);
  } catch (e) {
    root.appendChild(h("main", { class: "page page--narrow" }, h("h1", null, "Rechnungsarchiv"), h("p", { role: "alert" }, errMsg(e))));
    return;
  }
  setRenderer(route);
  window.addEventListener("hashchange", () => void route());

  Events.On(OrdnerAbgelegt, (ev) => {
    const drop = ev.data;
    if (!app.status?.hasArchive || app.status.archive.readOnly) return;
    if (drop.error) toast(drop.error, "error");
    else openArchiveDialog(drop.folder);
  });

  if (app.about.fake) {
    // Hooks for screenshots with sample data (RA_FAKE); not present otherwise.
    (window as unknown as Record<string, unknown>).raDebug = {
      archiveFolder: openArchiveDialog,
      exportDialog: openExportDialog,
      about: openAboutDialog,
      disclaimer: openDisclaimerDialog,
      rerender,
    };
  }
  await route();
}

void boot();
