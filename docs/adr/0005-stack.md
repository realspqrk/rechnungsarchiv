# ADR-0005: Go + Wails v3

**Status:** accepted 2026-11-18. Research: `docs/research/05-stack.md`; reviews by Codex and Grok.

## Decision
- **Go 1.27** (go.mod `toolchain go1.27.4`), pure Go in the core and CLI.
- Dependencies are deliberately few:
  - the standard library;
  - RFC 3161 / CMS parsing and checks on stdlib `encoding/asn1` + `crypto/x509` (own code; digitorus not needed);
  - `golang.org/x/crypto/ocsp` (OCSP evidence);
  - `danieljoos/wincred` (TSA credentials in Windows Credential Manager);
  - `russellhaering/goxmldsig` (verifying EU trusted-list XML signatures);
  - `software.sslmate.com/src/go-pkcs12` (client-certificate auth, e.g. Disig).
- **GUI: Wails v3, exact version pin**, vanilla TypeScript + Vite, no UI framework.
- One app binary for CLI and GUI (ADR-0008); the Wails window lives in the same executable, plus a tiny Windows-subsystem launcher.
- The GUI is a thin shell over the core. It renders no invoice HTML and allows no remote navigation.
- Rust was rejected because the only high-level RFC 3161 verifier there supports ECDSA only, and qualified TSAs typically sign with RSA. Rust's crate churn was a further concern.

## Consequences
- Wails v3 is new (stable since 2026-10-26). We pin the exact tag and do not chase minor releases.
- Fallback: since the shell is thin, switching to Wails v2.17.1+ or another framework stays cheap.
- Windows releases need Authenticode signing (followup).
