# Builds the signed Windows release of Rechnungsarchiv into dist\release:
# Rechnungsarchiv-win-Setup.exe (Velopack installer),
# Rechnungsarchiv-<Version>-win-x64.zip (the two signed programs, no
# installation) and SHA256SUMS.txt; these three are the downloads. Playbook:
# docs/dev/playbooks/release.md.
#
#   .\scripts\release.ps1 -Version 1.0.0 -SignMetadata D:\Code\electron-workspace\metadata.json
#
# -SignMetadata is the Azure Artifact Signing metadata.json (endpoint,
# account, certificate profile); it stays outside this repository. Signing
# uses the Azure login of this PC (az login / Visual Studio credentials).
param(
    [Parameter(Mandatory)] [string] $Version,
    [Parameter(Mandatory)] [string] $SignMetadata
)
$ErrorActionPreference = 'Stop'
# Every shipped exe must be signed by this publisher (not just "Valid").
$Publisher = 'CN=Gerhard Petermeir, O=Gerhard Petermeir, L=Bad Gleichenberg, S=Steiermark, C=AT'
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root 'dist'
$stage = Join-Path $dist 'stage'
$out = Join-Path $dist 'release'
$check = Join-Path $dist 'check'

if (-not (Test-Path $SignMetadata)) { throw "Signatur-Metadaten fehlen: $SignMetadata" }
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw "Version muss x.y.z sein: $Version" }
$info = Get-Content (Join-Path $root 'app\build\windows\info.json') -Raw | ConvertFrom-Json
$strings = $info.info.'0407'
foreach ($v in $info.fixed.file_version, $info.fixed.product_version, $strings.FileVersion, $strings.ProductVersion) {
    if ($v -ne $Version) { throw "app\build\windows\info.json nennt $v, nicht $Version" }
}

# 1. Build both executables for x64 with the version stamped in. The
# Windows resources (icon, manifest, version) are generated for the host
# architecture, so only an x64 host builds the x64 release.
if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') { throw "Release nur auf einem x64-PC bauen (hier: $env:PROCESSOR_ARCHITECTURE)" }
$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH }
Push-Location (Join-Path $root 'app')
try {
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'
    wails3 task build VERSION=$Version
    if ($LASTEXITCODE -ne 0) { throw "Build fehlgeschlagen" }
} finally {
    Pop-Location
    $env:GOOS = $saved.GOOS; $env:GOARCH = $saved.GOARCH
}

# 2. Stage exactly what ships, in fresh folders (a failed delete aborts).
foreach ($d in $stage, $out, $check) {
    if (Test-Path $d) { Remove-Item -Recurse -Force $d }
    New-Item -ItemType Directory $d | Out-Null
}
Copy-Item (Join-Path $root 'bin\rechnungsarchiv.exe'), (Join-Path $root 'bin\rechnungsarchiv-gui.exe') $stage
Copy-Item (Join-Path $root 'LICENSE') (Join-Path $stage 'LICENSE.txt')
$staged = (Get-ChildItem $stage -Recurse -Force | ForEach-Object Name | Sort-Object) -join ','
$expected = ('LICENSE.txt', 'rechnungsarchiv-gui.exe', 'rechnungsarchiv.exe' | Sort-Object) -join ','
if ($staged -ne $expected) { throw "Unerwartete Dateien im Paket: $staged" }
# The Windows resources made it into both programs (a resource built for
# another architecture is silently left out by Go).
foreach ($exe in 'rechnungsarchiv.exe', 'rechnungsarchiv-gui.exe') {
    $vi = (Get-Item (Join-Path $stage $exe)).VersionInfo
    if ($vi.FileVersion -ne $Version -or $vi.ProductVersion -ne $Version -or $vi.CompanyName -ne 'SPQRK IT-Dienstleistungen') {
        throw "${exe}: Windows-Ressourcen fehlen oder sind veraltet (FileVersion '$($vi.FileVersion)')"
    }
}
$reported = & (Join-Path $stage 'rechnungsarchiv.exe') --version
if ($LASTEXITCODE -ne 0 -or ($reported | ConvertFrom-Json).version -ne $Version) {
    throw "rechnungsarchiv.exe meldet '$reported', erwartet $Version"
}

# 3. Pack and sign. The launcher is the main exe (Start menu shortcut);
# it exits at once for Velopack's hooks, so the SDK check is skipped.
vpk -x -y pack `
    --packId Rechnungsarchiv --packVersion $Version --packTitle Rechnungsarchiv `
    --packAuthors 'SPQRK IT-Services' --packDir $stage `
    --mainExe rechnungsarchiv-gui.exe --icon (Join-Path $root 'app\build\windows\icon.ico') `
    --skipVeloAppCheck --channel win --outputDir $out `
    --azureTrustedSignFile $SignMetadata
if ($LASTEXITCODE -ne 0) { throw "vpk pack fehlgeschlagen" }

# 4. Every shipped executable must be signed by the publisher.
Expand-Archive (Join-Path $out 'Rechnungsarchiv-win-Portable.zip') $check
$exes = @(Get-ChildItem $out -Filter *.exe) + @(Get-ChildItem $check -Recurse -Filter *.exe)
foreach ($exe in $exes) {
    $sig = Get-AuthenticodeSignature $exe.FullName
    if ($sig.Status -ne 'Valid' -or $sig.SignerCertificate.Subject -ne $Publisher) {
        throw "$($exe.Name): Signatur $($sig.Status), $($sig.SignerCertificate.Subject)"
    }
    "{0,-40} {1}  {2}" -f $exe.Name, $sig.Status, $sig.SignerCertificate.Subject
}

# 5. The ZIP for use without installation: the signed programs only.
# (Velopack's own portable ZIP adds a starter named Rechnungsarchiv.exe at
# its top level, which reads like the CLI; it is not published.)
$zip = Join-Path $out "Rechnungsarchiv-$Version-win-x64.zip"
$files = 'rechnungsarchiv.exe', 'rechnungsarchiv-gui.exe', 'LICENSE.txt' | ForEach-Object { Join-Path $check "current\$_" }
Compress-Archive -Path $files -DestinationPath $zip

# 6. Checksums of the downloads, exactly these two files.
$downloads = Get-Item (Join-Path $out 'Rechnungsarchiv-win-Setup.exe'), $zip
$downloads | ForEach-Object {
    "{0}  {1}" -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash.ToLower(), $_.Name
} | Set-Content -Encoding ascii (Join-Path $out 'SHA256SUMS.txt')
Get-Content (Join-Path $out 'SHA256SUMS.txt')
