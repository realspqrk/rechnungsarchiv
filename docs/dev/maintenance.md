# maintenance.md — manual cleanup commands

## Contents
1. [Pending](#pending)

## Pending
- Remove agent test leftovers once no longer needed (gitignored, ~60 MB):
  `Remove-Item -Recurse -Force D:\Code\rechnungsarchiv\tmp\demo`
- After the Mensatek real-world proof, keep or delete the test archive (contains only demo invoices):
  `Remove-Item -Recurse -Force D:\Code\rechnungsarchiv\tmp\echttest-archiv`
  - Note: `init` remembered it as the default archive in `%APPDATA%\Rechnungsarchiv\settings.json`.
  - Delete that file, or run `init` for the real archive.
- Remove the Grok trust entry for this repo if no longer wanted: edit `%USERPROFILE%\.grok\trusted_folders.toml`.
