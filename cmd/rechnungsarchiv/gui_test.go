package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gui "github.com/realspqrk/rechnungsarchiv/app"
)

// A run without arguments opens the window only when the process is alone
// in a console of its own (Explorer, Start menu); in a terminal, or with
// redirected output, it is the CLI and prints help. Any argument is the CLI
// (`gui` is a command of the CLI).
func TestOpensWindow(t *testing.T) {
	for _, c := range []struct {
		args  []string
		alone bool
		want  bool
	}{
		{nil, true, true},
		{nil, false, false},
		{[]string{"gui"}, true, false},
		{[]string{"status"}, true, false},
		{[]string{"--help"}, true, false},
		{[]string{"--json"}, true, false},
	} {
		probed := false
		got := opensWindow(c.args, func() bool { probed = true; return c.alone })
		if got != c.want {
			t.Errorf("opensWindow(%q, alone=%v) = %v", c.args, c.alone, got)
		}
		if len(c.args) > 0 && probed {
			t.Errorf("opensWindow(%q) probed the console", c.args)
		}
	}
}

// stubGUI replaces the window and the console for one test and returns the
// options of every window opened.
func stubGUI(t *testing.T, alone bool) (*[]gui.Options, *[]string, *int) {
	t.Helper()
	var opened []gui.Options
	var alerts []string
	freed := 0
	oldOpen, oldAlone, oldFree, oldAlert := openGUI, consoleAlone, freeConsole, alert
	openGUI = func(o gui.Options) error { opened = append(opened, o); return nil }
	consoleAlone = func() bool { return alone }
	freeConsole = func() { freed++ }
	alert = func(msg string) { alerts = append(alerts, msg) }
	t.Cleanup(func() { openGUI, consoleAlone, freeConsole, alert = oldOpen, oldAlone, oldFree, oldAlert })
	return &opened, &alerts, &freed
}

func TestGUICommand(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	dir := t.TempDir()
	file := filepath.Join(dir, "rechnung.pdf")
	os.WriteFile(file, []byte("%PDF"), 0o644)

	// gui <ordner>: the absolute folder; from a terminal the console stays.
	opened, alerts, freed := stubGUI(t, false)
	t.Chdir(filepath.Dir(dir))
	if code, _, stderr := inProcess(t, true, "gui", filepath.Base(dir)); code != 0 {
		t.Fatalf("gui <ordner>: exit %d %s", code, stderr)
	}
	if len(*opened) != 1 || (*opened)[0].Folder != dir || *freed != 0 {
		t.Fatalf("gui <ordner>: opened %+v, freed %d", *opened, *freed)
	}
	// A later launch while the window runs: its folder, relative to its
	// working folder.
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	f := (*opened)[0].SecondLaunch
	if f == nil || f([]string{"x.exe", "gui", "sub"}, dir) != sub || f([]string{"x.exe", "gui", "rechnung.pdf"}, dir) != "" || f([]string{"x.exe"}, dir) != "" {
		t.Fatalf("second launch folder not resolved")
	}

	// gui without a folder, alone in its console (launcher, shortcut): the
	// console is released first. Output is not a terminal there.
	opened, _, freed = stubGUI(t, true)
	if code, _, stderr := inProcess(t, false, "gui"); code != 0 || len(*opened) != 1 || (*opened)[0].Folder != "" || *freed != 1 {
		t.Fatalf("gui: exit %d %s, opened %+v, freed %d", code, stderr, *opened, *freed)
	}

	// Errors: no window. Alone in a console, nobody reads stderr, so a
	// message box says it.
	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"gui", filepath.Join(dir, "fehlt")}, "NOT_FOUND"},
		{[]string{"gui", file}, "USAGE"},
		{[]string{"gui", dir, dir}, "USAGE"},
		{[]string{"gui", "--json"}, "USAGE"},
		{[]string{"gui", "--archiv", dir}, "USAGE"},
	} {
		opened, alerts, _ = stubGUI(t, true)
		code, _, stderr := inProcess(t, false, c.args...)
		if code != exitUsage || !strings.Contains(stderr, `"code":"`+c.code+`"`) || len(*opened) != 0 {
			t.Errorf("%q: exit %d %s, opened %v", c.args, code, stderr, *opened)
		}
		if len(*alerts) != 1 || (*alerts)[0] == "" {
			t.Errorf("%q: alerts %q", c.args, *alerts)
		}
	}
	// From a terminal the error goes to stderr only.
	_, alerts, _ = stubGUI(t, false)
	if code, _, _ := inProcess(t, true, "gui", file); code != exitUsage || len(*alerts) != 0 {
		t.Errorf("terminal error: exit %d, alerts %q", code, *alerts)
	}

	// gui --help is help, not a window.
	opened, _, _ = stubGUI(t, false)
	if code, out, _ := inProcess(t, true, "gui", "--help"); code != 0 || !strings.HasPrefix(out, "rechnungsarchiv gui") || len(*opened) != 0 {
		t.Errorf("gui --help: exit %d, opened %v:\n%s", code, *opened, out)
	}
}

// The binary without arguments, run by a script (output is a pipe), is the
// CLI: it prints help and opens no window.
func TestNoArgumentsWithPipeIsHelp(t *testing.T) {
	w := newWorld(t)
	help := w.run("").want(t, 0)["help"].(string)
	if !strings.Contains(help, "rechnungsarchiv gui") {
		t.Fatalf("help lacks gui:\n%s", help)
	}
}

// --help is the manual (ADR-0008): typical workflow first, then the
// commands, the exit codes and the agent rules.
func TestHelpIsTheManual(t *testing.T) {
	h := helpText(nil)
	order := []string{"Typischer Ablauf:", " init ", " provider set ", " archive --dry-run ", " archive ", " verify", " export ", " gui", "Befehle:", "Exit-Codes:", agentRules}
	at := 0
	for _, s := range order {
		i := strings.Index(h[at:], s)
		if i < 0 {
			t.Fatalf("help: %q missing after offset %d:\n%s", s, at, h)
		}
		at += i + len(s)
	}
	if find("gui") == nil || !strings.Contains(h, "\n  gui [<ordner>]") {
		t.Fatalf("gui not in the command list:\n%s", h)
	}
}

// sha is the SHA-256 of a file, hex.
func sha(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
