VERDICT changes: 2

| Round-3 finding | Status | Verification / minimal fix |
|---|---|---|
| Historical revocation | Not fixed | The 24-hour window does not guarantee that a hold remains visible. Hold at 10:00 (genTime), release at 10:05, good OCSP at 10:10 satisfies §5. Require evidence establishing status at genTime, or an applicable policy excluding certificateHold; otherwise Unbestimmt. Remove the assertion that 24 hours guarantees visibility. [RFC 5280](https://www.rfc-editor.org/rfc/rfc5280.html#section-5.3.1). |
| Missing accuracy | Fixed | §6.8 uses declared provider accuracy when absent from the token; without either, no exact latest-time assertion is made. |
| Unconditional UI footer | Fixed | Pruefen now gates the proven-state footer on isOk, excluding Unbestimmt and changed results. |
| Filtered export gaps | Fixed | Authenticated chain/tagmanifest bridges connect excluded deposits; reports distinguish them from missing bridges and use included evidence only. |
| CA-only trust anchoring | Fixed | §§5.7/6.6 explicitly permit the TSU itself as ServiceDigitalIdentity while retaining its validity, EKU and revocation checks. |

New defect — **P1: NextUpdate is not a guarantee that cached status remains unchanged (§6.6, ADR-0004).** Example: cached list issued November 1, NextUpdate December 1; withdrawal published November 10; token issued November 11. If refresh fails or the newer list is withheld, the newest cached list still passes NextUpdate ≥ genTime and falsely grants qualification. The obligation to publish changes does not establish that this client received them. ETSI defines NextUpdate as the latest publication deadline and explicitly permits/requires earlier updates. [ETSI TS 119 612 §5.3.15](https://www.etsi.org/deliver/etsi_TS/119600_119699/119612/02.04.01_60/ts_119612v020401p.pdf).

**Minimal fix:** establish historical status from a post-genTime authenticated list, or a successful authenticated retrieval of the current list after genTime. For offline verification without sufficient historical evidence, show unbestätigt; NextUpdate alone cannot establish freshness.
