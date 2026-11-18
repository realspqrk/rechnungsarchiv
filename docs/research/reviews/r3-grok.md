VERDICT changes: 1

The four round-2 items are fixed in the README and the UI. One normative file still describes the rejected US fallback.

| # | Status | Why |
|---|---|---|
| 1 Retention date | fixed | Prüfen shows “7 Jahre, gewöhnliches Fristende” and states that a non-calendar financial year or pending proceedings extend the period (§ 132 Abs. 1 BAO). The README states the same rule. |
| 2 Data sent / US fallback | fixed | README and Einstellungen say the provider receives the Prüfwert, the account name and the IP address, and no file names. The free US path is gone. ADR-0001: without a provider the Ablage stays “ausstehend”. Only EU QTST services are in the list. |
| 3 Art. 41(1) | fixed | README: no presumption if the service was not qualified at genTime; free evaluation is § 167 Abs. 2 BAO; Art. 41(1) only forbids denying effect solely because the stamp is electronic. The UI no longer cites Abs. 1 as the source of free evaluation. |
| 4 Opening copy | fixed | The title no longer says “nachweisbar unverändertes Archiv”. Main says “Kopie (Ablage) mit Prüfsumme”. “Versiegelt” is gone. Start and Main tie the stamp to a qualified EU service, which matches the removal of the free path. |

### New defect

**ADR-0007 still specifies the US fallback that round 2 removed.** The datenschutz note says EU QTSPs are preferred and “the free US provider is used only as a fallback, with a notice.” ADR-0001, the README and the UI say the opposite: no free provider, EU only. A record of processing written from ADR-0007 would document a third-country transfer the product no longer makes (GDPR Art. 30 must match the processing; Art. 44 is why the fallback was removed).

**Fix:** delete that sentence. State that the only recipient is the configured EU QTSP, and that the data sent are the Prüfwert, the account name and the IP address.

`AGENTS.md` still says “Only the Prüfwert leaves the machine.” That is the same overclaim. Change it to: the Prüfwert, the account name and the IP address leave; file names, per-file hashes and invoice bytes do not.

No other new claim error in the README or the mockups. Art. 41(2) stays limited to the time and the integrity of the Prüfwert.
