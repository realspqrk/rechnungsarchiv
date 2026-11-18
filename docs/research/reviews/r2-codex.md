VERDICT changes: 8

Four round-1 findings remain unresolved; four new defects follow. Deliberate scope limitations are accepted.

| R1 | Result | Verification / remaining fix |
|---|---|---|
| 1 — TSA compromise | Fixed | Compromise yields Unbestimmt; README explicitly accepts single-TSA backdating risk. Normal provider termination is distinguished from compromise. |
| 2 — Qualification | Not fixed | Spec §§6–7 retain certificate hashes/history without specifying time-scoped SDI/qualifier bindings or authenticated TL/LOTL provenance and bootstrap. Incorrect service/history association can overqualify a token. Preserve and evaluate those bindings and authenticated sources. [DSS](https://ec.europa.eu/digital-building-blocks/DSS/webapp-demo/doc/dss-documentation.html). |
| 3 — Bag profile | Fixed | Mandatory authenticated tags, inventory checks, path restrictions and bounded parsing now exist. |
| 4 — Revocation | Not fixed | §5 wrongly assumes monotonic status: certificateHold can be released; expired certificates can disappear from CRLs. Arbitrarily later good/non-listing does not establish historical validity. Require historical coverage, freshness, CRL scope/base-delta processing and applicable intermediate-certificate checks. [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280.html#section-5.3.1), [RFC 6960](https://www.rfc-editor.org/rfc/rfc6960.html#section-4.4.4). |
| 5 — Protocol/time | Not fixed | EKU and fuzz tests fixed. Requested-policy matching and timestamp accuracy remain absent; raw genTime can assert an unjustifiably early upper bound. Enforce policy matching and use genTime plus authenticated accuracy, including display rounding. [RFC 3161](https://www.rfc-editor.org/rfc/rfc3161.html#section-2.4.2). |
| 6 — Pending dates | Fixed | Local dates are assertions; stamping revalidates the bag. |
| 7 — Transactions | Fixed | Defined commit/recovery, immutable numbered attempts, no index; AGENTS requires temporary writes, fsync and rename for evidence. |
| 8 — Multiple writers | Fixed | One writer, unsupported synced targets, explicit predecessor/genesis checks. |
| 9 — UI claims | Not fixed | Pruefen still says “Die Dateien wurden seit dem genannten Zeitpunkt nicht verändert”; restoration after alteration remains invisible. Replace that footer with the adopted state-equality wording. It also lacks Unbestimmt/nonqualified result variants. |
| 10 — Longevity/YAGNI | Fixed | SHA-512 and reproducibility requirements cut; renewal deferred explicitly, with algorithm weakening recorded in followup. |

New defects, prioritized:

1. **P0 — Normative acceptance omits cryptographic verification (spec §5; ADR-0004 calls it the full list).** CMS structure, ESS identity and imprint matching do not verify the CMS signature or signed messageDigest. A fabricated token carrying a genuine public TSA certificate can satisfy the enumerated checks. Chain-at-genTime validation from the earlier ADR also disappeared. **Minimal fix:** explicitly require signature/signed-attribute verification and certificate-path validation to the selected trust anchor at genTime, including certificate constraints.

2. **P1 — Automatic deduplication loses archival context (§3).** A renamed duplicate is omitted, but skipped.txt identifies only an Ablage number, not the referenced payload/hash. An entirely duplicate folder leaves no record at all. A later-year export consequently cannot reconstruct that folder from its selected deposits. **Minimal fix:** remove automatic cross-deposit deduplication; archive the selected originals. Keeping it requires authenticated resolvable references and dependency-aware exports.

3. **P1 — Filtered exports cannot reproduce verification (§§6,8).** Exporting 2025 deposits starting at sequence 20 fails the mandatory genesis/contiguity checks. A reported date derived from an excluded 2026 successor has no supporting token/path in the export. **Minimal fix:** explicitly verify exports as subsets and include every chain/token dependency used by their reports, or recompute dates solely from included evidence.

4. **P1 — Inherited dates lose qualification provenance (§6.9).** A successor's free timestamp on November 10 can date a predecessor whose own qualified timestamp arrives November 18. Pairing the minimum date with the predecessor's qualified badge falsely grants the earlier date qualified status. **Minimal fix:** return the proving token, chain path and qualification with each derived date; show qualified and nonqualified bounds separately when different.
