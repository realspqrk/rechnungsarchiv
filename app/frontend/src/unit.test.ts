// Checks of the pure text modules (describe, summary, fmt). Run with
// `npm test` (scripts/unit.mjs bundles this file with rolldown and runs it
// in Node). Not part of the app bundle.

import type { CheckSummary, DepositReport } from "./api";
import { CONTENT, describe, MEANING, predecessorText, TOKEN } from "./describe";
import { dateTimeExact } from "./fmt";
import { checkSummary, lastCheckText } from "./summary";

let failed = 0;
function check(name: string, ok: boolean, detail: unknown = ""): void {
  if (!ok) {
    failed++;
    console.error(`FAIL ${name}`, detail);
  }
}
const has = (s: string, part: string) => s.includes(part);

function report(over: Omit<Partial<DepositReport>, "deposit"> & { deposit?: Partial<DepositReport["deposit"]> }): DepositReport {
  const base: DepositReport = {
    deposit: {
      seq: 2, name: "2026-10 Eingangsrechnungen", belegjahr: 2026, files: 9, bytes: 1000, state: "zeitgestempelt",
      qualification: "qualifiziert", reason: "", genTime: "2026-11-05T08:14:22Z", provenLatest: "2026-11-05T08:14:23Z",
      inheritedFrom: 0, provider: "Mensatek", file: "0002_x.zip",
    },
    readOnly: false, checkedAt: "2026-11-18T10:00:00Z", path: "", root: "sha256:00",
    files: { unchanged: 9, changed: 0, missing: 0, added: 0 }, changedPaths: [],
    stamp: {
      provider: "Mensatek", providerCountry: "ES", genTime: "2026-11-05T08:14:22Z", accuracyMs: 1000, provenLatest: "2026-11-05T08:14:23Z",
      qualification: "qualifiziert", trustListCountry: "ES", trustListDate: "2026-11-12", signatureValid: true, revocationSource: "OCSP", keyCompromise: false,
    },
    inherited: null, predecessor: 1, predecessorPresent: true, predecessorLinked: true, chainFindings: [],
    content: CONTENT.unchanged, tokenState: TOKEN.valid, retentionYears: 7, retentionUntil: "2033-12-31",
  };
  return { ...base, ...over, deposit: { ...base.deposit, ...(over.deposit ?? {}) } } as DepositReport;
}

// Finding 4: changed payload + compromised token → Verändert, but never
// "Der Zeitstempel selbst ist gültig".
{
  const v = describe(report({
    deposit: { state: "veraendert", provenLatest: "" }, content: CONTENT.changed, tokenState: TOKEN.compromised,
    files: { unchanged: 8, changed: 1, missing: 0, added: 0 },
  }));
  check("changed+compromised title", has(v.title, "Verändert"), v);
  check("changed+compromised no valid token", !has(v.lead + v.stampDetail + v.stampTitle, "selbst ist gültig"), v);
  check("changed+compromised names the lock", has(v.lead + v.stampDetail, "Schlüsselkompromittierung"), v);
}
// Finding 4: invalid bag + intact token: the token is not called invalid.
{
  const v = describe(report({ deposit: { state: "ungueltig", provenLatest: "" }, content: CONTENT.invalid, tokenState: TOKEN.valid }));
  check("invalid bag title", has(v.title, "Format"), v);
  check("invalid bag keeps the token valid", !has(v.stampTitle + v.stampDetail + v.lead, "Zeitstempel ungültig") && has(v.stampDetail, "selbst ist gültig"), v);
}
// …and an intact bag with an invalid token says so.
{
  const v = describe(report({ deposit: { state: "ungueltig", provenLatest: "" }, content: CONTENT.unchanged, tokenState: TOKEN.invalid, stamp: null }));
  check("invalid token title", has(v.title, "Zeitstempel fehlerhaft") && has(v.stampTitle, "ungültig"), v);
}
// Finding 9: no latest time without provenLatest.
{
  const r = report({ deposit: { provenLatest: "" } });
  r.stamp!.accuracyMs = 1000;
  r.stamp!.provenLatest = "";
  const v = describe(r);
  check("no provenLatest: no spätestens", !has(v.title + v.lead, "spätestens") && v.meaning === "plain" && !has(MEANING[v.meaning], "spätestens"), v);
}
{
  const v = describe(report({
    deposit: { state: "ausstehend", reason: "pruefdaten", provenLatest: "", genTime: "" }, stamp: null, tokenState: TOKEN.missing,
    inherited: { fromSeq: 3, provenLatest: "", genTime: "2026-11-06T08:00:00Z", qualification: "qualifiziert" },
  }));
  check("inherited without accuracy: genTime, no spätestens", !has(v.lead, "spätestens") && has(v.lead, "06.11.2026, 09:00:00 Uhr") && has(v.lead, "Genauigkeit nicht angegeben"), v);
}
{
  const v = describe(report({}));
  check("qualified: spätestens", has(v.title, "spätestens 05.11.2026, 09:14:23 Uhr") && v.meaning === "latest", v);
}
// Finding 10: the signed fraction is shown as signed.
check("exact fraction", dateTimeExact("2026-11-05T12:00:00.9Z") === "05.11.2026, 13:00:00,9 Uhr MEZ", dateTimeExact("2026-11-05T12:00:00.9Z"));
check("exact nanos", dateTimeExact("2026-11-05T12:00:00.900000001Z") === "05.11.2026, 13:00:00,900000001 Uhr MEZ", dateTimeExact("2026-11-05T12:00:00.900000001Z"));
check("exact whole", dateTimeExact("2026-11-05T12:00:59Z") === "05.11.2026, 13:00:59 Uhr MEZ");
{
  const v = describe(report({ stamp: { ...report({}).stamp!, genTime: "2026-11-05T12:00:00.900Z" } }));
  check("detail time keeps fraction", has(v.stampTime, "13:00:00,9 Uhr"), v.stampTime);
}
// Finding 7: presence is not a verified link.
check("linked", has(predecessorText(report({})), "Schließt an Ablage 0001 an"));
check("present, not linked", has(predecessorText(report({ predecessorLinked: false, chainFindings: ["Ablage 0002 verweist nicht auf den Prüfwert von Ablage 0001"] })), "nicht bestätigt"));
check("missing", has(predecessorText(report({ predecessorPresent: false, predecessorLinked: false })), "Lücke"));

// Finding 6 and W6: severity of the check banner.
function summary(over: Partial<CheckSummary>): CheckSummary {
  return { at: "2026-11-18T10:00:00Z", deposits: 2, files: 9, deviations: 0, findings: [], firstSeq: 1, lastSeq: 2, inherited: null,
    counts: { total: 2, stamped: 2, pending: 0, indeterminate: 0, modified: 0, invalid: 0 }, ...over };
}
check("all stamped: green", checkSummary(summary({}), "heute").tone === "green");
{
  const s = checkSummary(summary({ counts: { total: 2, stamped: 1, pending: 1, indeterminate: 0, modified: 0, invalid: 0 } }), "heute");
  check("pending: not green", s.tone === "amber" && !has(s.text, "keine Abweichung"), s);
}
{
  const s = checkSummary(summary({ counts: { total: 2, stamped: 1, pending: 0, indeterminate: 1, modified: 0, invalid: 0 } }), "heute");
  check("indeterminate: not green", s.tone === "grey", s);
}
// Code review 2 finding 3: a pending Ablage whose state is proven through a
// stamped successor is not "nicht belegt" (core and detail view say belegt).
{
  const pending = { total: 3, stamped: 1, pending: 2, indeterminate: 0, modified: 0, invalid: 0 };
  const s = checkSummary(summary({ deposits: 3, lastSeq: 3, counts: pending, inherited: [{ seq: 1, via: 3 }, { seq: 2, via: 3 }] }), "heute");
  check("inherited pending: amber", s.tone === "amber", s);
  check("inherited pending: proof named", has(s.text, "Ablagen 0001 und 0002: eigener Zeitstempel fehlt (Stand über Ablage 0003 belegt)"), s.text);
  check("inherited pending: never 'nicht belegt'", !has(s.text, "nicht belegt"), s.text);
  const m = checkSummary(summary({ deposits: 3, lastSeq: 3, counts: pending, inherited: [{ seq: 2, via: 3 }] }), "heute");
  check("mixed pending: own count", has(m.text, "Bei 1 Ablage fehlt noch ein gültiger Zeitstempel; ihr Zeitpunkt ist nicht belegt."), m.text);
  check("mixed pending: inherited named", has(m.text, "Ablage 0002: eigener Zeitstempel fehlt (Stand über Ablage 0003 belegt)"), m.text);
  const none = checkSummary(summary({ counts: { total: 2, stamped: 1, pending: 1, indeterminate: 0, modified: 0, invalid: 0 }, inherited: null }), "heute");
  check("pending without proof: nicht belegt", has(none.text, "ihr Zeitpunkt ist nicht belegt"), none.text);
}
check("modified: red",checkSummary(summary({ deviations: 1, counts: { total: 2, stamped: 1, pending: 0, indeterminate: 0, modified: 1, invalid: 0 } }), "heute").tone === "red");
{
  const s = checkSummary(summary({ deposits: 0, files: 0, firstSeq: 0, lastSeq: 0, counts: { total: 0, stamped: 0, pending: 0, indeterminate: 0, modified: 0, invalid: 0 } }), "heute");
  check("empty archive: no green, no '. .'", s.tone !== "green" && s.text === "Noch keine Ablage im Archiv.", s);
}
check("empty last check", lastCheckText(summary({ deposits: 0, firstSeq: 0, lastSeq: 0 }), 0) === "Noch keine Ablage kontrolliert");
check("never checked", lastCheckText(null, 3) === "Noch nicht kontrolliert");

if (failed) throw new Error(`${failed} check(s) failed`);
console.log("frontend unit checks: ok");
