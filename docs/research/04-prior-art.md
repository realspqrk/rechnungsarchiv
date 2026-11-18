# Prior art: seal and verify tools (research 2026-11-18)

Source: research subagent, web + GitHub. Unverified items are marked.

## Closest projects

| Project | What it does | Lesson for us |
|---|---|---|
| [GoBDify](https://github.com/Thomas-Mielke-Software/GoBDify) (C#, AGPL-3.0) | Writes `timestampNNNNN.sha256` per step (checkable with `sha256sum -c`) plus an RFC 3161 `.tst` per step. The previous `.sha256` is hashed into the next one, so the steps form a chain. "Paranoia mode" stamps with 3 TSAs (Certum, FreeTSA, DigiCert, Sectigo, GlobalSign, Apple, SSL.com). | It is the closest design. It proves an auditor can verify with stock tools (`sha256sum`, `openssl ts`). It has no package format (no ZIP or bag). |
| [Speicherstadt/belegt](https://github.com/Speicherstadt/belegt) (AGPL, beta) | Self-hosted GoBD archive server: PostgreSQL hash chain, optional S3 Object Lock, daily RFC 3161 stamp, Z3 auditor export, two-person deletion rule, ZUGFeRD/XRechnung validation. | A server is too heavy for our target. The two-person deletion rule and the auditor export are worth noting. |
| Zugpferd-GoBD-Archiv, open-invoice-germany, firsttris/haben#79 | Smaller hash-chain and seal variants. | Nothing new. |

We found **no Austrian, BAO-specific open-source tool**, and that is our niche.

## Standards to copy

- **BagIt (RFC 8493)**
  - Payload lives in `data/`. `manifest-sha256.txt` lines read `<hex>  <path>`, so `sha256sum -c` works on them too.
  - `tagmanifest-sha256.txt` covers the tag files, so one root hash covers everything.
  - `bag-info.txt` carries `Payload-Oxum` and `Bagging-Date`.
  - "Complete" (all files present) and "valid" (all hashes match) are separate states.
  - §6.1.1 covers NFC/NFD and case collisions in file names.
- **OCFL 1.1**: the digest file is written last, which makes it the atomic commit point. Use this if we ever need versioned archives.
- **hashdeep audit mode**: reports files as matched, missing, new or moved. Our `verify` uses the same categories.
- **OpenTimestamps**: a proof stays "pending" for about 1 to 12 hours until a Bitcoin block confirms it. Stamp via at least 2 calendars, then run `upgrade`.
- **RFC 4998 Evidence Records**
  - Bouncy Castle `tsp.ers` implements them, with renewal and hash-algorithm upgrade.
  - BSI [ERVerifyTool](https://github.com/de-bund-bsi-tr-esor/ERVerifyTool) (Apache-2.0) is the interoperability target if we later export ERS.
- **Sigstore Rekor v2** no longer issues a signed entry timestamp; it relies on an RFC 3161 TSA instead. The RFC 3161 token is therefore the primitive that matters.
- **immudb advisory GHSA-672p-m5jq-mrh8**: a malicious server could get the client to accept a forged proof. The verifier is the security-critical part, so keep it small and test it heavily.
- **RFC 8785 JCS**: canonical JSON. Test against Appendix B.

## Commercial products

- **DocuWare, MailStore**: keep an internal hash per document and check it on request. Nobody outside can verify it independently.
- **OriginStamp, Woleet, Guardtime KSI**: anchor a Merkle root of many hashes, either in Bitcoin or in a proprietary hash calendar.
- **Only a qualified eIDAS timestamp (Art. 41(2)) carries a legal presumption** that its date and time are accurate and that the data is intact. Austrian qualified TSAs: **A-Trust** and **GLOBALTRUST**, both supervised by RTR.
- OriginStamp warns that **re-rendering a PDF or re-encoding an XML file breaks every hash**, so originals must never be converted.

## Package-format lessons

- Use the ZIP profile of **ISO/IEC 21320-1**:
  - store or deflate only, no encryption, no multi-volume;
  - store files uncompressed (PDFs are already compressed, and stored entries are easier to recover);
  - set a fixed entry time, a sorted entry order, fixed attributes and no extra fields;
  - set the UTF-8 flag on every entry, and use zip64 only when needed.
- **Do not make the proof depend on byte-identical ZIP output.** Anchor the tagmanifest root and record the ZIP's SHA-256 as a secondary value.
- **The manifest goes inside the ZIP. The evidence (`.tsr`, `.ots`, chain entry) goes next to it as a sidecar**, because it is created after sealing.
- An unpacked BagIt folder is the most future-proof form, and a ZIP is the most convenient for a Windows auditor. Ship the ZIP, and let `verify` accept an extracted folder as well.
- **Never convert to PDF/A.** Any rendition is a separate, hashed and labelled derived file.

## Top lessons (applied in ADRs)

1. Each package is a valid BagIt bag.
2. Anchor one root per seal, never per-invoice hashes. Add a random `Seal-Nonce` so a published hash reveals nothing.
3. Use several independent anchors: a qualified TSA, a free TSA and OpenTimestamps.
4. Store the complete evidence offline: the TSR, the TSA certificate chain and the revocation data.
5. Hash-chain the seals, stamp the chain head, and renew in RFC 4998 style. Prefix every algorithm name (`sha256:`).
6. Treat paths strictly: NFC, `/`, and refuse names that collide by case or normalization. Real mtimes go into hashed metadata, not into ZIP headers.
7. Write the ZIP deterministically and test that it is reproducible.
8. `verify` is usable by an auditor, and also possible without our tool (`VERIFY.txt` with stock commands).

## Gaps (unverified)

- Internals of mailpiler, Paperless-ngx, BMD/RZL, ELO, Amagno and d.velop.
- Chainpoint appears to be defunct.
- Trillian appears to be superseded by Tessera.
