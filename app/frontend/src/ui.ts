// Shared UI building blocks.

import { h, icon, clear } from "./dom";
import { errMsg } from "./api";
import type { Tone } from "./text";

export type Variant = "primary" | "secondary" | "link" | "text" | "amber" | "plain-link" | "inline-link";

export function button(
  label: string | Node | (string | Node)[],
  onClick: (ev: MouseEvent) => void,
  variant: Variant = "secondary",
  attrs: Record<string, unknown> = {},
): HTMLButtonElement {
  const cls = ["btn", `btn--${variant}`, attrs.class].filter(Boolean).join(" ");
  return h("button", { type: "button", ...attrs, class: cls, onClick }, ...(Array.isArray(label) ? label : [label]));
}

/** Runs an async action while the button shows a busy label. */
export async function busy<T>(btn: HTMLButtonElement, busyLabel: string, fn: () => Promise<T>): Promise<T | undefined> {
  const old = Array.from(btn.childNodes);
  btn.disabled = true;
  btn.setAttribute("aria-busy", "true");
  btn.textContent = busyLabel;
  try {
    return await fn();
  } catch (e) {
    toast(errMsg(e), "error");
    return undefined;
  } finally {
    if (btn.isConnected) {
      btn.disabled = false;
      btn.removeAttribute("aria-busy");
      clear(btn);
      old.forEach((n) => btn.appendChild(n));
    }
  }
}

const TONE_ICON: Record<Tone | "blue", string> = {
  green: "check",
  neutral: "info",
  amber: "clock",
  grey: "info",
  red: "warn",
  blue: "search",
};

/** A full-width notice box (Main screen banners). */
export function banner(
  tone: Tone | "blue",
  label: string,
  title: string,
  text: string | Node | null,
  action: Node | null = null,
  iconName = TONE_ICON[tone],
): HTMLElement {
  return h(
    "section",
    { class: `banner banner--${tone}`, "aria-label": label },
    icon(iconName, 20, tone === "green" ? 2.4 : 2.2, "banner__icon"),
    h("span", { class: "banner__text" }, title ? h("strong", null, title) : null, title && text ? " " : null, text),
    action,
  );
}

let toastTimer = 0;

/** Short message; errors use role=alert so screen readers announce them. */
export function toast(message: string, kind: "error" | "ok" = "ok"): void {
  const box = document.getElementById("toasts");
  if (!box) return;
  clear(box);
  const el = h(
    "div",
    { class: `toast toast--${kind}`, role: kind === "error" ? "alert" : "status" },
    icon(kind === "error" ? "warn" : "check", 18, 2.2),
    h("span", null, message),
    h("button", { type: "button", class: "toast__close", "aria-label": "Meldung schließen", onClick: () => clear(box) }, "×"),
  );
  box.appendChild(el);
  window.clearTimeout(toastTimer);
  toastTimer = window.setTimeout(() => clear(box), kind === "error" ? 12000 : 5000);
}

export interface DialogHandle {
  el: HTMLDialogElement;
  body: HTMLElement;
  close: () => void;
  /** While locked, Esc and backdrop do not close (e.g. during archiving). */
  lock: (locked: boolean) => void;
}

/** Opens a modal native <dialog> (focus trap, Esc, aria-modal for free). */
export function openDialog(labelledBy: string, width: "narrow" | "wide" = "narrow", onClose?: () => void): DialogHandle {
  const body = h("div", { class: "dialog__body" });
  const el = h("dialog", { class: `dialog dialog--${width}`, "aria-labelledby": labelledBy }, body);
  let locked = false;
  el.addEventListener("cancel", (ev) => {
    if (locked) ev.preventDefault();
  });
  el.addEventListener("close", () => {
    el.remove();
    onClose?.();
  });
  document.body.appendChild(el);
  el.showModal();
  return {
    el,
    body,
    close: () => el.close(),
    lock: (l) => {
      locked = l;
    },
  };
}

/** Label of the read-only mode ("Archiv nur kontrollieren", spec §9). */
export function readOnlyBadge(): HTMLElement {
  return h("span", { class: "badge badge--blue" }, icon("search", 14, 2.2), "Nur kontrollieren");
}

/** Sub-page header: "‹ Übersicht / Title", with the read-only label if set. */
export function subHeader(title: string, width: "detail" | "settings", readOnly = false): HTMLElement {
  return h(
    "header",
    { class: "topbar" },
    h(
      "div",
      { class: `topbar__inner topbar__inner--${width}` },
      h("a", { href: "#/", class: "back-link" }, icon("back", 18, 2), "Übersicht"),
      h("span", { class: "muted", "aria-hidden": "true" }, "/"),
      h("h1", { class: "topbar__title" }, title),
      readOnly ? readOnlyBadge() : null,
    ),
  );
}

/**
 * Long dialog text: the content scrolls with a visible scrollbar, and the
 * footer says "Weiteren Text durch Scrollen lesen" until the end is reached.
 */
export function scrollCue(content: HTMLElement, actions: HTMLElement): void {
  const cue = h("span", { class: "scroll-cue", "aria-live": "polite" }, "Weiteren Text durch Scrollen lesen ", h("span", { "aria-hidden": "true" }, "↓"));
  actions.classList.add("dialog__actions--cue");
  actions.prepend(cue);
  content.classList.add("dialog__content--scroll");
  content.tabIndex = 0;
  content.setAttribute("role", "region");
  const update = () => {
    cue.hidden = content.scrollTop + content.clientHeight >= content.scrollHeight - 4;
  };
  content.addEventListener("scroll", update, { passive: true });
  window.addEventListener("resize", update);
  requestAnimationFrame(update);
}

/** Read-only path box with a button next to it. */
export function pathField(label: string, path: string, buttonEl: Node | null, id?: string): HTMLElement {
  return h(
    "div",
    { class: "field" },
    h("span", { class: "field__label", id }, label),
    h(
      "div",
      { class: "path-row" },
      h("span", { class: "path-box mono", "aria-labelledby": id }, path ? pathText(path) : "Noch nicht gewählt"),
      buttonEl,
    ),
  );
}

/**
 * A path as text that may wrap after each separator (\ or /) instead of
 * inside a name: "…\demo\<wbr>Betriebspruefung-2026". Copying the text
 * gives the path unchanged (<wbr> adds no characters).
 */
export function pathText(path: string): HTMLElement {
  // One element: inside a flex box (.path-box) every <wbr> would otherwise
  // become a flex item of its own.
  // Each segment is an inline-block (.path-seg), so a line breaks at a
  // separator first ("…\demo\" / "Betriebspruefung-2026") and inside a
  // segment only when the segment alone is wider than the box.
  const out = h("span", { class: "path-text" });
  for (const part of path.split(/(?<=[\\/])/)) {
    if (out.childNodes.length) out.append(h("wbr"));
    out.append(h("span", { class: "path-seg" }, part));
  }
  return out;
}

/** Replaces the content of a path box. */
export function setPath(el: HTMLElement, path: string): void {
  clear(el);
  el.append(pathText(path));
}

/** Short note next to actions that wait while another PC holds the lock. */
export function lockedNote(): HTMLElement {
  return h("span", { class: "locked-note" }, icon("lock", 14, 2), "Archiv gesperrt");
}
