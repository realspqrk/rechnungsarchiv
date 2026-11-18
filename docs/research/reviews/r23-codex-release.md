VERDICT changes: 11

1. High — AGENTS.md:13. Pasting the setup block launches the installer even if its signature is invalid. It also never compares the signer with Gerhard Petermeir. Make download errors terminating and throw unless both Authenticode status and publisher match. Check the installer exit code before using the installed CLI.

2. High — docs/dev/playbooks/release.md:40. The current release directory lacks the advertised 1.0.0 x64 ZIP. Its checksum manifest names Velopack’s Portable.zip instead. Publishing from these files cannot deliver the documented release. Generate the promised ZIP from the verified signed binaries, regenerate the manifest, and check both advertised filenames and hashes before tagging.

3. High — scripts/release.ps1:36. Cleanup suppresses deletion failures and reuses the staging directory. A locked but readable leftover file can therefore enter the signed installer alongside the intended files. Use a fresh empty staging directory or abort on cleanup failure and verify the staged file list before packing.

4. Medium — scripts/release.ps1:60. Selecting another valid Azure signing profile can pass every signature check and publish under the wrong identity. Enforce the documented publisher for the setup and shipped programs before generating checksums.

5. Medium — scripts/release.ps1:24 and :41. A stale product or localized version passes the resource check. The CLI check also accepts 11.0.0 when expecting 1.0.0. Compare all four resource versions and the parsed CLI JSON version exactly, and require a successful CLI exit.

6. Medium — AGENTS.md:29. A setup-only installation has no repository demo directory, so the required nine-invoice dry run fails. Provide a version-matched fixture and use its resolved path.

7. Medium — scripts/release.ps1:67. The ZIP name promises x64 even when built on Windows ARM64 with the host Go target. Pin the architecture or reject non-AMD64 executables before signing.

8. Low — AGENTS.md:17. The source fallback leaves the shell in app and resolves app/bin, while Wails writes to the repository’s bin directory. Return to the repository root before resolving the executable.

9. Low — README.md:94. After a setup-only install, the CLI examples rely on a PATH entry the instructions establish only for source builds. Show how to invoke the installed executable.

10. Low — docs/dev/playbooks/release.md:29. Start-Process joins ArgumentList elements without quoting the scratch path. A TEMP path containing spaces breaks the install target. Quote the target in the child command line.

11. Low — README.md:198. The FAQ still tells users to wait for a release. Point them to the 1.0.0 installation instructions.

The Windows launcher tests passed, including TestInstallerHooksExit. I did not run the signing script.
