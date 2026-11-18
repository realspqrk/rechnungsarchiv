VERDICT changes: 4

Six of the eight round-1 findings are fixed in the revised design. Two are not. Two new claim errors are in the README and the UI.

| # | Status | Why |
|---|---|---|
| 1 | fixed | ADR-0007 lists the procedure chapters (receipt and match to supply and payment, original e-invoice format, signature/EDI/PEPPOL evidence, marked duplicates). README states §11 and the E-Rechnung-UStV stay with the company. |
| 2 | fixed | “Richtigkeit von Zeit und Inhalt” is gone. README binds Art. 41(2) to the Prüfwert. Settings: a later switch applies only to new Ablagen. |
| 3 | fixed | Spec §5 stores chain and OCSP/CRL before Zeitgestempelt. Spec §6–§7 grants “qualifiziert” only if the pinned trust file shows QTST `granted` at genTime. No renewal in v1 is accepted. Remove the stale `followup.md` bullet that still defers this check. |
| 4 | fixed | ADR-0007: check the scan, archive, wait for the timestamp, only then destroy the paper; never replace an Ablage. The UI no longer calls the ZIP “schreibgeschützt”. Last-Ablage deletion is disclosed. |
| 5 | fixed | Same chapter list covers received format (ebInterface, ZUGFeRD, XRechnung), duplicates, and cash receipts/DEP as out of scope. |
| 6 | not fixed | Classes 7 / 10 voluntary / 12 / 22 and a user-set Belegjahr are in place. Dropping Sperre is acceptable while there is no deletion. The date is still 31.12. of Belegjahr+N, labelled “7 Jahre, § 132 BAO”. §132 Abs 1 moves the start when the financial year is not the calendar year, and pending proceedings extend it. The screen shows neither. |
| 7 | fixed | Spec §8 exports a file index (path, Belegjahr, belegt_spätestens) plus a printable report. Amounts stay in the bookkeeping, as decided. The procedure includes viewers and an annual restore test. |
| 8 | not fixed | No deletion in v1 is stated and accepted. README still says only the 32 bytes go out, and Einstellungen says only a checksum. Both omit account name and IP, which the DSGVO section admits. The no-contract default is DigiCert in the US. A notice is not a Chapter V tool (Art. 44 GDPR). |

### New defects

1. **Art. 41(1) is cited for the wrong rule.** README: a non-qualified stamp “unterliegt der freien Beweiswürdigung (Art. 41 Abs. 1 eIDAS, § 167 BAO)”. Art. 41(1) only forbids denying a stamp legal effect merely because it is electronic or not qualified. Free evaluation is §167 Abs 2 BAO. **Fix:** “Keine Vermutung nach Abs. 2. Die Behörde würdigt ihn frei (§ 167 Abs. 2 BAO). Abs. 1 verbietet nur, ihm die Wirkung allein wegen der elektronischen Form abzusprechen.” Sources: [Art. 41](https://eur-lex.europa.eu/eli/reg/2014/910/2024-10-18/eng), [§167 BAO](https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10003940&Paragraf=167).

2. **Opening copy still overstates the proof.** The README title “nachweisbar unverändertes Archiv” has no “seit T”. Main calls the Ablage a “versiegelte Kopie”, which reads as a qualified seal (Art. 3(27) eIDAS). Start and Main promise a qualified timestamp even when, without a contract, it is not qualified. **Fix:** “Archiv mit nachweisbarem Stand seit dem Zeitstempel”; “Kopie mit Prüfsumme”; “qualifizierter Zeitstempel nur mit eingerichtetem EU-Dienst”. Source: [Art. 3(27)](https://eur-lex.europa.eu/eli/reg/2014/910/2024-10-18/eng).

**Fixes for the two open items.** Item 6, under the date: “Gewöhnliches Ende. Ein abweichendes Wirtschaftsjahr und ein anhängiges Verfahren verlängern die Frist (§ 132 Abs. 1 BAO).” Item 8: “Übertragen werden der Prüfwert, der Kontoname und die IP-Adresse.” Use an EU QTSP unless a Chapter V tool for the US fallback is named. Sources: [§132 BAO](https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10003940&Paragraf=132), [Art. 44 GDPR](https://eur-lex.europa.eu/eli/reg/2016/679/oj).
