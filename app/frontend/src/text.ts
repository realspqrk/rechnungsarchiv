// German UI wording for the status model (docs/architecture.md, spec §5–6).
// Vocabulary: Archiv, Ablage, Zeitstempel, Prüfwert. Never "Siegel", never "Bestätigt".

import type { Deposit, Finding } from "./api";
import { seq } from "./fmt";

export const STATE = {
  stamped: "zeitgestempelt",
  pending: "ausstehend",
  indeterminate: "unbestimmt",
  modified: "veraendert",
  invalid: "ungueltig",
} as const;

export const QUAL = {
  qualified: "qualifiziert",
  notQualified: "nicht-qualifiziert",
  unconfirmed: "unbestaetigt",
} as const;

/** Short reason for tables: "Ausstehend · offline". */
export const REASON_SHORT: Record<string, string> = {
  "": "noch kein Zeitstempel",
  "kein-dienst": "kein Dienst eingerichtet",
  offline: "offline",
  zugang: "Zugangsdaten fehlen oder falsch",
  guthaben: "Guthaben aufgebraucht",
  pruefdaten: "Prüfdaten fehlen",
  anbieter: "Anbieterfehler",
};

/** Reason in a sentence: "Eigener Zeitstempel fehlt noch (keine Internetverbindung)." */
export const REASON_LONG: Record<string, string> = {
  "": "Grund wird beim nächsten Versuch ermittelt",
  "kein-dienst": "kein Zeitstempel-Dienst eingerichtet",
  offline: "keine Internetverbindung",
  zugang: "Zugangsdaten fehlen oder sind falsch",
  guthaben: "Guthaben beim Anbieter aufgebraucht",
  pruefdaten: "Prüfdaten zum Zeitstempel fehlen",
  anbieter: "Fehler beim Anbieter",
};

/** Result box after "Archivieren" when the stamp is pending. */
export const REASON_RESULT: Record<string, { title: string; text: string }> = {
  // Committed, but not checked again (e.g. cancelled after the commit).
  "": {
    title: "Zeitstempel noch nicht geholt",
    text: "Die Ablage ist gespeichert; archivieren Sie den Ordner nicht erneut. Der Zeitstempel wird beim nächsten Programmstart oder über „Jetzt nachholen“ geholt. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  "kein-dienst": {
    title: "Noch kein Zeitstempel-Dienst eingerichtet",
    text: "Die Ablage ist gespeichert. Sie erhält ihren Zeitstempel, sobald ein Dienst eingerichtet ist. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  offline: {
    title: "Keine Internetverbindung",
    text: "Die Ablage ist gespeichert. Der Zeitstempel wird beim nächsten Programmstart oder über „Jetzt nachholen“ geholt. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  zugang: {
    title: "Zugangsdaten fehlen oder sind falsch",
    text: "Die Ablage ist gespeichert. Prüfen Sie die Zugangsdaten unter Einstellungen; der Zeitstempel wird dann beim nächsten Programmstart oder über „Jetzt nachholen“ geholt. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  guthaben: {
    title: "Guthaben beim Zeitstempel-Dienst aufgebraucht",
    text: "Die Ablage ist gespeichert. Laden Sie beim Anbieter Guthaben auf; der Zeitstempel wird dann beim nächsten Programmstart oder über „Jetzt nachholen“ geholt. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  pruefdaten: {
    title: "Prüfdaten zum Zeitstempel fehlen",
    text: "Die Ablage ist gespeichert. Zertifikatskette oder Sperrstatus des Zeitstempels konnten nicht vollständig geladen werden; das Programm versucht es beim nächsten Programmstart oder über „Jetzt nachholen“ erneut. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
  anbieter: {
    title: "Der Zeitstempel-Dienst hat einen Fehler gemeldet",
    text: "Die Ablage ist gespeichert. Der Zeitstempel wird beim nächsten Programmstart oder über „Jetzt nachholen“ erneut angefordert. Bis dahin ist der Zeitpunkt nicht belegt.",
  },
};

export const QUAL_SHORT: Record<string, string> = {
  qualifiziert: "qualifiziert (eIDAS)",
  "nicht-qualifiziert": "nicht qualifiziert",
  unbestaetigt: "Qualifikation unbestätigt",
};

export const QUAL_PAREN: Record<string, string> = {
  qualifiziert: "qualifiziert",
  "nicht-qualifiziert": "nicht qualifiziert",
  unbestaetigt: "Qualifikation unbestätigt",
};

/** Visual tone of a deposit state. */
export type Tone = "green" | "neutral" | "amber" | "grey" | "red";

export function depositTone(d: Deposit): Tone {
  switch (d.state) {
    case STATE.stamped:
      return d.qualification === QUAL.qualified ? "green" : "neutral";
    case STATE.pending:
      return "amber";
    case STATE.indeterminate:
      return "grey";
    default:
      return "red";
  }
}

export function findingText(f: Finding): { title: string; text: string } {
  if (f.kind === "befund") {
    return {
      title: `${f.text}.`,
      text: "Die Ablagen passen nicht lückenlos zusammen. Kontrollieren Sie das Archiv und stellen Sie betroffene Ablagen aus der Sicherung wieder her.",
    };
  }
  if (f.kind === "nummer-doppelt") {
    return {
      title: `Nummer doppelt vergeben: Ablage ${seq(f.seq)}.`,
      text: "Zwei Ablagen tragen dieselbe Nummer. Prüfen Sie, ob eine davon aus einer anderen Sicherung stammt, und kontrollieren Sie beide.",
    };
  }
  return {
    title: `Lücke in der Nummerierung: Ablage ${seq(f.seq)} fehlt.`,
    text: `Ablage ${seq(f.successor)} verweist auf eine Ablage, die nicht im Archiv liegt. Stellen Sie sie aus der Sicherung wieder her.`,
  };
}

export const PROBLEM_TEXT: Record<string, string> = {
  name: "Name unter Windows nicht zulässig",
  collision: "Name kommt doppelt vor (Groß-/Kleinschreibung)",
  placeholder: "liegt nur in der Cloud, nicht auf diesem PC",
  link: "Verknüpfung, keine Datei",
};
