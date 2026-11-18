VERDICT changes: 2

The publisher and dry-run count corrections are complete. Static review found two gaps in the script changes.

1. scripts/release.ps1:36 — An emulated x64 PowerShell on Windows ARM64 can pass the AMD64 environment check while a native ARM64 Wails generates ARM64 resources. The x64 binaries can then ship without their icon, manifest or version information. Check the native machine type with IsWow64Process2 and verify the staged executables contain the expected resources before signing. [Microsoft’s emulation documentation](https://learn.microsoft.com/en-us/windows/arm/apps-on-arm-x86-emulation)

2. scripts/release.ps1:39 — GOOS and GOARCH change before Push-Location enters the protected block. If entering the app directory fails, the caller retains the release settings. Move both assignments inside the existing try block, after Push-Location succeeds.
