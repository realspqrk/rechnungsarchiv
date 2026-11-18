// Package app is the Rechnungsarchiv desktop GUI (Wails v3), a thin shell
// over the core. It has no main package of its own: the one executable
// rechnungsarchiv.exe opens it with `rechnungsarchiv gui` (ADR-0008,
// cmd/rechnungsarchiv). Build with `wails3 build` in this directory
// (README.md).
package app

import (
	"context"
	"embed"
	"io/fs"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// frontend/dist is built by Vite. On a clean checkout it only holds
// .gitkeep, so `go build ./...` works; the app then shows a hint page.
//
//go:embed all:frontend/dist
var assets embed.FS

const notBuiltPage = `<!doctype html><html lang="de"><meta charset="utf-8"><title>Rechnungsarchiv</title>
<body style="font-family:system-ui;margin:48px;color:#17201C;background:#F4F5F2">
<h1 style="font-size:22px">Oberfläche nicht gebaut</h1>
<p>Bitte im Ordner <code>app</code> <code>wails3 build</code> ausführen (siehe app/README.md).</p></body></html>`

func init() {
	// A folder waits in Shell.TakeFolders; the event carries no data.
	application.RegisterEvent[application.Void]("ordner-wartet")
	application.RegisterEvent[StatusChanged]("status-geaendert")
}

// Options configures one window run.
type Options struct {
	// Folder, if set, is offered in the "Ordner archivieren" dialog once
	// an archive is open for writing (rechnungsarchiv gui <ordner>).
	// Absolute path of an existing folder; the caller checks it. If a
	// window already runs, this process hands Folder over to it and exits;
	// that window checks it again.
	Folder string
}

// Run opens the window and returns when it is closed. It must be called on
// the main goroutine (Wails locks it to the main OS thread).
func Run(opts Options) error {
	// The real core is the default; RA_FAKE=<scenario> shows sample data
	// (screenshots, UI review). See README.md.
	shell := &Shell{debug: sys.debug}
	if opts.Folder != "" {
		shell.offer(handleDrop([]string{opts.Folder}))
	}
	var real *realService
	if scenario := os.Getenv("RA_FAKE"); scenario != "" {
		f, err := newFakeService(scenario)
		if err != nil {
			return err
		}
		shell.svc, shell.fake, shell.debug = f, f.scenario, true
	} else {
		real = newRealService(sys)
		shell.svc = real
	}
	handler, err := assetHandler()
	if err != nil {
		return err
	}

	// The second-instance listener runs on its own goroutine from inside
	// application.New, possibly before the window exists.
	var win atomic.Pointer[application.WebviewWindow]
	front := func() {
		if w := win.Load(); w != nil {
			w.Restore()
			w.Focus()
		}
	}
	app := application.New(application.Options{
		Name:           "Rechnungsarchiv",
		Description:    "Lokales Archiv für Rechnungen mit qualifiziertem Zeitstempel",
		Services:       []application.Service{application.NewService(shell)},
		Assets:         application.AssetOptions{Handler: handler},
		SingleInstance: singleInstanceOptions(opts.Folder, shell, front),
	})
	shell.app = app
	shell.setHint(func() { app.Event.Emit("ordner-wartet") })
	if real != nil {
		real.onChange = func() {
			busy := ""
			if st, err := real.Status(context.Background()); err == nil {
				busy = st.Busy
			}
			app.Event.Emit("status-geaendert", StatusChanged{Busy: busy})
		}
	}

	title := "Rechnungsarchiv"
	if shell.fake != "" {
		title += " – Beispieldaten (" + shell.fake + ")"
	}
	w := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            title,
		Width:            1280,
		Height:           860,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: application.NewRGB(0xF4, 0xF5, 0xF2),
		EnableFileDrop:   true,
		URL:              "/",
	})
	win.Store(w)
	w.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		shell.offer(handleDrop(e.Context().DroppedFiles()))
	})
	return app.Run()
}

func assetHandler() (http.Handler, error) {
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return nil, err
	}
	_, statErr := fs.Stat(dist, "index.html")
	if statErr != nil && os.Getenv("FRONTEND_DEVSERVER_URL") == "" { // wails3 dev proxies to Vite
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(notBuiltPage))
		}), nil
	}
	return application.AssetFileServerFS(dist), nil
}
