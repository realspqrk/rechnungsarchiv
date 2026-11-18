VERDICT changes: 1

NEW FINDING: The eleventh deviating file is still hidden by the collapsed file list.
Evidence: app/frontend/src/views/deposit.ts:121-140; describe.ts:210; reproduced screenshot eleven-changed-initial.png beside this report.
What is wrong: deviationsFirst correctly brings changed, missing and added files to the front, but fileTable then always slices to ten rows. With eleven changed files and five unchanged files, the initial view names only changed files 01-10; changed file 11 appears only after "Alle 16 Dateien anzeigen". The banner reports eleven deviations but does not name them individually. This violates the explicit requirement that a changed file must not hide behind expansion.
Concrete fix: Always render every deviating file. Apply the ten-row limit only to unchanged files, or use an initial limit of max(10, number of deviations). Collapsing again must preserve all deviations. Verify with more than ten changed/missing/added files.

Per owner request:
1. File list / changed-file visibility - PARTLY MET. 17-ablage-unveraendert.png presents filenames, size, date and individual results clearly. 20-ablage-veraendert.png correctly prioritizes its single changed file. The multiple-deviation boundary fails as described above.
2. Positive certificate wording - MET. 17 uses "Zertifikat gueltig (OCSP-Pruefung)"; 0002-2.png states "Sperrstatus: gueltig" with the OCSP response date. The positive result remains distinct from qualification.
3. Clear "unbestaetigt" meaning - MET for the supplied cases. 19 explicitly identifies the older trusted list and separates verified file integrity from outstanding qualification. 10 has the dotted hint; overview.ts supplies UNCONFIRMED_TIP, keyboard focus and an accessible explanation. Both PDF first pages qualify the result and state that no current list was retrieved during this offline check. No generic timestamp-failure implication found.
4. Long PDF names - MET. 0002-2.png wraps the uninterrupted 192-character filename and nested path within their own rows, without clipping, collisions or shifted hashes. PDF text extraction retains all characters; after removing layout whitespace, all six filename/hash pairs match the original Ablage ZIP, including both long entries.
5. About/imprint/support - MET. 32 shows the support section and all four buttons. Source and an isolated browser check confirm distinct github/kofi/paypal/phrasevault dispatch IDs. The name, address, email and UID are present at the scrollable end; Close remains visible at 960x609.

Method: supplied images/PDFs, current source, PDF text/hash comparison, and isolated headless Edge with mocked report data. No repository files edited, external support pages opened, or timestamps purchased.
