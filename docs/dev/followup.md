# followup.md — out-of-scope tasks

## Contents
1. [Owner tasks](#owner-tasks)
2. [Deferred features (cut from v1, revisit on demand)](#deferred-features-cut-from-v1-revisit-on-demand)

## Owner tasks
- **Qualified TSA for integration tests:**
  - Buy Mensatek credit (€10, never expires; `https://www.mensatek.com/en/precios.php`).
  - Confirm sale to an Austrian company with reverse charge, the endpoint URL, the policy OID and the accuracy.
  - Disig as second test provider (client cert).
  - Confirm Aruba's RFC 3161 endpoint before listing it as supported.
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
- **RFC 4998 ERS export** (Bouncy Castle / BSI ERVerifyTool interop).
- **macOS/Linux GUI builds.** The CLI builds everywhere.
