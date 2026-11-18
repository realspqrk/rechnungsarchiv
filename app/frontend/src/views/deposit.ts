// Ablage detail ("Ablage kontrollieren"): banner from the report state.

import { api, errMsg, list, type DepositReport } from "../api";
import { h, icon } from "../dom";
import { accuracy, country, date, dateTimeLong, n, plural, seq } from "../fmt";
import { QUAL, QUAL_PAREN, REASON_LONG, STATE, type Tone } from "../text";
import { busy, button, subHeader, toast } from "../ui";
import { stampPending } from "../actions";

interface View {
  tone: Tone;
  icon: "check" | "cross" | "clock" | "info";
  title: string;
  lead: string;
  stampTitle: string;
  stampTime: string;
  stampDetail: string;
  retry: "none" | "stamp" | "setup";
  meaning: boolean;
}

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

  let retryBtn: HTMLElement | null = null;
  if (v.retry === "stamp") {
    const b: HTMLButtonElement = button("Jetzt nachholen", async () => {
      await stampPending(b);
    });
    retryBtn = h("div", null, b);
  } else if (v.retry === "setup") {
    retryBtn = h("div", null, h("a", { href: "#/einstellungen", class: "btn btn--secondary" }, "Jetzt einrichten"));
  }

  let predecessor = "Erste Ablage dieses Archivs.";
  if (r.predecessor > 0) {
    predecessor = r.predecessorPresent
      ? `Schließt an Ablage ${seq(r.predecessor)} an.`
      : `Schließt an Ablage ${seq(r.predecessor)} an; diese fehlt im Archiv (Lücke).`;
  }

  const saveBtn = button("Archivprotokoll (PDF) speichern", () => {
    void busy(saveBtn, "Wird gespeichert …", async () => {
      const path = await api.SaveProtocol(d.seq);
      if (path) toast(`Archivprotokoll gespeichert: ${path}`);
    });
  }, "primary");

  return h(
    "div",
    { class: "screen" },
    subHeader(`Ablage ${seq(d.seq)} · ${d.name}`, "detail"),
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
        h("div", { class: "card card--pad2" }, h("h2", { class: "card__h card__h--tight" }, "Reihenfolge"), h("p", { class: "m0" }, predecessor)),
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
      v.meaning
        ? h(
            "p",
            { class: "muted small m0" },
            "Was das Ergebnis bedeutet: Die Dateien stimmen mit dem Stand überein, den der Zeitstempel spätestens für den genannten Zeitpunkt belegt. Es bestätigt nicht, dass die Rechnungen echt oder inhaltlich richtig sind, und nicht, dass keine Ablage fehlt.",
          )
        : null,
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

/** Maps the report to banner and stamp texts (wording from the approved mockup). */
function describe(r: DepositReport): View {
  const d = r.deposit;
  const st = r.stamp;
  const files = plural(d.files, "Datei", "Dateien");
  const all = d.files === 1 ? "Die Datei stimmt" : `Alle ${files} stimmen`;
  const provider = st ? `${st.provider}, ${country(st.providerCountry)}` : "";
  const T = dateTimeLong(d.provenLatest);
  const stampTime = st && st.genTime ? `${dateTimeLong(st.genTime)} (${accuracy(st.accuracyMs)})` : "";
  const revocation = st?.revocationSource ? `Zertifikat laut ${st.revocationSource} nicht gesperrt` : "";
  const joined = (...parts: string[]) => parts.filter(Boolean).join(" · ");
  const base = { stampTime, retry: "none" as const, meaning: true };

  switch (d.state) {
    case STATE.stamped: {
      const unchangedTitle = st && st.accuracyMs > 0 ? `Unverändert seit spätestens ${T}` : `Unverändert seit ${T} (Genauigkeit nicht angegeben)`;
      if (d.qualification === QUAL.qualified) {
        return {
          ...base,
          tone: "green",
          icon: "check",
          title: unchangedTitle,
          lead: `${all} mit dem Stand überein, den der Zeitstempel spätestens für diesen Zeitpunkt belegt. Beleg: ein qualifizierter Zeitstempel (Vermutung nach Art. 41 Abs. 2 eIDAS).`,
          stampTitle: `Qualifiziert (eIDAS) · ${provider}`,
          stampDetail: joined("Signatur gültig", revocation, `qualifiziert laut EU-Vertrauensliste ${st?.trustListCountry ?? ""} vom ${date(st?.trustListDate ?? "")}`),
        };
      }
      if (d.qualification === QUAL.notQualified) {
        return {
          ...base,
          tone: "neutral",
          icon: "check",
          title: unchangedTitle,
          lead: `${all} mit dem belegten Stand überein. Der Dienst war zu diesem Zeitpunkt laut EU-Vertrauensliste nicht qualifiziert: keine gesetzliche Vermutung, die Behörde würdigt den Zeitstempel frei.`,
          stampTitle: `Nicht qualifiziert · ${provider}`,
          stampDetail: joined("Signatur gültig", revocation, "laut EU-Vertrauensliste zu diesem Zeitpunkt nicht als qualifiziert eingetragen"),
        };
      }
      return {
        ...base,
        tone: "neutral",
        icon: "info",
        title: unchangedTitle,
        lead: `${all} mit dem belegten Stand überein. Ob der Dienst zu diesem Zeitpunkt qualifiziert war, ist mit den vorhandenen EU-Vertrauenslisten noch nicht bestätigt; das wird bei der nächsten Kontrolle mit Internetverbindung geprüft.`,
        stampTitle: `Qualifikation unbestätigt · ${provider}`,
        stampDetail: joined("Signatur gültig", revocation, "keine Vertrauensliste nach dem Stempelzeitpunkt vorhanden"),
      };
    }
    case STATE.modified: {
      const dev = r.files.changed + r.files.missing + r.files.added;
      const paths = list(r.changedPaths);
      const lead =
        dev === 1 && paths.length === 1
          ? `In der archivierten Kopie stimmt ${paths[0]} nicht mehr mit dem belegten Stand überein.`
          : `In der archivierten Kopie stimmen ${plural(dev, "Datei", "Dateien")} nicht mehr mit dem belegten Stand überein.`;
      return {
        ...base,
        tone: "red",
        icon: "cross",
        meaning: false,
        title: dev === 1 ? "Verändert: 1 Datei weicht ab" : `Verändert: ${n(dev)} Dateien weichen ab`,
        lead: `${lead} Kopieren Sie die Ablage aus Ihrer Sicherung in einen eigenen Ordner und kontrollieren Sie diese Kopie.`,
        stampTitle: st ? `${st.qualification === QUAL.qualified ? "Qualifiziert (eIDAS)" : "Zeitstempel"} · ${provider}` : "Kein Zeitstempel",
        stampDetail: st ? "Der Zeitstempel selbst ist gültig; abweichend ist der Dateiinhalt." : "",
      };
    }
    case STATE.invalid:
      return {
        ...base,
        tone: "red",
        icon: "cross",
        meaning: false,
        title: "Ungültig: Ablage oder Zeitstempel fehlerhaft",
        lead: "Die Ablage entspricht nicht dem vorgeschriebenen Format, oder ihr Zeitstempel ist fehlerhaft. Kopieren Sie die Ablage aus Ihrer Sicherung in einen eigenen Ordner und kontrollieren Sie diese Kopie.",
        stampTitle: st ? `Zeitstempel ungültig · ${provider}` : "Kein gültiger Zeitstempel",
        stampDetail: st && !st.signatureValid ? "Signatur ungültig" : "",
      };
    case STATE.indeterminate:
      return {
        ...base,
        tone: "grey",
        icon: "info",
        meaning: false,
        title: "Unbestimmt: Zeitpunkt nicht mehr sicher belegt",
        lead: st?.keyCompromise
          ? `${all} mit der Ablage überein. Der Anbieter hat sein Zeitstempel-Zertifikat wegen Schlüsselkompromittierung gesperrt; der Zeitpunkt ist damit nicht mehr sicher belegt (siehe „Bekannte Grenzen“ in der Anleitung).`
          : `${all} mit der Ablage überein. Die gespeicherten Prüfdaten reichen nicht aus, um den Zeitpunkt sicher zu belegen.`,
        stampTitle: `Zeitstempel · ${provider}`,
        stampDetail: st?.keyCompromise ? "Signaturzertifikat gesperrt (Grund: Schlüsselkompromittierung)" : "Prüfdaten unvollständig",
      };
    default: {
      // Ausstehend
      const why = REASON_LONG[d.reason] ?? d.reason;
      const inh = r.inherited;
      const via = inh
        ? ` Über die nachfolgende Ablage ${seq(inh.fromSeq)} ist der Stand belegt: spätestens ${dateTimeLong(inh.provenLatest)} (${QUAL_PAREN[inh.qualification] ?? inh.qualification}).`
        : " Bis dahin ist der Zeitpunkt nicht belegt.";
      if (r.readOnly) {
        return {
          ...base,
          stampTime: "",
          tone: "amber",
          icon: "clock",
          meaning: false,
          title: "Zeitstempel ausstehend",
          lead: `Eigener Zeitstempel fehlt.${inh ? via : ""}`,
          stampTitle: "Noch kein eigener Zeitstempel",
          stampDetail: "Im Modus „nur kontrollieren“ wird kein Zeitstempel geholt.",
        };
      }
      const noService = d.reason === "kein-dienst";
      return {
        ...base,
        stampTime: "",
        tone: "amber",
        icon: "clock",
        meaning: false,
        retry: noService ? "setup" : "stamp",
        title: "Zeitstempel ausstehend",
        lead: `Eigener Zeitstempel fehlt noch (${why}).${via}`,
        stampTitle: "Noch kein eigener Zeitstempel",
        stampDetail: noService
          ? "Wird geholt, sobald ein Zeitstempel-Dienst eingerichtet ist."
          : "Wird beim nächsten Programmstart, nach dem nächsten erfolgreichen Zeitstempel oder über „Jetzt nachholen“ geholt.",
      };
    }
  }
}

