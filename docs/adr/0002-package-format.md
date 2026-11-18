# ADR-0002: BagIt bag inside a ZIP, raw evidence files next to it

**Status:** accepted 2026-11-18. Revised after review round 1. Normative details: [`spec.md`](../spec.md) §1–§4.

## Context
The archive must stay readable for 7–22 years and be verifiable without our software. The ZIP is written once and never changed. The timestamp arrives after the ZIP is committed (or later, if the PC was offline).

## Decision
- **Ablage** = a ZIP using the Stored method. It holds exactly one **BagIt 1.0 bag** with a fixed, mandatory set of tag files: manifest-sha256, `chain.json`, `files.json` (original mtimes), `skipped.txt` (system junk only), `VERIFY.txt`. No other tag files are allowed.
- **Originals are stored byte-identical**, with no conversion (no PDF/A).
- **Prüfwert** = `sha256(tagmanifest-sha256.txt)`.
  - The tagmanifest covers every tag file, so chain and retention metadata cannot change without changing the Prüfwert.
  - `verify` also works on an extracted bag.
- **Nachweis** = the sidecar folder `<name>.nachweis/`, holding one immutable set of raw files per attempt: `n.tsq`, `n.tsr`, `n.chain.pem`, `n.rev/` (OCSP/CRL per certificate).
  - There is no index file; the files themselves are the source of truth.
  - The Nachweis is matched to its Ablage by the imprint, not the file name.
- **No deduplication** (removed after review round 2: it lost context and broke year-filtered exports). The dialog only *informs* about files already contained in earlier Ablagen.
- **File name:** `NNNN_<folder name>.zip`, with no date prefix (the name usually already contains the month).
- **Cut after review:**
  - SHA-512 manifests: they give no independent protection behind a SHA-256 root.
  - Byte-reproducible ZIP as a requirement.
  - `index.json`.

## Consequences
- Recovery after a crash is a defined commit order (spec §4).
- The read-only attribute is a convenience and is never presented as tamper protection.
