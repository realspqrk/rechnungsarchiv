# Anchoring options and costs (research 2026-11-18)

Source: research subagent, official docs and pricing pages. Open questions are listed at the end.

## Bottom line

- **Primary anchor:** a *qualified* eIDAS timestamp (RFC 3161) from an Austrian provider (GLOBALTRUST: current product page **€185/yr for 1,000 stamps**; the €20/100 tier only appears in 2022/23 articles, availability on request). It is taken over the root of a hash chain or Merkle tree and renewed in the style of RFC 4998.
- **Secondary anchor:** **OpenTimestamps** (Bitcoin). It is free, needs no account and does not depend on any vendor.
- **Free default:** two independent free timestamp authorities (DigiCert and FreeTSA) plus OpenTimestamps.
- **Object Lock (S3 WORM) is storage protection, not proof.** All such locks are tied to an account:
  - AWS deletes all content 90 days after the account is closed, Compliance mode included.
  - Backblaze deletes "locked or unlocked" data when the account is closed.
  - An auditor can only see the object if the account owner grants access, and then has to trust the provider's metadata.
  - At most it is an optional off-site copy of the *evidence files*.

## Comparison (12–50 records per year)

| Option | Legal weight (EU) | Third party can verify without our account | Longevity risk | Cost/yr |
|---|---|---|---|---|
| Qualified RFC 3161 TSA (GLOBALTRUST, A-Trust, D-Trust …) | **eIDAS Art. 41(2): legal presumption** that the time is correct and the data is intact | Yes: `openssl ts -verify` against the EU Trusted List | Medium: certificates expire, so renewal is needed | €185 (GLOBALTRUST 1,000/yr); cheaper options under research |
| Free RFC 3161 TSA (DigiCert, Sectigo, FreeTSA) | Art. 41(1) only: cannot be rejected for being electronic | Yes | Medium–high (no SLA; FreeTSA changed its certificate in 2026-03) | €0 |
| OpenTimestamps | No legal presumption; courts weigh it freely | Yes, needs no trust in any party (.ots file + Bitcoin block headers) | Low once "upgraded" | €0 |
| S3 Object Lock, Compliance mode (AWS, B2, IONOS, Scaleway …) | None (Cohasset only covers SEC 17a-4, a US rule) | No, only with account access | Data deleted when the account closes | ≈€0 (Hetzner: €78) |
| Sigstore Rekor v2 | None | Yes | High: shards are frozen, no retention promise, a signing key is required | €0 |
| Sigsum | None | Yes | Medium–high | €0 |
| Public GitHub repo | Weak | Partly | High: force-push, commit dates set by the author | €0 |

## Key facts

- **eIDAS 2.0** (Reg. 2024/1183) left Art. 41 unchanged and added two new qualified services:
  - qualified electronic archiving (Art. 45i/j, IR 2025/2532);
  - qualified electronic ledgers (Art. 45k/l, IR 2025/2531).

  A qualified ledger must be run by a qualified provider (QTSP), so Bitcoin does not count. Worth watching as a future option.
- **GLOBALTRUST** (Vienna, supervised by RTR):
  - Old articles (2022/23): €20 per year for 100 stamps, €185 per year for 1,000. **Current product page (checked 2026-11-18): 1,000 for €185/yr, 10,000 for €1,700/yr, no 100 tier** ("besondere Kontingente auf Anfrage").
  - Keeps issued timestamps for up to 35 years.
  - Endpoint `timestamp.globaltrust.eu:10318` requires a client certificate (TLS); HTTPS variants are listed.
- **GLOBALTRUST product details** (from globaltrust.eu, pages dated 2022/2023, current status unconfirmed):
  - Product: **"GLOBALTRUST QUALIFIED TIMESTAMP"**, a qualified service under RTR supervision.
  - Prices: old articles 100/yr €20; **current product page 1,000/yr €185, 10,000/yr €1,700, no 100 tier** (special quotas on request). VAT not stated. Holders of a GLOBALTRUST certificate get a free basic quota.
  - Ordering: via order.globaltrust.eu (`form=order_timestamp`).

  | Access | Endpoint | Authentication | Notes |
  |---|---|---|---|
  | TCP/TLS (RFC 3161 §3.3) | `timestamp.globaltrust.eu:10318` | client certificate issued by GLOBALTRUST | |
  | **HTTPS (for us)** | `https://timestamp.globaltrust.eu:10080` | **HTTP Basic** (credentials assigned by GLOBALTRUST) or client certificate | |
  | HTTPS | port 443 / 11080 | Basic or client certificate | token carries the TSA certificate only, no chain |
  | HTTPS | port 13080 | Basic | full chain; intended for Adobe |

  - Hash algorithms: SHA-256 and SHA-512. Not documented: key type, policy OID (see the Certificate Policy), test endpoint.
  - Issued stamps are archived for 35 years.
- **A-Trust** a.sign premium timestamp: business pricing on request only.
- **No free qualified TSA was found.**
- **OpenTimestamps:** the calendars (Alice, Bob, Finney, Catallaxy) combine submitted hashes into one Bitcoin transaction. A proof stays "pending" until it is upgraded, which usually takes hours.
- **RFC 4998 renewal:**
  - *Timestamp renewal* (when a certificate expires or a signature algorithm ages) does not need the original data.
  - *Hash-tree renewal* (when the hash algorithm weakens) needs the original data, so the ZIP files must stay accessible.
- **QLDB lesson:** Amazon QLDB went from general availability in 2019 to end of support in 2025, shorter than one 7-year retention period. A proof must therefore be **a self-contained file in an open format that can be verified offline**, with at least 2 independent trust roots.

## Recommended minimal subset (from RFC 4998 / TR-ESOR)

1. For each seal: SHA-256 plus SHA-512 as a hedge, in case one algorithm breaks.
2. One root per seal or batch, with one timestamp per anchor over that root.
3. Keep the full TSA response, the certificate chain and the OCSP/CRL data from issuance next to the archive.
4. A yearly `renew --check` that compares the TSA certificate's notAfter date against the algorithm guidance in ETSI TS 119 312 and BSI TR-02102.
5. OpenTimestamps runs in parallel. Embedding non-RFC 3161 attestations in an RFC 4998 record is not standardized.

## Open questions

- GLOBALTRUST's current status in the Trusted List, and whether its HTTPS endpoints work without a client certificate.
- A-Trust business pricing.
- Scaleway's free tier.
- What AWS does with the data on non-payment.
- Rekor v2 shard retention.

## Sources

- AWS Object Lock: <https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html>
- AWS account closure: <https://docs.aws.amazon.com/accounts/latest/reference/manage-acct-closing.html>
- Backblaze B2 non-payment procedures: <https://help.backblaze.com/hc/en-us/articles/219361957-B2-Non-payment-procedures>
- Hetzner Object Storage pricing: <https://www.hetzner.com/storage/object-storage/pricing/>
- Cloudflare R2 bucket locks: <https://developers.cloudflare.com/r2/buckets/bucket-locks/>
- eIDAS (consolidated text): <https://eur-lex.europa.eu/eli/reg/2014/910/2024-05-20/eng>
- IR 2025/2531: <https://eur-lex.europa.eu/eli/reg_impl/2025/2531/oj>
- IR 2025/2532: <https://eur-lex.europa.eu/eli/reg_impl/2025/2532/oj>
- GLOBALTRUST qualified timestamp: <https://globaltrust.eu/en/globaltrust-qualified-timestamp-qualified-time-stamp-according-to-eidas-vo-for-audit-proof-document-management/>
- A-Trust timestamp and LTV: <https://www.a-trust.at/en/products/signature_solutions_for_companies/timestamp_and_ltv>
- FreeTSA: <https://freetsa.org/>
- OpenTimestamps: <https://opentimestamps.org/>
- RFC 4998: <https://www.rfc-editor.org/rfc/rfc4998.html>
- BSI TR-03125 (TR-ESOR): <https://www.bsi.bund.de/EN/Themen/Unternehmen-und-Organisationen/Standards-und-Zertifizierung/Technische-Richtlinien/TR-nach-Thema-sortiert/tr03125/BSITR03125.html>
- Rekor sharding: <https://docs.sigstore.dev/rekor/sharding/>
- QLDB end of support: <https://www.infoq.com/news/2024/07/aws-kill-qldb>
- Marseille judgment (2025): <https://goodwinlaw.com/en/insights/publications/2025/04/insights-otherindustries-france-takes-pioneering-step>
