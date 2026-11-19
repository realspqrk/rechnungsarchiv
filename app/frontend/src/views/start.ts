// Start screen. No archive open (first start, remembered one unreachable):
// create an archive or open an existing one, side by side. "Neues Archiv
// anlegen …" from Einstellungen: only the create form, with the way back.

import { api, errMsg, list } from "../api";
import { h, icon } from "../dom";
import { app } from "../state";
import { openArchiveFolder, switchArchive } from "../actions";
import { button, pathText, setPath, toast } from "../ui";
import { openDisclaimerDialog } from "../dialogs/about";

/** While Go opens and checks an archive folder (large archives take a
 * while): nothing else can be done meanwhile. */
export function renderOpening(dir: string, title: string): HTMLElement {
  return h(
    "main",
    { class: "start" },
    h(
      "div",
      { class: "start__inner start__inner--narrow" },
      h("div", { class: "brand brand--large" }, icon("logo", 34, 1.8, "brand__logo"), h("h1", { class: "brand__name" }, "Rechnungsarchiv")),
      h(
        "section",
        { class: "card card--start", role: "status", "aria-live": "polite" },
        h("h2", { class: "card__title" }, title),
        h("div", { class: "path-box mono" }, dir),
        h("p", { class: "muted small m0" }, "Die Ablagen werden dabei kontrolliert; bei großen Archiven dauert das etwas."),
      ),
    ),
  );
}

/** back: an archive is open ("Neues Archiv anlegen …"): only the form,
 * with the way back to Einstellungen. */
export function renderStart(back = false): HTMLElement {
  const status = app.status;
  // After a failed "Archiv anlegen" the input is still there.
  let dir = app.draft?.dir || status?.suggestedDir || "";
  const disclaimer = app.disclaimer;

  const company = h("input", { id: "firma", type: "text", class: "input", autocomplete: "organization", value: app.draft?.company ?? "", placeholder: "z. B. Musterfirma GmbH" });
  // The input survives re-renders (opening another archive that fails or
  // is cancelled); it is dropped after a successful switch.
  const keep = () => (app.draft = { company: company.value, dir });
  company.addEventListener("input", keep);
  const dirBox = h("span", { class: "path-box mono", id: "archivordner-wert" }, pathText(dir));

  const change = button("Ändern …", async () => {
    try {
      const picked = await api.PickFolder("archiv");
      if (picked) {
        dir = picked;
        setPath(dirBox, picked);
        keep();
      }
    } catch (e) {
      toast(errMsg(e), "error");
    }
  }, "secondary", { "aria-describedby": "archivordner-label" });

  const create = button("Archiv anlegen", () => {
    keep();
    void switchArchive(dir, "Archiv wird angelegt …", () => api.CreateArchive({ company: company.value, dir }));
  }, "primary", { id: "start-anlegen" });

  const recent = list(status?.recent);
  const existing = back ? null : h(
    "section",
    { class: "card card--start card--start-side", "aria-labelledby": "vorhanden" },
    h("h2", { id: "vorhanden", class: "card__title" }, "Archiv schon vorhanden?"),
    recent.length
      ? h(
          "div",
          { class: "recent", role: "group", "aria-labelledby": "zuletzt-start" },
          h("div", { class: "field__label", id: "zuletzt-start" }, "Zuletzt geöffnet"),
          ...recent.map((r, i) =>
            h(
              "button",
              { type: "button", class: "recent__item", id: `recent-${i}`, onClick: () => void openArchiveFolder(r.dir) },
              h("span", { class: "recent__company" }, r.company),
              h("span", { class: "recent__dir mono" }, r.dir),
            ),
          ),
        )
      : h("p", { class: "muted small m0" }, "Zum Beispiel nach einem PC-Wechsel oder aus der Sicherung."),
    button("Bestehendes Archiv öffnen …", () => void openArchiveFolder(), "secondary", { id: "start-oeffnen" }),
  );

  return h(
    "main",
    { class: "start" },
    h(
      "div",
      { class: back ? "start__inner start__inner--narrow" : "start__inner" },
      back ? h("a", { href: "#/einstellungen", class: "back-link back-link--start", id: "zurueck" }, icon("back", 18, 2), "Zurück zu den Einstellungen") : null,
      h("div", { class: "brand brand--large" }, icon("logo", 34, 1.8, "brand__logo"), h("h1", { class: "brand__name" }, "Rechnungsarchiv")),
      h("p", { class: "lead-muted" }, "Archiviert Rechnungsordner auf diesem Computer so, dass spätere Veränderungen an den Ablagen erkennbar sind. Ihre Rechnungen verlassen den Computer nicht."),
      h(
        "div",
        { class: back ? "start__choices start__choices--one" : "start__choices" },
        h(
          "section",
          { class: "card card--start", "aria-labelledby": "neu" },
          h("h2", { id: "neu", class: "card__title" }, "Neues Archiv anlegen"),
          h("div", { class: "field" }, h("label", { for: "firma", class: "field__label" }, "Firma"), company),
          h(
            "div",
            { class: "field" },
            h("span", { class: "field__label", id: "archivordner-label" }, "Archivordner"),
            h("div", { class: "path-row" }, dirBox, change),
            h("div", { class: "hint", id: "archivordner-hinweis" }, "Hierher kopiert das Programm jeden Ordner, den Sie archivieren, als Ablage (ZIP-Datei). Ihre Rechnungsordner bleiben, wo sie sind."),
          ),
          h(
            "div",
            { class: "notice", role: "note" },
            h(
              "p",
              null,
              disclaimer?.short ?? "",
              " ",
              button("Vollständiger Text", () => openDisclaimerDialog(), "inline-link"),
            ),
            h("p", { class: "notice__consent" }, disclaimer?.consent ?? ""),
          ),
          h("div", { class: "actions actions--end" }, create),
        ),
        existing,
      ),
    ),
  );
}
