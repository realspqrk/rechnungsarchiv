// Texts of the main screen's check result. Pure (no DOM, no Go calls), so
// scripts/unit.mjs can check them.

import type { CheckSummary } from "./api";
import { n, plural, seq, whenRelative } from "./fmt";
import type { Tone } from "./text";

export interface Summary {
  tone: Tone;
  icon: "check" | "warn" | "info" | "clock";
  title: string;
  text: string;
}

/**
 * The banner after "Archiv kontrollieren". Severity follows the status
 * precedence (Verändert/Ungültig or a chain finding, then Unbestimmt, then
 * Ausstehend): green only when every Ablage is zeitgestempelt.
 */
export function checkSummary(c: CheckSummary, when = whenRelative(c.at)): Summary {
  const title = `Kontrolle abgeschlossen (${when}):`;
  if (c.deposits === 0) {
    return { tone: "neutral", icon: "info", title, text: "Noch keine Ablage im Archiv." };
  }
  const counts: string[] = [];
  if (c.counts.stamped) counts.push(`${n(c.counts.stamped)} zeitgestempelt`);
  if (c.counts.pending) counts.push(`${n(c.counts.pending)} ausstehend`);
  if (c.counts.indeterminate) counts.push(`${n(c.counts.indeterminate)} unbestimmt`);
  if (c.counts.modified) counts.push(`${n(c.counts.modified)} verändert`);
  if (c.counts.invalid) counts.push(`${n(c.counts.invalid)} ungültig`);
  const tail = counts.length ? ` ${counts.join(", ")}.` : "";
  const findings = (c.findings ?? []).length;
  const base = `${plural(c.deposits, "Ablage", "Ablagen")}, ${plural(c.files, "Datei", "Dateien")}`;
  if (c.deviations || findings) {
    const issues: string[] = [];
    if (c.deviations) issues.push(plural(c.deviations, "Ablage weicht ab", "Ablagen weichen ab"));
    if (findings) issues.push(plural(findings, "Befund im Archiv", "Befunde im Archiv"));
    return { tone: "red", icon: "warn", title, text: `${base}. ${issues.join(", ")}.${tail}` };
  }
  if (c.counts.indeterminate) {
    return { tone: "grey", icon: "info", title, text: `${base}. Bei ${plural(c.counts.indeterminate, "Ablage", "Ablagen")} ist der Zeitpunkt nicht sicher belegt.${tail}` };
  }
  if (c.counts.pending) {
    return { tone: "amber", icon: "clock", title, text: `${base}. Bei ${plural(c.counts.pending, "Ablage", "Ablagen")} fehlt noch ein gültiger Zeitstempel; ihr Zeitpunkt ist nicht belegt.${tail}` };
  }
  return { tone: "green", icon: "check", title, text: `${base}, keine Abweichung.${tail}` };
}

/** The "Zuletzt kontrolliert" tile. */
export function lastCheckText(lc: CheckSummary | null | undefined, total: number): string {
  if (!lc) return "Noch nicht kontrolliert";
  if (total === 0 || lc.deposits === 0) return "Noch keine Ablage kontrolliert";
  const findings = lc.findings ?? [];
  const gap = findings.find((f) => f.kind === "luecke");
  const dup = findings.find((f) => f.kind === "nummer-doppelt");
  const when = whenRelative(lc.at, true).replace(", ", " ").replace(" Uhr", "");
  if (gap) return `${when} · Lücke: Ablage ${seq(gap.seq)} fehlt`;
  if (dup) return `${when} · Nummer ${seq(dup.seq)} doppelt vergeben`;
  if (lc.deviations) return `${when} · ${plural(lc.deviations, "Abweichung", "Abweichungen")}`;
  return `${when} · Nummern ${seq(lc.firstSeq)}–${seq(lc.lastSeq)} ohne Lücke`;
}
