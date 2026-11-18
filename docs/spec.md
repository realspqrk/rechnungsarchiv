# Rechnungsarchiv — format and verification spec, v1 (normative)

MUST/SHOULD/MAY are used per RFC 2119. Rationale for decisions is in `adr/`. From release 1.0 on, changing this file is a format change and needs a new `format` number; before 1.0 it is revised within format 1. Revised after adversarial review rounds 1–2 (`research/reviews/`).

## 1. Archive folder

```
<Archiv>/
├── rechnungsarchiv.json          archive config (no secrets)
├── .lock                         exists only while a writer holds the archive
├── vertrauenslisten/             cached, signature-verified EU trusted lists (§7)
├── 0001_<Name>.zip               Ablage (immutable after commit)
├── 0001_<Name>.nachweis/         evidence for that Ablage (append-only)
│   ├── 1.tsq  1.provider  1.tsr  1.chain.pem  1.rev/…   attempt 1 (immutable; chain.pem append-only, §5)
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
- **Pairing:** a Nachweis belongs to an Ablage when `tsq.messageImprint == Prüfwert` of that bag. All `*.nachweis` folders in scope (archive, export, or the folder next to a single ZIP) are indexed by imprint; file names are only a hint. Stamping keeps writing into the folder already in use.
- **`n.provider`** holds the provider id used for attempt n. It is written after `n.tsq` and before sending, and is immutable.
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

A verifier MUST reject a ZIP that breaks any of these rules. It MUST bound its parsing (entry count, name length, total size; v1: payload sum at most 64 GiB per Ablage). It MUST also accept the extracted bag directory, including a single wrapper folder around it (as created by Windows "Extract all").

The v1 writer is stricter, and the v1 reader enforces it:
- general-purpose flags exactly 0x0800, so no data descriptors;
- no entry or archive comments;
- zip64 records only when a value needs them;
- every local header must match its central-directory entry, and entries must not overlap;
- entry order: `bagit.txt`, the payload, the other tag files, `tagmanifest-sha256.txt` last;
- no directory entries are written (empty ones are tolerated when reading).

## 3. Bag (BagIt 1.0, RFC 8493 profile)

Tag files are UTF-8 without BOM, with LF line endings and no line folding.

Value formats:
- dates are ISO `YYYY-MM-DD` (`Aufbewahrung-Bis: 2033-12-31`);
- `files.json` mtimes are UTC RFC 3339 to the second (`…Z`);
- the sequence in `External-Identifier` is not zero-padded;
- tag files whose hash matches but which cannot be parsed, are not canonical JCS, or are internally inconsistent (e.g. Aufbewahrung-Bis ≠ Belegjahr + N, External-Identifier ≠ chain.json) are **Ungültig**. All of the following are **mandatory**, and no other tag files are allowed:

| File | Content |
|---|---|
| `bagit.txt` | `BagIt-Version: 1.0` / `Tag-File-Character-Encoding: UTF-8` |
| `bag-info.txt` | Labels in this order: `Source-Organization`, `Bagging-Date` (local date, an assertion), `Payload-Oxum`, `External-Identifier` (`<archive_id>/<sequence>`), `Belegjahr`, `Aufbewahrung-Jahre`, `Aufbewahrung-Bis`, `Archiviert-Von` (`user@host`), `Rechnungsarchiv-Version`, `Seal-Nonce` (32 hex = 128 random bits) |
| `manifest-sha256.txt` | `<hex>  data/<path>`, one line per payload file, sorted by UTF-8 bytes; `%`, CR and LF percent-encoded as BagIt requires |
| `chain.json` | JCS: `{"archive_id":"…","format":1,"previous":"sha256:<hex>"\|null,"sequence":N}` |
| `files.json` | JCS array sorted by path: `{"mtime":"<RFC 3339>","path":"data/…","sha256":"<hex>","size":N}` |
| `skipped.txt` | `system\t<path>` for each input file not archived (Thumbs.db, desktop.ini, .DS_Store, `~$*`). Paths are relative to the input folder and percent-encoded like the manifest. May be empty. |
| `VERIFY.txt` | Fixed text (DE/EN) describing manual verification. The reader requires these exact bytes. The text is frozen from release 1.0 on; any later change needs a new `format` number (pre-release test bags with an older text are Ungültig by design). |
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
   - `n.tsq` and `n.provider` before sending;
   - `n.tsr`, then `n.chain.pem` (the validated path, including AIA-fetched certificates) as soon as it holds the issuer of every certificate that needs revocation evidence;
   - then `n.rev/*`, piece by piece.

   Existing files are never overwritten. The only exception is the append-only extension of `n.chain.pem` (§5).
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
  - `reqPolicy` = the provider's `request_policy`, only if set.
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
5. The imprint and the nonce match; the token policy is one of the provider's `expected_policies` (if any are listed).
6. The algorithms are on the allowlist:
   - hashes: SHA-256, SHA-384, SHA-512;
   - signatures: RSA ≥ 2048 (PKCS#1 v1.5 or PSS), ECDSA P-256 or P-384.
7. **Path validation (RFC 5280) at genTime** must succeed from the signer to a certificate named as `ServiceDigitalIdentity` of **any** QTST service in the verified trusted lists (§7). Providers may route to another QTSP (e.g. Mensatek → FNMT), so the anchor set is not limited to the configured provider; qualification is decided separately in §6 step 6. This includes validity periods, basicConstraints, keyUsage and name constraints. The named certificate is either:
   - a CA certificate in the signer's path; or
   - the signer (TSU) certificate itself. No CA basicConstraints are required of the TSU in that case, but EKU, validity and revocation are still checked.

**Validation material.** Fetch it right after acceptance and store it as `n.chain.pem` (all chain certificates) and `n.rev/` (one file per certificate). It is needed for the signer and for every intermediate CA in the path.
- **`n.chain.pem`** is a sequence of PEM `CERTIFICATE` blocks: the validated path from the signer, plus every issuer needed to check the revocation evidence.
  - If the path ends at the signer (the trusted list names the TSU certificate itself, §5 item 7), the file MUST also hold the signer's issuer. Take it from the token's certificates, else fetch it via the signer's AIA caIssuers URL. A certificate counts as the issuer only if its subject equals the signer's issuer name and its key verifies the signer's signature.
  - Writers SHOULD create the file only once it holds every issuer needed. Until then it would carry nothing the token does not.
  - An existing `n.chain.pem` MAY be extended, append-only. The new content MUST begin with exactly the stored bytes of the file and only append PEM `CERTIFICATE` blocks; a certificate-wise prefix match is not enough, since PEM readers may skip undecodable blocks. It replaces the old file atomically: temp file, fsync, rename over the old file. Any other change MUST be refused: a removed, reordered or altered certificate, or a stored file that does not parse. This lets an attempt whose chain was stored incomplete (an older version, or a failed AIA fetch) be completed within the 24 h window below, without a new timestamp.
  - Verifiers use the certificates in `n.chain.pem` only as candidates. The path is always built to a trusted-list anchor (§5 item 7). An issuer counts only if its key verifies the certificate's signature, and certificates that are not needed are ignored.
- Use OCSP for each certificate where an AIA OCSP URL exists. Otherwise use the complete CRL from its CDP. Delta CRLs and partitioned CRLs whose IDP does not cover the certificate do not count.
- **Accept the evidence only if:**
  - it is signed by the issuing CA, or by an OCSP responder that CA authorized (`id-kp-OCSPSigning`);
  - the certID or issuer matches;
  - **it covers genTime**: either its validity interval contains genTime (`thisUpdate ≤ genTime ≤ nextUpdate`, RFC 6960 semantics), or it was produced within 24 h after genTime while the certificate was still valid. Within 24 h only the missing pieces of the same attempt are fetched; no new timestamp is requested while the attempt can still be completed. If no such evidence can be obtained within 24 h, the attempt is discarded and a new timestamp is requested (new attempt, new genTime);
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
3. Payload against `manifest-sha256.txt`: report matched, changed, missing and added files (complete vs. valid). Any change, missing or added file → **Verändert**. A CRC mismatch on a payload entry also counts as changed. In an extracted directory, system junk (Thumbs.db, desktop.ini, .DS_Store, `~$*`) that the manifest does not list is reported as *ignored*, not added.
4. `Payload-Oxum`, `files.json` and the manifest must agree → **Ungültig** if not.
5. Find the Nachweis by imprint (§1) and take the earliest attempt that is valid per §5. The provider's `expected_policies` are enforced when a token is accepted at stamping time. At verification the issuing provider may be unknown (archives can switch providers), so the policy is reported but not re-checked. If no verified trusted list is available at all (no anchors), the token is **Unbestimmt**, not Ungültig. A file-name number that differs from `chain.json` is an archive-level finding, not Ungültig (file names are only hints).
   - No valid attempt → **Ausstehend** (with the §5 reason). Attempts whose token passes the cryptographic checks (§5 items 1–7) but lack timely validation material count as discarded, not invalid.
   - **Ungültig** only if no attempt is valid **and** some token fails a cryptographic check (§5 items 1–7). If a path cannot be built only because a stored issuer certificate is missing (top certificate not self-issued), the attempt is discarded (Ausstehend·prüfdaten), never green.
6. **Qualification** (§7). The token is **qualifiziert** if all of these hold:
   - the signer certificate, or a CA in its path, is a `ServiceDigitalIdentity` of a service of type `…/Svctype/TSA/QTST`;
   - that service's status history shows `granted` over an interval containing genTime;
   - this is read from a signature-verified trusted list that is either **issued after genTime** (the issue date is covered by the list's signature), or that **this verifier run itself** retrieved online and verified after genTime. Cached retrieval metadata is never evidence. `NextUpdate` alone never establishes freshness (ETSI TS 119 612 §5.3.15).

   Otherwise the token is **unbestätigt**. This is normal for an offline check of a recent stamp; it resolves once a newer list is cached or the check runs online. If the conditions hold but the status is not `granted` at genTime, the token is **nicht qualifiziert**.
7. **Key compromise.** Decided per certificate across the whole verified set (all attempts of all Ablagen, plus online evidence). If any valid revocation evidence shows the signer or a CA in its path revoked with reason `keyCompromise`, `cACompromise` or unspecified, the token is **Unbestimmt**, whatever the dates (RFC 3161 §4).
8. **Proven time.** `belegt_spätestens` is genTime + accuracy, rounded **up to the full second** for display (e.g. 09:14:22 ± 1 s → "spätestens 09:14:23 Uhr MEZ"). The raw genTime and the accuracy are shown separately in the details.
   - Accuracy comes from the token; if the token has none, from the declared accuracy of the provider recorded in `n.provider`, used only if that provider's trusted-list territory is consistent with the token's path. The configured provider never supplies facts for historical tokens.
   - Details show the raw signed genTime exactly, including its fraction (e.g. "13:00:00,9 Uhr MEZ").
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
  - `url_from_contract` (true when the endpoint is handed out with the contract, e.g. Disig; the UI then shows a URL field);
  - `request_policy` (OID sent as `reqPolicy`, usually empty) and `expected_policies` (OIDs accepted in the token; empty = not checked), kept separate because some providers forbid `reqPolicy`;
  - the country code of the trusted list that covers it.

  The file contains **only providers with a QTST service on their national list**. It holds connection data, not trust decisions. **It contains no prices.** Prices change and belong in the README only, with a date. The app links to the README and to the provider's website.
- **Trusted lists.** The app downloads the EU LOTL (`https://ec.europa.eu/tools/lotl/eu-lotl.xml`) and the national lists of the configured or used providers, and verifies their XML signatures:
  - the LOTL against the LOTL signing certificates pinned in the release (published in the EU Official Journal);
  - each national list against the certificates the LOTL names for it.

  Verified lists are cached as `vertrauenslisten/<CC>-<sequence>.xml` (exact signed bytes). If a different list reuses an existing sequence number, it is stored as `<CC>-<sequence>-2.xml` and so on; nothing is overwritten. Older versions are kept, so history is never lost.
- **Bootstrap.** A release ships a verified snapshot.
- **Refresh.** Lists are refreshed on start, right after every successful stamp, before export and during online verification, whenever the network is available. In the app's normal mode this makes fresh stamps show as qualifiziert at once, because the verifier itself fetched the list online. An offline auditor sees unbestätigt for stamps newer than the newest cached list's issue date.
- **Banner.** If the newest cached list is past its `NextUpdate` and a refresh fails, the main screen shows "Vertrauensliste veraltet – Internetverbindung prüfen oder Programm aktualisieren".

## 8. Export for an auditor

`export --von <Belegjahr> --bis <Belegjahr> <Ziel>` refreshes the trusted lists, if possible, and writes:
- the selected Ablagen and their Nachweise, unchanged;
- the **Brückendaten** for every excluded Ablage between the first and last selected one, under `brueckendaten/<NNNN>/`: only `chain.json` and `tagmanifest-sha256.txt` (no file names, no payload);
- the cached `vertrauenslisten/`;
- `rechnungsarchiv.exe`;
- `Archivprotokolle/<NNNN>_<Name>.archivprotokoll.pdf`: one per exported Ablage (§10);
- `DATEIVERZEICHNIS.csv`: one row per file with Ablage, path, size, sha256, Belegjahr and belegt_spätestens;
- `PRUEFANLEITUNG.txt`: how to verify with the exe, and how to verify manually.

The report is computed from the evidence included in the export. When the trusted lists could be refreshed just before (network available), the check also runs **online** (§6 step 7): the lists and revocation status fetched during it count, so a stamp newer than the newest published list is qualifiziert instead of unbestätigt; the Archivprotokolle state "online". Without network the check is offline. An offline re-check of the export can therefore still say unbestätigt until the provider's country publishes a newer list. The export never writes into the source archive.

## 9. Read-only mode

`verify` and the GUI's "Archiv nur kontrollieren" never write, lock or stamp. They may refresh trusted lists into a temporary cache outside the archive.

## 10. Archivprotokoll (PDF, one per Ablage)

One generator, `internal/report`, renders `verify.Report` for **one Ablage** as `<NNNN>_<Name>.archivprotokoll.pdf`. It is produced on demand ("Archivprotokoll speichern") and for every Ablage in an export. An Ablage can cover any period (a week, a month, a quarter …), as the user chooses.

The document is an **objective statement of fact**. It contains no disclaimers, no explanations, no instructions (no commands, no reference to manuals), no promotional wording and no assessments beyond the verification result. Facts stand in tables, not in sentences.

**Contents, in order:**
1. **Header:** "Archivprotokoll"; company; "Ablage NNNN „Name“ · Belegjahr JJJJ".
2. **Feststellung** (statement): a label/value table. Example (Zeitgestempelt):

   | | |
   |---|---|
   | Ergebnis | Zeitgestempelt · qualifiziert |
   | Inhalt | 64 Dateien, 31,4 MB |
   | Prüfwert | sha256:<64 hex> |
   | Zeitstempel | Mensatek (Spanien), 05.11.2026, 09:14:22 Uhr MEZ (± 1 s) |
   | Qualifikation | qualifizierter Zeitstempeldienst nach eIDAS zum Zeitpunkt des Zeitstempels (laut EU-Vertrauensliste) |
   | Kontrolle | 18.11.2026: 64 von 64 Dateien stimmen mit dem Dateiverzeichnis überein |
   | Belegt | Dateistand bestand spätestens am 05.11.2026, 09:14:23 Uhr MEZ (Zeitstempel) |

   The rows follow the actual status (Ausstehend, Verändert, …) exactly: an inherited proof adds "Verkettung" and the successor's timestamp; Verändert names the counts of changed, missing and added files; without a proven time "Belegt" says "kein Zeitpunkt belegt". It never claims more than §6 established.
3. **Angaben** (details), a label/value table:
   - archive ID and the Ablage file;
   - predecessor (number + **full** Prüfwert), stated neutrally (no "lückenlos");
   - archived on / by (local assertion, labelled as such);
   - chosen retention period and its ordinary end ("gewählte Frist 7 Jahre, gewöhnliches Ende 31.12.2033");
   - the check (time, online or offline);
   - sub-table **Zeitstempel**: provider, genTime as signed, accuracy and its source, serial number, policy OID, signing certificate, its serial number and issuer, revocation evidence stated factually and positively ("gültig · OCSP-Auskunft vom …"), qualification, trusted-list version used.
   - All times with time zone (local time + UTC offset).
4. **Dateiverzeichnis** (file list), **complete**, one table row per file: running number, size in bytes, original modification date, **full SHA-256**, path (last, so a long path wraps without moving the other columns). Sorted by path. The column header repeats on every page.
5. **Footer:** "Archivprotokoll Ablage NNNN · erstellt am … mit Rechnungsarchiv <version>", and "Seite x von y" on the right.

**Format:**
- A4 landscape (the file table needs the width for one row per file), embedded Unicode fonts, selectable text.
- Text extraction (`pdftotext -layout`) reads each table row on one line in order: labels are right-aligned next to their values.
- SHOULD be PDF/A-2b (v1: not achieved; gopdf cannot produce PDF/A, see followup).
- Past tense wherever a value describes the state at genTime.
- The PDF is derived from the verification result and is not itself evidence; `verify` remains authoritative. How to check an Ablage independently is described in `PRUEFANLEITUNG.txt` (export) and `VERIFY.txt` (Ablage), not in the PDF.

## 11. CLI

The conventions are set by ADR-0008, and the command reference is `docs/cli.md`.
- Output is JSON when `--json` is given or stdout is not a terminal.
- Errors are JSON objects on stderr.
- Exit codes are fixed.
- `archive` and `export` without a terminal require `--yes`.
