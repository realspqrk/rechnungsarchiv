VERDICT fully done

No new defects found in this confirmation round.

The file-list finding is resolved. In an isolated browser check using the current TypeScript modules and stylesheet, eleven mixed deviations (changed, missing and added files) were all visible initially. Expanding showed all sixteen files; collapsing restored eleven rows with every deviation still visible. describe.ts collapsedFiles and views/deposit.ts implement the corrected boundary. The frontend unit checks pass, including the eleven-deviation regression case.

The refreshed 19-ablage-unbestaetigt.png clearly separates intact files from outstanding qualification and gives a qualified online-check suggestion. The rewritten lead no longer embeds the reason in a broken "weil" sentence. present.UnconfirmedReason only describes an older list as the reason when it was not fetched during this check. The PDF no longer appends the unconditional no-current-list claim. CLI Hinweis wording, README "freiwilligen Beitrag", and the "Fehlt" / "Hinzugekommen" tiles match the requested corrections.

The export behavior is consistent with the accepted ADR-0004 decision. A verified list fetched during the current check may settle qualification even when its signed issue date precedes the timestamp; this is not reported as a defect. copyLists passes refresh success into the export's verification options, and spec section 8 and docs/cli.md describe the same behavior. TestExportChecksOnlineWhenPossible passes for both online-qualified and offline-unconfirmed cases.

0002-online-1.png and -2.png show "qualifiziert", identify the check as online, and explicitly mark ES list No. 190 and the additional OCSP response as retrieved during the check. Tables, continuation heading and long filenames remain legible. Text extraction preserves all six complete filename/hash pairs, verified against the original Ablage ZIP after removing layout whitespace.

Validation used supplied artifacts, source review, frontend unit checks, the targeted export test and a mocked-data browser boundary check. No repository files were edited, no real timestamp was purchased, and no new live provider check was initiated.
