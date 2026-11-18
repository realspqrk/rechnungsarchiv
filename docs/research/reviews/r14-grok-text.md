VERDICT changes: 4

Checked, no wording change. The Kurzfassung in docs/haftungsausschluss.md, the README callout (lines 19–21) and the Haftung line say the same thing. "Unveränderliche Ablagen" sits in the same sentence as "spätere Veränderungen werden erkennbar", and the next sentences put legal sufficiency on the user. That matches the owner's brief and stays a true description of write-once storage plus a detectable change. Banned approval words (BMF-genehmigt, zertifiziert, Siegel, Bestätigt) are absent. The long text limits a timestamp to "files existed in this state at latest at T" and names the gaps: Echtheit, Richtigkeit, Vollständigkeit. The Archivprotokoll stays a fact table and contains no disclaimer. "Belegt: Dateistand bestand spätestens …" is the allowed claim. "Zeitgestempelt · unbestätigt" is the specified qualifier when no trusted list issued after the stamp is available. The settings hint and "Ordnername als Name der Ablage nicht verwendbar" are accurate.

The liability sentence is plausible for B2B. "Die Haftung ist ausgeschlossen, soweit das Gesetz das zulässt" leaves Vorsatz and personal injury under the rules that cannot be waived in advance (§ 879 ABGB; § 9 Produkthaftungsgesetz). Between entrepreneurs, an exclusion of slight negligence is effective. A blanket exclusion in standard terms can still fail as unclear under § 879 Abs 3 ABGB, because Austrian courts do not trim an invalid clause down to the lawful remainder. The savings clause is the usual short way to claim only that remainder, and it fits free Apache-2.0 software. The longer "only slight negligence" list would be safer for the author; it is a different sentence from the one requested. No rewrite.

1. File: app/frontend/src/views/start.ts
Current: Hierher kopiert das Programm jeden Ordner, den Sie archivieren, als Ablage (ZIP-Datei mit Nachweis). Ihre Rechnungsordner bleiben, wo sie sind.
Proposed: Hierher kopiert das Programm jeden Ordner, den Sie archivieren, als Ablage (ZIP-Datei). Ihre Rechnungsordner bleiben, wo sie sind.
Reason: The parenthesis defines the Ablage as a ZIP that contains the Nachweis; timestamp, chain and revocation status are the sibling Nachweis folder.

2. File: app/frontend/src/dialogs/archive.ts
Current: Wird Ablage ${seq} „${name}“
Proposed: Wird zu Ablage ${seq} „${name}“
Reason: The future needs "zu"; as written the line is a clipped status, not a name preview.

3. File: internal/report/report.go
Current: Geändert
Proposed: Änderungsdatum
Reason: In this protocol "verändert" means the file no longer matches the manifest; the column is the original file date.

4. File: internal/report/statement.go
Current: Steuerdateien: weichen vom Prüfwert-Verzeichnis ab
Proposed: Begleitdateien: weichen vom Prüfwert-Verzeichnis ab
Reason: On a Betriebsprüfung PDF "Steuerdateien" reads as tax documents; the row is BagIt tag files that no longer match the tag manifest.
