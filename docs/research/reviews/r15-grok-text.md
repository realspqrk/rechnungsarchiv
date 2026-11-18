VERDICT changes: 1

Checked. The ZIP hint, „Änderungsdatum“, „Begleitdateien“ (PDF row and verifier messages) and „Wird als Ablage … gespeichert.“ are in place. The HTML comment is stripped from the disclaimer readers see; the Kurzfassung and the consent sentence still parse. Über shows the short paragraph once, with „Vollständiger Text“, and leaves the create-time consent on the start screen. „Zurück zum Archiv“ is the way back from Archiv wechseln. Continued PDF tables use „<Überschrift> – Fortsetzung“.

1. File: app/frontend/src/dialogs/archive.ts
Current: Wird als Ablage ${seq} „${name}“ gespeichert.
Proposed: Use that sentence only while „Archivieren“ is possible. While the button is disabled (no files, or a file or folder-name problem): Ablage ${seq} „${name}“
Reason: The line stays on screen next to „können nicht archiviert werden“, so „gespeichert“ is false in that state.
