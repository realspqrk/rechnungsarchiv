import "./fonts.css";
import "./style.css";

import { api, errMsg, Events, OrdnerAbgelegt, StatusGeaendert } from "./api";
import type { FolderDrop } from "./api";
import { clear, h } from "./dom";
import { app, rerender, setRenderer } from "./state";
import { toast } from "./ui";
import { onStatusChanged } from "./actions";
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

  Events.On(OrdnerAbgelegt, (ev) => onFolder(ev.data));

  // Background work in Go (stamping on start, trusted-list refresh).
  Events.On(StatusGeaendert, () => void onStatusChanged());

  if (app.about.debug) {
    // Hooks for screenshots (sample data or the testtsa environment); not present otherwise.
    (window as unknown as Record<string, unknown>).raDebug = {
      archiveFolder: openArchiveDialog,
      exportDialog: openExportDialog,
      about: openAboutDialog,
      disclaimer: openDisclaimerDialog,
      rerender: async () => {
        app.status = await api.Status();
        rerender();
      },
      status: () => api.Status(),
    };
  }
  await route();
  // rechnungsarchiv gui <ordner>: the folder from the command line, once.
  onFolder(await api.StartFolder());
}

/** A dropped folder, or the one given on the command line. */
function onFolder(drop: FolderDrop): void {
  if (!drop.folder && !drop.error) return;
  // Read-only mode, or another PC holds the lock: nothing is archived.
  if (!app.status?.hasArchive || app.status.archive.readOnly || app.status.lock) return;
  if (drop.error) toast(drop.error, "error");
  else openArchiveDialog(drop.folder);
}

void boot();
