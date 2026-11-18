# app/ — Windows GUI (Wails v3)

Thin shell over the core (AGENTS.md): it renders data from `Service` and adds native dialogs, Explorer and the browser. No business logic lives here.

## Build and run

Prerequisites: Go (the toolchain in `go.mod` is fetched automatically), Node.js 22+, WebView2 (part of Windows 11).

```sh
# once: the Wails CLI, pinned. Never @latest (resolves to a nightly).
# Install it with the module's toolchain, or the binding generator warns
# "file requires newer Go version go1.27" and may miss code.
GOTOOLCHAIN=go1.27.4 go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0

cd app
wails3 build                     # -> ../bin/rechnungsarchiv-gui.exe + ../bin/rechnungsarchiv.exe
wails3 build DEV=true            # with console and devtools
wails3 dev                       # live reload: Go rebuild + Vite dev server
wails3 task screenshots          # -> screenshots/*.png, every screen and state (sample data)
wails3 task screenshots:real     # -> screenshots/real-*.png (real service, demo archive, test TSA)
```

`wails3 build` runs `Taskfile.yml`: `npm ci` → `wails3 generate bindings -clean=true -ts -i` → `tsc` + `vite build` into `frontend/dist` → Windows resources (`build/windows`) → `go build` of the GUI and of the CLI next to it. Every export bundles that `rechnungsarchiv.exe` as the verifier; without it the export warns "Prüfprogramm konnte nicht beigelegt werden".

`go build ./...` and `go vet ./...` at the repo root work on a clean checkout: `frontend/dist/.gitkeep` is committed so the `go:embed` compiles, and the app then shows a "nicht gebaut" page.

## Real service and sample data

By default the GUI runs the real service (`real.go`, mapped to the DTOs in `map.go`): the same core and wiring as the CLI. On start it opens the remembered default archive (`%APPDATA%\Rechnungsarchiv\settings.json`, written by "Archiv anlegen" and "Bestehendes Archiv öffnen"). In normal mode it then refreshes the trusted lists and fetches pending timestamps in the background (`Status.busy`, event `status-geaendert`); "nur kontrollieren" never writes, locks or stamps. Credentials go to the Windows Credential Manager.

A lock held by another PC reaches the frontend as a `UIError` with code `gesperrt` (host, user, since). The UI then asks before `TakeOver`; a writer that is still running is never taken over.

Set `RA_FAKE` to show sample data instead (no disk access at all):

| `RA_FAKE` | Shows |
|---|---|
| `start` | no archive (start screen) |
| `leer` | new archive without an Ablage |
| `normal` (default) | 13 Ablagen, 0013 pending (offline), provider Mensatek |
| `kein-dienst` | no provider set up |
| `guthaben` | "Archivieren" ends pending: credit used up |
| `nur-kontrollieren` | backup opened read-only |
| `luecke` | Ablage 0007 missing |
| `zustaende` | one Ablage per state (0006–0013), stale trusted list |
| `disig` | client-certificate provider with URL from the contract |
| `abgelehnt` | folder with files that cannot be archived |
| `gesperrt` | archive locked by another PC (banner, takeover dialog) |
| `nachholen` | pending timestamps are fetched in the background (4 s) |

`RA_FAKE_DELAY_MS` sets the simulated latency (default 300).

The window title shows "Beispieldaten (<scenario>)" whenever the fake is active.

### In a normal browser

Wails server mode serves the same Go code over HTTP:

```sh
cd app/frontend && npm run build && cd ..
RA_FAKE=zustaende WAILS_SERVER_PORT=8080 go run -tags server .
# open http://localhost:8080 — native dialogs and drag & drop do not work there
```

`npm run screenshots` (in `frontend/`) uses this mode with the installed Microsoft Edge (`playwright-core`, no browser download).

### Real service against the test TSA

`wails3 task screenshots:real` runs `TestRealScreenshots` (`realshots_testtsa_test.go`, build tag `testtsa`): it builds a demo archive from `testdata/demo` through the real service with the test PKI, TSA and trusted lists of `internal/testenv`, starts the GUI in server mode with `-tags "server testtsa"` and `RECHNUNGSARCHIV_TESTENV` (`testhook_testtsa.go`), and runs `frontend/scripts/screenshots-real.mjs`. The demo lives in `../tmp/demo/` (git-ignored). Release builds contain neither the hook nor the test provider.

## Layout

| Path | Content |
|---|---|
| `main.go` | Wails setup, window, drag & drop event, embedded assets |
| `service.go` | `Service` interface and the JSON DTOs (spec §5–6 states, qualifiers, reasons) |
| `real.go` | the real `Service` over `internal/*` (archive, stamp, verify, export, report, creds, trust, tsa) |
| `map.go` | core results → DTOs (renaming and counting only) |
| `system.go` | outside world: credential store, trusted-list stores, TLS roots, bundled verifier |
| `testhook_testtsa.go` | `-tags testtsa` only: `RECHNUNGSARCHIV_TESTENV` points the GUI at the test world |
| `shell.go` | `Shell`, the only bound service: forwards to `Service`, plus dialogs, Explorer, browser (fixed URLs only) |
| `fake.go` | sample data for every UI state (`RA_FAKE`) |
| `frontend/src/` | vanilla TypeScript: `views/` (start, overview, deposit, settings), `dialogs/` (archive, export, about) |
| `frontend/src/fonts/` | IBM Plex Sans/Mono woff2 (latin, latin-ext) and their OFL licence texts |
| `frontend/bindings/` | generated by `wails3 generate bindings`; do not edit |
| `build/` | icon, manifest, version info, `wails3 dev` config |

The disclaimer is embedded from `docs/haftungsausschluss.md` by the root package (`disclaimer.go`), since `go:embed` cannot reach parent directories.
