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
};

type Renderer = () => void | Promise<void>;
let renderer: Renderer = () => {};

export function setRenderer(r: Renderer): void {
  renderer = r;
}

/** Start screen while an archive is open ("Anderes Archiv öffnen …"): the
 * open archive stays until another one is created or opened. */
export const SWITCH_ROUTE = "#/archiv-wechseln";

/** Re-renders the current route. */
export function rerender(): void {
  void renderer();
}

export function navigate(hash: string): void {
  if (location.hash === hash) rerender();
  else location.hash = hash;
}
