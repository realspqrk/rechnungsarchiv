// Actions used by several screens.

import { api, errMsg, isLocked } from "./api";
import { h } from "./dom";
import { plural } from "./fmt";
import { app, rerender } from "./state";
import { REASON_SHORT } from "./text";
import { busy, button, openDialog, toast } from "./ui";

export async function refreshStatus(): Promise<void> {
  app.status = await api.Status();
  rerender();
}

/** Re-reads the status after background work; re-renders the main screen only. */
export async function onStatusChanged(): Promise<void> {
  try {
    app.status = await api.Status();
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
export function confirmTakeOver(message: string): Promise<boolean> {
  return new Promise((resolve) => {
    let result = false;
    const dlg = openDialog("sperre-title", "narrow", () => resolve(result));
    const cancel = button("Abbrechen", () => dlg.close());
    const take = button("Übernehmen", () => {
      void busy(take, "Wird übernommen …", async () => {
        await api.TakeOver();
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
      ),
      h("div", { class: "dialog__actions" }, cancel, take),
    );
    cancel.focus();
  });
}

/** Runs fn; on a lock of another PC asks to take it over and runs fn once more. */
export async function withLock<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn();
  } catch (e) {
    if (!isLocked(e)) throw e;
    if (!(await confirmTakeOver(errMsg(e)))) throw new Error("Abgebrochen: Das Archiv wird anderswo verwendet.");
    return await fn();
  }
}

/** "Jetzt nachholen": fetch missing timestamps, then refresh. */
export async function stampPending(btn: HTMLButtonElement): Promise<void> {
  const r = await busy(btn, "Wird geholt …", () => withLock(() => api.StampPending()));
  if (!r) {
    await refreshStatus();
    return;
  }
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
    toast(`${plural(r.stamped, "Zeitstempel", "Zeitstempel")} geholt.`);
  } else {
    toast("Keine fehlenden Zeitstempel.");
  }
  await refreshStatus();
}
