VERDICT changes: 5

Four unresolved findings; one new defect. R2 identifiers below distinguish carried-over findings from its four new findings.

| Round-2 item | Status | Reason / minimal remaining fix |
|---|---|---|
| Carried R1-2: qualification provenance | Fixed | Authenticated LOTL/national XML, pinned bootstrap, retained versions and historical service status replace bare certificate hashes. |
| Carried R1-4: revocation | Not fixed | A certificate held at genTime, then released before notAfter, still passes §5 using a later good response. Rejecting responses that currently report hold does not close this case. Require evidence establishing historical status, or an explicitly supported no-hold certificate policy; otherwise Unbestimmt. [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280.html#section-5.3.1). |
| Carried R1-5: protocol/time | Not fixed | Policy matching and rounding are fixed. §6.8 still treats absent token accuracy as zero. Use the applicable authenticated TSA policy's accuracy when omitted; without a bound, do not assert an exact latest time. [RFC 3161](https://www.rfc-editor.org/rfc/rfc3161.html#section-2.4.2). |
| Carried R1-9: UI claims | Not fixed | Corrected footer and variants exist, but Pruefen renders the proven-state footer unconditionally, including Unbestimmt after key compromise. That state explicitly has no reliable proven time. Show the footer only when its assertion is established; describe hash agreement without trusted time otherwise. |
| New-1: cryptographic verification | Fixed | CMS signature/messageDigest and RFC 5280 validation at genTime are explicit. |
| New-2: deduplication | Fixed | Every selected invoice is retained; duplicate detection is informational. |
| New-3: filtered exports | Not fixed | Initial boundary and included-evidence rules are fixed. Internal filtered gaps still trigger Lücke: sequences 1/2/3 with Belegjahr 2025/2024/2025 export as 1/3 for 2025. Distinguish deliberate exclusions throughout the subset; do not infer chain continuity across them without bridging evidence. |
| New-4: inherited qualification | Fixed | Inheritance carries source and qualification; an own token's date is not replaced. |

New defect:

1. **P1 — CA-only trusted-list anchoring rejects legitimate TSUs (spec §§5.7,6.6).** Both clauses now require a listed **CA** certificate. A QTST ServiceDigitalIdentity can instead identify the signing TSU directly, which need not be a CA. Its genuine token cannot satisfy this mandatory acceptance rule. **Minimal fix:** support direct signer-to-SDI matching as well as paths to listed CA identities; preserve signer validity/EKU/revocation checks without requiring CA basicConstraints on the TSU. [ENISA, pp. 56–57](https://www.enisa.europa.eu/sites/default/files/publications/WP2016%203-2%2015%20Guidelines%20on%20Initiation%20of%20Qualified%20Trust%20Services.pdf).
