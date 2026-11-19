# followup.md — out-of-scope tasks

## Contents
1. [Owner tasks](#owner-tasks)
2. [Deferred features (cut from v1, revisit on demand)](#deferred-features-cut-from-v1-revisit-on-demand)
3. [Code health](#code-health)

## Owner tasks
- **Provider recommendation stays Mensatek** (owner, 2026-11-18): DGN and GLOBALTRUST are not pursued. The README guides through the Spanish customer area (browser translation, DeepL). Real-world proof done 2026-11-18 (see Ablagen 0001/0002 in tmp/echttest-archiv); providers.json holds the confirmed policy OID 0.4.0.2023.1.1 and accuracy 1 s.
- **Export check online (decided 2026-11-18):** with network the export's own check runs online, so its Archivprotokolle say "qualifiziert" for fresh stamps; an offline re-check of the export says "unbestätigt" until the provider's country publishes a newer list (the reason is stated in PDF, CLI and GUI). Revisit if auditors report confusion.
  - Confirm Aruba's RFC 3161 endpoint before listing it as supported; Disig endpoint comes only with its access certificate.
- **Acceptance route (ADR-0007):**
  - informal BMF/KSW enquiry;
  - quotes for a KFS/DV 1 Softwarebescheinigung;
  - a pilot user for a §118 BAO Auskunftsbescheid.

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

## Code health
- **Config data race in `app/real.go`** (Codex review, 2026-11-19): `background` dereferences `ar.Config.TSA`, `settingsOf` copies `c.ar.Config` and `Preview` reads the retention default without `s.mu`, while `SetSettings` replaces the Config struct under `s.mu` (`internal/archive/config.go` setters). Copy the needed fields under `s.mu` first. Confirm with `go test -race` (needs gcc/cgo on this PC).
