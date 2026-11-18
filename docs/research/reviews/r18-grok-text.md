VERDICT changes: 1

The earlier findings are in place. Deviations stay visible in the short file list. The PDF no longer says no list was fetched. „älter“ is only used when this check did not fetch the list. The „weil“ sentences are gone. README and Über say „freiwilligen Beitrag“. The counters say „Fehlt“ and „Hinzugekommen“. The online export protocol says „qualifiziert“ and „online“; a list fetched during that check may be older than the stamp. That matches the decision.

1. File: internal/export/guide.go (same sentence in docs/fuer-pruefer.md)
Current: Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, bleibt offen, ob der Dienst zu diesem Zeitpunkt qualifiziert war (unbestätigt), bis eine neuere Liste vorliegt.
Proposed: Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, sagt eine Offline-Kontrolle unbestätigt, bis eine neuere Liste vorliegt. Eine Liste, die die Kontrolle selbst aus dem Internet holt, entscheidet auch dann, wenn sie vor dem Stempel ausgestellt wurde.
Reason: The Archivprotokoll in the same export is „qualifiziert“ from ES list No. 190 of 04.11.2026 for a stamp on 18.11.2026, marked as fetched during the check. The guide still says the question stays open until a newer list exists.
