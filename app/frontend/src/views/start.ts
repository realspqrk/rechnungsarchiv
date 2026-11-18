// Erster Start: Archiv anlegen, or open an existing one.

import { api, errMsg } from "../api";
import { h, icon } from "../dom";
import { app, navigate } from "../state";
import { busy, button, toast } from "../ui";
import { openDisclaimerDialog } from "../dialogs/about";

export function renderStart(): HTMLElement {
  const status = app.status;
  let dir = status?.suggestedDir ?? "";
  const disclaimer = app.disclaimer;

  const company = h("input", { id: "firma", type: "text", class: "input", autocomplete: "organization", value: "", placeholder: "z. B. Musterfirma GmbH" });
  const dirBox = h("span", { class: "path-box mono", id: "archivordner-wert" }, dir);

  const change = button("Ändern …", async () => {
    try {
      const picked = await api.PickFolder("archiv");
      if (picked) {
        dir = picked;
        dirBox.textContent = picked;
      }
    } catch (e) {
      toast(errMsg(e), "error");
    }
  }, "secondary", { "aria-describedby": "archivordner-label" });

  const create = button("Archiv anlegen", () => {
    void busy(create, "Archiv wird angelegt …", async () => {
      app.status = await api.CreateArchive({ company: company.value, dir });
      app.checked = null;
      navigate("#/");
    });
  }, "primary");

  const open = (readOnly: boolean) => async () => {
    try {
      const picked = await api.PickFolder("archiv");
      if (!picked) return;
      app.status = await api.OpenArchive(picked, readOnly);
      app.checked = null;
      navigate("#/");
    } catch (e) {
      toast(errMsg(e), "error");
    }
  };

  return h(
    "main",
    { class: "start" },
    h(
      "div",
      { class: "start__inner" },
      h("div", { class: "brand brand--large" }, icon("logo", 34, 1.8, "brand__logo"), h("h1", { class: "brand__name" }, "Rechnungsarchiv")),
      h("p", { class: "lead-muted" }, "Archiviert Rechnungsordner auf diesem Computer so, dass jede spätere Veränderung erkennbar ist. Ihre Rechnungen verlassen den Computer nicht."),
      h(
        "section",
        { class: "card card--start", "aria-labelledby": "neu" },
        h("h2", { id: "neu", class: "card__title" }, "Archiv anlegen"),
        h("div", { class: "field" }, h("label", { for: "firma", class: "field__label" }, "Firma"), company),
        h(
          "div",
          { class: "field" },
          h("span", { class: "field__label", id: "archivordner-label" }, "Archivordner"),
          h("div", { class: "path-row" }, dirBox, change),
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
      h(
        "div",
        { class: "start__links" },
        button("Bestehendes Archiv öffnen", open(false), "link"),
        button("Archiv nur kontrollieren (z. B. Sicherung, Betriebsprüfung)", open(true), "link"),
      ),
    ),
  );
}
