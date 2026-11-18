# followup.md — out-of-scope tasks

## Contents
1. [Owner tasks](#owner-tasks)
2. [Deferred features (cut from v1, revisit on demand)](#deferred-features-cut-from-v1-revisit-on-demand)

## Owner tasks
- **Qualified TSA real-world proof:** Mensatek credit bought 2026-11-18 (reverse charge confirmed, Basic auth works with the "Sellado de Tiempo" credentials). Pending: credit booking → `rechnungsarchiv stamp --archiv tmp/echttest-archiv`, then verify --online, manual openssl check, Archivprotokoll. Record the real policy OID and accuracy in providers.json.
  - Confirm Aruba's RFC 3161 endpoint before listing it as supported; Disig endpoint comes only with its access certificate.
- **DGN as default provider** (owner): register at https://www.dgn.de/dgn-zeitstempeldienst/ (German, 5 stamps/month free, QTST granted on the DE list). Confirm Austrian customers, auth method, endpoint URL. If OK, add `dgn` to providers.json and make it the README/GUI recommendation.
- **GLOBALTRUST small quota** (owner): ask sales@globaltrust.eu for ~100 stamps/year citing their eIDAS best-price guarantee (Mensatek: 233 stamps for EUR 10). If good, make GLOBALTRUST the German-language recommendation in the README (Mensatek's customer area is Spanish only; Disig needs a client certificate, Slovak e-shop, endpoint not public).
- **Acceptance route (ADR-0007):**
  - informal BMF/KSW enquiry;
  - quotes for a KFS/DV 1 Softwarebescheinigung;
  - a pilot user for a §118 BAO Auskunftsbescheid.
- **Authenticode signing** for `rechnungsarchiv.exe` and the installer: Azure Artifact Signing or an OV certificate. SmartScreen blocks unsigned exes in Kanzlei/BMF IT. Until then, the export's Archivprotokoll PDFs let people read the result without running anything.

## Deferred features (cut from v1, revisit on demand)
- **Second TSA / RFC 3161 renewal** (a new token over the old `.tsr`):
  - the only countermeasure to a later TSU key compromise (known limitation);
  - also needed before SHA-256 or RSA weaken.
- **Deletion after retention** (ADR-0006):
  - `delete-expired` keeps `chain.json` + `tagmanifest-sha256.txt` as a tombstone, plus a timestamped Löschvermerk;
  - `verify` flags VORZEITIG GELÖSCHT.
- **S3 Object Lock completeness witness** (ADR-0001/0003): optional PUT of `<seq>-<pruefwert>.json` with retain-until. Needs live version listing in `verify`.
- **Prüfwertliste to the Steuerberater** (an external completeness checkpoint for the newest Ablage).
- **OpenTimestamps anchor.**
- **ID Austria approval:**
  - the owner signs a generated Archivprotokoll PDF; the app checks prefix, PAdES and A-Trust chain, then timestamps the signed PDF;
  - blockers: free signature is for private persons only, PAdES B-B has no trusted time, certificates expire after 5 years (`docs/research/09-id-austria.md`).
- **PDF/A-2b for the Archivprotokoll** (spec §10 SHOULD): gopdf cannot produce it; options: post-process with a PDF/A library or switch generator.
- **RFC 4998 ERS export** (Bouncy Castle / BSI ERVerifyTool interop).
- **macOS/Linux GUI builds.** The CLI builds everywhere.
- **tsa evidence robustness:** keep every evidence file per certificate (`ReadAttempts` keeps one per CertKey, so a bad stored OCSP answer blocks completion until the 24 h window ends); add `Fetcher.Evidence(cert, issuer, genTime)` and `tsa.WriteProvider` instead of the local workarounds in internal/stamp.
- **GUI performance on large archives:** every detail view, protocol and archive action re-verifies the whole archive (correct, slow for many GB). Cache the last `verify.Report` per archive generation.
