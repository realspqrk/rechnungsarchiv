# ADR-0007: Route to tax-office acceptance and required documentation

**Status:** accepted 2026-11-18, revised after review round 1. Research: `docs/research/02-finanzamt-akzeptanz.md`, `reviews/r1-grok.md`.

## Context
- The BMF does not certify archiving software.
- The timestamp proves *the time and integrity of the Prüfwert* (eIDAS Art. 41(2)). It does **not** cover:
  - §11 UStG / E-Rechnung-UStV (authenticity of origin, via an internal control procedure);
  - the completeness of what was archived;
  - readability over 7–22 years.

  These are obligations of the *procedure*, not of the tool.

## Decision

### 1. Technical
RKSV pattern:
- chain;
- qualified timestamp;
- independent verifier;
- auditor export (spec §8).

### 2. Documentation (German, in the repo)
- `docs/rechtsgrundlagen.md`: maps each feature to its legal basis, and states plainly what the tool does *not* cover.
- `docs/verfahrensdokumentation.md`: a template with these chapters:
  - receiving and checking invoices, matching them to delivery and payment (§11 UStG, E-Rechnung-UStV §1, UStR Rz 1564g);
  - keeping e-invoices (ebInterface, ZUGFeRD, XRechnung) in the format received, keeping signature/EDI/PEPPOL evidence with them, and marking duplicates (UStR Rz 1565, 1570);
  - scanning paper: check the scan → archive → wait for the timestamp → only then destroy the paper (UStR Rz 1566, §132 Abs 2 BAO). Never replace an Ablage;
  - archiving rhythm (freely chosen: weekly, monthly, quarterly …), responsibilities, and only one writing PC;
  - backup (2nd medium, made *after* the timestamp) and an **annual restore test** with `verify`;
  - readability: viewers for PDF/XML, the export format, the accounting data coming from the bookkeeping system (§132 Abs 3 BAO);
  - retention and deletion (§132 BAO, §18 UStG, GDPR Art. 5(1)(e));
  - out of scope: cash receipts and DEP (RKSV), books and accounts (UGB §212).
- `docs/datenschutz.md`: a note for the record of processing activities.
  - Legal basis: Art. 6(1)(c).
  - Recipient: the timestamp provider (the Prüfwert, the account name and the IP address).
  - The only recipient is the configured EU QTSP. It receives the Prüfwert, the account name and the IP address. There is no third-country transfer: no free or US provider is offered.
- `docs/fuer-pruefer.md`: a guide for auditors (Betriebsprüfer) and Steuerberater.

### 3. Organisational (owner)
- An informal enquiry to the BMF and KSW.
- A KFS/DV 1 software attestation for a tagged release.
- A §118 BAO ruling via a pilot user.
- Collecting experience from real tax audits.

## Consequences
- Never "BMF-genehmigt" (approved by the BMF) or "zertifiziert" (certified).
- UI and docs use these phrasings:

  | Never | Instead |
  |---|---|
  | — | "stimmen mit dem spätestens am … belegten Stand überein" |
  | "Richtigkeit von Zeit und Inhalt" | "Vermutung der Richtigkeit von Zeitpunkt und Unversehrtheit (Art. 41 Abs. 2 eIDAS)" |
