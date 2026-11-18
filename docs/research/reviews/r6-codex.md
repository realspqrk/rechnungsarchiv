VERDICT changes: 1

| Round-5 item | Result |
|---|---|
| Contradictory evidence conditions | Fixed in normative spec §5, but ADR-0004 still mandates that every piece of revocation evidence be “issued at or after genTime”. This contradicts the permitted interval-covering alternative. |
| Cached retrieval metadata | Fixed. Spec §§6.6–7 and ADR-0004 require a signed post-genTime issue date or retrieval by the verifier during the current run; cached retrieval times cannot establish qualification. |

**Remaining fix:** remove ADR-0004's unconditional “issued at or after genTime” bullet, or restrict it to the post-genTime alternative. Otherwise the ADR still directs rejection of evidence issued before genTime whose validity interval covers it, despite the corrected normative specification.

No other remaining defects identified within this verification scope. The accepted hold residual remains closed.
