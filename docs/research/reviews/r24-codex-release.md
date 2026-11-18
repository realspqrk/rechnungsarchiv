VERDICT changes: 4

The eleven round-22 fixes are present. Both published-file hashes match SHA256SUMS.txt. The ZIP contains LICENSE.txt and byte-identical copies of the two checked executables. Both programs and Setup have valid signatures from the expected publisher.

1. AGENTS.md:15 — The install check accepts any valid signer whose subject starts with the same common name. A certificate with a different organization or country can pass, even though the release script rejects it. Compare the full publisher subject used in scripts/release.ps1:18.

2. scripts/release.ps1:37 — On a native ARM64 build host, GOARCH=amd64 targets the Go binaries correctly, but app/Taskfile.yml:46 generates Windows resources using Wails Task’s host-derived {{ARCH}}. Go excludes the resulting _arm64.syso files from the x64 binaries, dropping their manifest, icon and version resources. Pass the target architecture into the resource task explicitly and use it for both resource filenames and generator arguments.

3. scripts/release.ps1:39 — The finally block deletes GOOS and GOARCH even if the caller had set them. After success or failure, subsequent builds in the same PowerShell session can target the wrong platform. Save both original values and restore them in finally.

4. AGENTS.md:34 — The dry-run instructions say count equals the number of files. A folder containing nine invoices and Thumbs.db has ten files and correctly reports nine. Define count as the number of included files after exclusions.
