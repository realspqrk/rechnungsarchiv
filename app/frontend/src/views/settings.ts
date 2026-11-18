// Einstellungen: archive data, retention default, timestamp provider.
// Every change is saved at once (no "Speichern" button).

import { api, errMsg, list, type Provider, type Settings } from "../api";
import { h } from "../dom";
import { country, date } from "../fmt";
import { app, navigate, rerender, SWITCH_ROUTE } from "../state";
import { button, pathText, setPath, subHeader, toast } from "../ui";
import { withLock } from "../actions";

export async function renderSettings(): Promise<HTMLElement> {
  const [settings, providers] = await Promise.all([api.Settings(), api.Providers()]);
  return build(settings, list(providers));
}

function build(s: Settings, providers: Provider[]): HTMLElement {
  const saved = h("span", { class: "saved", role: "status", "aria-live": "polite" });
  const flash = () => {
    saved.textContent = "Gespeichert.";
    window.setTimeout(() => (saved.textContent = ""), 2500);
  };
  const save = async (fn: () => Promise<Settings>) => {
    try {
      const next = await withLock(fn);
      flash();
      return next;
    } catch (e) {
      toast(errMsg(e), "error");
      return null;
    }
  };

  // --- Archiv -------------------------------------------------------------
  const company = h("input", { id: "firma", type: "text", class: "input", value: s.company, autocomplete: "organization" });
  company.addEventListener("change", async () => {
    if (await save(() => api.SetSettings({ company: company.value, retentionDefault: null, providerId: null }))) {
      app.status = await api.Status();
    }
  });

  const retention = h(
    "select",
    { id: "frist", class: "input input--select input--max" },
    ...list(s.retentionOptions).map((o) => h("option", { value: String(o.years), selected: o.years === s.retentionDefault }, o.label)),
  );
  retention.addEventListener("change", () => void save(() => api.SetSettings({ company: null, retentionDefault: Number(retention.value), providerId: null })));

  const archiveSection = h(
    "section",
    { class: "card card--form", "aria-labelledby": "s-archiv" },
    h("h2", { id: "s-archiv", class: "card__title card__title--17" }, "Archiv"),
    h("div", { class: "field" }, h("label", { for: "firma", class: "field__label" }, "Firma"), company),
    h(
      "div",
      { class: "field" },
      h("span", { class: "field__label", id: "l-ordner" }, "Archivordner"),
      h(
        "div",
        { class: "path-row" },
        h("span", { class: "path-box mono", "aria-labelledby": "l-ordner" }, pathText(s.archiveDir)),
        // The open archive stays until another one is chosen (back link).
        button("Anderes Archiv öffnen …", () => navigate(SWITCH_ROUTE)),
      ),
      h("div", { class: "hint" }, "Hier liegen alle Ablagen. Sichern Sie diesen Ordner regelmäßig auf einen zweiten Datenträger – am besten nach jeder Archivierung. Der Zeitstempel macht Veränderungen an den Ablagen erkennbar; vor Verlust schützt er nicht."),
    ),
    h("div", { class: "field" }, h("label", { for: "frist", class: "field__label" }, "Aufbewahrung für neue Ablagen"), retention),
  );

  // --- Zeitstempel ----------------------------------------------------------
  const current = providers.find((p) => p.id === s.providerId) ?? null;
  const select = h(
    "select",
    { id: "tsa-anbieter", class: "input input--select" },
    current ? null : h("option", { value: "", selected: true }, "Bitte wählen …"),
    ...providers.map((p) =>
      h("option", { value: p.id, selected: p.id === s.providerId }, `${p.name} (${country(p.country)})${p.recommended ? " · empfohlen" : ""}`),
    ),
  );
  select.addEventListener("change", async () => {
    if (await save(() => api.SetSettings({ company: null, retentionDefault: null, providerId: select.value }))) {
      app.status = await api.Status();
      rerender();
    }
  });

  const tsSection = h(
    "section",
    { class: "card card--form card--gap12", "aria-labelledby": "s-ts" },
    h("h2", { id: "s-ts", class: "card__title card__title--17" }, "Zeitstempel"),
    h(
      "p",
      { class: "muted small m0" },
      "Jede Ablage erhält einen qualifizierten Zeitstempel (eIDAS): Nur dann gilt EU-weit die gesetzliche Vermutung, dass Zeitpunkt und Unversehrtheit stimmen (Art. 41 Abs. 2). Zum Dienst gehen der Prüfwert, Ihr Kontoname und Ihre IP-Adresse – keine Rechnung, kein Dateiname.",
    ),
    h(
      "div",
      { class: "provider-row" },
      h("div", { class: "field field--grow" }, h("label", { for: "tsa-anbieter", class: "field__label" }, "Anbieter"), select),
      current?.website
        ? button(["Konto beim Anbieter anlegen ", h("span", { "aria-hidden": "true" }, "↗")], async () => {
            try {
              await api.OpenProviderWebsite(current.id);
            } catch (e) {
              toast(errMsg(e), "error");
            }
          }, "link", { "aria-label": `Konto bei ${current.name} anlegen (öffnet den Browser)` })
        : null,
      button("Preise vergleichen", async () => {
        try {
          await api.OpenPrices();
        } catch (e) {
          toast(errMsg(e), "error");
        }
      }, "link", { class: "btn--small-text", "aria-label": "Preise vergleichen (öffnet die Projektseite im Browser)" }),
    ),
    current ? credentialFields(current, s, save) : null,
    h(
      "div",
      { class: "hint" },
      "Änderungen werden sofort gespeichert. Zugangsdaten nur auf diesem PC (Windows-Anmeldeinformationen). Ob alles passt, zeigt der erste Zeitstempel. · ",
      s.trustListDate
        ? `EU-Vertrauensliste: Stand ${date(s.trustListDate)}, wird automatisch aktualisiert. `
        : "EU-Vertrauensliste: noch nicht geladen; sie wird mit Internetverbindung automatisch geladen. ",
      saved,
    ),
  );

  return h("div", { class: "screen" }, subHeader("Einstellungen", "settings"), h("main", { class: "page page--settings" }, archiveSection, tsSection));
}

function credentialFields(p: Provider, s: Settings, save: (fn: () => Promise<Settings>) => Promise<Settings | null>): HTMLElement {
  const c = s.credentials;
  const stored = c.hasPassword ? "gespeichert" : "";
  const user = h("input", { id: "tsa-user", type: "text", class: "input", value: c.user, autocomplete: "username" });
  const pass = h("input", { id: "tsa-pass", type: "password", class: "input", placeholder: stored, autocomplete: "new-password" });
  const p12Pass = h("input", { id: "p12-pass", type: "password", class: "input", placeholder: stored, autocomplete: "new-password" });
  const url = h("input", { id: "tsa-url", type: "url", class: "input", value: c.url, placeholder: "https://…", inputmode: "url" });
  let p12Path = c.p12Path;
  const p12Box = h("span", { class: "path-box mono" }, p12Path ? pathText(p12Path) : "Noch nicht gewählt");

  const store = () =>
    save(() =>
      api.SetCredentials({
        user: user.value,
        password: pass.value,
        p12Path,
        p12Password: p12Pass.value,
        url: url.value,
      }),
    ).then((next) => {
      if (next) {
        pass.value = "";
        p12Pass.value = "";
        if (next.credentials.hasPassword) {
          pass.placeholder = "gespeichert";
          p12Pass.placeholder = pass.placeholder;
        }
      }
    });
  for (const el of [user, pass, p12Pass, url]) el.addEventListener("change", () => void store());

  const rows: HTMLElement[] = [];
  if (p.auth === "clientcert") {
    rows.push(
      h(
        "div",
        { class: "field-row field-row--end" },
        h(
          "div",
          { class: "field field--grow2" },
          h("span", { class: "field__label", id: "l-p12" }, "Zugangszertifikat (.p12)"),
          h(
            "div",
            { class: "path-row" },
            p12Box,
            button("Auswählen …", async () => {
              try {
                const picked = await api.PickP12();
                if (picked) {
                  p12Path = picked;
                  setPath(p12Box, picked);
                  await store();
                }
              } catch (e) {
                toast(errMsg(e), "error");
              }
            }, "secondary", { "aria-describedby": "l-p12" }),
          ),
        ),
        h("div", { class: "field field--grow1" }, h("label", { for: "p12-pass", class: "field__label" }, "Zertifikatspasswort"), p12Pass),
      ),
    );
  } else {
    rows.push(
      h(
        "div",
        { class: "field-row" },
        h("div", { class: "field field--half" }, h("label", { for: "tsa-user", class: "field__label" }, "Benutzer"), user),
        h("div", { class: "field field--half" }, h("label", { for: "tsa-pass", class: "field__label" }, "Passwort"), pass),
      ),
    );
  }
  if (p.urlFromContract) {
    rows.push(
      h(
        "div",
        { class: "field" },
        h("label", { for: "tsa-url", class: "field__label" }, "Adresse des Dienstes (laut Vertrag)"),
        url,
        h("div", { class: "hint" }, "Diese Adresse nennt Ihnen der Anbieter mit dem Vertrag."),
      ),
    );
  }
  return h("div", { class: "cred" }, ...rows);
}
