# ADR-0001: One proof mechanism — a qualified RFC 3161 timestamp per Ablage

**Status:** accepted 2026-11-18. Revised twice:
- after owner feedback ("most legal, simplest, YAGNI");
- after review round 2 (free fallback removed).

## Context
Legal weight of the options considered (research `03-anchoring.md`):

| Mechanism | Legal status |
|---|---|
| **Qualified eIDAS timestamp** | **Statutory presumption** that date/time and integrity are correct (Art. 41(2)), recognised EU-wide |
| Non-qualified timestamp | No presumption; the authority evaluates it freely (§167 Abs 2 BAO). Art. 41(1) only forbids denying it effect solely for being electronic |
| S3 Object Lock / OpenTimestamps | Free evaluation of evidence, no defined status |

Austrian law requires none of these. Only a qualified timestamp carries a presumption. Qualified stamps are cheap (Mensatek: €10 credit that never expires, ≈ €0.04 per stamp; see `08-qtsp-preise.md`).

## Decision
- **Exactly one mechanism:** a **qualified** RFC 3161 timestamp over the Prüfwert of each Ablage, from a provider in the built-in list. That list only contains services that are QTST "granted" in the EU Trusted List.
- **No free fallback.** A free US TSA would send the IP address and account name outside the EU (GDPR Art. 44) and carries no legal presumption. Without a configured provider, Ablagen stay **Ausstehend · kein Dienst eingerichtet** and are stamped once one is set up.
- **A single provider is enough.** If it ceases to exist, existing tokens remain verifiable (ADR-0004). New Ablagen go to another provider, chosen in settings.
- **Not in v1:**
  - OpenTimestamps;
  - S3;
  - a second TSA / renewal;
  - ID Austria approval (`followup.md`).

## Consequences
- One code path, one file set per Ablage, no cloud account.
- The provider receives the Prüfwert, the login (account name and password, or client certificate) and the IP address. Nothing about the invoices.
- Online tests use free TSAs (DigiCert, FreeTSA) in tests only, never in the product.
