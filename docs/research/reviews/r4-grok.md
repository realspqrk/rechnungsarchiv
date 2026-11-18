VERDICT changes: 1

Round 3 is fixed in ADR-0007 and in the AGENTS rule. One sentence in the same file still contradicts it.

ADR-0007 now says the only recipient is the configured EU QTSP, which receives the Prüfwert, the account name and the IP address, and that there is no free or US provider and no third-country transfer. The old fallback sentence is gone.

The AGENTS rule matches that: the Prüfwert, the TSA account name and the IP address leave; file names, per-file hashes, invoice bytes and sidecars do not.

The opening line of `AGENTS.md` still says only the 32-byte Prüfwert leaves the machine. **Fix:** use the same wording as the rule.

README pass: no remaining claim error. Art. 41(2) is limited to the time and the integrity of the Prüfwert. A non-qualified stamp is free evaluation under § 167 Abs. 2 BAO. Art. 41(1) only bars denying effect because the stamp is electronic. The provider receives the Prüfwert, account name and IP address. Only EU providers are used. § 132 states the financial-year start and the extension for pending proceedings. Key compromise and a missing last Ablage are disclosed as limits. The tool is not described as BMF-certified.
