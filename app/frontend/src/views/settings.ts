// Einstellungen: company, retention, timestamp service, and the "Archiv"
// card (folder, "Archiv wechseln", switcher.ts). Every change is saved at
// once.

import { api, errMsg, list, type Provider, type Settings, type Status } from "../api";
import { h, icon } from "../dom";
import { country, date } from "../fmt";
import { acceptStatus, app, rerender } from "../state";
import { button, pathText, setPath, subHeader, toast } from "../ui";
import { withLock } from "../actions";
import { settleOpeningQuestion } from "../dialogs/pending";
import { archiveCard } from "./switcher";

export async function renderSettings(): Promise<HTMLElement> {
  const [settings, providers] = await Promise.all([api.Settings(), api.Providers()]);
  return build(settings, list(providers));
}

/** What the "Zeitstempel-Dienst" badge says. It cannot know whether the
 * credentials are right: that only the first timestamp shows. */
function serviceState(s: Settings, p: Provider | null): { tone: "green" | "amber"; text: string } {
  if (!p) return { tone: "amber", text: "Nicht eingerichtet" };
  const c = s.credentials;
  const complete = (p.auth === "clientcert" ? !!c.p12Path : !!c.user) && c.hasPassword && (!p.urlFromContract || !!c.url);
  return complete ? { tone: "green", text: "Zugangsdaten gespeichert" } : { tone: "amber", text: "Zugangsdaten fehlen" };
}

function build(s: Settings, providers: Provider[]): HTMLElement {
  const session = app.status?.session ?? 0;
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

  // --- Firma und Aufbewahrung ---------------------------------------------
  const company = h("input", { id: "firma", type: "text", class: "input", value: s.company, autocomplete: "organization" });
  company.addEventListener("change", async () => {
    if (await save(() => api.SetSettings({ company: company.value, retentionDefault: null, providerId: null }))) {
      acceptStatus(await api.Status());
    }
  });

  const retention = h(
    "select",
    { id: "frist", class: "input input--select input--max" },
    ...list(s.retentionOptions).map((o) => h("option", { value: String(o.years), selected: o.years === s.retentionDefault }, o.label)),
  );
  retention.addEventListener("change", () => void save(() => api.SetSettings({ company: null, retentionDefault: Number(retention.value), providerId: null })));

  const companySection = h(
    "section",
    { class: "card card--form", "aria-labelledby": "s-firma" },
    h("h2", { id: "s-firma", class: "card__title card__title--17" }, "Firma und Aufbewahrung"),
    h("div", { class: "field" }, h("label", { for: "firma", class: "field__label" }, "Firma"), company),
    h("div", { class: "field" }, h("label", { for: "frist", class: "field__label" }, "Aufbewahrung für neue Ablagen"), retention),
  );

  // --- Zeitstempel-Dienst ---------------------------------------------------
  const current = providers.find((p) => p.id === s.providerId) ?? null;
  const badge = h("span", { class: "badge", role: "status" });
  const stateHint = h("p", { class: "hint m0" });
  const showState = (next: Settings) => {
    const st = serviceState(next, current);
    badge.className = `badge badge--${st.tone}`;
    badge.textContent = st.text;
    stateHint.textContent =
      st.tone === "green"
        ? "Ob der Zugang beim Anbieter funktioniert, zeigt der erste Zeitstempel."
        : current
          ? "Tragen Sie die Zugangsdaten ein, die Sie vom Anbieter erhalten haben."
          : "Ohne Dienst bleiben neue Ablagen ausstehend, bis einer eingerichtet ist.";
  };
  showState(s);

  const select = h(
    "select",
    { id: "tsa-anbieter", class: "input input--select input--max" },
    current ? null : h("option", { value: "", selected: true }, "Bitte wählen …"),
    ...providers.map((p) =>
      h("option", { value: p.id, selected: p.id === s.providerId }, `${p.name} (${country(p.country)})${p.recommended ? " · empfohlen" : ""}`),
    ),
  );
  select.addEventListener("change", async () => {
    const settled = settleOpeningQuestion(session); // a setup done here is not answered with the opening question
    const ok = await save(() => api.SetSettings({ company: null, retentionDefault: null, providerId: select.value }));
    settled(!!ok);
    if (ok) {
      acceptStatus(await api.Status());
      // The new provider's fields appear; the keyboard stays on the choice.
      await rerender();
      document.getElementById("tsa-anbieter")?.focus();
    }
  });

  const external = (label: string, aria: string, run: () => Promise<void>) =>
    button([label, " ", h("span", { "aria-hidden": "true" }, "↗")], async () => {
      try {
        await run();
      } catch (e) {
        toast(errMsg(e), "error");
      }
    }, "link", { "aria-label": aria });

  const tsSection = h(
    "section",
    { class: "card card--form", "aria-labelledby": "s-ts" },
    h("div", { class: "card__head" }, h("h2", { id: "s-ts", class: "card__title card__title--17" }, "Zeitstempel-Dienst"), badge),
    h("div", { class: "field" }, h("label", { for: "tsa-anbieter", class: "field__label" }, "Anbieter"), select),
    current ? credentialFields(current, s, save, showState, session) : null,
    stateHint,
    h(
      "div",
      { class: "link-row" },
      current?.website ? external("Konto beim Anbieter anlegen", `Konto bei ${current.name} anlegen (öffnet den Browser)`, () => api.OpenProviderWebsite(current.id)) : null,
      external("Preise und Einrichtung", "Preise und Einrichtung (öffnet die Projektseite im Browser)", () => api.OpenPrices()),
      // Only providers proven in real use are offered.
      external("Weitere Anbieter? Auf GitHub melden", "Weitere Anbieter? Auf GitHub melden (öffnet GitHub im Browser)", () => api.OpenIssues()),
    ),
    h(
      "p",
      { class: "info-line" },
      icon("info", 16, 2),
      h("span", null, "Zum Dienst gehen nur der Prüfwert, Ihre Anmeldung (Benutzer und Passwort oder Zugangszertifikat) und Ihre IP-Adresse – keine Rechnung, kein Dateiname. Gespeichert sind die Zugangsdaten nur auf diesem PC (Windows-Anmeldeinformationen)."),
    ),
  );

  return h(
    "div",
    { class: "screen" },
    subHeader("Einstellungen", "settings"),
    h(
      "main",
      { class: "page page--settings" },
      h("p", { class: "muted small m0 autosave" }, "Änderungen werden sofort gespeichert. ", saved),
      companySection,
      tsSection,
      archiveCard(app.status as Status),
      h(
        "p",
        { class: "hint m0" },
        s.trustListDate
          ? `EU-Vertrauensliste: Stand ${date(s.trustListDate)}, wird automatisch aktualisiert.`
          : "EU-Vertrauensliste: noch nicht geladen; sie wird mit Internetverbindung automatisch geladen.",
      ),
    ),
  );
}

function credentialFields(
  p: Provider,
  s: Settings,
  save: (fn: () => Promise<Settings>) => Promise<Settings | null>,
  showState: (next: Settings) => void,
  session: number,
): HTMLElement {
  const c = s.credentials;
  const stored = c.hasPassword ? "gespeichert" : "";
  const user = h("input", { id: "tsa-user", type: "text", class: "input", value: c.user, autocomplete: "username" });
  const pass = h("input", { id: "tsa-pass", type: "password", class: "input", placeholder: stored, autocomplete: "new-password" });
  const p12Pass = h("input", { id: "p12-pass", type: "password", class: "input", placeholder: stored, autocomplete: "new-password" });
  const url = h("input", { id: "tsa-url", type: "url", class: "input", value: c.url, placeholder: "https://…", inputmode: "url" });
  let p12Path = c.p12Path;
  const p12Box = h("span", { class: "path-box mono" }, p12Path ? pathText(p12Path) : "Noch nicht gewählt");

  const edits = { pass: 0, p12: 0 };
  pass.addEventListener("input", () => edits.pass++);
  p12Pass.addEventListener("input", () => edits.p12++);
  const store = () => {
    // Clear only the secrets this request sent and nobody edited since: the
    // user may type while the save runs.
    const sent = { password: pass.value, p12Password: p12Pass.value, edits: { ...edits } };
    const settled = settleOpeningQuestion(session); // a setup done here is not answered with the opening question
    return save(() =>
      api.SetCredentials({
        user: user.value,
        password: sent.password,
        p12Path,
        p12Password: sent.p12Password,
        url: url.value,
      }),
    ).then((next) => {
      settled(!!next);
      if (next) {
        if (edits.pass === sent.edits.pass) pass.value = "";
        if (edits.p12 === sent.edits.p12) p12Pass.value = "";
        if (next.credentials.hasPassword) {
          pass.placeholder = "gespeichert";
          p12Pass.placeholder = pass.placeholder;
        }
        showState(next);
      }
    });
  };
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
