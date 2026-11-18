package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The launcher passes its arguments unchanged after `gui`, quoted so that
// the started program's parser (Go's os.Args) gives them back exactly.
func TestCommandLine(t *testing.T) {
	exe := `C:\Program Files\Rechnungsarchiv\rechnungsarchiv.exe`
	q := `"` + exe + `" gui`
	for _, c := range []struct {
		args []string
		want string
	}{
		{nil, q},
		{[]string{`D:\Scans\2026-11 Eingangsrechnungen`}, q + ` "D:\Scans\2026-11 Eingangsrechnungen"`},
		{[]string{`D:\Scans`}, q + ` D:\Scans`},
		// A trailing backslash before the closing quote is doubled.
		{[]string{`D:\Neuer Ordner\`}, q + ` "D:\Neuer Ordner\\"`},
		{[]string{`a"b`, ""}, q + ` a\"b ""`},
	} {
		if got := commandLine(exe, c.args); got != c.want {
			t.Errorf("commandLine(%q)\n got %s\nwant %s", c.args, got, c.want)
		}
	}
}

// The main program is rechnungsarchiv.exe in the launcher's own folder,
// never one found on PATH or in the working folder.
func TestTarget(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "rechnungsarchiv-gui.exe")
	if _, err := target(self); err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("missing main program: %v", err)
	}
	os.Mkdir(filepath.Join(dir, mainExe), 0o755) // a folder is not the program
	if _, err := target(self); err == nil {
		t.Fatal("a folder named rechnungsarchiv.exe was accepted")
	}
	other := t.TempDir()
	main := filepath.Join(other, mainExe)
	os.WriteFile(main, []byte("MZ"), 0o755)
	if got, err := target(filepath.Join(other, "rechnungsarchiv-gui.exe")); err != nil || got != main {
		t.Fatalf("target = %q, %v", got, err)
	}
}

// Round trip: the started process sees exactly `gui <args…>`. The test
// binary plays the main program: flag parsing stops at "gui", every test
// runs, and TestEchoArgs prints os.Args and exits.
func TestStartPassesArguments(t *testing.T) {
	if os.Getenv("LAUNCH_ECHO") != "" {
		return
	}
	args := []string{`D:\Scans\2026-11 Eingangsrechnungen\`, `C:\x y\"z"`, "ä ö", "", `\\server\share\a b`, "--json"}
	cmd := exec.Command(os.Args[0])
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: commandLine(os.Args[0], args)}
	cmd.Env = append(os.Environ(), "LAUNCH_ECHO=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out[:strings.IndexByte(string(out), '\n')], &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	want := append([]string{"gui"}, args...)
	if !slices.Equal(got, want) {
		t.Fatalf("started with %q, want %q", got, want)
	}
}

// TestEchoArgs is the stand-in main program of TestStartPassesArguments.
func TestEchoArgs(t *testing.T) {
	if os.Getenv("LAUNCH_ECHO") == "" {
		t.Skip("helper process")
	}
	data, _ := json.Marshal(os.Args[1:])
	os.Stdout.Write(append(data, '\n'))
	os.Exit(0)
}

// The installer runs the main exe for its hooks and waits for it; the
// launcher must not open the window then.
func TestInstallerHooksExit(t *testing.T) {
	for _, args := range [][]string{{"--veloapp-install", "1.0.0"}, {"--veloapp-updated", "1.0.1"}, {"--veloapp-obsolete"}, {"--veloapp-uninstall"}} {
		if !installerHook(args) {
			t.Errorf("%v not recognised", args)
		}
	}
	for _, args := range [][]string{nil, {`D:\Rechnungen`}, {"--help"}, {"x", "--veloapp-install"}} {
		if installerHook(args) {
			t.Errorf("%v taken for a hook", args)
		}
	}
}
