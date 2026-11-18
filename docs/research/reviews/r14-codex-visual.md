VERDICT changes: 2

Review scope: supplied screenshots and both rendered PDF versions, source walkthrough, and text extraction from new.pdf. Workflow simulation was based on those artifacts; no live archive, paid timestamp, or repository edit was performed. Paths below are relative to D:/Code/rechnungsarchiv.

1. Disclaimer still contains implementation prose and duplicated content.
Evidence: .review/shots/32-ueber.png and 33-haftungsausschluss.png; docs/haftungsausschluss.md:5; app/frontend/src/dialogs/about.ts:61-63.
What is wrong: The first-start notice communicates "as is" and the user's responsibility clearly. However, the full disclaimer opens by explaining where the program displays its own text and quoting the acknowledgement. "Über" displays the short disclaimer, then repeats it inside the full disclaimer. This forces scrolling through repeated material and falls short of the owner's request for a short disclaimer.
Concrete fix: Remove the display/acknowledgement instructions from the user-facing Markdown. Keep one concise disclaimer covering as-is provision, liability within legal limits, and responsibility for legal suitability. In "Über", render that text once and link to any remaining full text instead of embedding the same summary again. Keep docs/haftungsausschluss.md as the sole text source.

2. PDF timestamp table continues without a section heading.
Evidence: .review/pdf/after-1.png and after-2.png; internal/report/report.go:409-444.
What is wrong: Page 1 ends at Policy-OID; page 2 starts directly with Signaturzertifikat. A Betriebsprüfer reading the second printed page must reconstruct that these are still timestamp details. The footer identifies the Ablage, but the continuation has no section label. This weakens the otherwise clear technical hierarchy.
Concrete fix: Repeat "Zeitstempel (Fortsetzung)" when that table crosses a page boundary, reserving space for the heading and first complete row. Keep the existing margins and row spacing; do not compress the document merely to force the table onto page 1.

Owner requests:
1. Gear icon - MET. 03-main-normal and 27-einstellungen-basic show a recognizable gear leading to settings; overview.ts uses icon("gear") with an accessible label and title.
2. Short as-is disclaimer / legal suitability is the user's responsibility - PARTLY MET. Both substance requirements are explicit in 01-start. Full-dialog presentation still needs finding 1.
3. Factual Archivprotokoll, technical tables, better spacing, orientation evaluation - MOSTLY MET; finding 2 remains. Compared with before-1/2, after-1/2 replace dense prose and the independent-check guide with factual tables, clear rules and whitespace. No disclaimer or procedural instructions remain. Landscape is preferable here: all nine files fit one row each with the complete hash beside the filename. Portrait previously split metadata across lines and pages. Retain landscape.
4. No Ablage-name field; clarify Archivordner as destination - MET. 01-start explicitly explains the destination and preserved source folders; 27 reinforces where Ablagen live. 11-archivieren shows the selected source and derived name as text, with no name input. archive.ts submits p.suggestedName.
5. Company/archive location away from product logo - MET. 03 and 08 place them in a separate heading below the toolbar. The read-only badge remains attached to the relevant archive.

Bookkeeper simulation: Enter company, choose destination, create archive; follow "Jetzt einrichten", select Mensatek and enter credentials; return, select the monthly invoice folder, inspect derived name, count, duplicate/skipped-file warnings, Belegjahr and retention; archive. 13 gives an unambiguous completion and backup reminder; 14/15 distinguish saved files from a pending timestamp and supply recovery actions. No workflow blocker found in this walkthrough.

Betriebsprüfer simulation: Page 1 identifies company/Ablage and clearly reports "Zeitgestempelt · unbestätigt", nine matching files, offline checking and the qualification limitation. Page 2 provides complete file hashes. pdftotext -layout preserves all nine full rows; every extracted filename/hash pair matches the corresponding demo file's SHA-256, including umlauts. Raw extraction also preserves whole rows; default extraction keeps filename/hash pairs together but separates some other columns. Actual viewer clipboard behavior was not exercised. No wrong hash association found.
