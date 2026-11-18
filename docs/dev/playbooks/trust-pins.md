# Playbook: EU trusted-list pins and bootstrap (before every release)

1. Run `go test -tags online ./internal/trust -run TestOnline -count=1`. Its `checkPins` step fails if the live LOTL self-pointer announces a signer that is not pinned.
2. If it fails:
   1. Find the newest OJ C notice "Information related to data on Member States' trusted lists …". The current one is C/2026/1944 of 15.4.2026. EUR-Lex blocks bots; use `publications.europa.eu/resource/celex/52026XC01944`.
   2. Check that every SHA-256 fingerprint in the notice matches the certificates in the LOTL self-pointer.
   3. Replace `internal/trust/pins/*.pem` with exactly that set. File names follow `lotl-<first 16 hex of sha256>.pem`.
3. Re-run the test with `-update-bootstrap`. This rewrites `internal/trust/bootstrap/*.xml.gz` from the lists it just verified.
4. Commit pins and bootstrap together in one commit.

Pivot LOTLs (TS 119 615) are not implemented. After a key change, an old binary fails closed (every qualification shows `unbestätigt`) until it is updated.
