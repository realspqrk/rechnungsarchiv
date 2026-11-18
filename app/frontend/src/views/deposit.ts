// Ablage detail ("Ablage kontrollieren"): banner from the report state.

import { api, errMsg, list } from "../api";
import { h, icon } from "../dom";
import { date, n, seq } from "../fmt";
import { app } from "../state";
import { busy, button, lockedNote, subHeader, toast } from "../ui";
import { stampPending } from "../actions";
import { describe, MEANING, predecessorText } from "../describe";

export async function renderDeposit(seqNo: number): Promise<HTMLElement> {
  const r = await api.VerifyDeposit(seqNo);
  const d = r.deposit;
  const v = describe(r);

  const filesTiles = h(
    "div",
    { class: "mini-tiles mini-tiles--4" },
    mini("Unverändert", r.files.unchanged),
    mini("Verändert", r.files.changed),
    mini("Fehlend", r.files.missing),
    mini("Hinzugefügt", r.files.added),
  );
  const changed = list(r.changedPaths);

  // While another PC holds the lock nothing is fetched here; the banner on
  // the main screen offers the takeover.
  const locked = !!app.status?.lock;
  let retryBtn: HTMLElement | null = null;
  if (v.retry === "stamp") {
    const b: HTMLButtonElement = button("Jetzt nachholen", async () => {
      await stampPending(b);
    }, "secondary", { disabled: locked });
    retryBtn = h("div", { class: "actions" }, b, locked ? lockedNote() : null);
  } else if (v.retry === "setup") {
    retryBtn = h("div", null, h("a", { href: "#/einstellungen", class: "btn btn--secondary" }, "Jetzt einrichten"));
  }

  const chainFindings = list(r.chainFindings);

  const saveBtn = button("Archivprotokoll (PDF) speichern", () => {
    void busy(saveBtn, "Wird gespeichert …", async () => {
      const path = await api.SaveProtocol(d.seq);
      if (path) toast(`Archivprotokoll gespeichert: ${path}`);
    });
  }, "primary");

  return h(
    "div",
    { class: "screen" },
    subHeader(`Ablage ${seq(d.seq)} · ${d.name}`, "detail", r.readOnly),
    h(
      "main",
      { class: "page page--detail" },
      h(
        "section",
        { class: `result result--${v.tone}`, "aria-label": "Ergebnis" },
        h("span", { class: "result__icon" }, icon(v.icon, 26, v.icon === "clock" ? 2.2 : 2.6)),
        h("div", null, h("h2", { class: "result__title" }, v.title), h("p", { class: "result__lead" }, v.lead)),
      ),
      h(
        "section",
        { class: "card card--pad", "aria-labelledby": "h-dateien" },
        h("h2", { class: "card__h", id: "h-dateien" }, "Dateien"),
        filesTiles,
        changed.length > 1
          ? h("ul", { class: "path-list" }, ...changed.map((p) => h("li", { class: "mono mono--small" }, p)))
          : null,
      ),
      h(
        "section",
        { class: "card card--pad card--stack", "aria-labelledby": "h-zeitstempel" },
        h("h2", { class: "card__h card__h--tight", id: "h-zeitstempel" }, "Zeitstempel"),
        h("div", { class: "stamp-row" }, h("span", { class: "stamp-row__title" }, v.stampTitle), v.stampTime ? h("span", null, v.stampTime) : null),
        h("div", { class: "muted small" }, v.stampDetail),
        retryBtn,
      ),
      h(
        "section",
        { class: "pair", "aria-label": "Reihenfolge und Aufbewahrung" },
        h(
          "div",
          { class: "card card--pad2" },
          h("h2", { class: "card__h card__h--tight" }, "Reihenfolge"),
          h("p", { class: "m0" }, predecessorText(r)),
          chainFindings.length ? h("ul", { class: "finding-list" }, ...chainFindings.map((f) => h("li", null, f))) : null,
        ),
        h(
          "div",
          { class: "card card--pad2" },
          h("h2", { class: "card__h card__h--tight" }, "Aufbewahrung"),
          h("p", { class: "m0" }, `Belegjahr ${d.belegjahr} · gewählte Frist ${r.retentionYears} Jahre, gewöhnlich bis ${date(r.retentionUntil)}`),
          h("p", { class: "muted small mt4" }, "Ein abweichendes Wirtschaftsjahr oder ein anhängiges Verfahren verlängern die Frist (§ 132 Abs. 1 BAO)."),
        ),
      ),
      h(
        "details",
        { class: "card card--details" },
        h("summary", null, "Technische Details"),
        h("div", { class: "details__label" }, "Prüfwert"),
        h("div", { class: "mono break" }, r.root),
      ),
      v.meaning !== "none" ? h("p", { class: "muted small m0" }, MEANING[v.meaning]) : null,
      h(
        "div",
        { class: "actions" },
        saveBtn,
        button("Im Explorer zeigen", async () => {
          try {
            await api.RevealDeposit(d.seq);
          } catch (e) {
            toast(errMsg(e), "error");
          }
        }),
      ),
    ),
  );
}

function mini(label: string, value: number): HTMLElement {
  return h("div", { class: "mini-tile" }, h("div", { class: "mini-tile__label" }, label), h("div", { class: "mini-tile__value" }, n(value)));
}
