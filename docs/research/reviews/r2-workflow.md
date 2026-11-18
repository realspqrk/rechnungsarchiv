VERDICT changes: 9

Internal Claude subagent, round 2 (condensed). Of the 15 round-1 items, 13 are fixed or rejected-OK. One is only partly fixed. Rejected-OK: the per-Ablage retention select, and deferring Authenticode.

**Partly fixed, #10: archive-wide view.** "Archiv kontrollieren" and "nur kontrollieren" still open the single-Ablage page. Missing pieces:
- a folder picker for read-only mode;
- a place to show Lücke, Nummer doppelt vergeben, nicht qualifiziert and Unbestimmt.

**New**
1. **Mockup contradicts spec.** It shows Mensatek "qualifiziert" at genTime 03.11.2026, but the TL status is granted since 04.11.2026, and the TL snapshot date is 01.11.
2. **Trust list goes stale.** `next_update` is about 6 months ahead, so after that everything shows "unbestätigt" and there is no update path. Fix: an update banner, and the export refuses to run while stale.
3. **Duplicate-skip across Belegjahre** drops files from a year-range export and shortens their retention. Fix: dedupe only within the same Belegjahr.
4. **Providers not usable with HTTP Basic only.** Disig needs a client certificate; Namirial goes through sales; Certigna's API is undocumented. Fix: support only workable providers, or mark the others.
5. **"qualifiziert" hard-coded** on Start and Main, although the free default is not qualified.
6. **Pending wording vs spec §6.9.** A pending Ablage still reads "nicht belegt" after a successor has been stamped. Fix: "belegt spätestens T (über Ablage N)".
7. **No "nothing new" state** for when all files are duplicates.
8. **Naming:** "Kontrollbericht" and PRUEFBERICHT are the same report under two names; "sollte" vs "darf".
