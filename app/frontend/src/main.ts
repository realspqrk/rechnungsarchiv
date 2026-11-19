import "./fonts.css";
import "./style.css";

import { api, errMsg, Events, OrdnerWartet, StatusGeaendert } from "./api";
import { FolderInbox, gateOf } from "./folders";
import { clear, h } from "./dom";
import { app, releaseHold, rerender, setRenderer, setFolderKick, NEW_ARCHIVE_ROUTE, acceptStatus } from "./state";
import { toast } from "./ui";
import { onStatusChanged } from "./actions";
import { renderStart, renderOpening } from "./views/start";
import { renderMain } from "./views/overview";
import { renderDeposit } from "./views/deposit";
import { renderSettings } from "./views/settings";
import { archiveDialogOpen, openArchiveDialog } from "./dialogs/archive";
import { maybeAskPending, openingUndecided, pendingQuestionOpen } from "./dialogs/pending";
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

let routeSeq = 0;
let lastHash = "";

/**
 * Remembers the focused element and returns a function that focuses its
 * counterpart on the re-rendered screen: the same id, else the same tag,
 * class and text (e.g. "Ordner auswählen …").
 */
function focusKeeper(): () => void {
  const el = document.activeElement;
  if (!(el instanceof HTMLElement) || el === document.body) return () => {};
  const { id, tagName, className } = el;
  const text = el.textContent ?? "";
  return () => {
    const twin = id
      ? document.getElementById(id)
      : Array.from(root.getElementsByTagName(tagName)).find((c) => c.className === className && c.textContent === text);
    (twin as HTMLElement | null | undefined)?.focus();
  };
}

async function route(): Promise<void> {
  // A render that waited for Go (settings, Ablage detail) while a newer
  // one started is dropped: it may belong to the archive before a switch.
  const seq = ++routeSeq;
  releaseHold();
  // Re-renders (status events, polling) keep the keyboard where it was.
  const refocus = focusKeeper();
  const status = app.status;
  const view = root.firstElementChild;
  let next: HTMLElement;
  try {
    if (app.opening) {
      next = renderOpening(app.opening.dir, app.opening.title);
    } else if (!status || !status.hasArchive) {
      next = renderStart();
    } else {
      const hash = location.hash || "#/";
      const m = hash.match(/^#\/ablage\/(\d+)$/);
      if (m) next = await renderDeposit(Number(m[1]));
      else if (hash === NEW_ARCHIVE_ROUTE) next = renderStart(true);
      else if (hash === "#/einstellungen" && !status.archive.readOnly) next = await renderSettings();
      else next = renderMain();
    }
  } catch (e) {
    if (seq !== routeSeq) return;
    toast(errMsg(e), "error");
    next = renderMain();
  }
  if (seq !== routeSeq) return;
  if (view) clear(root);
  root.appendChild(next);
  window.scrollTo(0, 0);
  refocus();
  // Back from "Neues Archiv anlegen …": the keyboard continues where it left.
  const hash = location.hash || "#/";
  if (lastHash === NEW_ARCHIVE_ROUTE && hash === "#/einstellungen") document.getElementById("archiv-wechseln")?.focus();
  lastHash = hash;
  // Once per opening: pending timestamps are asked about (never fetched
  // by themselves), before any waiting folder is offered.
  maybeAskPending(app.status);
  // Every status change re-renders: after "Archiv anlegen" or "öffnen",
  // and when a dialog closes, a waiting folder is offered.
  void offerFolders();
}

async function boot(): Promise<void> {
  // Background work in Go (the check and trusted-list refresh after
  // opening, the recent list). Listening starts before the first Status, so no change
  // between that snapshot and the first render is missed.
  Events.On(StatusGeaendert, () => void onStatusChanged());
  try {
    const [about, disclaimer, status] = await Promise.all([api.About(), api.Disclaimer(), api.Status()]);
    app.about = about;
    app.disclaimer = disclaimer;
    acceptStatus(status);
  } catch (e) {
    root.appendChild(h("main", { class: "page page--narrow" }, h("h1", null, "Rechnungsarchiv"), h("p", { role: "alert" }, errMsg(e))));
    return;
  }
  setRenderer(route);
  setFolderKick(() => void offerFolders());
  window.addEventListener("hashchange", () => void route());

  // A folder waits in Go (command line, a later launch, a drop); the event
  // only says "take it now". Listening starts here, after the boot calls, so
  // the queue is taken once more after the first render.
  Events.On(OrdnerWartet, () => void takeFolders());

  if (app.about.debug) {
    // Hooks for screenshots (sample data or the testtsa environment); not present otherwise.
    (window as unknown as Record<string, unknown>).raDebug = {
      archiveFolder: openArchiveDialog,
      exportDialog: openExportDialog,
      about: openAboutDialog,
      disclaimer: openDisclaimerDialog,
      rerender: async () => {
        acceptStatus(await api.Status());
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
      // The question about pending timestamps is decided first.
      const step = inbox.next(gateOf(location.hash === NEW_ARCHIVE_ROUTE || app.opening || app.picking ? null : app.status, archiveDialogOpen() || pendingQuestionOpen() || openingUndecided(app.status)));
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
