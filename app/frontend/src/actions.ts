// Actions used by several screens.

import { api, errMsg, isLocked, type Status, type StampResult } from "./api";
import { h } from "./dom";
import { plural } from "./fmt";
import { app, rerender, acceptStatus } from "./state";
import { REASON_SHORT } from "./text";
import { busy, button, openDialog, toast } from "./ui";

/** Keyboard focus back to the element with this id on the (re-rendered)
 * screen, else to the main heading or the start screen's "öffnen". */
function refocus(id = ""): void {
  (document.getElementById(id) ?? document.getElementById("archiv-titel") ?? document.getElementById("start-oeffnen"))?.focus();
}

/** id of the focused element: where the keyboard returns after a switch. */
function focusedId(): string {
  return document.activeElement instanceof HTMLElement ? document.activeElement.id : "";
}

/**
 * Switches the archive (open or create): one at a time. Until Go answers,
 * the screen "Archiv wird …" replaces everything, so the previous archive
 * cannot be used and handed-over folders wait. Returns whether it worked;
 * on failure the previous screen comes back with the message.
 */
export async function switchArchive(dir: string, title: string, run: () => Promise<Status>, from = focusedId()): Promise<boolean> {
  if (app.opening) return false;
  // Nothing begun for the previous archive stays open (export, prompts).
  document.querySelectorAll<HTMLDialogElement>("dialog[open]").forEach((d) => d.close());
  app.opening = { dir, title };
  await rerender();
  try {
    const next = await run();
    acceptStatus(next);
    app.checked = null;
    app.opening = null;
    app.draft = null;
    // To the main screen without a hashchange round trip, then focus.
    if (location.hash !== "#/") history.replaceState(null, "", "#/");
    await rerender();
    refocus();
    return true;
  } catch (e) {
    app.opening = null;
    await rerender();
    toast(errMsg(e), "error");
    refocus(from);
    return false;
  }
}

/**
 * Opens an archive: dir from "Zuletzt geöffnet", otherwise the folder the
 * user picks. A folder this PC cannot write to opens view only (Go).
 */
export async function openArchiveFolder(dir = ""): Promise<void> {
  if (app.opening || app.picking) return;
  const from = focusedId();
  let picked = dir;
  if (!picked) {
    app.picking = true;
    try {
      picked = await api.PickFolder("archiv");
    } catch (e) {
      toast(errMsg(e), "error");
    } finally {
      app.picking = false;
    }
  }
  if (!picked) {
    await rerender(); // offers folders that waited meanwhile
    refocus(from);
    return;
  }
  await switchArchive(picked, "Archiv wird geöffnet …", () => api.OpenArchive(picked), from);
}

export async function refreshStatus(): Promise<void> {
  acceptStatus(await api.Status());
  rerender();
}

/** Re-reads the status after background work; re-renders the main screen only. */
export async function onStatusChanged(): Promise<void> {
  try {
    if (!acceptStatus(await api.Status())) return;
  } catch {
    return;
  }
  const hash = location.hash || "#/";
  if (hash === "#/" || hash === "#") rerender();
}

/**
 * Asks before taking over the lock of another PC (spec §1). The lock is
 * only removed after "Übernehmen"; Go refuses while that writer still runs.
 */
export function confirmTakeOver(message: string, session: number): Promise<boolean> {
  return new Promise((resolve) => {
    let result = false;
    const dlg = openDialog("sperre-title", "narrow", () => resolve(result));
    const cancel = button("Abbrechen", () => dlg.close());
    // Inside the dialog: a toast would sit behind the modal backdrop.
    const error = h("p", { class: "problem-box", role: "alert", hidden: true });
    const take: HTMLButtonElement = button("Übernehmen", () => {
      error.hidden = true;
      void busy(take, "Wird übernommen …", async () => {
        try {
          await api.TakeOver(session); // Go refuses if another archive is open now
        } catch (e) {
          error.textContent = errMsg(e); // e.g. the program still runs there
          error.hidden = false;
          cancel.focus();
          return;
        }
        result = true;
        dlg.close();
      });
    }, "primary");
    dlg.body.append(
      h(
        "div",
        { class: "dialog__content" },
        h("h1", { id: "sperre-title", class: "dialog__title" }, "Archiv wird anderswo verwendet"),
        h("p", { class: "m0" }, message),
        h(
          "p",
          { class: "muted small m0" },
          "Übernehmen Sie die Sperre nur, wenn auf dem anderen PC kein Rechnungsarchiv mehr läuft, etwa nach einem Absturz. Zwei Programme, die gleichzeitig in dasselbe Archiv schreiben, sind nicht vorgesehen.",
        ),
        error,
      ),
      h("div", { class: "dialog__actions" }, cancel, take),
    );
    cancel.focus();
  });
}

/** Runs fn; on a lock of another PC asks to take it over and runs fn once more. */
export async function withLock<T>(fn: () => Promise<T>, session = app.status?.session ?? 0): Promise<T> {
  try {
    return await fn();
  } catch (e) {
    if (!isLocked(e)) throw e;
    if (!(await confirmTakeOver(errMsg(e), session))) throw new Error("Abgebrochen: Das Archiv wird anderswo verwendet.");
    return await fn();
  }
}

/** "Jetzt nachholen": fetch missing timestamps, then refresh. */
export async function stampPending(btn: HTMLButtonElement): Promise<void> {
  const session = app.status?.session ?? 0;
  const r = await busy(btn, "Wird geholt …", () => withLock(() => api.StampPending(session), session));
  if (!r) {
    await refreshStatus();
    return;
  }
  await reportStamp(r);
}

/** Tells what a stamping run did, then refreshes the status. */
export async function reportStamp(r: StampResult): Promise<void> {
  const warnings = r.warnings ?? [];
  if (r.failed > 0) {
    // Changed, invalid or unreadable Ablagen are never stamped (CLI exit 5).
    const head = `${plural(r.failed, "Ablage wurde", "Ablagen wurden")} nicht gestempelt, weil sie verändert, ungültig oder unlesbar ${r.failed === 1 ? "ist" : "sind"}. Bitte „Archiv kontrollieren“.`;
    toast([head, ...warnings].join(" "), "error");
  } else if (r.pending > 0) {
    const why = REASON_SHORT[r.reason] ?? "";
    toast([`${plural(r.pending, "Zeitstempel", "Zeitstempel")} weiterhin ausstehend${why ? ` (${why})` : ""}.`, ...warnings].join(" "), "error");
  } else if (warnings.length) {
    toast(warnings.join(" "), "error");
  } else if (r.stamped > 0) {
    // Counts Ablagen: a free completion is no second timestamp.
    toast(`${plural(r.stamped, "Ablage", "Ablagen")} zeitgestempelt.`);
  } else {
    toast("Keine fehlenden Zeitstempel.");
  }
  await refreshStatus();
}
