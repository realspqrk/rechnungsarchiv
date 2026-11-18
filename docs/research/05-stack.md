# Stack: Rust vs Go (research 2026-11-18)

Sources: research subagent, crates.io/proxy.golang.org/GitHub/OSV APIs, and reading the verification source code.

**Result: Go.** One core library, a CLI and a thin GUI.

| Area | Rust | Go |
|---|---|---|
| Full RFC 3161 verification | `sigstore-tsa` 0.16 accepts **ECDSA P-256/P-384 only**. It does not check ESSCertID and does not check revocation (CRL/OCSP). Qualified TSAs (A-Trust, GLOBALTRUST, DigiCert, Sectigo, FreeTSA) typically sign with **RSA**, so this crate is ruled out. A self-built stack (x509-tsp + cms 0.3-rc + rustls-webpki) would be ~800–1,200 LOC on RC crates. | stdlib `crypto/x509` (chain + `ExtKeyUsageTimeStamping`) plus `digitorus/timestamp` + `digitorus/pkcs7` (`VerifyWithChainAtTime`). `sigstore/timestamp-authority/pkg/verification` is the reference. The ESSCertIDv2 certHash comparison is ~40 LOC to write ourselves. |
| Known CVEs | none in OSV | timestamp-authority CVE-2026-39984 (it verified the wrong cert in the cert bag) → we must test with an injected cert bag |
| OpenTimestamps | `opentimestamps` 0.2 (2023), parser only | `nbd-wtf/opentimestamps` v0.4 (stamp/upgrade/verify via Esplora/bitcoind), small. The format is simple (a 1-byte op-tag tree with LEB128 lengths), ~1–1.5k LOC to own |
| Deterministic ZIP | `zip` 9.x: heavy major-version churn; uses the current time if no time is set | stdlib `archive/zip`, stable for 10+ years. Trap: setting `Modified` adds the 0x5455 extra field → use `CreateRaw`/legacy fields instead |
| GUI | Tauri 2.12 (strongest framework) | Wails v2.17 (mature), Wails v3.0 (stable since 2026-10-26), Fyne (CGO, not native) |
| Longevity | crate churn, large dependency tree | Go 1 compatibility promise, reproducible builds (`-trimpath`), `govulncheck` with reachability analysis |
| Ecosystem | sigstore-rs, RustCrypto | rekor, Tessera, sigsum-go, Trillian, timestamp-authority — the transparency and timestamping ecosystem is mostly Go |

**Main risks:**
- The GUI layer depends on a small maintainer group behind Wails.
- Go binaries get antivirus false positives on Windows.

**Mitigation:**
- Keep the GUI a thin shell over the core (`Seal()`, `Verify()`), so the framework stays replaceable.
- Sign releases with Authenticode.
- Make builds reproducible.
