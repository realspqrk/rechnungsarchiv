// Session state of the shell. All data comes from Go; nothing is derived
// here beyond what the screens display.

import type { AboutInfo, CheckSummary, DisclaimerText, Status } from "./api";

export const app = {
  status: null as Status | null,
  about: null as AboutInfo | null,
  disclaimer: null as DisclaimerText | null,
  /** Result of "Archiv kontrollieren" in this session (banner on Main). */
  checked: null as CheckSummary | null,
  /** Table shows all Ablagen instead of the newest five. */
  showAll: false,
  /** An archive is being opened or created: nothing else can be done until
   * Go answers (screen "Archiv wird geöffnet …" / "… angelegt …"). */
  opening: null as { dir: string; title: string } | null,
  /** The folder picker for an archive is open: handed-over folders wait. */
  picking: false,
  /** Start form input, kept when creating the archive failed. */
  draft: null as { company: string; dir: string } | null,
  /** Status.session whose opening question ("Ausstehende Zeitstempel") was
   * decided: asked or nothing to ask. Asked once per opening. */
  asked: 0,
};

/**
 * Takes a status from Go unless it is older than the shown one: a poll or
 * refresh answered late (e.g. from before an archive switch) must not bring
 * an earlier state back (Status.rev only grows).
 */
export function acceptStatus(next: Status): boolean {
  if (app.status && next.rev < app.status.rev) return false;
  app.status = next;
  return true;
}

type Renderer = () => void | Promise<void>;
let renderer: Renderer = () => {};

export function setRenderer(r: Renderer): void {
  renderer = r;
}

/** Start screen while an archive is open ("Neues Archiv anlegen …"): the
 * open archive stays until another one is created or opened. */
export const NEW_ARCHIVE_ROUTE = "#/neues-archiv";

// While a menu is open, re-renders (polling during background work) wait
// until it closes, so the menu does not vanish under the pointer. The hold
// belongs to the menu that took it (its dispose function).
let holder: (() => void) | null = null;
let missed = false;

export function hold(dispose: () => void): void {
  holder = dispose;
}

/** Ends the hold of this menu; a missed re-render runs now. */
export function release(dispose: () => void): Promise<void> {
  if (holder !== dispose) return Promise.resolve();
  holder = null;
  if (!missed) return Promise.resolve();
  missed = false;
  return Promise.resolve(renderer());
}

/** A new route replaces the screen: the open menu is disposed (its
 * listeners removed) without another render. */
export function releaseHold(): void {
  const dispose = holder;
  holder = null;
  missed = false;
  dispose?.();
}

/** Re-renders the current route. */
export function rerender(): Promise<void> {
  if (holder) {
    missed = true;
    return Promise.resolve();
  }
  return Promise.resolve(renderer());
}

// Offers handed-over folders that waited (main.ts), without a re-render:
// e.g. from Einstellungen, whose form must keep its input.
let folderKick: () => void = () => {};

export function setFolderKick(f: () => void): void {
  folderKick = f;
}

export function kickFolders(): void {
  folderKick();
}

export function navigate(hash: string): void {
  if (location.hash === hash) void rerender();
  else location.hash = hash;
}
