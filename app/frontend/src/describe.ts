// Texts of the Ablage detail view, derived only from the DepositReport.
// Pure (no DOM, no Go calls), so scripts/unit.mjs can check it.
//
// The bag result (content) and the token result (tokenState) come from Go
// separately; the aggregate state never stands in for either of them. A
// "spätestens" time is stated only when Go sent one (provenLatest).

import type { DepositReport } from "./api";
import { accuracy, country, date, dateTimeExact, dateTimeLong, n, plural, seq } from "./fmt";
import { QUAL, QUAL_PAREN, REASON_LONG, STATE, type Tone } from "./text";

export interface View {
  tone: Tone;
  icon: "check" | "cross" | "clock" | "info";
  title: string;
  lead: string;
  stampTitle: string;
  stampTime: string;
  stampDetail: string;
  retry: "none" | "stamp" | "setup";
  /** The "Was das Ergebnis bedeutet" note: with a latest time, without, or none. */
  meaning: "latest" | "plain" | "none";
}

export const CONTENT = { unchanged: "unveraendert", changed: "veraendert", invalid: "ungueltig" } as const;
export const TOKEN = { valid: "gueltig", invalid: "ungueltig", compromised: "kompromittiert", evidence: "pruefdaten", missing: "fehlt" } as const;

/** The token result in one line, independent of the files. */
function tokenDetail(r: DepositReport): string {
  switch (r.tokenState) {
    case TOKEN.valid:
      return "Der Zeitstempel selbst ist gültig.";
    case TOKEN.invalid:
      return "Der Zeitstempel besteht die kryptografische Prüfung nicht.";
    case TOKEN.compromised:
      return "Signaturzertifikat gesperrt (Grund: Schlüsselkompromittierung); der Zeitpunkt ist nicht sicher belegt.";
    case TOKEN.evidence:
      return "Prüfdaten zum Zeitstempel unvollständig; der Zeitpunkt ist nicht sicher belegt.";
    default:
      return "Es gibt keinen gültigen Zeitstempel.";
  }
}

function tokenTitle(r: DepositReport, provider: string): string {
  const p = provider ? ` · ${provider}` : "";
  switch (r.tokenState) {
    case TOKEN.valid:
      return `${r.stamp?.qualification === QUAL.qualified ? "Qualifiziert (eIDAS)" : "Zeitstempel gültig"}${p}`;
    case TOKEN.invalid:
      return `Zeitstempel ungültig${p}`;
    case TOKEN.compromised:
      return `Zeitstempel gesperrt${p}`;
    case TOKEN.evidence:
      return `Zeitstempel nicht sicher belegt${p}`;
    default:
      return "Kein gültiger Zeitstempel";
  }
}

export function describe(r: DepositReport): View {
  const d = r.deposit;
  const st = r.stamp;
  const files = plural(d.files, "Datei", "Dateien");
  const all = d.files === 1 ? "Die Datei stimmt" : `Alle ${files} stimmen`;
  const provider = st ? `${st.provider}, ${country(st.providerCountry)}` : "";
  const latest = d.provenLatest ? dateTimeLong(d.provenLatest) : "";
  const stampTime = st && st.genTime ? `${dateTimeExact(st.genTime)} (${accuracy(st.accuracyMs)})` : "";
  const revocation = st?.revocationSource ? `Zertifikat laut ${st.revocationSource} nicht gesperrt` : "";
  const joined = (...parts: string[]) => parts.filter(Boolean).join(" · ");
  const base = { stampTime, retry: "none" as const, meaning: (latest ? "latest" : "plain") as View["meaning"] };

  switch (d.state) {
    case STATE.stamped: {
      // Without a stated accuracy there is no "spätestens" time (spec §6 step 8).
      const title = latest
        ? `Unverändert seit spätestens ${latest}`
        : `Unverändert seit dem Zeitstempel vom ${dateTimeExact(st?.genTime || d.genTime)} (Genauigkeit nicht angegeben)`;
      const matches = latest
        ? `${all} mit dem Stand überein, den der Zeitstempel spätestens für diesen Zeitpunkt belegt.`
        : `${all} mit dem Stand überein, den der Zeitstempel belegt; ohne Angabe der Genauigkeit ist kein spätester Zeitpunkt belegt.`;
      if (d.qualification === QUAL.qualified) {
        return {
          ...base,
          tone: "green",
          icon: "check",
          title,
          lead: `${matches} Beleg: ein qualifizierter Zeitstempel (Vermutung nach Art. 41 Abs. 2 eIDAS).`,
          stampTitle: `Qualifiziert (eIDAS) · ${provider}`,
          stampDetail: joined("Signatur gültig", revocation, `qualifiziert laut EU-Vertrauensliste ${st?.trustListCountry ?? ""} vom ${date(st?.trustListDate ?? "")}`),
        };
      }
      if (d.qualification === QUAL.notQualified) {
        return {
          ...base,
          tone: "neutral",
          icon: "check",
          title,
          lead: `${matches} Der Dienst war zu diesem Zeitpunkt laut EU-Vertrauensliste nicht qualifiziert: keine gesetzliche Vermutung, die Behörde würdigt den Zeitstempel frei.`,
          stampTitle: `Nicht qualifiziert · ${provider}`,
          stampDetail: joined("Signatur gültig", revocation, "laut EU-Vertrauensliste zu diesem Zeitpunkt nicht als qualifiziert eingetragen"),
        };
      }
      return {
        ...base,
        tone: "neutral",
        icon: "info",
        title,
        lead: `${matches} Ob der Dienst zu diesem Zeitpunkt qualifiziert war, ist mit den vorhandenen EU-Vertrauenslisten noch nicht bestätigt; das wird bei der nächsten Kontrolle mit Internetverbindung geprüft.`,
        stampTitle: `Qualifikation unbestätigt · ${provider}`,
        stampDetail: joined("Signatur gültig", revocation, "keine Vertrauensliste nach dem Stempelzeitpunkt vorhanden"),
      };
    }
    case STATE.modified:
    case STATE.invalid: {
      const restore = "Kopieren Sie die Ablage aus Ihrer Sicherung in einen eigenen Ordner und kontrollieren Sie diese Kopie.";
      const dev = r.files.changed + r.files.missing + r.files.added;
      const paths = r.changedPaths ?? [];
      let title: string;
      let content: string;
      if (r.content === CONTENT.invalid) {
        title = "Ungültig: Ablage entspricht nicht dem Format";
        content = "Die Ablage entspricht nicht dem vorgeschriebenen Format.";
      } else if (r.content === CONTENT.changed) {
        title = dev === 1 ? "Verändert: 1 Datei weicht ab" : dev > 0 ? `Verändert: ${n(dev)} Dateien weichen ab` : "Verändert: Ablage weicht ab";
        content =
          dev === 1 && paths.length === 1
            ? `In der archivierten Kopie stimmt ${paths[0]} nicht mehr mit dem belegten Stand überein.`
            : `In der archivierten Kopie stimmen ${plural(dev, "Datei", "Dateien")} nicht mehr mit dem belegten Stand überein.`;
      } else {
        title = "Ungültig: Zeitstempel fehlerhaft";
        content = `${all} mit der Ablage überein.`;
      }
      return {
        ...base,
        tone: "red",
        icon: "cross",
        meaning: "none",
        title,
        lead: `${content} ${tokenDetail(r)} ${restore}`,
        stampTitle: tokenTitle(r, provider),
        stampDetail: r.tokenState === TOKEN.valid && r.content !== CONTENT.unchanged
          ? `Der Zeitstempel selbst ist gültig; ${r.content === CONTENT.invalid ? "fehlerhaft ist die Ablage" : "abweichend ist der Dateiinhalt"}.`
          : tokenDetail(r),
      };
    }
    case STATE.indeterminate: {
      const compromised = r.tokenState === TOKEN.compromised || !!st?.keyCompromise;
      return {
        ...base,
        tone: "grey",
        icon: "info",
        meaning: "none",
        title: "Unbestimmt: Zeitpunkt nicht mehr sicher belegt",
        lead: compromised
          ? `${all} mit der Ablage überein. Der Anbieter hat sein Zeitstempel-Zertifikat wegen Schlüsselkompromittierung gesperrt; der Zeitpunkt ist damit nicht mehr sicher belegt (siehe „Bekannte Grenzen“ in der Anleitung).`
          : `${all} mit der Ablage überein. Die gespeicherten Prüfdaten reichen nicht aus, um den Zeitpunkt sicher zu belegen.`,
        stampTitle: provider ? `Zeitstempel · ${provider}` : "Zeitstempel",
        stampDetail: compromised ? "Signaturzertifikat gesperrt (Grund: Schlüsselkompromittierung)" : "Prüfdaten unvollständig",
      };
    }
    default: {
      // Ausstehend
      const why = REASON_LONG[d.reason] ?? d.reason;
      const inh = r.inherited;
      let via = " Bis dahin ist der Zeitpunkt nicht belegt.";
      if (inh) {
        const q = QUAL_PAREN[inh.qualification] ?? inh.qualification;
        via = inh.provenLatest
          ? ` Über die nachfolgende Ablage ${seq(inh.fromSeq)} ist der Stand belegt: spätestens ${dateTimeLong(inh.provenLatest)} (${q}).`
          : ` Über die nachfolgende Ablage ${seq(inh.fromSeq)} ist der Stand durch deren Zeitstempel vom ${dateTimeExact(inh.genTime)} belegt (${q}; Genauigkeit nicht angegeben, daher kein spätester Zeitpunkt).`;
      }
      const pending = { ...base, stampTime: "", tone: "amber" as const, icon: "clock" as const, meaning: "none" as const, title: "Zeitstempel ausstehend", stampTitle: "Noch kein eigener Zeitstempel" };
      if (r.readOnly) {
        return { ...pending, lead: `Eigener Zeitstempel fehlt.${inh ? via : ""}`, stampDetail: "Im Modus „nur kontrollieren“ wird kein Zeitstempel geholt." };
      }
      const noService = d.reason === "kein-dienst";
      return {
        ...pending,
        retry: noService ? "setup" : "stamp",
        lead: `Eigener Zeitstempel fehlt noch (${why}).${via}`,
        stampDetail: noService
          ? "Wird geholt, sobald ein Zeitstempel-Dienst eingerichtet ist."
          : "Wird beim nächsten Programmstart, nach dem nächsten erfolgreichen Zeitstempel oder über „Jetzt nachholen“ geholt.",
      };
    }
  }
}

/** The "Reihenfolge" card: presence of the predecessor is not a verified link. */
export function predecessorText(r: DepositReport): string {
  const p = seq(r.predecessor);
  if (r.predecessor === 0) return "Erste Ablage dieses Archivs.";
  if (!r.predecessorPresent) return `Verweist auf Ablage ${p}; diese fehlt im Archiv (Lücke).`;
  if (r.predecessorLinked) return `Schließt an Ablage ${p} an: Ihr Verweis nennt genau den Prüfwert von Ablage ${p}.`;
  return `Ablage ${p} liegt im Archiv, die Verbindung zu ihr ist aber nicht bestätigt.`;
}

export const MEANING: Record<View["meaning"], string> = {
  latest:
    "Was das Ergebnis bedeutet: Die Dateien stimmen mit dem Stand überein, den der Zeitstempel spätestens für den genannten Zeitpunkt belegt. Es bestätigt nicht, dass die Rechnungen echt oder inhaltlich richtig sind, und nicht, dass keine Ablage fehlt.",
  plain:
    "Was das Ergebnis bedeutet: Die Dateien stimmen mit dem Stand überein, den der Zeitstempel belegt; ohne Angabe der Genauigkeit ist kein spätester Zeitpunkt belegt. Es bestätigt nicht, dass die Rechnungen echt oder inhaltlich richtig sind, und nicht, dass keine Ablage fehlt.",
  none: "",
};
