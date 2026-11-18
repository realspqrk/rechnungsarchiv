# followup.md — out-of-scope tasks

## Contents
1. [Owner tasks](#owner-tasks)
2. [Deferred features (cut from v1, revisit on demand)](#deferred-features-cut-from-v1-revisit-on-demand)

## Owner tasks
- **Qualified TSA real-world proof: done 2026-11-18.** tmp/echttest-archiv: Ablage 0001 (Mensatek, 15:54:59 UTC, after the TSU-issuer fix) and 0002 (17:09:00 UTC, first attempt, file names of 192 characters and a 298-character path), 0002 links to the Prüfwert of 0001, `verify --online` qualified, openssl `Verification: OK` with `-partial_chain`. Two credits used. Still to do: record the real policy OID (0.4.0.2023.1.1) and accuracy (1 s) in providers.json.
- **Export check online (decided 2026-11-18):** with network the export's own check runs online, so its Archivprotokolle say "qualifiziert" for fresh stamps; an offline re-check of the export says "unbestätigt" until the provider's country publishes a newer list (the reason is stated in PDF, CLI and GUI). Revisit if auditors report confusion.
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
- **Attempt clutter on refused requests:** every refused or unanswered request (offline, 401, 402) leaves a new `n.tsq` + `n.provider` (seen: attempt 6 after five 402s). Reuse the newest unanswered attempt's request when no token came back, keeping write-before-send.
- **GUI performance on large archives:** every detail view, protocol and archive action re-verifies the whole archive (correct, slow for many GB). Cache the last `verify.Report` per archive generation.
