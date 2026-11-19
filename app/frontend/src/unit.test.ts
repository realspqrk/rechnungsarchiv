// Checks of the pure text modules (describe, summary, fmt). Run with
// `npm test` (scripts/unit.mjs bundles this file with rolldown and runs it
// in Node). Not part of the app bundle.

import type { CheckSummary, DepositReport, FolderDrop } from "./api";
import { collapsedFiles, CONTENT, describe, deviationsFirst, MEANING, predecessorText, TOKEN } from "./describe";
import { FolderInbox, gateOf } from "./folders";
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
      qualification: "qualifiziert", trustListCountry: "ES", trustListDate: "2026-11-12", signatureValid: true, revocationSource: "OCSP", keyCompromise: false, qualificationNote: "",
    },
    inherited: null, predecessor: 1, predecessorPresent: true, predecessorLinked: true, chainFindings: [],
    content: CONTENT.unchanged, tokenState: TOKEN.valid, retentionYears: 7, retentionUntil: "2033-12-31", fileList: [],
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
// Owner: "Qualifikation unbestätigt" says why and how it resolves.
{
  const r = report({ deposit: { qualification: "unbestaetigt" } });
  r.stamp!.qualification = "unbestaetigt";
  r.stamp!.qualificationNote = "die vorliegende EU-Vertrauensliste (ES Nr. 190, ausgestellt 04.11.2026) ist älter als der Zeitstempel";
  const v = describe(r);
  check("unconfirmed: what is open and how it resolves", has(v.lead, "noch offen") && has(v.lead, "„Archiv kontrollieren“"), v);
  check("unconfirmed: reason in the detail", has(v.stampDetail, "Die vorliegende EU-Vertrauensliste"), v);
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

// Findings 1 and 4: handed-over folders wait until an archive is open for
// writing, then each is offered once; read-only mode drops them with a note.
{
  type S = Parameters<typeof gateOf>[0];
  const st = (over: { hasArchive?: boolean; readOnly?: boolean; lock?: boolean }): S =>
    ({ hasArchive: over.hasArchive ?? true, archive: { readOnly: over.readOnly ?? false }, lock: over.lock ? { host: "PC2" } : null }) as unknown as S;
  const drop = (folder: string, error = "") => ({ folder, error }) as FolderDrop;
  const inbox = new FolderInbox();
  inbox.add([drop("D:\\Scans\\2026-11")]);
  check("no status yet: waits", inbox.next(gateOf(null, false)) === null && inbox.size === 1);
  check("start screen: waits", inbox.next(gateOf(st({ hasArchive: false }), false)) === null && inbox.size === 1);
  check("locked: waits", inbox.next(gateOf(st({ lock: true }), false)) === null && inbox.size === 1);
  check("dialog open: waits", inbox.next(gateOf(st({}), true)) === null && inbox.size === 1);
  // "Archiv anlegen" → writable: offered once.
  const step = inbox.next(gateOf(st({}), false));
  check("after Archiv anlegen: offered", step?.folder === "D:\\Scans\\2026-11", step);
  check("offered once", inbox.next(gateOf(st({}), false)) === null && inbox.size === 0);

  // The same folder twice (start + second launch) is one offer; order kept.
  inbox.add([drop("A"), drop("B")]);
  inbox.add(null);
  inbox.add([drop("A")]);
  check("deduplicated", inbox.size === 2);
  check("fifo A", inbox.next(gateOf(st({}), false))?.folder === "A");
  check("fifo B", inbox.next(gateOf(st({}), false))?.folder === "B");

  // Code review 9: the same folder handed over again while its dialog is
  // open is not offered a second time after that dialog closes.
  inbox.add([drop("A")]);
  check("offered A", inbox.next(gateOf(st({}), false))?.folder === "A");
  inbox.add([drop("A")], true);
  check("A while its dialog is open: not queued", inbox.next(gateOf(st({}), true)) === null && inbox.size === 0);
  check("dialog closed: no second offer", inbox.next(gateOf(st({}), false)) === null);
  inbox.add([drop("A")]);
  check("later request for A: offered again", inbox.next(gateOf(st({}), false))?.folder === "A");
  // Code review 10: a request after the dialog closed but before the next
  // render (next() not yet called) is kept, not discarded as a duplicate.
  inbox.add([drop("A")], false);
  check("request after close, before render: kept", inbox.size === 1 && inbox.next(gateOf(st({}), false))?.folder === "A");
  inbox.next(gateOf(st({}), false));

  // View only (write-protected folder): not offered, dropped with one short note.
  inbox.add([drop("A"), drop("B")]);
  const ro = inbox.next(gateOf(st({ readOnly: true }), false));
  check("read-only: note, no dialog", !!ro?.note && !ro.folder && has(ro.note, "nicht archiviert"), ro);
  check("read-only: dropped", inbox.size === 0 && inbox.next(gateOf(st({}), false)) === null);

  // A drop error is shown when it can matter, never later on another screen.
  inbox.add([drop("", "Bitte ziehen Sie genau einen Ordner hierher.")]);
  check("error shown when writable", inbox.next(gateOf(st({}), false))?.error === "Bitte ziehen Sie genau einen Ordner hierher.");
  inbox.add([drop("", "x"), drop("C")]);
  check("start screen: errors dropped, folder kept", inbox.next(gateOf(st({ hasArchive: false }), false)) === null && inbox.size === 1);
  check("then the folder", inbox.next(gateOf(st({}), false))?.folder === "C");
  inbox.add([drop("", "x")]);
  check("read-only: error alone gives no note", inbox.next(gateOf(st({ readOnly: true }), false)) === null && inbox.size === 0);
}

// The detail view's file list: a changed file never hides behind "Alle anzeigen".
{
  const f = (path: string, state: string) => ({ path, size: 1, mtime: "", state });
  const got = deviationsFirst([f("a", "unveraendert"), f("b", "veraendert"), f("c", "unveraendert"), f("z", "hinzugekommen")]).map((x) => x.path).join("");
  check("deviating files first, each part in order", got === "bzac", got);
  // Code review 16 / visual review 7: eleven deviations, all visible collapsed.
  const many = deviationsFirst([...Array.from({ length: 11 }, (_, i) => f(`v${i}`, "veraendert")), ...Array.from({ length: 5 }, (_, i) => f(`u${i}`, "unveraendert"))]);
  const shown = collapsedFiles(many, 10);
  check("collapsed: every deviation shown", shown.length === 11 && shown.every((x) => x.state === "veraendert"), shown.length);
  check("collapsed: fills up with unchanged", collapsedFiles(deviationsFirst([f("a", "veraendert"), ...Array.from({ length: 12 }, (_, i) => f(`u${i}`, "unveraendert"))]), 10).length === 10);
}

if (failed) throw new Error(`${failed} check(s) failed`);
console.log("frontend unit checks: ok");
