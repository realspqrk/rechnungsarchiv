// "Ausstehende Zeitstempel": opening an archive never stamps by itself.
// When a run could fetch something (Status.due), the user is asked once per
// opening (program start or switch), with what it costs.

import { api, errMsg, type Status } from "../api";
import { h } from "../dom";
import { n } from "../fmt";
import { acceptStatus, app, kickFolders, rerender } from "../state";
import { button, openDialog } from "../ui";
import { reportStamp, withLock } from "../actions";
import { archiveDialogOpen } from "./archive";

let openNow = false;
let retrying = false;

/** The question is open: handed-over folders wait until it is decided. */
export function pendingQuestionOpen(): boolean {
  return openNow;
}

/** The opening question of this (writable) archive is not decided yet:
 * handed-over folders wait for it, so it always comes first. */
export function openingUndecided(s: Status | null): boolean {
  return !!s?.hasArchive && !s.archive.readOnly && app.asked !== s.session;
}

// Provider or credential saves in flight for savingSession: the question
// waits for them.
let saving = 0;
let savingSession = 0;

/**
 * The user changes provider or credentials in Einstellungen of the archive
 * opened in session. Call when the save starts and the returned function
 * with its outcome. A successful setup is not answered with the opening
 * question (the overview offers "Jetzt nachholen"); while saves run, the
 * question waits; if none succeeded, it is asked as usual.
 */
export function settleOpeningQuestion(session: number): (saved: boolean) => void {
  if (app.status?.session !== session) return () => {}; // a page of an archive no longer open
  if (savingSession !== session) {
    savingSession = session;
    saving = 0;
  }
  saving++;
  let done = false;
  return (saved) => {
    // Only for the archive still open: an old answer after a switch must
    // not decide the next archive's question.
    if (done || savingSession !== session || app.status?.session !== session) return;
    done = true;
    saving--;
    if (saved) {
      app.asked = session;
      kickFolders(); // folders that waited for the decision
    }
    // Undecided again: the overview asks now (Einstellungen keeps its input).
    else if (saving === 0 && (location.hash === "#/" || location.hash === "")) void rerender();
  };
}

/**
 * Asks once per opening, when nothing else is going on. Until the check
 * after opening is done (Status.busy) a zero count may still change (e.g.
 * the trusted lists are loaded first), so it waits for it.
 */
export function maybeAskPending(s: Status | null): void {
  if (!openingUndecided(s) || app.opening || openNow) return;
  const st = s as Status;
  if (savingSession === st.session && saving > 0) return; // a setup is being saved
  const count = st.due.paid + st.due.free;
  if (count === 0 && st.busy) return;
  // Another PC holds the lock: waiting is the way, not "Jetzt holen". Asked
  // once the lock is gone ("Archiv kontrollieren", "Sperre übernehmen").
  if (st.lock) return;
  // Another dialog first (e.g. "Über", a dropped folder): asked when it closes.
  if (archiveDialogOpen() || document.querySelector("dialog[open]")) {
    retryOnClose();
    return;
  }
  app.asked = st.session;
  if (count > 0) ask(st);
}

/** Tries again when the next dialog closes ("close" does not bubble). */
function retryOnClose(): void {
  if (retrying) return;
  retrying = true;
  document.addEventListener(
    "close",
    () => {
      retrying = false;
      // The render asks (route) and then offers folders that waited.
      window.setTimeout(() => void rerender(), 0);
    },
    { capture: true, once: true },
  );
}

function ask(s: Status): void {
  openNow = true;
  const session = s.session;
  const provider = s.provider?.name ?? "dem Anbieter";
  const { paid, free } = s.due;
  const dlg = openDialog("frage-title", "narrow", () => {
    openNow = false;
    void rerender(); // offers folders that waited meanwhile
  });
  // Inside the dialog: a toast would sit behind the modal backdrop.
  const error = h("p", { class: "problem-box", role: "alert", hidden: true });
  const later = button("Später", () => dlg.close(), "secondary", { id: "frage-spaeter" });
  const now: HTMLButtonElement = button("Jetzt holen", async () => {
    now.disabled = true;
    later.disabled = true;
    now.textContent = "Wird geholt …";
    error.hidden = true;
    dlg.lock(true);
    try {
      // A lock of another PC is asked about on top; this question stays
      // open meanwhile, so handed-over folders keep waiting.
      const r = await withLock(() => api.StampPending(session), session);
      dlg.lock(false);
      dlg.close();
      await reportStamp(r);
      return;
    } catch (e) {
      error.textContent = errMsg(e);
      error.hidden = false;
      // Go may have seen a lock meanwhile: the overview shows it at once.
      try {
        acceptStatus(await api.Status());
      } catch {
        // the message above is what matters
      }
    }
    dlg.lock(false);
    now.disabled = false;
    later.disabled = false;
    now.textContent = "Jetzt holen";
    later.focus();
  }, "primary", { id: "frage-holen" });

  const items: HTMLElement[] = [];
  if (paid > 0) {
    items.push(
      h(
        "li",
        null,
        h("strong", null, paid === 1 ? "1 neuer Zeitstempel" : `Bis zu ${n(paid)} neue Zeitstempel`),
        ` – ${paid === 1 ? "wird" : "werden"} von ${provider} verrechnet`,
      ),
    );
  }
  if (free > 0) {
    items.push(
      h(
        "li",
        null,
        h("strong", null, free === 1 ? "1 Ablage" : `${n(free)} Ablagen`),
        `: Prüfdaten ${free === 1 ? "zum bereits bezahlten Zeitstempel" : "zu bereits bezahlten Zeitstempeln"} ergänzen – kostenlos (nur bis 24 Stunden nach dem Zeitstempel)`,
      ),
    );
  }
  dlg.body.append(
    h(
      "div",
      { class: "dialog__content" },
      h("h1", { id: "frage-title", class: "dialog__title" }, "Ausstehende Zeitstempel"),
      h("p", { class: "m0" }, "In diesem Archiv ist noch etwas offen:"),
      h("ul", { class: "due-list" }, ...items),
      h("p", { class: "m0" }, h("strong", null, "Empfehlung: jetzt holen."), " Bis dahin bleiben diese Ablagen „ausstehend“."),
      h("p", { class: "muted small m0" }, "Später geht es jederzeit über „Jetzt nachholen“ in der Übersicht."),
      error,
    ),
    h("div", { class: "dialog__actions" }, later, now),
  );
  // It appears by itself at start: Enter must not buy a timestamp unasked.
  later.focus();
}
