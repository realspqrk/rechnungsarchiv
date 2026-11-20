//go:build testtsa

package main

import (
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
)

func init() { testTags = "testtsa" }

// tsaWorld is a world whose binary talks to the test TSA and trusts the
// synthetic trusted lists (testhook_testtsa.go).
func tsaWorld(t *testing.T) (*world, *testenv.Env) {
	e := testenv.New(t)
	lists := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req, _ := http.NewRequest(http.MethodGet, r.URL.Query().Get("u"), nil)
		resp, err := e.Lists.Web.RoundTrip(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}))
	t.Cleanup(lists.Close)
	w := newWorld(t)
	root := t.TempDir()
	cfg := testConfig{
		Provider:   e.Provider,
		TLSRoots:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.Server.Certificate().Raw})),
		LOTLPins:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.Lists.LOTLSigner.Raw})),
		ListsProxy: lists.URL,
		CredsFile:  filepath.Join(root, "creds.json"),
		Cache:      filepath.Join(root, "cache"),
	}
	data, _ := json.Marshal(cfg)
	p := filepath.Join(root, "testenv.json")
	os.WriteFile(p, data, 0o644)
	w.env = append(w.env, testEnvVar+"="+p)
	return w, e
}

// The README recipe and the auditor command, end to end.
func TestRecipeEndToEnd(t *testing.T) {
	w, e := tsaWorld(t)
	id := e.Provider.ID
	dir := filepath.Join(t.TempDir(), "Rechnungsarchiv")
	// Every stdout document below matches its command's published output
	// schema, every stderr line error_document or warning_document.
	check := schemaChecker(t, w, true)
	check("providers", w.run("", "providers"), 0, true)

	// rechnungsarchiv init "<dir>" --firma "Musterfirma GmbH"
	check("init", w.run("", "init", dir, "--firma", "Musterfirma GmbH"), 0, true)

	// rechnungsarchiv provider set <id> --user <name>, password on stdin.
	r := check("provider set", w.run(testenv.Password+"\r\n", "provider", "set", id, "--user", testenv.User), 0, true)
	ps := r.want(t, 0)
	if get(ps, "provider", "id") != id || get(ps, "credentials", "stored") != true {
		t.Fatalf("provider set %v", ps)
	}
	show := check("provider show", w.run("", "provider", "show"), 0, true)
	if sh := show.want(t, 0); get(sh, "credentials", "user") != testenv.User || get(sh, "credentials", "password") != true {
		t.Fatalf("provider show %v", sh)
	}
	for _, out := range []string{r.stdout, r.stderr, show.stdout, show.stderr} {
		if strings.Contains(out, testenv.Password) {
			t.Fatalf("password in output: %s", out)
		}
	}
	w.run("", "provider", "set", id, "--user", testenv.User).errObj(t, "SECRET_MISSING")
	// A provider not offered (not proven in real use) is refused before the
	// secret is read; the archive keeps its provider.
	if e := w.run("x\r\n", "provider", "set", "namirial", "--user", "u").errObj(t, "USAGE"); !strings.Contains(e["message"].(string), "nicht angeboten") {
		t.Fatalf("hidden provider %v", e)
	}
	// Also before its own argument checks (Disig wants --p12).
	if e := w.run("", "provider", "set", "disig", "--user", "u").errObj(t, "USAGE"); !strings.Contains(e["message"].(string), "nicht angeboten") {
		t.Fatalf("hidden provider with its own flags %v", e)
	}
	if sh := w.run("", "provider", "show").want(t, 0); get(sh, "provider", "id") != id {
		t.Fatalf("provider changed %v", sh)
	}
	w.run("x\n", "provider", "set", id, "--p12", "a.p12").errObj(t, "USAGE")

	st := check("status", w.run("", "status", "--json"), 0, true).want(t, 0)
	if get(st, "provider", "id") != id || st["credentials"] != true {
		t.Fatalf("status %v", st)
	}

	// archive --dry-run, then without --yes (exit 8), then with --yes.
	plan := check("archive", w.run("", "archive", "--dry-run", demo(testenv.Incoming)), 0, true).want(t, 0)
	if plan["provider"] != id || plan["count"] != 9.0 {
		t.Fatalf("plan %v", plan)
	}
	// Consent refused: no document, only the error.
	check("archive", w.run("", "archive", demo(testenv.Incoming)), 8, false)
	if zips, _ := filepath.Glob(filepath.Join(dir, "*.zip")); len(zips) != 0 {
		t.Fatalf("written without consent: %v", zips)
	}
	a := check("archive", w.run("", "archive", demo(testenv.Incoming), "--yes"), 0, true).want(t, 0)
	outs := get(a, "stamp", "outcomes").([]any)
	if a["committed"] != true || len(outs) != 1 || get(outs[0], "stamped") != true {
		t.Fatalf("archive %v", a)
	}

	// stamp: nothing missing. verify: timestamped.
	if s := check("stamp", w.run("", "stamp"), 0, true).want(t, 0); len(s["outcomes"].([]any)) != 0 {
		t.Fatalf("stamp %v", s)
	}
	rep := check("verify", w.run("", "verify", "--json"), 0, true).want(t, 0)
	d0 := rep["deposits"].([]any)[0]
	if rep["status"] != "timestamped" || get(d0, "proven", "at") == nil {
		t.Fatalf("verify %v", rep)
	}
	// Online: the lists just fetched make the stamp qualified.
	e.PublishLists(time.Now())
	if rep := check("verify", w.run("", "verify", dir, "--online"), 0, true).want(t, 0); get(rep["deposits"].([]any)[0], "qualification") != "qualified" {
		t.Fatalf("verify --online %v", rep["deposits"])
	}

	// A provider error after the commit: exit 7, the Ablage is stored.
	e.SetMode(testenv.Denied)
	// After the commit the receipt is printed despite the error.
	r = check("archive", w.run("", "archive", demo(testenv.Outgoing), "--yes"), 7, true)
	if out := r.want(t, 7); out["committed"] != true || get(out, "deposit", "sequence") != 2.0 {
		t.Fatalf("archive %v", out)
	}
	if d := r.errObj(t, "PROVIDER_ERROR")["details"]; get(d, "reason") != "zugang" {
		t.Fatalf("details %v", d)
	}
	check("verify", w.run("", "verify"), 6, true) // pending: still a report
	e.SetMode(testenv.Normal)
	if s := check("stamp", w.run("", "stamp"), 0, true).want(t, 0); len(s["outcomes"].([]any)) != 1 {
		t.Fatalf("stamp %v", s)
	}
	w.run("", "verify").want(t, 0)

	// Export: consent, then the auditor command in the export folder.
	dst := filepath.Join(t.TempDir(), "Export 2026")
	check("export", w.run("", "export", "--von", "2026", "--bis", "2026", dst), 8, false)
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("export written without consent")
	}
	ex := check("export", w.run("", "export", "--von", "2026", "--bis", "2026", dst, "--yes"), 0, true).want(t, 0)
	if len(ex["exported"].([]any)) != 2 || get(ex, "report", "status") != "timestamped" {
		t.Fatalf("export %v", ex)
	}
	// .\rechnungsarchiv.exe verify "<Export-Ordner>", run in the export folder.
	audit := check("verify", w.runIn(dst, filepath.Join(dst, export.ExeName), "", "verify", dst), 0, true).want(t, 0)
	if audit["mode"] != "export" || audit["status"] != "timestamped" {
		t.Fatalf("audit %v", audit)
	}
	w.runIn(dst, filepath.Join(dst, export.ExeName), "", "verify", filepath.Join(dst, "0001_2026-10 Eingangsrechnungen.zip")).want(t, 0)
	// The verifier is this very program, CLI and GUI in one (ADR-0008).
	if a, b := sha(t, bin), sha(t, filepath.Join(dst, export.ExeName)); a != b {
		t.Fatal("the export does not bundle the running program")
	}

	// Archivprotokoll for one Ablage.
	pdf := filepath.Join(t.TempDir(), "protokoll.pdf")
	pr := check("protocol", w.run("", "protocol", "0001", "--out", pdf), 0, true).want(t, 0)
	if b, _ := os.ReadFile(pdf); !strings.HasPrefix(string(b), "%PDF") || pr["status"] != "timestamped" {
		t.Fatalf("protocol %v", pr)
	}

	// The same results in a terminal: German, no JSON.
	old := sys
	t.Cleanup(func() { sys = old })
	if err := loadTestEnv(w.env[len(w.env)-1][len(testEnvVar)+1:]); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"status", "--archiv", dir}, "2 (2 zeitgestempelt, 0 ausstehend)"},
		{[]string{"verify", "--archiv", dir}, "Zeitgestempelt"},
		{[]string{"verify", dst}, "Ergebnis: in Ordnung (2 Ablagen"},
		{[]string{"stamp", "--archiv", dir}, "Keine fehlenden Zeitstempel"},
		{[]string{"provider", "show", "--archiv", dir}, "Passwort gespeichert"},
	} {
		var out, errb strings.Builder
		code := run(t.Context(), c.args, ioEnv{stdin: strings.NewReader(""), stdout: &out, stderr: &errb, stdoutTTY: true})
		if code != 0 || !strings.Contains(out.String(), c.want) || strings.Contains(out.String(), testenv.Password) {
			t.Fatalf("%v: exit %d\n%s%s", c.args, code, out.String(), errb.String())
		}
		t.Logf("%v:\n%s", c.args, out.String())
	}

	// Tamper with Ablage 1: verify finds it (exit 5) and stamp leaves it.
	zip := filepath.Join(dir, "0001_2026-10 Eingangsrechnungen.zip")
	os.Chmod(zip, 0o644)
	b, _ := os.ReadFile(zip)
	b[len(b)/2] ^= 0xff
	os.WriteFile(zip, b, 0o644)
	rep = check("verify", w.run("", "verify"), 5, true).want(t, 5)
	if rep["status"] != "changed" && rep["status"] != "invalid" {
		t.Fatalf("tampered %v", rep["status"])
	}
	w.run("", "verify", dst).want(t, 0) // the export is a separate copy
}
