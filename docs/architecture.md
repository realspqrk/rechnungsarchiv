# Architecture

**Status:** accepted 2026-11-18.
- Four-eyes review by Codex (Astra) and Grok 4.7. The owner then simplified the design ("most legal, simplest, YAGNI").
- Adversarial review rounds 1–2: Codex, Grok, and an internal workflow simulation. Results in `research/reviews/`.

**Where things live:**
- byte format and verification (normative): [`spec.md`](spec.md)
- decisions: [`adr/`](adr/)
- research: [`research/`](research/)

## What it does
1. An Austrian SME drags in a folder of invoices and clicks **"Ordner archivieren"**.
2. The tool writes the new files as a ZIP (an **Ablage**) into the local **Archiv**.
3. It gets **one qualified timestamp** (eIDAS) for the Ablage's hash (the **Prüfwert**).
4. It stores the timestamp, together with all the data needed to check it later, next to the ZIP.

Invoices never leave the PC.

Later, `kontrollieren` reports:

> "Die Dateien stimmen mit dem spätestens am T belegten Stand überein."
> (The files match the state proven to exist no later than T.)

For a qualified timestamp, the law presumes that the time and the integrity of the Prüfwert are correct (Art. 41(2)).

**Not proven, and the UI says so:**
- that the invoices are genuine or correct;
- that nothing is missing (a deleted *last* Ablage is not detectable);
- anything about the time before the timestamp.

## Terms (UI ↔ code)
| UI (DE) | Code | Meaning |
|---|---|---|
| **Archiv** | `Archive` | One folder per company, containing `rechnungsarchiv.json` |
| **Ablage** | `Deposit` | `NNNN_<Name>.zip`, a "Kopie eines Ordners mit Prüfsumme" (copy of a folder with checksum) |
| **Zeitstempel** | `Evidence` | The `NNNN_<Name>.nachweis/` folder: `n.tsq/.tsr/.chain.pem/.rev/` |
| **Prüfwert** | `Root` | `sha256(tagmanifest-sha256.txt)`. Shown only in the details view |

## Data flow
```
Ordner ──archivieren──► NNNN_Name.zip            BagIt bag, written once (spec §2–4)
                          └─ Prüfwert (32 bytes) ──RFC 3161──► qualified EU TSA (provider list, spec §7)
                                                                │  token + chain + OCSP/CRL
                          NNNN_Name.nachweis/  ◄────────────────┘   (spec §5)
chain.json: each Ablage names its predecessor → gaps/forks detectable; proven date via successors
```

## Components (Go, `github.com/realspqrk/rechnungsarchiv`)
| Package | Responsibility |
|---|---|
| `internal/bag` | Write and read bags, path rules, ZIP profile, duplicate detection |
| `internal/archive` | Archive folder, lock, numbering, commit/recovery, pending queue |
| `internal/tsa` | RFC 3161 client (Basic / client cert), evidence collection, token verification, `providers.json` |
| `internal/trust` | EU trusted lists: LOTL + national lists fetch, XML-signature check against pinned LOTL certs, cache, qualification at genTime |
| `internal/verify` | **The only** verification engine → `Report` |
| `internal/export` | Export for the tax audit (spec §8) |
| `internal/report` | Archivprotokoll PDF per Ablage: factual statement and complete file list, rendered from `verify.Report` (spec §10) |
| `cmd/rechnungsarchiv` | The one app binary (ADR-0008): CLI for people, scripts and AI agents (`status`, `init`, `archive`, `stamp`, `verify`, `export`, `providers`, `provider set/show`, `protocol`, `schema`; JSON + fixed exit codes) and the window (`gui`, or no arguments without a console) |
| `cmd/rechnungsarchiv-gui` | Tiny Windows-subsystem launcher: starts `rechnungsarchiv.exe gui …` without a console flash; no logic |
| `app/` | Wails v3 GUI library (package `app`, `Run`), thin shell; credentials in Windows Credential Manager |

## Status (one source of truth: `verify.Report`, spec §6)
| UI | When |
|---|---|
| ✅ **Zeitgestempelt · qualifiziert** | Bag valid, token + evidence valid, QTST "granted" at genTime |
| ☑️ **Zeitgestempelt · nicht qualifiziert** | Token valid, but the service was not QTST "granted" at genTime (neutral icon, not green) |
| ☑️ **Zeitgestempelt · unbestätigt** | No trusted list issued after genTime is cached and no online check was possible (e.g. offline audit of a recent stamp) → resolves with a newer list or online check |
| 🕒 **Ausstehend · \<Grund\>** | kein Dienst eingerichtet / offline / Zugangsdaten fehlen oder falsch / Guthaben aufgebraucht / Prüfdaten fehlen / Anbieterfehler. A successor's stamp may prove it "spätestens … (über Ablage N)" |
| ⚠️ **Unbestimmt** | E.g. TSU key compromised; evidence insufficient |
| ❌ **Verändert** / **Ungültig** | Hash differs / format or token invalid → "aus Sicherung wiederherstellen" (restore from backup) |
| Archive level | **Lücke** (gap), **Nummer doppelt vergeben** (number assigned twice) |

Pending Ablagen are never stamped by themselves. On opening, the GUI asks once when a run could fetch something (`stamp.Plan`: new, paid requests vs. free completions of tokens already paid for, spec §5); "Jetzt holen", "Jetzt nachholen" and `stamp` fetch them. Archiving stamps only the new Ablage.

There is **never** a fallback to another provider, and there is no free provider: the built-in list only contains qualified EU services.

## Modes
- **Normal:** the user's own archive, single writer.
- **Read-only** (`verify`, and the GUI's "Nur Ansicht" for a folder it cannot write to): no writing, no lock, no timestamp. Auditors and backup drills use `verify`.

## Known limitations (also stated in the README and `fuer-pruefer.md`)
- If the provider's key is compromised later, backdated forgeries cannot be ruled out with only one TSA. The countermeasure, a renewal stamp from a second provider, is deferred.
- The tool does not detect deletion of the most recent Ablage.
- Only one PC may write, and synced folders cannot be used as a write target.

## Not in v1 → `docs/dev/followup.md`
- OpenTimestamps
- S3
- second TSA / renewal
- Prüfwertliste
- ID Austria approval
- deletion
- EU TL live check
- macOS/Linux GUI
