# ADR-0004: Verifying timestamps and qualification

**Status:** accepted 2026-11-18. Revised after review rounds 1–2. Normative: [`spec.md`](../spec.md) §5–§7.

## Context
The verifier is the security boundary. Several failure modes shaped this decision:
- **CVE-2026-39984:** the wrong certificate in the bag was verified.
- **Qualification is not "chain plus OID":** one CA may issue both qualified and non-qualified services.
- **Revocation is not strictly monotonic.** `certificateHold` can be lifted, and expired certificates drop off CRLs.
- **A compromised TSU key allows backdating** (RFC 3161 §4).

## Decision
- **Token acceptance** (spec §5). All of these checks must pass:
  - CMS signature and `messageDigest`;
  - ESSCertID(v2) signer binding;
  - a critical, sole timeStamping EKU;
  - imprint, nonce and expected policy;
  - the algorithm allowlist;
  - **RFC 5280 path validation at genTime** up to a certificate named by the provider's trusted-list service. This may be a CA in the path or the TSU certificate itself.
- **Validation material.** At stamping time, store the chain plus OCSP or a full CRL for the signer and **every intermediate CA**. Each piece of evidence must be:
  - properly signed;
  - **covering genTime** (validity interval contains genTime, or produced within 24 h after it), else the attempt is repeated. A hold lifted before the evidence was produced is undetectable afterwards; this is a documented residual risk, as in ETSI EN 319 102-1;
  - `good`, where hold counts as not good.

  The Ablage stays **Ausstehend** until all of it is stored. With it, verification works offline even after the provider has ceased to exist.
- **Qualification comes from authenticated sources.** The app fetches the EU LOTL and the national trusted lists and verifies their XML signatures:
  - the LOTL against signing certificates pinned in the release;
  - each national list against the certificates the LOTL names for it.

  Every version is kept in `vertrauenslisten/` inside the archive, and is included in exports. A token counts as **qualifiziert** only if its path anchors at a QTST service identity that was `granted` at genTime, according to a verified list **issued** after genTime (the issue date is signed), or one fetched online by the verifier itself during this run. Cached retrieval times and `NextUpdate` never prove freshness. Otherwise the result is **unbestätigt**: normal for an offline check of a recent stamp, and it resolves with a newer list or an online check.
- **Key compromise.** Revocation for `keyCompromise`, `cACompromise` or an unspecified reason → **Unbestimmt**.
  - Known limitation, documented in the README and `fuer-pruefer.md`: with only one TSA, a backdated forgery after a later key compromise cannot be ruled out. Renewal by a second provider is deferred.
- **Proven time** = genTime + accuracy, taken from the token or else from the provider's declared accuracy; if neither exists, "Genauigkeit nicht angegeben". It is inherited from a successor only when the Ablage has no token of its own, and then labelled with its source and qualification.
- **Mandatory tests:**
  - certificate injection;
  - manipulated signed attributes;
  - forged signature;
  - replay / nonce;
  - wrong imprint or policy;
  - foreign, stale or hold OCSP;
  - expired-certificate CRL;
  - non-exclusive EKU;
  - keyCompromise;
  - trusted list too old or with an invalid signature;
  - a token from before the QTST grant date;
  - malformed input (fuzzing).

## Update 2026-11-18: export check online

The export's own check (spec §8) runs online when the trusted lists could be refreshed. Reason: national lists appear every few weeks, so an offline check of a fresh stamp says unbestätigt, and the Archivprotokoll an auditor reads first would say so for every recent Ablage. Online, the list fetched during the check decides, as above, and the protocol states "online". An offline re-check of the same export may still say unbestätigt until a newer list exists; PDF, CLI and GUI then name the reason (`present.UnconfirmedReason`).
