import "./fonts.css";
import "./style.css";

import { api, errMsg, Events, OrdnerWartet, StatusGeaendert } from "./api";
import { FolderInbox, gateOf } from "./folders";
import { clear, h } from "./dom";
import { app, rerender, setRenderer, SWITCH_ROUTE } from "./state";
import { toast } from "./ui";
import { onStatusChanged } from "./actions";
import { renderStart } from "./views/start";
import { renderMain } from "./views/overview";
import { renderDeposit } from "./views/deposit";
import { renderSettings } from "./views/settings";
import { archiveDialogOpen, openArchiveDialog } from "./dialogs/archive";
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
      else if (hash === SWITCH_ROUTE) next = renderStart(true);
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
  // Every status change re-renders: after "Archiv anlegen" or "öffnen",
  // and when a dialog closes, a waiting folder is offered.
  void offerFolders();
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

  // A folder waits in Go (command line, a later launch, a drop); the event
  // only says "take it now". Listening starts here, after the boot calls, so
  // the queue is taken once more after the first render.
  Events.On(OrdnerWartet, () => void takeFolders());

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
  ready = true;
  await takeFolders();
}

// Handed-over folders wait here until an archive is open for writing
// (folders.ts); each is offered once.
const inbox = new FolderInbox();
let ready = false; // first render done
let offering = false; // a dialog is being opened

async function takeFolders(): Promise<void> {
  try {
    const drops = await api.TakeFolders();
    inbox.add(drops, offering || archiveDialogOpen());
  } catch {
    return;
  }
  await offerFolders();
}

async function offerFolders(): Promise<void> {
  if (!ready || offering) return;
  offering = true;
  try {
    for (;;) {
      // While another archive is being chosen, handed-over folders wait.
      const step = inbox.next(gateOf(location.hash === SWITCH_ROUTE ? null : app.status, archiveDialogOpen()));
      if (!step) break;
      if (step.error) toast(step.error, "error");
      else if (step.note) toast(step.note);
      else if (step.folder) await openArchiveDialog(step.folder);
    }
  } finally {
    offering = false;
  }
}

void boot();
