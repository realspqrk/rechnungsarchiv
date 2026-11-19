# Playbook: signed Windows release

Goal: a GitHub release with `Rechnungsarchiv-win-Setup.exe` (Velopack installer), `Rechnungsarchiv-<x.y.z>-win-x64.zip` (the two programs, no installation) and `SHA256SUMS.txt`, every exe signed with the SPQRK Azure Artifact Signing profile.

Merge to `main` and the release itself need the owner's Go.

## Before

1. `main` is clean; `go test -tags testtsa ./...`, `go vet ./...`, `gofmt -l .`, and in `app/frontend` `npx tsc --noEmit` and `npm test` pass.
2. Set the version in `app/build/windows/info.json` (both `file_version`/`product_version` and the `0407` strings). `scripts/release.ps1` refuses a mismatch. Dev builds report `0.1.0-dev` (`internal/archive.Version`); only the release build stamps the version.
3. Within format 1 the byte format stays frozen (spec header); prose and app behaviour in `docs/spec.md` may change.
4. **Leak gate:** `go run ./internal/tools/leakcheck` prints `leakcheck: clean`. It covers every blob of every branch and tag, plus the files to be committed. `release.ps1` runs it before building and again with `-artifacts` on the downloads before the checksums; it stops on any finding. Private strings (TSA account name, private addresses) go in `%APPDATA%\Rechnungsarchiv\leakcheck-private.txt`, outside the repo. A finding that was already pushed: rotate the secret first, then purge history (owner decision, force push).
5. Tools on this PC: `wails3` v3.1.0, Node, `vpk` (dotnet global tool, 1.2.0), an Azure login with access to the signing account (`az account show`), the Artifact Signing dlib (`%LOCALAPPDATA%\Microsoft\MicrosoftArtifactSigningClientTools`). The signing `metadata.json` lives in `D:\Code\electron-workspace\metadata.json`; pass its path, never copy it into this repo.

## Build and sign

```powershell
.\scripts\release.ps1 -Version 1.0.0 -SignMetadata D:\Code\electron-workspace\metadata.json
```

Run it without `2>&1`: Windows PowerShell 5.1 turns native stderr into errors under `$ErrorActionPreference = 'Stop'`.

The script builds with the version stamped in, stages `rechnungsarchiv.exe`, `rechnungsarchiv-gui.exe` and `LICENSE.txt`, runs `vpk pack` (main exe: the launcher, `--skipVeloAppCheck`, signing via `--azureTrustedSignFile`), checks `Get-AuthenticodeSignature` = Valid for every exe, builds the ZIP and the checksums in `dist\release\`.

## Check

1. Silent install into a scratch folder (Start menu and desktop shortcuts are created; uninstall removes them):
   ```powershell
   $t = "$env:TEMP\ra-install"
   Start-Process .\dist\release\Rechnungsarchiv-win-Setup.exe -ArgumentList '--silent', '--installto', "`"$t`"" -Wait
   & "$t\current\rechnungsarchiv.exe" --version          # {"version":"x.y.z"}
   Start-Process "$t\Update.exe" -ArgumentList '--uninstall','--silent' -Wait
   ```
   The installer runs `rechnungsarchiv-gui.exe --veloapp-install …` and waits; the launcher exits at once for every `--veloapp-*` argument. An install that hangs means that check broke.

   The scratch install reuses the shortcut names of a real installation on this PC, and its uninstall deletes them. Afterwards, if `%LOCALAPPDATA%\Rechnungsarchiv` exists, run `.\dist\release\Rechnungsarchiv-win-Setup.exe --silent` (updates the real install and restores the shortcuts).
2. Do not smoke-test the window against the real `%APPDATA%` (known.md: it stamps the remembered default archive).

## Publish

```powershell
git tag -a vX.Y.Z -m "Rechnungsarchiv X.Y.Z"; git push origin vX.Y.Z
gh release create vX.Y.Z dist\release\Rechnungsarchiv-win-Setup.exe dist\release\Rechnungsarchiv-X.Y.Z-win-x64.zip dist\release\SHA256SUMS.txt --title "Rechnungsarchiv X.Y.Z" --notes-file <notes.md>
```

The README links `releases/latest/download/Rechnungsarchiv-win-Setup.exe`; keep that file name. Velopack's own `*-Portable.zip`, `*.nupkg` and feed files stay local (no auto-update without the Velopack SDK).
