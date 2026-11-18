VERDICT changes: 6

The caution box matches the owner’s warning. Über says „freiwilliger Beitrag“, not „Spende“, and the imprint (person, address, e-mail, UID) is enough for this sole trader. „Zertifikat gültig (OCSP-Prüfung)“ and „gültig · OCSP-Auskunft“ state the check positively. „Änderungsdatum“ beside „Kontrolle“ is the file date, not the verdict.

1. File: internal/report/statement.go
Current: unbestätigt: …; bei dieser Kontrolle wurde keine aktuelle Liste abgerufen
Proposed: unbestätigt: plus UnconfirmedReason only
Reason: „abgerufen“ is false when this check did fetch a list that is still older than the stamp. Online or offline is already in the Angaben row.

2. File: app/frontend/src/describe.ts
Current: ist noch offen, weil die vorliegende EU-Vertrauensliste (…) ist älter als der Zeitstempel.
Proposed: ist noch offen. Leave the reason as its own sentence on the stamp line.
Reason: After „weil“ the verb belongs at the end. The screenshot is a broken sentence, and the stamp line repeats the same fact.

3. File: cmd/rechnungsarchiv/render.go
Current: Hinweis: Qualifikation unbestätigt, weil die vorliegende EU-Vertrauensliste (…) ist älter …
Proposed: Hinweis: Qualifikation unbestätigt. Die vorliegende EU-Vertrauensliste (…) ist älter …
Reason: Same „weil“ error, including „weil es liegt keine … vor“.

4. File: app/frontend/src/text.ts
Current: Mit Internetverbindung lädt „Archiv kontrollieren“ die aktuelle Liste und klärt das; sonst klärt es die nächste Liste, die der Staat des Anbieters veröffentlicht.
Proposed: Mit Internetverbindung holt „Archiv kontrollieren“ die aktuelle EU-Vertrauensliste. Gibt es noch keine Liste nach dem Zeitstempel, bleibt die Qualifikation offen.
Reason: „klärt es“ has no clear subject, and an online check does not settle the question while the newest list is still older than the stamp.

5. File: README.md
Current: Bitte erwägen Sie eine Spende
Proposed: Bitte erwägen Sie einen freiwilligen Beitrag
Reason: „Spende“ reads as a charitable gift and a possible tax deduction. A payment to this sole trader is voluntary income; Über already says „freiwilliger Beitrag“.

6. File: app/frontend/src/views/deposit.ts
Current: counters „Fehlend“ and „Hinzugefügt“; row status „fehlt“ and „hinzugekommen“
Proposed: counters „Fehlt“ and „Hinzugekommen“, same words as the rows
Reason: One result should not have two names. „Datei“, „Größe“, „Änderungsdatum“ and „Kontrolle“ are clear.
