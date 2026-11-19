package main

// One binary for CLI and GUI (ADR-0008): `rechnungsarchiv gui [<ordner>]`
// opens the window; so does a run without arguments that was started from
// Explorer or the Start menu. Everything else is the CLI.

import (
	"os"
	"path/filepath"

	gui "github.com/realspqrk/rechnungsarchiv/app"
)

// Replaced by tests: the window and the console of this process
// (console_windows.go, console_other.go).
var (
	openGUI      = gui.Run
	consoleAlone = osConsoleAlone // this process is alone in its console
	freeConsole  = osFreeConsole  // detach from it (its window closes)
	alert        = osAlert        // message box for errors nobody else sees
)

// opensWindow reports whether a run with these arguments opens the window
// directly. Only a run without arguments whose process is alone in a
// console of its own does (started from Explorer or the Start menu). In a
// terminal the console is shared with the shell; scripts and agents
// redirect the output. Both get the CLI, which prints help.
func opensWindow(args []string, alone func() bool) bool {
	return len(args) == 0 && alone()
}

// startedAlone is the probe of opensWindow: alone in the console, and the
// output goes to that console (not to a pipe or file).
func startedAlone(stdoutTTY bool) func() bool {
	return func() bool { return stdoutTTY && consoleAlone() }
}

// gui runs the gui command: check the folder, release a console of our own
// (its window would stay open next to the GUI), open the window.
func (a *app) gui(inv *invocation) error {
	alone := consoleAlone() // launcher, shortcut or Explorer: nobody reads stderr
	fail := func(err error) error {
		if alone {
			alertError(err)
		}
		return err
	}
	if inv.flag("json") {
		return fail(usageErr("gui öffnet das Fenster und gibt keine Daten aus; --json passt nicht dazu.", "rechnungsarchiv gui --help"))
	}
	if inv.str("archiv") != "" {
		return fail(usageErr("gui öffnet das zuletzt verwendete Archiv; ein anderes wählen Sie im Fenster über den Firmennamen (Bestehendes Archiv öffnen).", "rechnungsarchiv gui --help"))
	}
	var opts gui.Options
	if len(inv.args) == 1 {
		dir, err := guiFolder(inv.args[0], "")
		if err != nil {
			return fail(err)
		}
		opts.Folder = dir
	}
	// A window already running gets opts.Folder (parsed and checked here,
	// exactly like on the first launch) and this process exits in Run.
	if alone {
		freeConsole()
	}
	if err := openGUI(opts); err != nil {
		return fail(err)
	}
	return nil
}

// guiFolder makes p absolute (relative to cwd, or the current folder if
// cwd is "") and checks that it is a folder.
func guiFolder(p, cwd string) (string, error) {
	if cwd != "" && !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err // NOT_FOUND
	}
	if !fi.IsDir() {
		return "", usageErr("gui: kein Ordner: "+abs, "Einen Ordner mit Rechnungen angeben, keine einzelne Datei.")
	}
	return abs, nil
}

// alertError shows err in a message box, for a gui run without a console.
func alertError(err error) {
	ce := classify(err)
	msg := ce.Message
	if ce.Hint != "" && ce.Code != "USAGE" {
		msg += "\n\n" + ce.Hint
	}
	alert(msg)
}
