# Legal basis Austria (research 2026-11-18)

Source: research subagent. Statutes checked in RIS (current consolidated versions). UStR quoted via BMF/USP and the Linde mirror. **[UNVERIFIED]** = could not be confirmed. Not legal advice.

## 1. Retention periods

| Norm | Period | Start / note |
|---|---|---|
| §132 Abs 1 BAO | **7 years** for books, records and documents (Belege) | From the **end of the calendar year the document relates to**. Extended as long as the documents matter for **pending proceedings**. Unchanged since 1999. |
| §212 UGB | 7 years | From the end of the calendar year |
| §11 Abs 2 UStG | 7 years for invoice copies; for e-invoices, authenticity, integrity and legibility must be ensured for 7 years | Version in force since 24.12.2025 (BGBl I 97/2025) |
| §18 Abs 10 UStG | **22 years** for records on Grundstücke (§6 Abs 1 Z 9 lit a); 12 years for certain WGG cases | UStR Rz 1569 |
| §18 Abs 12 UStG | 10 years (OSS / special electronic records) | |
| §207 Abs 2, §209 Abs 3 BAO | Limitation period of **10 years** for evaded taxes; absolute 10 years | Practical advice: keep documents for about 10 years. Not a legal duty. |

Germany cut its period to 8 years (BEG IV). **That change does not apply in Austria.**

## 2. Electronic storage

- **§131 Abs 1 Z 6 lit b BAO** (and §190 Abs 4 UGB): no change may hide the original content. This is Austria's "Unveränderbarkeit" (immutability) requirement.
- **§131 Abs 3 BAO**: storage on data carriers is allowed if "inhaltsgleiche, vollständige und geordnete Wiedergabe bis zum Ablauf der gesetzlichen Aufbewahrungsfrist jederzeit gewährleistet" (content-identical, complete and orderly reproduction is guaranteed at all times until the retention period ends).
- **§132 Abs 2 BAO** (and §190 Abs 5 UGB): documents may be kept on data carriers if reproduction is complete, orderly, content-identical and true to the original. "Urschriftgetreu" (true to the original) is not required for documents that only exist electronically.
- **§132 Abs 3 BAO**: the taxpayer must, at their own cost and within a reasonable time, provide the means to make documents readable. Where needed, they must also provide durable reproductions readable without such means, on data carriers.
- **Replacing paper with scans is allowed** (UStR Rz 1566). Conditions:
  - the scan is true to the original;
  - it cannot be changed afterwards (WORM, a "revisionssicher" set-up archive, or a combination of technical and organisational measures).
  - There is no Austrian TR-RESISCAN (unlike Germany). No statutory tax exceptions were found. For civil-law reasons, keeping original contracts and deeds is still advisable.

## 3. Original format of e-invoices

- A reproduction true to the original is **not required** (BMF, USP, UStR Rz 1570). A printout is allowed.
- If converted: "aus den aufbewahrten Daten zweifelsfrei hervorgehen" (it must be beyond doubt from the kept data) that the content did not change.
- Rz 1570: **proofs of authenticity and integrity are part of the invoice** and must be kept with it: signature, signature check log, EDI agreement, documentation of the control procedure.
- Rz 1565: an invoice issued in several formats (e.g. PDF + XML) must be marked as such. Otherwise tax is owed simply because it was invoiced (Steuerschuld kraft Rechnungslegung).
- **PDF/A is not mentioned** in Austrian law or guidance.
- [UNVERIFIED] Which part of a ZUGFeRD hybrid file (PDF or XML) is the binding one.
- **Consequence for us:**
  - Keep originals byte-identical; that exceeds the minimum and makes "zweifelsfrei" easy to show.
  - Archive proof files (signature logs, control-procedure documentation) together with the invoices.

## 4. Authenticity, procedure documentation, KFS/DV 1

- The wording "innerbetriebliches Steuerungsverfahren … verlässlicher Prüfpfad" is **not** in the Austrian UStG. It comes from **§1 E-Rechnung-UStV**. Authenticity and integrity are "jedenfalls gewährleistet" (in any case ensured) by any of:
  - an internal control procedure with a reliable audit trail,
  - USP/PEPPOL,
  - a qualified signature or seal,
  - EDI.
- UStR Rz 1564f: free choice of procedure. **Rz 1564g: the procedure must be documented** (not per invoice).
- No general statutory **procedure documentation (Verfahrensdokumentation)** requirement in the BAO. KFS/DV 1 treats one as part of orderly bookkeeping. If you rely on "organisatorische Maßnahmen" (organisational measures), you effectively need one.
- **KFS/DV 1** (KSW, 21.06.2011), from excerpts: changes must be "für einen sachverständigen Dritten in angemessener Zeit nachvollziehbar" (traceable by an expert third party within reasonable time). The procedure documentation falls under the retention obligation. [UNVERIFIED: the archiving paragraphs in detail; the PDF could not be retrieved.]
- There is no GoBD in Austria. Mapping of the German concepts:
  - Unveränderbarkeit → §131 Abs 1 Z 6 lit b BAO
  - Datenzugriff (data access) → §132 Abs 3 BAO (handing over data carriers; no remote access regime)

## 5. Developments 2026–2030

- **ViDA = Directive (EU) 2025/516:**
  - National e-invoicing obligations are possible without derogation since 14.04.2025.
  - Intra-EU B2B: structured e-invoicing (EN 16931) plus digital reporting from **01.07.2030**.
  - National systems must be aligned by 2035.
- **Austria:** e-invoicing is mandatory for B2G only (since 2014, via ebInterface/PEPPOL). No B2B draft law or timeline yet (as of 11/2026).
- **§132a BAO:** digital cash-register receipts allowed from **01.10.2026** (PDF, text, XML), so more digital incoming receipts.
- Retention periods: **not shortened** in Austria.

## 6. GDPR

- Legal basis for retention: Art 6(1)(c) and Art 17(3)(b) GDPR. Art 5(1)(e) requires **deletion once the period has ended**. So we need a deletion date per package, and deleting a package **must not break the verifiability of the other packages**.
- **EDPB Guidelines 02/2025 v2.0 (07.07.2026), para 52:** a salted hash on a public ledger is personal data. After the salt is deleted, the hash "should not be linkable".
  - Mitigations:
    - a random nonce per package;
    - publish only the seal root, never per-file hashes;
    - delete the package (and therefore the nonce) when the period ends, which makes the published hash unlinkable.
  - Document this in the GDPR record of processing activities.
- CJEU C-413/23 P (2025): relative concept of personal data. A TSA/OTS calendar cannot re-identify anyone from the hash, so for them it is likely not personal data.

## 7. Tax audit (Außenprüfung)

- §138 Abs 2 and §144 Abs 2 BAO: duty to present documents. §132 Abs 3: readable within a reasonable time.
- WKO FAQ (as of 03/2026):
  - The audit usually covers 3 years.
  - Standard: "sachverständiger Dritter … in angemessener Zeit" (an expert third party within reasonable time).
  - Accounting data as text or XML; PDF is not allowed for that data. This applies to accounting data, not to invoice images.
- Data formats: Erlass AÖF 2000/138; SAF-T AT optional since 2009.
- §184 Abs 3 BAO: formal defects that cast doubt on substantive correctness lead to **Schätzung** (estimation by the tax office).

## Sources

- RIS BAO §§131, 132, 138, 144, 184, 207, 209: <https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10003940&Paragraf=132>
- RIS UGB §§190, 212: <https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10001702&Paragraf=212>
- RIS UStG §§11, 18: <https://www.ris.bka.gv.at/NormDokument.wxe?Abfrage=Bundesnormen&Gesetzesnummer=10004873&Paragraf=11>
- E-Rechnung-UStV §1: <https://www.jusline.at/gesetz/erustv/paragraf/1>
- UStR Rz 1565–1570: <https://linda.lindeverlag.at/Dokument/76550_11___2___4___/>
- UStR Rz 1564f–h: <https://linda.lindeverlag.at/Dokument/76550_11___2___3a___4___/>
- BMF e-invoice: <https://www.bmf.gv.at/themen/steuern/fuer-unternehmen/umsatzsteuer/informationen/elektronische-rechnung-ueberblick-ueber-die-aenderungen-durch-das-abgabenaenderungsgesetz-2012.html>
- USP e-invoice: <https://www.usp.gv.at/themen/steuern-finanzen/umsatzsteuer-ueberblick/weitere-informationen-zur-umsatzsteuer/vorsteuerabzug-und-rechnung/e-rechnung.html>
- BMF SAF-T: <https://www.bmf.gv.at/themen/steuern/fristen-verfahren/bundesabgabenordnung/standard-audit-file-tax.html>
- WKO FAQ Außenprüfung: <https://www.wko.at/steuern/aussenpruefung-vormals-betriebspruefung-faq>
- KSW Fachgutachten DV: <https://ksw.or.at/unsere-expertise/fachgutachten/datenverarbeitung/>
- ViDA: <https://eur-lex.europa.eu/eli/dir/2025/516/oj/eng>
- EDPB Guidelines 02/2025 v2.0: <https://www.edpb.europa.eu/system/files/2026-07/edpb_guidelines_202502_blockchain_v2_en.pdf>
- CJEU C-413/23 P: <https://curia.europa.eu/jcms/jcms/p1_5127926/sv>
