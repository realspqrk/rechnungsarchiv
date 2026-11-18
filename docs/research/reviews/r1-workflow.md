VERDICT changes: 15

Internal Claude subagent, round 1. It simulated three workflows: a bookkeeper, a Steuerberater, and a Betriebsprüfer 6 years later (TSA gone bankrupt, original PC gone). Condensed record.

**P1**
1. **No TSA error states.** Cases: wrong password, credit used up, offline. Fix:
   - Always commit locally.
   - Show the pending reason.
   - Never fall back silently to the free TSA.
   - Retry pending stamps whenever any stamp succeeds.
   - The summary tile shows the worst status.
2. **Late invoice into an already archived month** produces duplicates. Fix: hash-match against existing Ablagen ("64 von 65 bereits in Nr. 0011 → nur 1 neue").
3. **No first-run screen and no "Archiv öffnen".** Opening someone else's archive must be strictly read-only: no auto-stamping, no lock, no writes.
4. **Two PCs on a NAS:**
   - Credentials are per PC.
   - A stale `.lock` needs a takeover path.
   - A copied archive creates a fork, which needs its own wording.
   - Cloud placeholder files must be refused.
   - The docs say "never copy an archive to work in it".
5. **No export dialog.** It needs a Belegjahr range and a target folder. An unsigned .exe gets blocked by SmartScreen, so put a pre-generated Prüfbericht at the top of the export.

**P2**
6. **Overclaims:**
   - "unabhängigen Zeitstempeln" (plural)
   - "Nummerierung lückenlos" (implies completeness)
   - "Bestätigt" (could be read as "confirmed by the Finanzamt")
   - "Richtigkeit von Zeit und Inhalt"
   - the result disclaimer lacks "nicht, dass nichts fehlt"
   - "später umstellbar" must say it applies to new Ablagen only
7. **Date column** shows the unproven PC date. Show the timestamp date instead.
8. **Missing revocation (LTV) data** leaves an Ablage Unbestimmt forever. It must stay Ausstehend until token + chain + revocation data are stored.
9. **Belegjahr** from the newest file date is wrong for Dec/Jan. Take it from the folder name. Drop the "Nachträglich" label.
10. **Result screen** lacks the pending, not-qualified, Unbestimmt, gap and fork states. "Alle prüfen" should be archive-wide. "Verändert" needs a next step: "aus Sicherung wiederherstellen".
11. **Pairing and naming:**
    - Pair ZIP and Nachweis by Prüfwert, not filename.
    - "Ändern …" → "Anderes Archiv öffnen".
    - The name prefill repeats YYYY-MM.

**P3: clutter**
12. Remove:
    - the per-Ablage retention picker;
    - Sperre;
    - the auto-retry checkbox;
    - the Prüfwert on the done screen;
    - export on the detail page;
    - the dual-purpose ZIP drop.
13. Replace the free TSA URL field with a picklist of allowlisted QTSPs. Say whether "Verbindung testen" uses up a stamp.
14. **Vocabulary:**
    - "Prüf-" is overloaded → actions "Archiv kontrollieren" and "Export für Betriebsprüfung".
    - Use only "Zeitstempel" in the UI.
    - Explain "Ablage" once.
    - "nicht qualifiziert" gets a neutral check, not green.
15. **`docs/spec.md` is missing** but is cited as normative.
