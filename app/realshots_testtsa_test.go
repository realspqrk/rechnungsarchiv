//go:build testtsa

package app

// Screenshots of the real service (not the fake) against a demo archive
// built from testdata/demo with the test TSA. Run with
//
//	wails3 task screenshots:real     (or: RA_REAL_SHOTS=<folder> go test -tags testtsa -run TestRealScreenshots ./app)
//
// The test builds the archive in-process, then starts the GUI in Wails
// server mode (rechnungsarchiv gui, built with -tags "server testtsa")
// pointed at the same test world through RECHNUNGSARCHIV_TESTENV, and
// drives it with
// frontend/scripts/screenshots-real.mjs. The test TSA and the trusted-list
// server live in this test process, so it stays up until the script ends.

import (
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
)

func TestRealScreenshots(t *testing.T) {
	out := os.Getenv("RA_REAL_SHOTS")
	if out == "" {
		t.Skip("set RA_REAL_SHOTS=<folder> (wails3 task screenshots:real)")
	}
	out, _ = filepath.Abs(out)
	appDir, _ := os.Getwd()
	repo := filepath.Dir(appDir)
	if _, err := os.Stat(filepath.Join(appDir, "frontend", "dist", "index.html")); err != nil {
		t.Fatal("frontend not built: run npm run build in app/frontend first")
	}

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

	// A readable, user-name-free location (tmp/ is ignored by git).
	root := filepath.Join(repo, "tmp", "demo")
	os.RemoveAll(root)
	home := root
	for _, d := range []string{filepath.Join(root, "bin"), filepath.Join(root, "appdata")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("APPDATA", filepath.Join(root, "appdata"))
	goBin := filepath.Join(runtime.GOROOT(), "bin", "go")
	// The one binary (ADR-0008): its export bundles itself as the verifier.
	exe := filepath.Join(root, "bin", "rechnungsarchiv.exe")
	build := exec.Command(goBin, "build", "-tags", "server testtsa", "-o", exe, "./cmd/rechnungsarchiv")
	build.Dir, build.Stdout, build.Stderr = repo, os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		t.Fatalf("go build: %v", err)
	}

	cfg := testConfig{
		Provider:   e.Provider,
		TLSRoots:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.Server.Certificate().Raw})),
		LOTLPins:   string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.Lists.LOTLSigner.Raw})),
		ListsProxy: lists.URL,
		CredsFile:  filepath.Join(root, "creds.json"),
		Cache:      filepath.Join(root, "cache"),
		Home:       home,
	}
	data, _ := json.Marshal(cfg)
	cfgPath := filepath.Join(root, "testenv.json")
	if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// The demo archive, built through the real service like the GUI does.
	var sys system
	if err := applyTestConfig(&sys, cfg); err != nil {
		t.Fatal(err)
	}
	s := newRealService(sys)
	ctx := t.Context()
	must(s.CreateArchive(ctx, CreateArchiveRequest{Company: "Muster Tischlerei GmbH", Dir: filepath.Join(home, "Rechnungsarchiv")}))
	id := e.Provider.ID
	must(s.SetSettings(ctx, SettingsUpdate{ProviderID: &id}))
	must(s.SetCredentials(ctx, Credentials{User: testenv.User, Password: testenv.Password}))
	res := must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7}))
	if res.Deposit.State != StateStamped {
		t.Fatalf("demo archive: %+v", res)
	}
	s.wait()
	must(0, s.CloseArchive(ctx))
	s.Shutdown()

	port := 18790
	logf, _ := os.Create(filepath.Join(root, "gui.log"))
	defer logf.Close()
	srv := exec.Command(exe, "gui")
	srv.Env = append(os.Environ(), testEnvVar+"="+cfgPath, fmt.Sprintf("WAILS_SERVER_PORT=%d", port))
	srv.Stdout, srv.Stderr = logf, logf
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer srv.Process.Kill()
	url := fmt.Sprintf("http://localhost:%d/", port)
	for i := 0; ; i++ {
		if r, err := http.Get(url); err == nil {
			r.Body.Close()
			break
		}
		if i > 100 {
			t.Fatal("GUI server not reachable")
		}
		time.Sleep(100 * time.Millisecond)
	}

	node := exec.Command("node", filepath.Join("scripts", "screenshots-real.mjs"), url, out, testenv.Demo(testenv.Outgoing))
	node.Dir, node.Stdout, node.Stderr = filepath.Join(appDir, "frontend"), os.Stdout, os.Stderr
	if err := node.Run(); err != nil {
		t.Fatalf("screenshots: %v (GUI log: %s)", err, filepath.Join(root, "gui.log"))
	}
}
