# Rechnungsarchiv — format and verification spec, v1 (normative)

MUST/SHOULD/MAY are used per RFC 2119. Rationale for decisions is in `adr/`. Changing this file is a format change and needs a new `format` number. Revised after adversarial review rounds 1–2 (`research/reviews/`).

## 1. Archive folder

```
<Archiv>/
├── rechnungsarchiv.json          archive config (no secrets)
├── .lock                         exists only while a writer holds the archive
├── vertrauenslisten/             cached, signature-verified EU trusted lists (§7)
├── 0001_<Name>.zip               Ablage (immutable after commit)
├── 0001_<Name>.nachweis/         evidence for that Ablage (append-only)
│   ├── 1.tsq  1.tsr  1.chain.pem  1.rev/…           attempt 1 (immutable files)
│   └── 2.tsq  …                                      further attempts, if any
└── …
```

- **`rechnungsarchiv.json`** (JCS, RFC 8785):

  ```
  {"archive_id":"<uuid-v4>","company":"…","format":1,"retention_default":7,"tsa":"<provider-id>"|null}
  ```

  `tsa` is an id from the built-in provider list (§7). `null` means no provider is set up yet.
- **Credentials** are kept per PC in the OS credential store (Windows Credential Manager), never in the archive:
  - HTTP Basic: user and password; or
  - a client certificate: the path to a PKCS#12 file, with its password in the credential store.
- **Pairing:** a Nachweis belongs to an Ablage when `tsq.messageImprint == Prüfwert` of that bag. File names are only a hint.
- **One writer.**
  - `.lock` holds `{host, user, pid, since}` as JSON.
  - A lock left by a dead pid on the same host MAY be taken over silently.
  - A lock from another host MAY be taken over only after the user confirms.
  - Synced or cloud folders are not supported as a write target. Files with cloud-placeholder attributes (`FILE_ATTRIBUTE_OFFLINE`, `RECALL_ON_*`) MUST be refused as input.

## 2. Ablage ZIP

The ZIP has exactly one top-level directory, `<NNNN>_<Name>/`, and that directory is the bag.

| Rule | Value |
|---|---|
| Method | Stored (0) only |
| Name encoding | UTF-8, flag bit 11 on every entry |
| Entry time | fixed 1980-01-01 00:00; real mtimes are in `files.json` |
| Extra fields | none, except zip64 (0x0001) when needed |
| Forbidden | encryption; multi-disk; duplicate names (after NFC + Unicode case fold); absolute paths; `..`; backslashes; entries outside the bag dir; symlinks |

A verifier MUST reject a ZIP that breaks any of these rules. It MUST bound its parsing (entry count, name length, total size). It MUST also accept the extracted bag directory.

## 3. Bag (BagIt 1.0, RFC 8493 profile)

Tag files are UTF-8 without BOM, with LF line endings and no line folding. All of the following are **mandatory**, and no other tag files are allowed:

| File | Content |
|---|---|
| `bagit.txt` | `BagIt-Version: 1.0` / `Tag-File-Character-Encoding: UTF-8` |
| `bag-info.txt` | Labels in this order: `Source-Organization`, `Bagging-Date` (local date, an assertion), `Payload-Oxum`, `External-Identifier` (`<archive_id>/<sequence>`), `Belegjahr`, `Aufbewahrung-Jahre`, `Aufbewahrung-Bis`, `Archiviert-Von` (`user@host`), `Rechnungsarchiv-Version`, `Seal-Nonce` (32 hex = 128 random bits) |
| `manifest-sha256.txt` | `<hex>  data/<path>`, one line per payload file, sorted by UTF-8 bytes; `%`, CR and LF percent-encoded as BagIt requires |
| `chain.json` | JCS: `{"archive_id":"…","format":1,"previous":"sha256:<hex>"\|null,"sequence":N}` |
| `files.json` | JCS array sorted by path: `{"mtime":"<RFC 3339>","path":"data/…","sha256":"<hex>","size":N}` |
| `skipped.txt` | `system\t<path>` for each input file not archived (Thumbs.db, desktop.ini, .DS_Store, `~$*`). May be empty. |
| `VERIFY.txt` | Fixed text (DE/EN) describing manual verification |
| `tagmanifest-sha256.txt` | `<hex>  <tagfile>` for exactly the 7 tag files above, in this fixed order: bagit.txt, bag-info.txt, chain.json, files.json, manifest-sha256.txt, skipped.txt, VERIFY.txt. It never lists itself. |

- **Prüfwert** = SHA-256 over the exact bytes of `tagmanifest-sha256.txt`, written as `sha256:<64 hex>`.
- **Payload rules:**
  - names are NFC-normalized;
  - names that collide after NFC + case fold are rejected;
  - names invalid on Windows are rejected (reserved names, trailing dot or space, forbidden characters);
  - symlinks, reparse points and placeholders are rejected;
  - empty directories are ignored.
- **Hashing:** files are hashed from the bytes **as they are written** into the ZIP, in a single read. Size and mtime are compared before and after, and a mismatch aborts.
- **No deduplication.** Every selected file is archived. Before archiving, the UI MAY tell the user that identical files already exist in earlier Ablagen, but this has no effect on the format.

## 4. Commit order (crash safety)

1. Acquire `.lock`.
2. Write `<NNNN>_<Name>.zip.tmp`, fsync it, close it.
3. Re-open it and verify it fully (§6 steps 1–4).
4. Rename it to `.zip` with no-replace semantics and set the read-only attribute. *This is the commit.*
5. Create `.nachweis/`. Each attempt writes new files with no-replace semantics:
   - `n.tsq` before sending;
   - then `n.tsr`, `n.chain.pem` and `n.rev/*`.

   Existing files are never overwritten.
6. Release `.lock`.

**Recovery on start:**
- Delete leftover `*.tmp` files.
- An Ablage without a complete, valid attempt is **Ausstehend**.
- `stamp` re-verifies the bag before every request.

## 5. Timestamp request and evidence

**Request.**
- Build a `TimeStampReq` v1 with:
  - `messageImprint` = SHA-256 / Prüfwert;
  - `certReq = TRUE`;
  - a fresh 64-bit random `nonce`;
  - `reqPolicy` set to the provider's expected policy OID if the provider list names one.
- Send it as an HTTPS POST of `application/timestamp-query`, with HTTP Basic or a TLS client certificate as configured.
- Do not follow redirects.
- **There is no fallback to any other provider.**

**Accept** the response only if **all** of the following hold:
1. `PKIStatus` is granted (0 or 1), and the token is a CMS SignedData with eContentType id-ct-TSTInfo.
2. **Cryptographic verification:**
   - the signed attribute `messageDigest` equals the hash of the encapsulated TSTInfo;
   - the CMS signature over the signed attributes verifies with the signer certificate's public key.
3. The signer certificate is the one identified by ESSCertID or ESSCertIDv2, using the hash algorithm the token states. It is not merely any certificate in the bag.
4. The signer certificate carries a **critical, sole** `id-kp-timeStamping` EKU.
5. The imprint, the nonce and the policy match (expected OID, if one is listed).
6. The algorithms are on the allowlist:
   - hashes: SHA-256, SHA-384, SHA-512;
   - signatures: RSA ≥ 2048 (PKCS#1 v1.5 or PSS), ECDSA P-256 or P-384.
7. **Path validation (RFC 5280) at genTime** must succeed from the signer to a certificate that the provider's trusted-list service identity names (§7). This includes validity periods, basicConstraints, keyUsage and name constraints. The named certificate is either:
   - a CA certificate in the signer's path; or
   - the signer (TSU) certificate itself. No CA basicConstraints are required of the TSU in that case, but EKU, validity and revocation are still checked.

**Validation material.** Fetch it right after acceptance and store it as `n.chain.pem` (all chain certificates) and `n.rev/` (one file per certificate). It is needed for the signer and for every intermediate CA in the path.
- Use OCSP for each certificate where an AIA OCSP URL exists. Otherwise use the complete CRL from its CDP. Delta CRLs and partitioned CRLs whose IDP does not cover the certificate do not count.
- **Accept the evidence only if:**
  - it is signed by the issuing CA, or by an OCSP responder that CA authorized (`id-kp-OCSPSigning`);
  - the certID or issuer matches;
  - **it covers genTime**: either its validity interval contains genTime (`thisUpdate ≤ genTime ≤ nextUpdate`, RFC 6960 semantics), or it was produced within 24 h after genTime while the certificate was still valid. If no such evidence can be obtained within 24 h, the attempt is discarded and a new timestamp is requested (new attempt, new genTime);
  - *Residual risk (documented):* a `certificateHold` set before genTime and lifted before the evidence was produced cannot be detected by any after-the-fact evidence (RFC 5280 `removeFromCRL`). This matches the limits of ETSI EN 319 102-1 validation.
  - the status is `good`, or the certificate is not listed.
- `certificateHold` or any revoked status is not good.

An Ablage is **Zeitgestempelt** only when the token and all validation material are stored and valid. Until then it is **Ausstehend**, with one of these reasons:

| Reason | Meaning |
|---|---|
| `kein-dienst` | `tsa = null` |
| `offline` | no network |
| `zugang` | 401/403, or no credentials on this PC |
| `guthaben` | the provider reports its quota exhausted |
| `prüfdaten` | validation material is missing or invalid; retried |
| `anbieter` | any other error |

**Time sanity.** If |genTime − local clock at receipt| > 5 min, add a warning to the report. It only indicates a wrong PC clock.

## 6. Verification (single engine: `internal/verify`)

For each Ablage:
1. ZIP rules (§2) → **Ungültig** if broken.
2. The tag files are exactly the mandatory set (§3) and match `tagmanifest-sha256.txt` → **Verändert** if not.
3. Payload against `manifest-sha256.txt`: report matched, changed, missing and added files (complete vs. valid). Any change, missing or added file → **Verändert**.
4. `Payload-Oxum`, `files.json` and the manifest must agree → **Ungültig** if not.
5. Find the Nachweis by imprint (§1) and take the earliest attempt that is valid per §5.
   - No valid attempt → **Ausstehend** (with the §5 reason). Attempts whose token passes the cryptographic checks (§5 items 1–7) but lack timely validation material count as discarded, not invalid.
   - **Ungültig** only if no attempt is valid **and** some token fails a cryptographic check (§5 items 1–7).
6. **Qualification** (§7). The token is **qualifiziert** if all of these hold:
   - the signer certificate, or a CA in its path, is a `ServiceDigitalIdentity` of a service of type `…/Svctype/TSA/QTST`;
   - that service's status history shows `granted` over an interval containing genTime;
   - this is read from a signature-verified trusted list that is either **issued after genTime** (the issue date is covered by the list's signature), or that **this verifier run itself** retrieved online and verified after genTime. Cached retrieval metadata is never evidence. `NextUpdate` alone never establishes freshness (ETSI TS 119 612 §5.3.15).

   Otherwise the token is **unbestätigt**. This is normal for an offline check of a recent stamp; it resolves once a newer list is cached or the check runs online. If the conditions hold but the status is not `granted` at genTime, the token is **nicht qualifiziert**.
7. **Key compromise.** If any valid revocation evidence (stored, or fetched online when available) shows the signer or a CA in its path revoked with reason `keyCompromise`, `cACompromise` or unspecified, the token is **Unbestimmt**, whatever the dates (RFC 3161 §4).
8. **Proven time.** `belegt_spätestens` is genTime + accuracy, rounded up to the full minute for display.
   - Accuracy comes from the token; if the token has none, from the provider's declared accuracy in `providers.json`.
   - If neither exists, show genTime with the note "Genauigkeit nicht angegeben" and assert no exact latest time.
   - If the Ablage has no valid own token, it MAY instead inherit the earliest `belegt_spätestens` of a valid successor in the verified set, because every successor commits to it through the chain.
   - An inherited time is labelled "über Ablage N" and carries **that** token's qualification.
   - An Ablage with its own token never inherits.

For the verified set (a whole archive, or an export subset):

9. **Chain:**
   - `archive_id` is the same everywhere;
   - `previous` equals the predecessor's Prüfwert;
   - sequence numbers are contiguous.

   In a whole archive: exactly one genesis (`previous = null`, sequence 1). A gap → **Lücke** (gap); the same number twice → **Nummer doppelt vergeben** (number assigned twice).

   In an export subset (§8), the first included Ablage's predecessor is reported neutrally as "Vorgänger nicht im Export".
   - Excluded Ablagen between included ones are bridged by their **Brückendaten** (`chain.json` + `tagmanifest-sha256.txt`). `sha256(tagmanifest)` must equal the Prüfwert, and chain.json's hash must match its tagmanifest line.
   - A bridged Ablage is reported as "nicht im Export".
   - A gap without Brückendaten is a **Lücke**.

**Status precedence:** Ungültig > Verändert > Unbestimmt > Ausstehend > Zeitgestempelt.

Qualifiers:
- Zeitgestempelt: qualifiziert, nicht qualifiziert or unbestätigt.
- Ausstehend: the §5 reason.

## 7. Providers and trusted lists

- **`internal/tsa/providers.json`** (built in) lists supported providers. Each entry has:
  - id and display name;
  - country;
  - TSA URL;
  - auth type (`basic` or `clientcert`);
  - the expected policy OID, if known;
  - the country code of the trusted list that covers it.

  The file contains **only providers with a QTST service on their national list**. It holds connection data, not trust decisions. **It contains no prices.** Prices change and belong in the README only, with a date. The app links to the README and to the provider's website.
- **Trusted lists.** The app downloads the EU LOTL (`https://ec.europa.eu/tools/lotl/eu-lotl.xml`) and the national lists of the configured or used providers, and verifies their XML signatures:
  - the LOTL against the LOTL signing certificates pinned in the release (published in the EU Official Journal);
  - each national list against the certificates the LOTL names for it.

  Verified lists are cached as `vertrauenslisten/<CC>-<sequence>.xml`. Older versions are kept, so history is never lost.
- **Bootstrap.** A release ships a verified snapshot.
- **Refresh.** Lists are refreshed on start, right after every successful stamp, before export and during online verification, whenever the network is available. In the app's normal mode this makes fresh stamps show as qualifiziert at once, because the verifier itself fetched the list online. An offline auditor sees unbestätigt for stamps newer than the newest cached list's issue date.
- **Banner.** If the newest cached list is past its `NextUpdate` and a refresh fails, the main screen shows "Vertrauensliste veraltet – Internetverbindung prüfen oder Programm aktualisieren".

## 8. Export for an auditor

`export --von <Belegjahr> --bis <Belegjahr> <Ziel>` refreshes the trusted lists, if possible, and writes:
- the selected Ablagen and their Nachweise, unchanged;
- the **Brückendaten** for every excluded Ablage between the first and last selected one: only `chain.json` and `tagmanifest-sha256.txt` (no file names, no payload);
- the cached `vertrauenslisten/`;
- `rechnungsarchiv.exe`;
- `Archivprotokolle/<NNNN>_<Name>.archivprotokoll.pdf`: one per exported Ablage (§10);
- `DATEIVERZEICHNIS.csv`: one row per file with Ablage, path, size, sha256, Belegjahr and belegt_spätestens;
- `PRUEFANLEITUNG.txt`: how to verify with the exe, and how to verify manually.

The report is computed **only from evidence included in the export**. The export never writes into the source archive.

## 9. Read-only mode

`verify` and the GUI's "Archiv nur kontrollieren" never write, lock or stamp. They may refresh trusted lists into a temporary cache outside the archive.

## 10. Archivprotokoll (PDF, one per Ablage)

One generator, `internal/report`, renders `verify.Report` for **one Ablage** as `<NNNN>_<Name>.archivprotokoll.pdf`. It is produced on demand ("Archivprotokoll speichern") and for every Ablage in an export. An Ablage can cover any period (a week, a month, a quarter …), as the user chooses.

The document is an **objective statement of fact**. It contains no disclaimers, no promotional wording and no assessments beyond the verification result.

**Contents, in order:**
1. **Header:**
   - "Archivprotokoll";
   - company;
   - Ablage number and name;
   - Belegjahr.
2. **Feststellung** (statement, a short factual paragraph). Example:

   > Ablage 0011 „2026-10 Eingangsrechnungen" enthält 64 Dateien (31,4 MB). Ihr Prüfwert ist sha256:c41e…a1b8. Für diesen Prüfwert besteht ein Zeitstempel von Mensatek (Spanien) vom 05.11.2026, 09:14:22 Uhr (± 1 s); der Dienst ist laut EU-Vertrauensliste zu diesem Zeitpunkt ein qualifizierter Zeitstempeldienst nach eIDAS. Bei der Kontrolle am 18.11.2026 stimmten alle 64 Dateien mit dem spätestens zu diesem Zeitpunkt belegten Stand überein.

   The wording follows the actual status (Ausstehend, Verändert, …) exactly. It never claims more than §6 established.
3. **Angaben** (details):
   - archive ID;
   - sequence number and predecessor (number + Prüfwert);
   - archived on / by (local assertion, labelled as such);
   - retention period up to;
   - timestamp details: provider, serial number, policy OID, accuracy, signing certificate, issuer;
   - trusted-list version used.
4. **Dateiverzeichnis** (file list), **complete**: running number, path, size, original modification date, **full SHA-256**. Sorted by path. Continues over as many pages as needed.
5. **Unabhängige Kontrolle** (independent verification), described factually:
   - with `rechnungsarchiv verify`;
   - manually: the bag's `sha256sum -c` lines plus `openssl ts -verify -digest <Prüfwert> -in 1.tsr`.
6. **Footer:** "Archivprotokoll Ablage NNNN · erstellt am … mit Rechnungsarchiv <version> · Seite x von y".

**Format:**
- A4, embedded Unicode fonts, selectable text.
- SHOULD be PDF/A-2b.
- The PDF is derived from the verification result and is not itself evidence; `verify` remains authoritative.

## 11. CLI

The conventions are set by ADR-0008, and the command reference is `docs/cli.md`.
- Output is JSON when `--json` is given or stdout is not a terminal.
- Errors are JSON objects on stderr.
- Exit codes are fixed.
- `archive` and `export` without a terminal require `--yes`.
