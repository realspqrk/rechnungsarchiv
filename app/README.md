# app/ — Windows GUI (Wails v3)

Thin shell over the core (AGENTS.md): it renders data from `Service` and adds native dialogs, Explorer and the browser. No business logic lives here.

This folder is the library package `app` (`app.Run`), not a program. The one executable `rechnungsarchiv.exe` (`cmd/rechnungsarchiv`) contains the CLI and this GUI (ADR-0008):

- `rechnungsarchiv gui [<ordner>]` opens the window. A folder opens the "Ordner archivieren" dialog for it once an archive is open for writing (after "Archiv anlegen" too; read-only mode drops it with a note). A second launch while the window runs brings the window to the front and hands over the folder its own command line already checked (Wails single instance, `AdditionalData`); this window checks it again. All handed-over folders (command line, later launch, drop) wait in `Shell.TakeFolders` until the frontend takes them; the event `ordner-wartet` is only a hint (`src/folders.ts` decides when to offer them).
- No arguments, started from Explorer or the Start menu, also opens the window: the process is alone in its console (`GetConsoleProcessList`) and stdout is that console. The console is released first (`FreeConsole`).
- Every other invocation is the CLI. In a terminal, `rechnungsarchiv` without arguments prints help.
- `rechnungsarchiv-gui.exe` (`cmd/rechnungsarchiv-gui`, built with `-H windowsgui`, no Wails, no core) starts `rechnungsarchiv.exe gui <args…>` from its own folder with `CREATE_NO_WINDOW` and exits, so shortcuts flash no console. It shows a message box if `rechnungsarchiv.exe` is missing. Shortcuts point to it.
- Exports bundle the running binary (`os.Executable`) as the verifier.

## Build and run

Prerequisites: Go (the toolchain in `go.mod` is fetched automatically), Node.js 22+, WebView2 (part of Windows 11).

```sh
# once: the Wails CLI, pinned. Never @latest (resolves to a nightly).
# Install it with the module's toolchain, or the binding generator warns
# "file requires newer Go version go1.27" and may miss code.
GOTOOLCHAIN=go1.27.4 go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0

cd app
wails3 build                     # -> ../bin/rechnungsarchiv.exe (CLI + GUI) + ../bin/rechnungsarchiv-gui.exe (launcher)
wails3 build DEV=true            # with devtools
wails3 task run                  # ../bin/rechnungsarchiv.exe gui
wails3 dev                       # live reload: Go rebuild + Vite dev server
wails3 task screenshots          # -> screenshots/*.png, every screen and state (sample data)
wails3 task screenshots:real     # -> screenshots/real-*.png (real service, demo archive, test TSA)
```

`wails3 build` runs `Taskfile.yml`: `npm ci` → `wails3 generate bindings -clean=true -ts -i` → `tsc` + `vite build` into `frontend/dist` → Windows resources (`build/windows`; one `.syso` in each `cmd/` package, removed after the build) → `go build -tags production` of `cmd/rechnungsarchiv` (a console program; the version goes into `internal/archive.Version`) → `go build -ldflags "-H windowsgui"` of the launcher `cmd/rechnungsarchiv-gui`.

Without `wails3`: `npm run build` in `frontend/`, then `go build -o bin/rechnungsarchiv.exe ./cmd/rechnungsarchiv` at the repo root (no icon, devtools on).

`go build ./...` and `go vet ./...` at the repo root work on a clean checkout: `frontend/dist/.gitkeep` is committed so the `go:embed` compiles, and the app then shows a "nicht gebaut" page.

## Real service and sample data

By default the GUI runs the real service (`real.go`, mapped to the DTOs in `map.go`): the same core and wiring as the CLI. On start it opens the remembered default archive (`%APPDATA%\Rechnungsarchiv\settings.json`, or `settings.json` in `RECHNUNGSARCHIV_CONFIG_DIR` when set; written by "Archiv anlegen" and "Bestehendes Archiv öffnen"). It then refreshes the trusted lists and re-checks in the background (`Status.busy`, event `status-geaendert`). Opening never stamps: when `Status.due` is not zero the window asks once ("Ausstehende Zeitstempel"). A folder it cannot write to opens view only ("Nur Ansicht"), which never writes, locks or stamps. Credentials go to the Windows Credential Manager.

A lock held by another PC reaches the frontend as a `UIError` with code `gesperrt` (host, user, since). The UI then asks before `TakeOver`; a writer that is still running is never taken over.

Set `RA_FAKE` to show sample data instead (no disk access at all):

| `RA_FAKE` | Shows |
|---|---|
| `start` | no archive (start screen) |
| `leer` | new archive without an Ablage |
| `normal` (default) | 13 Ablagen, 0013 pending (offline), provider Mensatek |
| `kein-dienst` | no provider set up |
| `guthaben` | "Archivieren" ends pending: credit used up |
| `schreibgeschuetzt` | backup in a write-protected folder: view only |
| `luecke` | Ablage 0007 missing |
| `zustaende` | one Ablage per state (0006–0013), stale trusted list |
| `disig` | client-certificate provider with URL from the contract |
| `abgelehnt` | folder with files that cannot be archived |
| `gesperrt` | archive locked by another PC (banner, takeover dialog) |
| `nachholen` | on opening: the question about pending timestamps (1 new, 1 free completion) |

`RA_FAKE_DELAY_MS` sets the simulated latency (default 300).

The window title shows "Beispieldaten (<scenario>)" whenever the fake is active.

### In a normal browser

Wails server mode serves the same Go code over HTTP:

```sh
cd app/frontend && npm run build && cd ..
RA_FAKE=zustaende WAILS_SERVER_PORT=8080 go run -tags server ../cmd/rechnungsarchiv gui
# open http://localhost:8080 — native dialogs and drag & drop do not work there
```

`npm run screenshots` (in `frontend/`) builds `cmd/rechnungsarchiv` with `-tags server` and uses this mode with the installed Microsoft Edge (`playwright-core`, no browser download).

### Real service against the test TSA

`wails3 task screenshots:real` runs `TestRealScreenshots` (`realshots_testtsa_test.go`, build tag `testtsa`): it builds a demo archive from `testdata/demo` through the real service with the test PKI, TSA and trusted lists of `internal/testenv`, builds `cmd/rechnungsarchiv` with `-tags "server testtsa"`, starts `rechnungsarchiv.exe gui` with `RECHNUNGSARCHIV_TESTENV` (both `testhook_testtsa.go` files run: the CLI's registers the test provider, this package's points the GUI at the test world), and runs `frontend/scripts/screenshots-real.mjs`. The demo lives in `../tmp/demo/` (git-ignored). Release builds contain neither the hook nor the test provider.

## Layout

| Path | Content |
|---|---|
| `run.go` | `Run(Options)`: Wails setup, window, single instance, drag & drop event, embedded assets |
| `service.go` | `Service` interface and the JSON DTOs (spec §5–6 states, qualifiers, reasons) |
| `real.go` | the real `Service` over `internal/*` (archive, stamp, verify, export, report, creds, trust, tsa) |
| `map.go` | core results → DTOs (renaming and counting only) |
| `system.go` | outside world: credential store, trusted-list stores, TLS roots, suggested folders |
| `testhook_testtsa.go` | `-tags testtsa` only: `RECHNUNGSARCHIV_TESTENV` points the GUI at the test world |
| `shell.go` | `Shell`, the only bound service: forwards to `Service`, plus dialogs, Explorer, browser (fixed URLs only) |
| `fake.go` | sample data for every UI state (`RA_FAKE`) |
| `frontend/src/` | vanilla TypeScript: `views/` (start, overview, deposit, settings), `dialogs/` (archive, export, about) |
| `frontend/src/fonts/` | IBM Plex Sans/Mono woff2 (latin, latin-ext) and their OFL licence texts |
| `frontend/bindings/` | generated by `wails3 generate bindings`; do not edit |
| `build/` | icon, manifest, version info, `wails3 dev` config |

The disclaimer is embedded from `docs/haftungsausschluss.md` by the root package (`disclaimer.go`), since `go:embed` cannot reach parent directories.
