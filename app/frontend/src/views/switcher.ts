// The "Archiv" card of Einstellungen: where the archive folder is, and
// "Archiv wechseln ▾" (Zuletzt geöffnet, Bestehendes Archiv öffnen …,
// Neues Archiv anlegen …). Most users have one archive, so this lives here
// and not on the main screen.

import { api, errMsg, list, type Status } from "../api";
import { clear, h, icon } from "../dom";
import { app, hold, navigate, NEW_ARCHIVE_ROUTE, release } from "../state";
import { openArchiveFolder } from "../actions";
import { button, pathText, toast } from "../ui";

export function archiveCard(s: Status): HTMLElement {
  return h(
    "section",
    { class: "card card--form", "aria-labelledby": "s-archiv" },
    h("div", { class: "card__head" }, h("h2", { id: "s-archiv", class: "card__title card__title--17" }, "Archiv"), switcher(s)),
    h("span", { class: "field__label", id: "s-ordner" }, "Archivordner"),
    h(
      "div",
      { class: "path-row" },
      h("span", { class: "path-box mono", "aria-labelledby": "s-ordner" }, pathText(s.archive.dir)),
      button("Im Explorer öffnen", async () => {
        try {
          await api.OpenFolder(s.archive.dir);
        } catch (e) {
          toast(errMsg(e), "error");
        }
      }),
    ),
    h(
      "p",
      { class: "info-line info-line--warn" },
      icon("warn", 16, 2),
      h("span", null, "Kopieren Sie den ganzen Archivordner auf einen zweiten Datenträger: nach jeder Archivierung und wenn ein Zeitstempel nachgeholt wurde. Der Zeitstempel macht Veränderungen an den Ablagen erkennbar; vor Verlust schützt er nicht."),
    ),
  );
}

/** "Archiv wechseln ▾" with its menu (WAI-ARIA menu button). */
function switcher(s: Status): HTMLElement {
  const items: HTMLButtonElement[] = [];
  const item = (run: () => void, ...label: (Node | string)[]) => {
    const b = h("button", { type: "button", role: "menuitem", tabindex: "-1", class: "switcher__item" }, ...label);
    b.addEventListener("click", () => {
      close(false);
      run();
    });
    items.push(b);
    return b;
  };

  const menu = h("div", { id: "archiv-menu", role: "menu", class: "switcher__menu", "aria-labelledby": "archiv-wechseln", hidden: true });
  /** Built when the menu opens: "Zuletzt geöffnet" is loaded in the
   * background and may arrive after this page was rendered. */
  const fill = () => {
    items.length = 0;
    clear(menu);
    const now = app.status?.session === s.session ? app.status : s;
    const recent = list(now.recent);
    menu.append(
      ...(recent.length
        ? [
            h(
              "div",
              { role: "group", "aria-labelledby": "zuletzt" },
              h("div", { class: "switcher__label", id: "zuletzt", role: "none" }, "Zuletzt geöffnet"),
              ...recent.map((r) =>
                item(
                  () => void openArchiveFolder(r.dir),
                  h("span", { class: "switcher__company" }, r.company),
                  h("span", { class: "switcher__dir mono" }, r.dir),
                ),
              ),
            ),
            h("div", { role: "separator", class: "switcher__sep" }),
          ]
        : []),
      item(() => void openArchiveFolder(), icon("folder", 16, 2), "Bestehendes Archiv öffnen …"),
      item(() => navigate(NEW_ARCHIVE_ROUTE), icon("plus", 16, 2), "Neues Archiv anlegen …"),
    );
  };

  const trigger = h(
    "button",
    { type: "button", id: "archiv-wechseln", class: "btn btn--secondary switcher__btn", "aria-haspopup": "menu", "aria-expanded": "false", "aria-controls": "archiv-menu" },
    "Archiv wechseln",
    icon("chevronDown", 18, 2, "switcher__chevron"),
  );

  const focusItem = (i: number) => items[(i + items.length) % items.length]?.focus();
  // "click", not "pointerdown": the clicked element outside handles its
  // click first; only then may a waiting re-render replace the screen.
  const onOutside = (ev: MouseEvent) => {
    if (!wrap.contains(ev.target as Node)) close(false);
  };
  const dispose = () => {
    menu.hidden = true;
    trigger.setAttribute("aria-expanded", "false");
    document.removeEventListener("click", onOutside);
  };
  function open(): void {
    fill();
    menu.hidden = false;
    trigger.setAttribute("aria-expanded", "true");
    hold(dispose);
    document.addEventListener("click", onOutside);
    focusItem(0);
  }
  function close(refocus: boolean): void {
    if (menu.hidden) return;
    dispose();
    if (refocus) trigger.focus();
    // A re-render that waited replaces this trigger: focus the new one.
    void release(dispose).then(() => {
      if (refocus) document.getElementById("archiv-wechseln")?.focus();
    });
  }

  trigger.addEventListener("click", () => (menu.hidden ? open() : close(true)));
  trigger.addEventListener("keydown", (ev) => {
    if (ev.key === "ArrowDown" && menu.hidden) {
      ev.preventDefault();
      open();
    }
  });
  menu.addEventListener("keydown", (ev) => {
    const i = items.indexOf(document.activeElement as HTMLButtonElement);
    switch (ev.key) {
      case "ArrowDown":
        focusItem(i + 1);
        break;
      case "ArrowUp":
        focusItem(i - 1);
        break;
      case "Home":
        focusItem(0);
        break;
      case "End":
        focusItem(-1);
        break;
      case "Escape":
      case "Tab":
        // Tab: a re-render that waited replaces the page; continue from the
        // (new) trigger instead of losing the focus.
        close(true);
        break;
      default:
        return;
    }
    ev.preventDefault();
  });

  const wrap = h("div", { class: "switcher" }, trigger, menu);
  return wrap;
}
