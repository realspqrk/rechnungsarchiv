package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
)

// link makes alias lead to target: a junction on Windows (no admin rights
// needed), a symbolic link elsewhere.
func link(t *testing.T, alias, target string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", alias, target).CombinedOutput(); err != nil {
			t.Fatalf("mklink: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
}

// inProcess runs the command line in this process with a terminal as
// stdout (or not) and returns exit code, stdout and stderr.
func inProcess(t *testing.T, tty bool, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, ioEnv{stdin: strings.NewReader(""), stdout: &out, stderr: &errb, stdoutTTY: tty})
	return code, out.String(), errb.String()
}

// pendingArchive is an archive (no provider) with Ablage 1 committed.
func pendingArchive(t *testing.T, w *world) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Archiv")
	w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)
	w.run("", "archive", demo(demoIncoming), "--yes").want(t, 3)
	return dir
}

// Review 3 finding 1: the Archivprotokoll never enters the archive, not
// through a junction to it, and not when the archive folder itself is
// given as <ablage>.
func TestProtocolNeverIntoArchive(t *testing.T) {
	w := newWorld(t)
	dir := pendingArchive(t, w)
	zip := filepath.Join(dir, "0001_"+demoIncoming+".zip")
	alias := filepath.Join(t.TempDir(), "alias")
	link(t, alias, dir)
	before := tree(t, dir)

	for _, args := range [][]string{
		{"protocol", "1", "--out", filepath.Join(alias, "p.pdf")},
		{"protocol", "1", "--out", alias},
		{"protocol", zip, "--out", filepath.Join(alias, "p.pdf")},
		{"protocol", filepath.Join(alias, filepath.Base(zip)), "--out", filepath.Join(alias, "p.pdf")},
		{"protocol", filepath.Join(alias, filepath.Base(zip)), "--out", filepath.Join(dir, "p.pdf")},
		{"protocol", dir, "--out", filepath.Join(dir, "p.pdf")},
		{"protocol", dir, "--out", filepath.Join(t.TempDir(), "p.pdf")},
		{"protocol", strings.TrimSuffix(zip, ".zip") + ".nachweis", "--out", filepath.Join(t.TempDir(), "p.pdf")},
	} {
		r := w.run("", args...)
		r.want(t, 2)
		r.errObj(t, "USAGE")
	}
	if tree(t, dir) != before {
		t.Fatalf("protocol wrote into the archive:\n%s", tree(t, dir))
	}

	// The destination is checked on its own: an input outside any archive
	// (a copy of the zip) or inside another archive must not let the PDF
	// into an archive.
	dir2 := pendingArchive(t, w)
	before2 := tree(t, dir2)
	data, err := os.ReadFile(zip)
	if err != nil {
		t.Fatal(err)
	}
	ext := filepath.Join(t.TempDir(), filepath.Base(zip))
	if err := os.WriteFile(ext, data, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"protocol", ext, "--out", filepath.Join(dir, "inside.pdf")},
		{"protocol", ext, "--out", filepath.Join(alias, "inside.pdf")},
		{"protocol", ext, "--out", filepath.Join(dir, "sub", "inside.pdf")},
		{"protocol", zip, "--out", filepath.Join(dir2, "inside.pdf")},
	} {
		r := w.run("", args...)
		r.want(t, 2)
		r.errObj(t, "USAGE")
	}
	if tree(t, dir) != before || tree(t, dir2) != before2 {
		t.Fatalf("protocol wrote into an archive:\n%s\n%s", tree(t, dir), tree(t, dir2))
	}

	// The documented inputs still work.
	out := filepath.Join(t.TempDir(), "p.pdf")
	if pr := w.run("", "protocol", zip, "--out", out).want(t, 0); pr["file"] != out {
		t.Fatalf("protocol %v", pr)
	}
	out = filepath.Join(t.TempDir(), "q.pdf")
	if pr := w.run("", "protocol", "0001", "--out", out).want(t, 0); pr["file"] != out {
		t.Fatalf("protocol %v", pr)
	}
}

// Review 3 finding 3: the follow-up command of a dry run repeats the
// resolved archive and every chosen option, quoted for PowerShell, even
// when the remembered default archive is another one.
func TestDryRunFollowUpCommand(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv(archive.ConfigDirEnv, cfg)
	chosen := filepath.Join(t.TempDir(), "O'Brien Archiv")
	other := filepath.Join(t.TempDir(), "Standard")
	for _, d := range []string{chosen, other} { // other becomes the default
		if code, _, e := inProcess(t, true, "init", d, "--firma", "X GmbH"); code != 0 {
			t.Fatal(e)
		}
	}
	code, out, e := inProcess(t, true, "archive", "--dry-run", demo(demoIncoming), "--archiv", chosen,
		"--name", "Kopie d'Ablage", "--belegjahr", "2025", "--aufbewahrung", "10")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, out, e)
	}
	want := "rechnungsarchiv archive '" + demo(demoIncoming) + "' --archiv '" + strings.ReplaceAll(chosen, "'", "''") +
		"' --name 'Kopie d''Ablage' --belegjahr 2025 --aufbewahrung 10 --yes"
	if !strings.Contains(out, want) {
		t.Fatalf("follow-up command missing:\nwant %s\n%s", want, out)
	}
}

// Review 3 finding 4: an unreadable Ablage is a failed outcome of stamp
// (exit 5), not "nothing missing".
func TestStampUnreadableDeposit(t *testing.T) {
	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "Archiv")
	w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)
	os.WriteFile(filepath.Join(dir, "0001_broken.zip"), []byte("kein zip"), 0o644)
	r := w.run("", "stamp")
	out := r.want(t, 5)
	outs, _ := out["outcomes"].([]any)
	if len(outs) != 1 || get(outs[0], "stamped") != false || get(outs[0], "sequence") != 1.0 {
		t.Fatalf("stamp %v", out)
	}
	if !strings.Contains(r.stderr, `"warning"`) || !strings.Contains(r.stderr, "0001") {
		t.Fatalf("stderr %s", r.stderr)
	}
	code, text, e := inProcess(t, true, "stamp", "--archiv", dir)
	if code != 5 || strings.Contains(text, "Keine fehlenden") || !strings.Contains(text, "nicht lesbar") {
		t.Fatalf("exit %d:\n%s%s", code, text, e)
	}
}

// Review 3 finding 5: a broken rechnungsarchiv.json is a configuration
// error (exit 3), and provider set does not rewrite it.
func TestBadConfigExit3(t *testing.T) {
	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "Archiv")
	w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)
	p := filepath.Join(dir, "rechnungsarchiv.json")
	good, _ := os.ReadFile(p)
	bad := append(append([]byte{}, good...), []byte(`{"tsa":"x"}`)...)
	os.WriteFile(p, bad, 0o644)
	for _, args := range [][]string{{"status"}, {"verify"}, {"provider", "show"}} {
		r := w.run("", args...)
		r.want(t, 3)
		r.errObj(t, "CONFIG")
	}
	w.run("geheim\n", "provider", "set", "mensatek", "--user", "x").want(t, 3)
	if b, _ := os.ReadFile(p); !bytes.Equal(b, bad) {
		t.Fatalf("bad config rewritten: %s", b)
	}
}

// Review 3 finding 6: --json=true and --json=false decide the mode.
func TestJSONBooleanSyntax(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv(archive.ConfigDirEnv, cfg)
	isJSON := func(s string) bool { return json.Valid([]byte(strings.TrimSpace(s))) }
	for _, c := range []struct {
		tty  bool
		args []string
		json bool
	}{
		{true, []string{"providers", "--json=true"}, true},
		{true, []string{"--json=true", "providers"}, true},
		{true, []string{"providers", "--json"}, true},
		{true, []string{"providers", "--json=false"}, false},
		{true, []string{"providers"}, false},
		{false, []string{"providers", "--json=false"}, true}, // no terminal: always JSON
	} {
		code, out, e := inProcess(t, c.tty, c.args...)
		if code != 0 || isJSON(out) != c.json {
			t.Fatalf("%v (tty %v): exit %d json %v\n%s%s", c.args, c.tty, code, isJSON(out), out, e)
		}
	}
	// Errors: JSON with --json=true, and whenever stdout is no terminal.
	for _, c := range []struct {
		tty  bool
		args []string
		json bool
	}{
		{true, []string{"gibtsnicht", "--json=true"}, true},
		{true, []string{"status", "--json=true"}, true}, // no archive
		{true, []string{"status", "--json=false"}, false},
		{false, []string{"providers", "--json=vielleicht"}, true},
		{false, []string{"gibtsnicht"}, true},
	} {
		code, out, e := inProcess(t, c.tty, c.args...)
		if code == 0 || out != "" || isJSON(e) != c.json {
			t.Fatalf("%v (tty %v): exit %d\n%s%s", c.args, c.tty, code, out, e)
		}
	}
}

// The help alias honours --json like every command, and the flag does not
// disturb the command lookup.
func TestHelpAliasJSONFlag(t *testing.T) {
	for _, c := range []struct {
		args []string
		cmd  string
	}{
		{[]string{"help", "status", "--json"}, "status"},
		{[]string{"help", "provider", "set", "--json=true"}, "provider set"},
		{[]string{"help", "--json", "provider", "set"}, "provider set"},
	} {
		code, out, e := inProcess(t, true, c.args...)
		var got map[string]string
		if code != 0 || json.Unmarshal([]byte(out), &got) != nil {
			t.Fatalf("%v: exit %d\n%s%s", c.args, code, out, e)
		}
		if want := helpText(find(c.cmd)); got["help"] != want {
			t.Fatalf("%v: wrong help:\n%s", c.args, got["help"])
		}
	}
	// --json=false stays human in a terminal.
	code, out, _ := inProcess(t, true, "help", "status", "--json=false")
	if code != 0 || json.Valid([]byte(strings.TrimSpace(out))) {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

// Review 3 finding 7: help and docs agree on provider set … --url.
func TestProviderSetURLDocumented(t *testing.T) {
	w := newWorld(t)
	h := w.run("", "help", "provider", "set").want(t, 0)["help"].(string)
	if !strings.Contains(h, "--url") || !strings.Contains(h, "Disig") {
		t.Fatalf("help provider set:\n%s", h)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `provider set disig --p12 "`) || !strings.Contains(string(doc), `--url "https://`) {
		t.Fatal("docs/cli.md lacks the Disig recipe with --url")
	}
}
