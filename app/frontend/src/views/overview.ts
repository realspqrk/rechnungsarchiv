// Main screen ("Übersicht"): banners, drop zone, tiles, table of Ablagen.

import { api, list, type Deposit, type Status } from "../api";
import { h, icon } from "../dom";
import { dateTimeShort, n, plural, seq } from "../fmt";
import { app, navigate, rerender } from "../state";
import { findingText, QUAL, QUAL_SHORT, REASON_SHORT, STATE, depositTone } from "../text";
import { banner, busy, button, lockedNote, readOnlyBadge } from "../ui";
import { checkSummary, lastCheckText } from "../summary";
import { confirmTakeOver, onStatusChanged, stampPending } from "../actions";
import { openArchiveDialog } from "../dialogs/archive";
import { openExportDialog } from "../dialogs/export";
import { openAboutDialog, openDisclaimerDialog } from "../dialogs/about";

const FIRST_ROWS = 5;
const POLL_MS = 1000;
let pollTimer = 0;

/** While Go works in the background (Status.busy), re-read the status. */
function pollWhileBusy(s: Status): void {
  window.clearTimeout(pollTimer);
  if (!s.busy) return;
  pollTimer = window.setTimeout(() => void onStatusChanged(), POLL_MS);
}

export function renderMain(): HTMLElement {
  const s = app.status as Status;
  const ro = s.archive.readOnly;
  // Another PC holds the writer lock: nothing can be archived or stamped
  // here until it is released or taken over (banner). Checking and the
  // export stay available.
  const locked = !ro && !!s.lock;
  const deposits = list(s.deposits);
  const findings = list(s.findings);
  pollWhileBusy(s);

  const checkBtn = button("Archiv kontrollieren", () => {
    void busy(checkBtn, "Kontrolle läuft …", async () => {
      app.checked = await api.Verify();
      app.status = await api.Status();
      rerender();
    });
  });

  const header = h(
    "header",
    { class: "topbar" },
    h(
      "div",
      { class: "topbar__inner topbar__inner--main" },
      h("div", { class: "brand" }, icon("logo", 26, 1.8, "brand__logo"), h("span", { class: "brand__name brand__name--small" }, "Rechnungsarchiv")),
      h("div", { class: "topbar__archive" }, s.archive.company, " · ", h("span", { class: "mono mono--small" }, s.archive.dir), ro ? [" ", readOnlyBadge()] : null),
      h(
        "nav",
        { class: "topbar__nav", "aria-label": "Aktionen" },
        checkBtn,
        button("Export für Betriebsprüfung", () => openExportDialog()),
        ro ? null : h("a", { href: "#/einstellungen", class: "btn btn--secondary btn--icon", "aria-label": "Einstellungen", title: "Einstellungen" }, icon("gear", 20, 1.8)),
      ),
    ),
  );

  const banners: (HTMLElement | null)[] = [];
  if (ro) {
    banners.push(
      banner(
        "blue",
        "Modus",
        "Nur kontrollieren.",
        `Dieses Archiv (${s.archive.dir}) wird nicht verändert, nichts wird gestempelt oder gespeichert.`,
        button("Kontrolle beenden", async () => {
          await api.CloseArchive();
          app.status = await api.Status();
          app.checked = null;
          navigate("#/");
        }, "secondary", { class: "btn--on-banner" }),
      ),
    );
  }
  if (s.lock && !ro) {
    const l = s.lock;
    const since = dateTimeShort(l.since);
    const takeBtn: HTMLButtonElement = button("Sperre übernehmen …", async () => {
      const msg = `Das Archiv wird auf ${l.host} von ${l.user} verwendet (seit ${since}).`;
      if (await confirmTakeOver(msg)) await stampPending(takeBtn);
    }, "secondary", { class: "btn--on-banner" });
    banners.push(
      banner(
        "amber",
        "Sperre",
        `Archiv ist auf ${l.host} gesperrt`,
        `(Benutzer ${l.user}, seit ${since}). Solange die Sperre besteht, wird hier nichts archiviert und kein Zeitstempel geholt.`,
        takeBtn,
        "lock",
      ),
    );
  }
  if (s.busy === "stempeln") {
    banners.push(banner("neutral", "Hintergrund", "", "Ausstehende Zeitstempel werden geholt …", null, "clock"));
  }
  if (s.trustList.stale) {
    banners.push(banner("amber", "Vertrauensliste", "", "Vertrauensliste veraltet – Internetverbindung prüfen oder Programm aktualisieren", null, "warn"));
  }
  if (app.checked) banners.push(checkBanner());
  if (!ro && !s.provider) {
    banners.push(
      banner(
        "amber",
        "Zeitstempel-Dienst fehlt",
        "Noch kein Zeitstempel-Dienst eingerichtet.",
        "Sie können schon archivieren; die Ablagen erhalten ihren Zeitstempel, sobald ein Dienst eingerichtet ist.",
        h("a", { href: "#/einstellungen", class: "btn btn--primary" }, "Jetzt einrichten"),
      ),
    );
  }
  for (const f of findings) {
    const t = findingText(f);
    banners.push(banner("red", "Befund", t.title, t.text, null, "warn"));
  }

  let drop: HTMLElement | null = null;
  if (locked) {
    // No data-file-drop-target: Wails delivers no drop here while locked.
    drop = h(
      "section",
      { class: "dropzone dropzone--locked", "aria-label": "Ordner archivieren (gesperrt)", "aria-disabled": "true", id: "dropzone" },
      icon("folderUp", 44, 1.6, "dropzone__icon"),
      h("div", { class: "dropzone__title" }, "Ordner mit Rechnungen hierher ziehen"),
      h("div", { class: "dropzone__text" }, "Solange das Archiv auf einem anderen PC gesperrt ist, kann hier nichts archiviert werden."),
      lockedNote(),
      button("Ordner auswählen …", () => {}, "primary", { class: "dropzone__btn", disabled: true }),
    );
  } else if (!ro) {
    drop = h(
      "section",
      { class: "dropzone", "aria-label": "Ordner archivieren", "data-file-drop-target": "", id: "dropzone" },
      icon("folderUp", 44, 1.6, "dropzone__icon"),
      h("div", { class: "dropzone__title" }, "Ordner mit Rechnungen hierher ziehen"),
      h("div", { class: "dropzone__text" }, "Das Archiv erhält eine Kopie (Ablage) mit Prüfsumme und dem Zeitstempel eines qualifizierten EU-Dienstes. Ihr Ordner bleibt unverändert."),
      button("Ordner auswählen …", async () => {
        const folder = await api.PickFolder("ablage");
        if (folder) openArchiveDialog(folder);
      }, "primary", { class: "dropzone__btn" }),
    );
  }

  return h(
    "div",
    { class: "screen" },
    header,
    h(
      "main",
      { class: "page page--main" },
      ...banners,
      drop,
      tiles(s),
      table(deposits, ro, locked),
      footer(),
    ),
  );
}

function checkBanner(): HTMLElement {
  const v = checkSummary(app.checked!);
  return banner(v.tone, "Kontrollergebnis", v.title, v.text, null, v.icon);
}

function tiles(s: Status): HTMLElement {
  const c = s.counts;
  let cls = "state--green";
  let iconName = "check";
  let main = "";
  const rest: string[] = [];
  const add = (v: number, label: string) => v && rest.push(`${n(v)} ${label}`);
  if (c.total === 0) {
    cls = "";
    iconName = "";
    main = "Noch keine Ablage";
  } else if (c.invalid || c.modified) {
    cls = "state--red";
    iconName = "cross";
    main = c.modified ? `${plural(c.modified, "Ablage", "Ablagen")} verändert` : `${plural(c.invalid, "Ablage", "Ablagen")} ungültig`;
    if (c.modified) add(c.invalid, "ungültig");
    add(c.indeterminate, "unbestimmt");
    add(c.pending, "ausstehend");
    add(c.stamped, "zeitgestempelt");
  } else if (c.indeterminate) {
    cls = "state--grey";
    iconName = "info";
    main = `${n(c.indeterminate)} unbestimmt`;
    add(c.pending, "ausstehend");
    add(c.stamped, "zeitgestempelt");
  } else if (c.pending) {
    cls = "state--amber";
    iconName = "clock";
    main = `${plural(c.pending, "Zeitstempel", "Zeitstempel")} ausstehend`;
    add(c.stamped, "zeitgestempelt");
  } else {
    main = c.total === 1 ? "Zeitgestempelt" : `Alle ${n(c.total)} zeitgestempelt`;
  }

  const last = lastCheckText(s.lastCheck, c.total);

  return h(
    "section",
    { class: "tiles", "aria-label": "Zustand des Archivs" },
    h("div", { class: "tile tile--small" }, h("div", { class: "tile__label" }, "Ablagen"), h("div", { class: "tile__value" }, n(c.total))),
    h(
      "div",
      { class: "tile" },
      h("div", { class: "tile__label" }, "Zustand"),
      h(
        "div",
        { class: `tile__state ${cls}` },
        iconName ? icon(iconName, 18, 2.2) : null,
        main,
        rest.length ? h("span", { class: "tile__rest" }, ` · ${rest.join(" · ")}`) : null,
      ),
    ),
    h("div", { class: "tile" }, h("div", { class: "tile__label" }, "Zuletzt kontrolliert"), h("div", { class: "tile__text" }, last)),
  );
}

function table(deposits: Deposit[], ro: boolean, locked: boolean): HTMLElement {
  if (deposits.length === 0) {
    return h(
      "section",
      { class: "card card--table card--empty", "aria-label": "Ablagen" },
      h("p", null, ro || locked ? "Noch keine Ablage." : "Noch keine Ablage. Ziehen Sie einen Ordner mit Rechnungen in das Feld oben oder wählen Sie „Ordner auswählen …“."),
    );
  }
  const shown = app.showAll ? deposits : deposits.slice(0, FIRST_ROWS);
  const more = deposits.length - shown.length;
  return h(
    "section",
    { class: "card card--table", "aria-label": "Ablagen" },
    h(
      "div",
      { class: "table-scroll" },
      h(
        "table",
        { class: "table" },
        h(
          "thead",
          null,
          h("tr", null, ...["Nr.", "Ablage", "Belegjahr", "Dateien", "Zeitstempel", "Status"].map((t) => h("th", { scope: "col" }, t))),
        ),
        h("tbody", null, ...shown.map((d) => row(d, ro, locked))),
      ),
    ),
    more > 0 || app.showAll
      ? h(
          "div",
          { class: "table__more" },
          button(
            more > 0 ? `Weitere ${plural(more, "Ablage", "Ablagen")} anzeigen` : "Nur die neuesten anzeigen",
            () => {
              app.showAll = !app.showAll;
              rerender();
            },
            "text",
          ),
        )
      : null,
  );
}

function row(d: Deposit, ro: boolean, locked: boolean): HTMLElement {
  let stamp = "–";
  if (d.state === STATE.pending) stamp = "ausstehend";
  else if (d.genTime) stamp = dateTimeShort(d.genTime);
  return h(
    "tr",
    null,
    h("td", { class: "mono muted" }, seq(d.seq)),
    h("td", null, h("a", { href: `#/ablage/${d.seq}`, class: "row-link" }, d.name)),
    h("td", null, String(d.belegjahr)),
    h("td", null, n(d.files)),
    h("td", null, stamp),
    h("td", null, statusCell(d, ro, locked)),
  );
}

export function statusCell(d: Deposit, ro: boolean, locked = false): HTMLElement {
  const tone = depositTone(d);
  let label = "";
  let sub: (string | Node)[] = [];
  let iconName = "check";
  switch (d.state) {
    case STATE.stamped:
      label = "Zeitgestempelt";
      sub = [QUAL_SHORT[d.qualification] ?? ""];
      iconName = d.qualification === QUAL.unconfirmed ? "info" : "check";
      break;
    case STATE.pending: {
      label = "Ausstehend";
      iconName = "clock";
      sub = [REASON_SHORT[d.reason] ?? d.reason];
      if (!ro && d.reason !== "kein-dienst") {
        const b: HTMLButtonElement = button("Jetzt nachholen", () => void stampPending(b), "plain-link", {
          disabled: locked,
          title: locked ? "Archiv gesperrt: erst die Sperre übernehmen" : undefined,
        });
        sub.push(" · ", b);
      }
      break;
    }
    case STATE.indeterminate:
      label = "Unbestimmt";
      iconName = "info";
      sub = ["Zeitpunkt nicht sicher belegt"];
      break;
    case STATE.modified:
      label = "Verändert";
      iconName = "cross";
      sub = ["aus Sicherung wiederherstellen"];
      break;
    default:
      label = "Ungültig";
      iconName = "cross";
      sub = ["aus Sicherung wiederherstellen"];
  }
  return h(
    "div",
    { class: "status-cell" },
    h("span", { class: `status-cell__label state--${tone}` }, icon(iconName, 16, iconName === "check" ? 2.4 : 2.2), label),
    h("span", { class: "status-cell__sub" }, ...sub),
  );
}

function footer(): HTMLElement {
  return h(
    "footer",
    { class: "footer" },
    icon("lock", 16, 1.8),
    h("span", { class: "footer__text" }, "Zum Zeitstempel-Dienst gehen der Prüfwert, Ihr Kontoname und Ihre IP-Adresse – keine Rechnung, kein Dateiname."),
    h(
      "span",
      { class: "footer__links" },
      button("Haftungsausschluss", () => openDisclaimerDialog(), "inline-link"),
      " · ",
      button("Über", () => openAboutDialog(), "inline-link"),
    ),
  );
}
