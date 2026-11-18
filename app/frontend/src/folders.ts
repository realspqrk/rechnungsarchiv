// Folders handed to the window (command line, a later launch, a drop) wait
// here until an archive is open for writing; then each is offered once in
// "Ordner archivieren". Pure module (unit.test.ts): no DOM, no Wails calls.

import type { FolderDrop, Status } from "./api";

/** What decides whether a folder can be offered now. */
export interface Gate {
  hasArchive: boolean;
  readOnly: boolean;
  /** Another PC holds the lock: nothing can be archived until it is taken over. */
  locked: boolean;
  /** An "Ordner archivieren" dialog is open (or opening). */
  dialogOpen: boolean;
}

export function gateOf(status: Status | null, dialogOpen: boolean): Gate {
  return {
    hasArchive: !!status?.hasArchive,
    readOnly: !!status?.archive?.readOnly,
    locked: !!status?.lock,
    dialogOpen,
  };
}

/** One thing to do now: open the dialog for a folder, or show a message. */
export interface Step {
  folder?: string;
  error?: string;
  note?: string;
}

export const READ_ONLY_NOTE = "Nur kontrollieren: Der übergebene Ordner wird nicht archiviert.";

export class FolderInbox {
  private items: FolderDrop[] = [];
  /** The folder whose dialog is open; a handover of it meanwhile is not queued again. */
  private active = "";

  get size(): number {
    return this.items.length;
  }

  /** Adds what Go handed over (Shell.TakeFolders); a folder already waiting or being offered is not added twice. */
  add(drops: FolderDrop[] | null | undefined): void {
    for (const d of drops ?? []) {
      if (d.folder) {
        if (d.folder !== this.active && !this.items.some((i) => i.folder === d.folder)) this.items.push({ folder: d.folder, error: "" } as FolderDrop);
      } else if (d.error) {
        this.items.push({ folder: "", error: d.error } as FolderDrop);
      }
    }
  }

  /**
   * The next step for this gate, or null to keep waiting. Folders wait for
   * a writable archive; read-only mode drops them with one note. A drop
   * error only counts while archiving is possible, never later.
   */
  next(g: Gate): Step | null {
    if (!g.dialogOpen) this.active = ""; // its dialog is closed: a later request may offer it again
    if (!g.hasArchive || g.locked || g.dialogOpen || g.readOnly) {
      const hadFolder = this.items.some((i) => i.folder);
      this.items = g.readOnly && g.hasArchive ? [] : this.items.filter((i) => i.folder);
      return g.readOnly && g.hasArchive && hadFolder ? { note: READ_ONLY_NOTE } : null;
    }
    const d = this.items.shift();
    if (!d) return null;
    if (!d.folder) return { error: d.error };
    this.active = d.folder;
    return { folder: d.folder };
  }
}
