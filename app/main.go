// Command app is the Rechnungsarchiv desktop GUI (Wails v3), a thin shell
// over the core. Build with `wails3 build` in this directory (README.md).
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"

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
	application.RegisterEvent[FolderDrop]("ordner-abgelegt")
	application.RegisterEvent[StatusChanged]("status-geaendert")
}

func main() {
	// The real core is the default; RA_FAKE=<scenario> shows sample data
	// (screenshots, UI review). See README.md.
	shell := &Shell{debug: sys.debug}
	var real *realService
	if scenario := os.Getenv("RA_FAKE"); scenario != "" {
		f, err := newFakeService(scenario)
		if err != nil {
			log.Fatal(err)
		}
		shell.svc, shell.fake, shell.debug = f, f.scenario, true
	} else {
		real = newRealService(sys)
		shell.svc = real
	}

	app := application.New(application.Options{
		Name:        "Rechnungsarchiv",
		Description: "Lokales Archiv für Rechnungen mit qualifiziertem Zeitstempel",
		Services:    []application.Service{application.NewService(shell)},
		Assets:      application.AssetOptions{Handler: assetHandler()},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "at.rechnungsarchiv.gui",
		},
	})
	shell.app = app
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
	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            title,
		Width:            1280,
		Height:           860,
		MinWidth:         960,
		MinHeight:        640,
		BackgroundColour: application.NewRGB(0xF4, 0xF5, 0xF2),
		EnableFileDrop:   true,
		URL:              "/",
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		app.Event.Emit("ordner-abgelegt", handleDrop(e.Context().DroppedFiles()))
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func assetHandler() http.Handler {
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	_, statErr := fs.Stat(dist, "index.html")
	if statErr != nil && os.Getenv("FRONTEND_DEVSERVER_URL") == "" { // wails3 dev proxies to Vite
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(notBuiltPage))
		})
	}
	return application.AssetFileServerFS(dist)
}
