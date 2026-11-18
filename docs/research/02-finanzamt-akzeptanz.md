# Path to tax-authority acceptance (research 2026-11-18)

Source: research subagent, primary sources (RIS, BMF, USP, WKO). These are assessments, not legal advice.

## Bottom line

- **The BMF does not certify or approve archiving software.** We found no such procedure. The law sets requirements, not product approvals.
- **Not even cash registers get type approval** (WKO, Kassenrichtlinie 2012).
- **A blanket "Go" for the software is not available.** The strongest achievable combination:
  1. an independent **software attestation by a Wirtschaftsprüfer** based on KFS/DV 1 (optionally also IDW PS 880);
  2. a **§118 BAO Auskunftsbescheid (binding ruling)** obtained by a pilot user, limited to the §11 UStG question and binding only for that user;
  3. a **track record** of passed tax audits (Außenprüfungen).

## Legal basis (requirements, not approvals)

- **§131 Abs 1 Z 6 lit b BAO:** entries must not be altered in a way that hides the original content, and changes must be traceable.
- **§131 Abs 3 / §132 Abs 2–3 BAO:**
  - Data media are allowed if a complete, content-identical and orderly reproduction is possible until the retention period ends.
  - The period is 7 years.
  - On request, the business must provide the means to read the records at its own cost, plus readable reproductions where needed.
- **§11 Abs 2 UStG + E-Rechnung-UStV:** authenticity of origin, integrity of content and legibility must be ensured for 7 years, by both the issuer and the recipient.
  - Accepted methods include a qualified signature or seal, EDI, USP/PEPPOL, or an **internal control procedure with a reliable audit trail**.
  - The proof of authenticity and integrity (signature, verification record, etc.) is **part of the invoice and must be kept with it**.
  - A conversion is allowed only if it is "beyond doubt" that the content did not change.

## RKSV precedent (cash register security regulation): what Austria accepts as "secure"

- Every receipt is signed (ES256) and chained to the previous one with SHA-256.
- The signature certificate comes from a qualified trust service provider (QTSP) and is registered via FinanzOnline.
- The DEP (data capture log) can be exported as JSON at any time.
- The **BMF Belegcheck** app lets anyone verify receipts independently.
- Formal approval exists only for "closed overall systems" with more than 30 registers, via a court-certified expert's report (§§20–22 RKSV).

**Takeaway for us:** legal certainty in Austria comes from **open technical standards + QTSP + independent verification + a verifier tool**, not from a ministry blessing a product. Our design copies that pattern:

- hash chain;
- qualified timestamp;
- a standalone verifier (the equivalent of "Belegcheck");
- a JSON export.

## §118 BAO Auskunftsbescheid (binding ruling)

- **Eligible topics (Abs 2):** reorganisations, tax groups, international tax law, **VAT law (Umsatzsteuerrecht)**, and abuse under §22.
- **Who may apply:** only a taxpayer, about their own facts that have not yet happened. The software vendor cannot apply.
- **Likely not eligible:** "Is this method compliant with §131/132 BAO?", because that is procedural law.
- **Arguably eligible:** "Does the archive preserve authenticity, integrity and legibility under §11 Abs 2 UStG, for input VAT deduction?"
- **Risk:** the ruling can be refused if the tax consequences are not "significant", or if the question is treated as factual rather than legal.
- **Fee:**

  | Applicant's turnover | Fee |
  |---|---|
  | up to €400k | €1,500 |
  | over €400k | €3,000 |
  | over €700k | €5,000 |
  | medium-sized company | €10,000 |
  | large company or group | €20,000 |
  | rejected or withdrawn | €500 |

- **Effect:** binds only for the applicant and only for the facts described. **It must not be advertised as "BMF-approved".**

## How vendors back their claims

- **ELO:** software attestation by EY Vienna (2022), based on KFS/DV 1 + IDW PS 880, for one specific release, tested "under laboratory conditions".
  - Excluded from the attestation: the customer's internal control system (IKS), the procedure documentation (Verfahrensdokumentation), and scan completeness.
- **MailStore, d.velop:** version-specific IDW PS 880 attestations.
- **FreeFinance:** claims KFS/DV 1 + PS 880 but names no auditor.
- **BMD:** only a GoBD export certificate for its cash register.
- **RZL:** claims certification but names no certifying body.

## Action plan

| # | Step | Effect | Effort / cost (estimate) |
|---|---|---|---|
| 1 | Compliance mapping (each feature → its paragraph) | Basis for everything else | Internal |
| 2 | Hardening: offline verifier, auditor export, Verfahrensdokumentation template, qualified timestamp | Technical credibility | Engineering + QTSP fee (GLOBALTRUST currently €185/yr; cheaper options under research) |
| 3 | Software attestation by an Austrian Wirtschaftsprüfer (KFS/DV 1) on a tagged release; re-attest each major release | Strongest market standard, but not binding | Unverified guess: low to mid five-figure euro amount. Consider sponsoring |
| 4 | Informal enquiry: BMF contact form / Steuerombudsdienst, plus WKO and KSW | Not binding | Low |
| 5 | Pilot user files a §118 application via their Steuerberater, §11 UStG question only | Binding for that user only | €1,500 fee + Steuerberater opinion; high rejection risk |
| 6 | Collect anonymised audit outcomes | De facto acceptance | Ongoing |

## Open points

- The full text of §118 BAO as amended by BGBl I 97/2025 (in force from 2027).
- The UStR 2000 wording at Rz 1566/1570 (to check on findok).
- Real price quotes from audit firms.

## Sources

- [§118 BAO](https://www.ris.bka.gv.at/Dokumente/Bundesnormen/NOR40254848/NOR40254848.html)
- [§131 BAO](https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10003940&Paragraf=131)
- [§132 BAO](https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10003940&Paragraf=132)
- [RKSV](https://www.ris.bka.gv.at/GeltendeFassung.wxe?Abfrage=Bundesnormen&Gesetzesnummer=20009390)
- [WKO, Kassenrichtlinie 2012](https://www.wko.at/steuern/kassenrichtlinie-2012)
- [BMF, E-Rechnung](https://www.bmf.gv.at/themen/steuern/fuer-unternehmen/umsatzsteuer/informationen/elektronische-rechnung-ueberblick-ueber-die-aenderungen-durch-das-abgabenaenderungsgesetz-2012.html)
- [USP, E-Rechnung](https://www.usp.gv.at/themen/steuern-finanzen/umsatzsteuer-ueberblick/weitere-informationen-zur-umsatzsteuer/vorsteuerabzug-und-rechnung/e-rechnung.html)
- [ELO/EY software attestation](https://www.elo.com/files/public/downloads/compliance/Softwarebescheinigung_ELO_21_IDW_PS_880.pdf)
- [MailStore software attestation](https://www.mailstore.com/de/wp-content/uploads/sites/2/2026/07/softwarebescheinigung-idw-ps-880-mailstore-server-26.3.pdf)
- [BMF contact](https://www.bmf.gv.at/kontakt)
