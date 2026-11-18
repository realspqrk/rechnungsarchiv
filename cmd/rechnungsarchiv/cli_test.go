package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
)

// The CLI is tested end to end: TestMain builds the real binary (with the
// testtsa tag when the tests run with it) and every test runs it as a
// separate process, without a terminal, so the output is JSON.

var (
	bin      string
	testTags string // "testtsa" in builds with that tag (e2e_test.go)
)

const demoIncoming = "2026-10 Eingangsrechnungen"

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rechnungsarchiv-cli-")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "rechnungsarchiv.exe")
	args := []string{"build", "-o", bin, "-ldflags", "-X github.com/realspqrk/rechnungsarchiv/internal/archive.Version=9.9.9-test"}
	if testTags != "" {
		args = append(args, "-tags", testTags)
	}
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), append(args, ".")...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// world is one isolated user: own settings folder, own working folder.
type world struct {
	t   *testing.T
	cwd string
	env []string
}

func newWorld(t *testing.T) *world {
	root := t.TempDir()
	w := &world{t: t, cwd: filepath.Join(root, "work")}
	os.MkdirAll(w.cwd, 0o755)
	cfg := filepath.Join(root, "appdata")
	w.env = append(os.Environ(), "APPDATA="+cfg, "XDG_CONFIG_HOME="+cfg)
	return w
}

type result struct {
	code           int
	stdout, stderr string
}

func (w *world) run(stdin string, args ...string) result {
	w.t.Helper()
	return w.runIn(w.cwd, bin, stdin, args...)
}

func (w *world) runIn(dir, exe, stdin string, args ...string) result {
	w.t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir, cmd.Env = dir, w.env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := result{stdout: out.String(), stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		r.code = ee.ExitCode()
	case err != nil:
		w.t.Fatal(err)
	}
	return r
}

// want checks the exit code and returns the decoded stdout.
func (r result) want(t *testing.T, code int) map[string]any {
	t.Helper()
	if r.code != code {
		t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
	m := map[string]any{}
	if strings.TrimSpace(r.stdout) != "" {
		if err := json.Unmarshal([]byte(r.stdout), &m); err != nil {
			t.Fatalf("stdout is not one JSON object: %v\n%s", err, r.stdout)
		}
	}
	return m
}

// errObj returns the error object on stderr and checks its shape.
func (r result) errObj(t *testing.T, code string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(r.stderr), "\n") {
		var m map[string]map[string]any
		if json.Unmarshal([]byte(line), &m) != nil || m["error"] == nil {
			continue
		}
		e := m["error"]
		if e["code"] != code || e["message"] == "" {
			t.Fatalf("error %v, want code %s", e, code)
		}
		for k := range e {
			if k != "code" && k != "message" && k != "hint" && k != "details" {
				t.Fatalf("unexpected key %q in error object", k)
			}
		}
		return e
	}
	t.Fatalf("no error object on stderr: %q", r.stderr)
	return nil
}

func get(m any, path ...string) any {
	for _, p := range path {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[p]
	}
	return m
}

func demo(name string) string {
	abs, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "demo", name))
	return abs
}

// tree lists every path below dir with its size.
func tree(t *testing.T, dir string) string {
	var b strings.Builder
	filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, _ := de.Info()
		fmt.Fprintf(&b, "%s %d\n", p, fi.Size())
		return nil
	})
	return b.String()
}

func TestHelpSchemaVersion(t *testing.T) {
	w := newWorld(t)
	help := w.run("", "--help").want(t, 0)["help"].(string)
	if !strings.HasSuffix(help, agentRules) || !strings.Contains(help, "status --json") {
		t.Fatalf("help does not end with the agent rules:\n%s", help)
	}
	for _, c := range commands {
		args := append(strings.Fields(c.Name), "--help")
		h := w.run("", args...).want(t, 0)["help"].(string)
		if !strings.HasPrefix(h, "rechnungsarchiv "+c.Name) || !strings.HasSuffix(h, agentRules) {
			t.Fatalf("%s --help:\n%s", c.Name, h)
		}
	}
	if h := w.run("", "help", "provider", "set").want(t, 0)["help"].(string); !strings.Contains(h, "--p12") {
		t.Fatalf("help provider set:\n%s", h)
	}
	if v := w.run("", "--version").want(t, 0)["version"]; v != "9.9.9-test" {
		t.Fatalf("version %v", v)
	}

	s := w.run("", "schema", "--json").want(t, 0)
	var names []string
	for _, c := range s["commands"].([]any) {
		names = append(names, c.(map[string]any)["name"].(string))
	}
	for _, n := range []string{"status", "init", "archive", "stamp", "verify", "export", "providers", "provider set", "provider show", "schema", "protocol"} {
		if !strings.Contains(" "+strings.Join(names, " ; ")+" ", n) {
			t.Fatalf("schema lacks %s: %v", n, names)
		}
	}
	if codes := s["exit_codes"].([]any); len(codes) != 9 {
		t.Fatalf("exit codes %v", codes)
	}
	if !json.Valid([]byte(w.run("", "schema").stdout)) {
		t.Fatal("schema without --json is not JSON")
	}
}

func TestProvidersHaveNoPrices(t *testing.T) {
	w := newWorld(t)
	r := w.run("", "providers")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	var ps []map[string]any
	if err := json.Unmarshal([]byte(r.stdout), &ps); err != nil || len(ps) < 5 {
		t.Fatalf("providers %v: %s", err, r.stdout)
	}
	allowed := map[string]bool{"id": true, "name": true, "country": true, "auth": true, "url": true, "url_from_contract": true, "trusted_list": true}
	for _, p := range ps {
		for k := range p {
			if !allowed[k] {
				t.Fatalf("unexpected key %q", k)
			}
		}
	}
	low := strings.ToLower(r.stdout)
	for _, bad := range []string{"price", "preis", "cost", "kosten", "eur", "\\u20ac", "€"} {
		if strings.Contains(low, bad) {
			t.Fatalf("providers output mentions %q", bad)
		}
	}
}

func TestUsageErrorsNeverEchoSecrets(t *testing.T) {
	w := newWorld(t)
	const secret = "Sehr-Geheim-4711"
	for _, args := range [][]string{
		{"provider", "set", "mensatek", "--user", "x", "--password=" + secret},
		{"provider", "set", "mensatek", "--user", "x", "--passwort", secret},
		{"provider", "set", "mensatek", "--user", "x", secret},
		{"provider", "set", "mensatek", secret, "--user", "x"},
		{"archive", "ordner", "--pin", secret},
		{"status", "--" + secret},
	} {
		r := w.run("", args...)
		r.want(t, 2)
		r.errObj(t, "USAGE")
		if strings.Contains(r.stdout+r.stderr, secret) {
			t.Fatalf("%v echoes the secret: %s", args[0], r.stderr)
		}
	}
	r := w.run("", "gibtsnicht")
	if r.want(t, 2); r.stdout != "" {
		t.Fatalf("stdout on error: %q", r.stdout)
	}
	r.errObj(t, "USAGE")
	w.run("", "archive", "a", "b").errObj(t, "USAGE")
	w.run("", "export", "ziel").errObj(t, "USAGE")
	w.run("", "init", "x").errObj(t, "USAGE") // --firma missing
	w.run("", "archive", "--belegjahr", "zwei", "x").errObj(t, "USAGE")
}

func TestNoArchive(t *testing.T) {
	w := newWorld(t)
	r := w.run("", "status")
	r.want(t, 3)
	if h := r.errObj(t, "NO_ARCHIVE")["hint"].(string); !strings.Contains(h, "init") {
		t.Fatalf("hint %q", h)
	}
	w.run("", "verify", "--archiv", w.cwd).errObj(t, "NO_ARCHIVE")
	r = w.run("", "verify", filepath.Join(w.cwd, "fehlt.zip"))
	r.want(t, 2)
	r.errObj(t, "NOT_FOUND")
}

// Without a provider: init, dry run, consent, lock contention, takeover,
// and an archive whose timestamp stays pending.
func TestArchiveConsentAndLock(t *testing.T) {
	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "Mein Archiv")
	init := w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)
	if get(init, "archive", "path") != dir || get(init, "default") != true {
		t.Fatalf("init %v", init)
	}
	w.run("", "init", dir, "--firma", "X").errObj(t, "ARCHIVE_EXISTS")

	// status finds the remembered archive from another folder.
	st := w.run("", "status").want(t, 0)
	if get(st, "archive", "source") != "default" || get(st, "provider") != nil || get(st, "deposits", "total") != 0.0 {
		t.Fatalf("status %v", st)
	}
	if st := w.runIn(dir, bin, "", "status").want(t, 0); get(st, "archive", "source") != "cwd" {
		t.Fatalf("status in archive %v", st)
	}

	before := tree(t, dir)
	plan := w.run("", "archive", "--dry-run", demo(demoIncoming)).want(t, 0)
	if plan["dry_run"] != true || plan["count"] != 9.0 || plan["sequence"] != 1.0 || plan["belegjahr"] != 2026.0 ||
		plan["file"] != "0001_2026-10 Eingangsrechnungen.zip" || len(plan["notes"].([]any)) == 0 {
		t.Fatalf("plan %v", plan)
	}
	r := w.run("", "archive", demo(demoIncoming))
	r.want(t, 8)
	if d := r.errObj(t, "CONSENT_REQUIRED")["details"]; get(d, "action") != "archive" {
		t.Fatalf("details %v", d)
	}
	if tree(t, dir) != before {
		t.Fatal("dry run or refused archive wrote into the archive")
	}

	// A foreign PC's lock: exit 4 with host and time, never taken silently.
	foreign := `{"host":"anderer-pc","pid":4242,"since":"2026-11-18T08:00:00Z","user":"anna"}`
	os.WriteFile(filepath.Join(dir, ".lock"), []byte(foreign), 0o644)
	r = w.run("", "archive", demo(demoIncoming), "--yes")
	r.want(t, 4)
	if d := r.errObj(t, "LOCKED")["details"]; get(d, "host") != "anderer-pc" || get(d, "since") != "2026-11-18T08:00:00Z" {
		t.Fatalf("details %v", d)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, ".lock")); string(b) != foreign {
		t.Fatal("foreign lock touched")
	}
	w.run("", "archive", demo(demoIncoming), "--take-over").errObj(t, "USAGE")
	if st := w.run("", "status").want(t, 0); get(st, "lock", "host") != "anderer-pc" {
		t.Fatalf("status lock %v", st["lock"])
	}

	// After the user confirmed: take over, commit; no provider → exit 3.
	r = w.run("", "archive", demo(demoIncoming), "--yes", "--take-over")
	out := r.want(t, 3)
	r.errObj(t, "NO_PROVIDER")
	if out["committed"] != true || get(out, "deposit", "sequence") != 1.0 || get(out, "deposit", "count") != 9.0 {
		t.Fatalf("archive %v", out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lock left behind")
	}
	st = w.run("", "status").want(t, 0)
	if get(st, "deposits", "pending") != 1.0 || get(st, "pending").([]any)[0].(map[string]any)["reason"] != "kein-dienst" {
		t.Fatalf("status %v", st)
	}
	rep := w.run("", "verify").want(t, 6)
	if rep["status"] != "pending" || rep["mode"] != "archive" {
		t.Fatalf("verify %v", rep)
	}

	// Another process holds the writer lock: exit 4 with its host and pid.
	ar, err := archive.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	release, err := ar.Lock()
	if err != nil {
		t.Fatal(err)
	}
	r = w.run("", "stamp", "--archiv", dir)
	r.want(t, 4)
	host, _ := os.Hostname()
	if d := r.errObj(t, "LOCKED")["details"]; get(d, "host") != host || get(d, "pid") != float64(os.Getpid()) {
		t.Fatalf("details %v", d)
	}
	// A live writer's lock cannot be taken over, even when asked to.
	w.run("", "stamp", "--yes", "--take-over").want(t, 4)
	release()
	r = w.run("", "stamp")
	r.want(t, 3)
	r.errObj(t, "NO_PROVIDER")

	// The Archivprotokoll never goes into the archive folder.
	w.run("", "protocol", "1", "--out", filepath.Join(dir, "p.pdf")).errObj(t, "USAGE")
	pr := w.run("", "protocol", "1").want(t, 0)
	if pr["file"] != filepath.Join(w.cwd, "0001_2026-10 Eingangsrechnungen.archivprotokoll.pdf") || pr["status"] != "pending" {
		t.Fatalf("protocol %v", pr)
	}
	w.run("", "protocol", "1").errObj(t, "EXISTS")
	w.run("", "protocol", "7").errObj(t, "NOT_FOUND")
}

// The human output (a terminal) is German and renders the same results.
func TestHumanOutput(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	dir := filepath.Join(t.TempDir(), "Archiv")
	human := func(code int, args ...string) string {
		t.Helper()
		var out, errb bytes.Buffer
		got := run(context.Background(), args, ioEnv{stdin: strings.NewReader(""), stdout: &out, stderr: &errb, stdoutTTY: true})
		if got != code {
			t.Fatalf("%v: exit %d, want %d\n%s%s", args, got, code, out.String(), errb.String())
		}
		return out.String() + errb.String()
	}
	for _, c := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"providers"}, 0, "Benutzer/Passwort"},
		{[]string{"init", dir, "--firma", "Musterfirma GmbH"}, 0, "Archiv angelegt"},
		{[]string{"status"}, 0, "keiner eingerichtet"},
		{[]string{"archive", "--dry-run", demo(demoIncoming)}, 0, "Probelauf"},
		{[]string{"archive", demo(demoIncoming)}, 8, "Fehler: archive braucht die Zustimmung"},
		{[]string{"provider", "show"}, 0, "keiner eingerichtet"},
		{[]string{"verify"}, 0, "Ergebnis: in Ordnung (0 Ablagen"},
		{[]string{"gibtsnicht"}, 2, "Fehler: unbekannter Befehl"},
	} {
		if out := human(c.code, c.args...); !strings.Contains(out, c.want) || strings.Contains(out, `{"`) {
			t.Fatalf("%v:\n%s", c.args, out)
		}
	}
}

// Workflow simulation W2/W3: the consent hint fits the action (export has
// no --dry-run), and stamp's --yes is only needed with --take-over.
func TestConsentHintPerAction(t *testing.T) {
	w := newWorld(t)
	dir := filepath.Join(t.TempDir(), "Archiv")
	w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)

	r := w.run("", "archive", demo(demoIncoming))
	r.want(t, 8)
	if h, _ := r.errObj(t, "CONSENT_REQUIRED")["hint"].(string); !strings.Contains(h, "archive --dry-run") {
		t.Errorf("archive hint %q", h)
	}
	r = w.run("", "export", "--von", "2026", "--bis", "2026", filepath.Join(t.TempDir(), "Export"))
	r.want(t, 8)
	if h, _ := r.errObj(t, "CONSENT_REQUIRED")["hint"].(string); strings.Contains(h, "dry-run") || !strings.Contains(h, "Belegjahre und Ziel") {
		t.Errorf("export hint %q", h)
	}

	for _, c := range commands {
		for _, f := range c.Flags {
			if f.Name != "yes" {
				continue
			}
			takeOverOnly := strings.Contains(f.Help, "nur zusammen mit --take-over")
			if c.Consent == takeOverOnly {
				t.Errorf("%s --yes: %q (consent %v)", c.Name, f.Help, c.Consent)
			}
		}
	}
}
