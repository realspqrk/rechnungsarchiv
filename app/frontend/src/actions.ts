// Actions used by several screens.

import { api } from "./api";
import { plural } from "./fmt";
import { app, rerender } from "./state";
import { REASON_SHORT } from "./text";
import { busy, toast } from "./ui";

export async function refreshStatus(): Promise<void> {
  app.status = await api.Status();
  rerender();
}

/** "Jetzt nachholen": fetch missing timestamps, then refresh. */
export async function stampPending(btn: HTMLButtonElement): Promise<void> {
  const r = await busy(btn, "Wird geholt …", () => api.StampPending());
  if (!r) return;
  if (r.pending > 0) {
    const why = REASON_SHORT[r.reason] ?? "";
    toast(`${plural(r.pending, "Zeitstempel", "Zeitstempel")} weiterhin ausstehend${why ? ` (${why})` : ""}.`, "error");
  } else if (r.stamped > 0) {
    toast(`${plural(r.stamped, "Zeitstempel", "Zeitstempel")} geholt.`);
  }
  await refreshStatus();
}
