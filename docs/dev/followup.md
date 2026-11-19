# followup.md — out-of-scope tasks

## Contents
1. [Owner tasks](#owner-tasks)
2. [Deferred features (cut from v1, revisit on demand)](#deferred-features-cut-from-v1-revisit-on-demand)
3. [Code health](#code-health)

## Owner tasks
- **Provider recommendation stays Mensatek** (owner, 2026-11-18): DGN and GLOBALTRUST are not pursued. The README guides through the Spanish customer area (browser translation, DeepL). Real-world proof done 2026-11-18 (see Ablagen 0001/0002 in tmp/echttest-archiv); providers.json holds the confirmed policy OID 0.4.0.2023.1.1 and accuracy 1 s.
- **Export check online (decided 2026-11-18):** with network the export's own check runs online, so its Archivprotokolle say "qualifiziert" for fresh stamps; an offline re-check of the export says "unbestätigt" until the provider's country publishes a newer list (the reason is stated in PDF, CLI and GUI). Revisit if auditors report confusion.
- **Only Mensatek is offered (owner, 2026-11-19):** Disig, Namirial, Aruba and GLOBALTRUST are `hidden` in providers.json (known, verified, not selectable for new setups). Offer one again only after a real-world stamp with it (Aruba: confirm the RFC 3161 endpoint; Disig: endpoint comes with the access certificate). Requests come in via GitHub issues ("Weitere Anbieter? Melden Sie sich").
- **Acceptance route (ADR-0007):**
  - informal BMF/KSW enquiry;
  - quotes for a KFS/DV 1 Softwarebescheinigung;
  - a pilot user for a §118 BAO Auskunftsbescheid.

## Deferred features (cut from v1, revisit on demand)
- **Second TSA / RFC 3161 renewal** (a new token over the old `.tsr`):
  - the only countermeasure to a later TSU key compromise (known limitation);
  - also needed before SHA-256 or RSA weaken.
- **Deletion after retention** (ADR-0006):
  - `delete-expired` keeps `chain.json` + `tagmanifest-sha256.txt` as a tombstone, plus a timestamped Löschvermerk;
  - `verify` flags VORZEITIG GELÖSCHT.
- **S3 Object Lock completeness witness** (ADR-0001/0003): optional PUT of `<seq>-<pruefwert>.json` with retain-until. Needs live version listing in `verify`.
- **Prüfwertliste to the Steuerberater** (an external completeness checkpoint for the newest Ablage).
- **OpenTimestamps anchor.**
- **ID Austria approval:**
  - the owner signs a generated Archivprotokoll PDF; the app checks prefix, PAdES and A-Trust chain, then timestamps the signed PDF;
  - blockers: free signature is for private persons only, PAdES B-B has no trusted time, certificates expire after 5 years (`docs/research/09-id-austria.md`).
- **PDF/A-2b for the Archivprotokoll** (spec §10 SHOULD): gopdf cannot produce it; options: post-process with a PDF/A library or switch generator.
- **RFC 4998 ERS export** (Bouncy Castle / BSI ERVerifyTool interop).
- **macOS/Linux GUI builds.** The CLI builds everywhere.
- **tsa evidence robustness:** keep every evidence file per certificate (`ReadAttempts` keeps one per CertKey, so a bad stored OCSP answer blocks completion until the 24 h window ends); add `Fetcher.Evidence(cert, issuer, genTime)` and `tsa.WriteProvider` instead of the local workarounds in internal/stamp.
- **GUI performance on large archives:** every detail view, protocol and archive action re-verifies the whole archive (correct, slow for many GB). Cache the last `verify.Report` per archive generation.

## Code health
- **"Jetzt nachholen" is archive-wide** (row link, Ablage detail, archive dialog): it runs StampPending for every pending Ablage, without showing the count first. Fine while a backlog is rare (the opening question shows the counts). If users trip over it: open the question dialog with fresh counts from these links, or give StampPending an optional Prüfwert filter (`stamp.Options.Roots` exists). (Visual review 1.0.2 #3.)
- **Opening question is not a hard cap** (code review 1.0.2 #1, accepted): "Jetzt holen" runs an unrestricted StampPending. If another PC committed meanwhile (not supported: one writer) or a trusted-list refresh reclassifies an attempt, it may buy more than shown. A cap would pass the quoted paid count and stop before extra paid requests.
- **Compromised token → Unbestimmt, but Due.Paid counts it** (code review 1.0.2 #5, rare): the question offers a new stamp; after "Später" the row (Unbestimmt) has no "Jetzt nachholen". Add an archive-level action whenever Due is not zero.
- **Owner wording to check** (visual review 1.0.2): the disclaimer's "unveränderliche Ablagen" could read as protection against change or deletion; the privacy line "Zum Dienst gehen nur der Prüfwert, Ihr Kontoname und Ihre IP-Adresse" omits that the password is sent to log in (HTTP Basic).
- **Config data race in `app/real.go`** (Codex review, 2026-11-19): `background` dereferences `ar.Config.TSA`, `settingsOf` copies `c.ar.Config` and `Preview` reads the retention default without `s.mu`, while `SetSettings` replaces the Config struct under `s.mu` (`internal/archive/config.go` setters). Copy the needed fields under `s.mu` first. Confirm with `go test -race` (needs gcc/cgo on this PC).
- **Overview banner "Einrichtung fortsetzen"** (visual review 1.0.2): the main screen hides the setup banner once a provider is chosen, even if credentials are missing (e.g. after a PC change; credentials live in the old PC's credential store). Needs `Status` to carry the credential state (`settingsOf` logic) and a banner in `overview.ts`.
- **Wording on main surfaces** (visual review 1.0.2): footer and settings say "Prüfwert"; AGENTS.md wants it in details only. Proposal: "Prüfsumme der Ablage" on main surfaces; read-only banner "Das Archiv bleibt unverändert. Es werden keine Zeitstempel geholt."
- **settings.json across processes** (code review 1.0.2): GUI and CLI writing `recent` at the same moment can drop one entry (last rename wins; temp files are unique). A cross-process lock is not worth it yet.

## Social media screenshots (LinkedIn series, requested 2026-11-19)
**Request from Gerhard's social bot (SPQRK social posts), approved by Gerhard 2026-11-19. Media only: do not change product behaviour, UI texts or features for this.** Replaces the shorter list from earlier the same day.

### Ask: one reusable media kit in the repo, regenerated per release
- **Location:** `docs/media/` (proposal): `docs/media/README.md` + `docs/media/manifest.json` + `docs/media/<version>/*.png` (keep the latest under `docs/media/latest/`).
- **Reproducible:** one script, e.g. `app/frontend/scripts/screenshots-media.mjs` + `wails3 task screenshots:media`, built on the existing `screenshots.mjs` / `screenshots-real.mjs` scenarios. Re-run per release; same IDs, same file names.
- **Demo data only:** company `Musterfirma GmbH`, archive `D:\Buchhaltung\Archiv`, invented suppliers as in `fake.go`. No real customers, accounts, IPs or credentials (password fields masked).
- **Consistent demo clock:** "Heute" = 19.11.2026 10:32 (Europe/Vienna); Ablagen 2026-09 to 2026-11; the same numbers in every shot (13 Ablagen, 0011 = 64 Dateien unverändert, etc.).
- **Two variants per shot:**
  - `focus`: element screenshot of ONE panel (Playwright `locator.screenshot`, ~16 CSS px padding), viewport **600 CSS px** wide, **deviceScaleFactor 3**. Used for LinkedIn 4:5 and X 1:1 crops (shown ~920 px wide, must stay readable on a phone).
  - `full`: whole window, viewport 1280×860 CSS, **deviceScaleFactor 2**, full page for non-modal screens.
- **Format:** PNG, lossless, sRGB, no browser chrome, no mouse cursor, no focus rings unless the shot is about focus. German UI, de-AT locale. Light theme only (there is no dark theme; do not add one for this).
- **Naming:** `SM-<nn>-<slug>-<focus|full>@<scale>x.png`, e.g. `SM-05-archivieren-zeitgestempelt-focus@3x.png`.
- **Manifest** (`manifest.json` + table in README): id, file, screen/state, scenario (`RA_FAKE` value or real/test TSA), app version, git commit, viewport, scale, CSS selector of the focused panel, date generated.
- Real-service shots (`screenshots:real`, test TSA) are welcome where the sample-data service cannot show it; label them in the manifest.

### Shot list (priority = needed-by date; plan dates are LinkedIn post dates)
| ID | Screen / state | Variants | Used by (plan) | Needed by |
|---|---|---|---|---|
| SM-01 | Start: "Archiv anlegen" with Firma + Archivordner filled | focus, full | P2 How-to step 1 (Di 24.11.) | Mo 23.11. |
| SM-02 | Einstellungen: provider chosen (Mensatek), credentials entered/masked, status OK | focus, full | P2 step 2; P11 FAQ | Mo 23.11. |
| SM-03 | Overview, empty archive with drop zone "Ordner mit Rechnungen hierher ziehen" | focus (drop zone), full | P2 step 3 | Mo 23.11. |
| SM-04 | Archive dialog before archiving: folder preview, file count, Aufbewahrung 7 Jahre | focus, full | P2 step 3; P10 | Mo 23.11. |
| SM-05 | Archive dialog done: "Zeitgestempelt" | focus, full | P2 step 4; X teaser | Mo 23.11. |
| SM-06 | Overview normal: Ablagen list with "Zeitgestempelt, qualifiziert (eIDAS)" + one "Ausstehend" | focus (list), full | P2; X/README hero; P12 | Mo 23.11. |
| SM-07 | Ablage detail 0011 "Unverändert seit spätestens …" + Dateien 64/0/0/0 | focus | P1 (done, bot capture), P3 fallback, later reposts | Mi 25.11. |
| SM-08 | Terminal: `rechnungsarchiv status` on the demo archive (~80 columns, large font, dark terminal) | full | P4 Build in public (Di 01.12.) | Mo 30.11. |
| SM-09 | Terminal: `rechnungsarchiv verify` result on the demo archive | full | P4; P6 | Mo 30.11. |
| SM-10 | Comparison inputs (text, not image): sources/price range for WORM storage or archive systems behind README "ein paar tausend Euro"; current provider prices | text in README of `docs/media/` | P5 Vergleich (Do 03.12.) | Di 01.12. |
| SM-11 | Ablage detail "Verändert" (a file changed after archiving) | focus | P6 Feature (Di 09.12.) | Mo 07.12. |
| SM-12 | Overview after "Archiv kontrollieren" with result panel (incl. one problem state) | focus, full | P6 | Mo 07.12. |
| SM-13 | Overview "Lücke" (missing Ablage number) | focus | P6; FAQ later | Mo 07.12. |
| SM-14 | Export dialog "Export für Betriebsprüfung" before export (Inhalt summary) | focus, full | P8 Use case (Di 15.12.) | Fr 11.12. |
| SM-15 | Export done | focus | P8 | Fr 11.12. |
| SM-16 | Archivprotokoll PDF, page 1 (and the fact-table page) rendered to PNG at 2x | full | P8; P11 | Fr 11.12. |
| SM-17 | Export folder in Windows Explorer (file list, real Windows, demo data) | full | P8 | Fr 11.12. |
| SM-18 | Footer line "Zum Zeitstempel-Dienst gehen der Prüfwert, Ihr Kontoname und Ihre IP-Adresse …" | focus | P9 Datenschutz (Do 17.12.) | Di 15.12. |
| SM-19 | Ablage detail "Technische Details" expanded (Prüfwert, chain, OCSP) | focus | P9; P4 | Di 15.12. |
| SM-20 | Aufbewahrung panel "bis 31.12.2033" + dialog "Aufbewahrung ändern" | focus | P10 Belegjahr (Do 07.01.) | Mo 04.01. |
| SM-21 | Ablage detail Zeitstempel panel "Qualifiziert (eIDAS) · Mensatek, Spanien" | focus | P11 FAQ (Di 12.01.) | Do 07.01. |
| SM-22 | Ablage states "nicht qualifiziert" and "unbestätigt" | focus | P11 | Do 07.01. |
| SM-23 | Ausstehend/offline + "Jetzt nachholen", and "Zeitstempel werden geholt" | focus | P11; FAQ "kein Internet" | Do 07.01. |
| SM-24 | 1.0.2: archive menu (recent archives / open / new) | focus, full | P12 Release (Do 14.01.) | Mo 11.01. |
| SM-25 | 1.0.2: "Neues Archiv" flow | focus | P12 | Mo 11.01. |
| SM-26 | "Über" dialog with version number | focus | P12; every release card | Mo 11.01. |
| SM-27 | Setup/Start menu entry on Windows (real install, demo user) | full | P12; How-to follow-ups | Mo 11.01. |
| SM-28 | App icon PNG 1024×1024, transparent | file | release cards (X 1:1) | Mo 11.01. |
| SM-29 | Banner "kein Dienst eingerichtet" (setup not finished) | focus | later How-to / FAQ | when convenient |
| SM-30 | Short user-visible changelog per release (plain list in `docs/media/<version>/CHANGES.md`) | text | P12 and every release post | per release |

Contact: answer or questions via Gerhard; the social bot reads `docs/media/manifest.json`.
