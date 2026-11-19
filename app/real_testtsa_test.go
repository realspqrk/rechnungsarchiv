//go:build testtsa

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
)

// testSystem points the real service at the test world: test TSA, synthetic
// trusted lists, an in-memory credential store.
func testSystem(t *testing.T, e *testenv.Env) system {
	t.Helper()
	cache, home := t.TempDir(), t.TempDir()
	// The export bundles the verifier; any file stands in for it here.
	exe := filepath.Join(t.TempDir(), "rechnungsarchiv.exe")
	if err := os.WriteFile(exe, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", t.TempDir()) // archive.RememberDefault
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	return system{
		creds: creds.NewMemory(),
		trust: func(dir string) (*trust.Store, error) {
			d := filepath.Join(dir, trust.DirName)
			return e.TrustAt([]string{d}, d), nil
		},
		trustRead: func(dir string) (*trust.Store, error) {
			return e.TrustAt([]string{filepath.Join(dir, trust.DirName), cache}, cache), nil
		},
		trustExport: func(dir string) (*trust.Store, error) {
			return e.OfflineTrust(filepath.Join(dir, trust.DirName)), nil
		},
		rootCAs:  e.RootCAs(),
		verifier: func() (string, error) { return exe, nil },
		home:     func() (string, error) { return home, nil },
	}
}

func newTestService(t *testing.T, e *testenv.Env) *realService {
	s := newRealService(testSystem(t, e))
	t.Cleanup(s.Shutdown)
	return s
}

// must fails the test (by panicking) on err; Go cannot pass t next to a
// multi-value call.
// session is the current Status.Session, as a preview would carry it.
func session(s *realService) int {
	return must(s.Status(context.Background())).Session
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// The GUI's main path, end to end: create → provider + credentials →
// archive the demo folder (commit, stamp) → verify → DTO states → export →
// Archivprotokoll.
func TestRealServiceEndToEnd(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	dir := filepath.Join(t.TempDir(), "Archiv")

	if st := must(s.Status(ctx)); st.HasArchive || st.SuggestedDir == "" {
		t.Fatalf("start screen %+v", st)
	}
	st := must(s.CreateArchive(ctx, CreateArchiveRequest{Company: "Muster Tischlerei GmbH", Dir: dir}))
	s.wait()
	if !st.HasArchive || st.Provider != nil || st.Archive.ReadOnly || st.Archive.Company != "Muster Tischlerei GmbH" {
		t.Fatalf("created %+v", st)
	}
	if got := archive.DefaultDir(); got != dir {
		t.Fatalf("default archive %q", got)
	}

	id := e.Provider.ID
	must(s.SetSettings(ctx, SettingsUpdate{ProviderID: &id}))
	set := must(s.SetCredentials(ctx, Credentials{User: testenv.User, Password: testenv.Password}))
	if set.ProviderID != id || set.Credentials.User != testenv.User || !set.Credentials.HasPassword {
		t.Fatalf("settings %+v", set)
	}
	// Empty password field: the stored secret is kept.
	set = must(s.SetCredentials(ctx, Credentials{User: testenv.User}))
	if b, _ := json.Marshal(set); strings.Contains(string(b), testenv.Password) || !set.Credentials.HasPassword {
		t.Fatalf("settings leak or lost the secret: %s", b)
	}

	pv := must(s.Preview(ctx, testenv.Demo(testenv.Incoming)))
	if pv.Files != 9 || pv.SuggestedYear != 2026 || pv.NextSeq != 1 || pv.SuggestedName != testenv.Incoming || len(pv.Problems) != 0 {
		t.Fatalf("preview %+v", pv)
	}
	res := must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: pv.Folder, Name: pv.SuggestedName, Belegjahr: pv.SuggestedYear, RetentionYears: 7}))
	d := res.Deposit
	if d.Seq != 1 || d.State != StateStamped || d.GenTime == "" || d.ProvenLatest == "" || d.Provider != e.Provider.Name || res.Warning != "" {
		t.Fatalf("archived %+v", res)
	}
	// The lists were refreshed online right after the stamp (spec §7).
	if d.Qualification != QualQualified {
		t.Fatalf("qualification %q", d.Qualification)
	}

	dr := must(s.VerifyDeposit(ctx, 1))
	if dr.Stamp == nil || dr.Stamp.AccuracyMs != 1000 || !dr.Stamp.SignatureValid || dr.Files.Unchanged != 9 ||
		!strings.HasPrefix(dr.Root, "sha256:") || dr.RetentionUntil != "2033-12-31" || dr.Predecessor != 0 || dr.ReadOnly {
		t.Fatalf("deposit report %s", testenv.JSON(dr))
	}
	if _, err := os.Stat(dr.Path); err != nil {
		t.Fatal(err)
	}
	// The detail view lists every file with its own check result.
	if len(dr.FileList) != 9 || !slices.IsSortedFunc(dr.FileList, func(a, b FileEntry) int { return strings.Compare(a.Path, b.Path) }) {
		t.Fatalf("file list %s", testenv.JSON(dr.FileList))
	}
	for _, f := range dr.FileList {
		if f.State != FileUnchanged || f.Size <= 0 || f.MTime == "" || strings.HasPrefix(f.Path, "data/") {
			t.Fatalf("file entry %+v", f)
		}
	}

	// A provider error after the commit: the Ablage stays, pending · zugang.
	e.SetMode(testenv.Denied)
	res = must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Outgoing), Name: testenv.Outgoing, Belegjahr: 2026, RetentionYears: 7}))
	if res.Deposit.Seq != 2 || res.Deposit.State != StatePending || res.Deposit.Reason != ReasonAccess {
		t.Fatalf("pending %+v", res)
	}
	e.SetMode(testenv.Normal)
	if sr := must(s.StampPending(ctx, session(s))); sr.Stamped != 1 || sr.Pending != 0 {
		t.Fatalf("stamp %+v", sr)
	}

	c := must(s.Verify(ctx))
	if c.Deposits != 2 || c.Counts.Stamped != 2 || c.Deviations != 0 || c.FirstSeq != 1 || c.LastSeq != 2 || c.Files != pv.Files+countFiles(t, testenv.Outgoing) {
		t.Fatalf("check %+v", c)
	}
	st = must(s.Status(ctx))
	if len(st.Deposits) != 2 || st.Deposits[0].Seq != 2 || st.Counts.Stamped != 2 || st.LastCheck == nil || st.Lock != nil ||
		st.Provider == nil || st.Provider.ID != id || st.TrustList.Date == "" || st.TrustList.Stale {
		t.Fatalf("status %s", testenv.JSON(st))
	}
	if dr := must(s.VerifyDeposit(ctx, 2)); dr.Predecessor != 1 || !dr.PredecessorPresent {
		t.Fatalf("predecessor %+v", dr)
	}

	plan := must(s.ExportPlan(ctx, 0, 0))
	if !slices.Equal(plan.Years, []int{2026}) || plan.Deposits != 2 || plan.SuggestedTarget == "" {
		t.Fatalf("plan %+v", plan)
	}
	ex := must(s.Export(ctx, ExportOptions{Session: session(s), From: 2026, To: 2026, Target: filepath.Join(t.TempDir(), "Export")}))
	if ex.Deposits != 2 || len(ex.Warnings) != 0 {
		t.Fatalf("export %+v", ex)
	}

	pdf := filepath.Join(t.TempDir(), "protokoll.pdf")
	if err := s.SaveProtocol(ctx, 1, pdf); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(pdf); !strings.HasPrefix(string(b), "%PDF") {
		t.Fatal("no PDF")
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(dir, "p.pdf")); err == nil {
		t.Fatal("protocol written into the archive folder")
	}
	// Nor through a junction that leads into the archive (known.md).
	junction := filepath.Join(t.TempDir(), "Verknuepfung")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, dir).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(junction, "p.pdf")); !errors.Is(err, errIntoArchive) {
		t.Fatalf("protocol through a junction: %v", err)
	}

	// Tampering shows up as Verändert in the row and the check.
	zip := filepath.Join(dir, st.Deposits[1].File)
	os.Chmod(zip, 0o644)
	b, _ := os.ReadFile(zip)
	b[len(b)/2] ^= 0xff
	os.WriteFile(zip, b, 0o644)
	c = must(s.Verify(ctx))
	if c.Deviations != 1 {
		t.Fatalf("tampered check %+v", c)
	}
	if st = must(s.Status(ctx)); st.Deposits[1].State != StateModified && st.Deposits[1].State != StateInvalid {
		t.Fatalf("tampered row %+v", st.Deposits[1])
	}
}

func countFiles(t *testing.T, folder string) int {
	n := 0
	filepath.WalkDir(testenv.Demo(folder), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "Thumbs.db" && d.Name() != "desktop.ini" {
			n++
		}
		return nil
	})
	return n
}

// tree records every file below dir with size, mode and modification
// time, and every folder by name. Folder times are left out: the listing's
// copy of them is updated lazily by NTFS (known.md).
func tree(t *testing.T, dir string) map[string]string {
	m := map[string]string{}
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		if d.IsDir() {
			m[p] = "dir"
			return nil
		}
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		m[p] = fmt.Sprint(fi.ModTime(), fi.Mode(), fi.Size())
		return nil
	})
	return m
}

// A folder this PC cannot write to opens view only and never writes,
// locks or stamps (spec §9).
func TestRealServiceViewOnlyWritesNothing(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	e.Stamp()
	e.Add(testenv.Outgoing, archive.Options{}) // pending: must stay pending
	before := tree(t, e.Dir)

	s := newTestService(t, e)
	denyWrite(s, e.Dir)
	ctx := t.Context()
	st := must(s.OpenArchive(ctx, e.Dir))
	s.wait()
	if !st.Archive.ReadOnly || !strings.Contains(st.Archive.ReadOnlyReason, "Kein Schreibzugriff") || len(st.Deposits) != 2 || st.Due != (Due{}) {
		t.Fatalf("status %+v", st)
	}
	must(s.Verify(ctx))
	if dr := must(s.VerifyDeposit(ctx, 1)); !dr.ReadOnly || dr.Deposit.State != StateStamped {
		t.Fatalf("deposit %+v", dr)
	}
	must(s.ExportPlan(ctx, 0, 0))
	if err := s.SaveProtocol(ctx, 1, filepath.Join(t.TempDir(), "p.pdf")); err != nil {
		t.Fatal(err)
	}
	company := "x"
	for name, err := range map[string]error{
		"archive":     second(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Incoming), Name: "x", Belegjahr: 2026})),
		"preview":     second(s.Preview(ctx, testenv.Demo(testenv.Incoming))),
		"stamp":       second(s.StampPending(ctx, session(s))),
		"settings":    second(s.SetSettings(ctx, SettingsUpdate{Company: &company})),
		"credentials": second(s.SetCredentials(ctx, Credentials{User: "x", Password: "y"})),
		"takeover":    s.TakeOver(ctx, session(s)),
	} {
		if !errors.Is(err, errReadOnly) {
			t.Errorf("%s in read-only mode: %v", name, err)
		}
	}
	if st := must(s.Status(ctx)); st.Deposits[0].State != StatePending {
		t.Fatalf("read-only mode stamped: %+v", st.Deposits[0])
	}
	if after := tree(t, e.Dir); !mapsEqual(before, after) {
		t.Fatalf("read-only mode changed the archive folder:\nbefore %v\nafter  %v", before, after)
	}
	if archive.DefaultDir() == e.Dir {
		t.Fatal("a read-only archive became the default archive")
	}
}

func second[T any](_ T, err error) error { return err }

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func lockErr(t *testing.T, err error) *UIError {
	t.Helper()
	var ue *UIError
	if !errors.As(err, &ue) || ue.Code != CodeLocked || ue.Lock == nil {
		t.Fatalf("want lock error, got %v", err)
	}
	return ue
}

// Another writer: a German message with host and time, takeover only of a
// lock whose writer is gone, never of a running one.
func TestRealServiceLock(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	if err := s.sys.creds.Set(e.Archive.Config.ArchiveID, e.Provider.ID, creds.Credential{User: testenv.User, Password: testenv.Password}); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	must(s.OpenArchive(ctx, e.Dir))
	s.wait()

	// A running writer (another handle holds the OS lock).
	other := must(archive.Open(e.Dir))
	release := must(other.Lock())
	ue := lockErr(t, second(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7})))
	host, _ := os.Hostname()
	if !strings.Contains(ue.Message, "verwendet (seit") || !strings.EqualFold(ue.Lock.Host, host) || !strings.Contains(ue.Message, "übernehmen") {
		t.Fatalf("message %q / %+v", ue.Message, ue.Lock)
	}
	if st := must(s.Status(ctx)); st.Lock == nil || len(st.Deposits) != 0 {
		t.Fatalf("status %+v", st)
	}
	lockErr(t, s.TakeOver(ctx, session(s))) // never from a writer that is still running
	release()
	res := must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7}))
	if res.Deposit.State != StateStamped {
		t.Fatalf("after release %+v", res)
	}
	if st := must(s.Status(ctx)); st.Lock != nil {
		t.Fatalf("lock still shown %+v", st.Lock)
	}

	// A lock left behind by a crashed program on another PC: taken over
	// after the user confirmed.
	foreign := `{"host":"BUCHHALTUNG-PC","pid":4242,"since":"2026-11-18T08:00:00Z","user":"anna"}`
	if err := os.WriteFile(filepath.Join(e.Dir, ".lock"), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	ue = lockErr(t, second(s.StampPending(ctx, session(s))))
	if ue.Lock.Host != "BUCHHALTUNG-PC" || ue.Lock.User != "anna" || !strings.Contains(ue.Message, "BUCHHALTUNG-PC") {
		t.Fatalf("foreign lock %+v", ue)
	}
	if err := s.TakeOver(ctx, session(s)); err != nil {
		t.Fatal(err)
	}
	must(s.StampPending(ctx, session(s)))
	if _, err := os.Stat(filepath.Join(e.Dir, ".lock")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf(".lock left: %v", err)
	}
}

// Setting up the provider after archiving without one updates the status at
// once: no restart is needed to offer "Jetzt nachholen".
func TestRealServiceProviderSetupAfterArchive(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	must(s.CreateArchive(ctx, CreateArchiveRequest{Company: "Muster Tischlerei GmbH", Dir: filepath.Join(t.TempDir(), "Archiv")}))
	s.wait()
	res := must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7}))
	if res.Deposit.State != StatePending || res.Deposit.Reason != ReasonNoService {
		t.Fatalf("archived without provider %+v", res.Deposit)
	}

	id := e.Provider.ID
	must(s.SetSettings(ctx, SettingsUpdate{ProviderID: &id}))
	if d := must(s.Status(ctx)).Deposits[0]; d.State != StatePending || d.Reason == ReasonNoService {
		t.Fatalf("after choosing the provider %+v", d)
	}
	// Without credentials the next run says why; storing them drops that.
	must(s.StampPending(ctx, session(s)))
	if d := must(s.Status(ctx)).Deposits[0]; d.Reason != ReasonAccess {
		t.Fatalf("without credentials %+v", d)
	}
	must(s.SetCredentials(ctx, Credentials{User: testenv.User, Password: testenv.Password}))
	if d := must(s.Status(ctx)).Deposits[0]; d.State != StatePending || d.Reason != "" {
		t.Fatalf("after storing credentials %+v", d)
	}
	// Removing the provider again says so at once.
	none := ""
	must(s.SetSettings(ctx, SettingsUpdate{ProviderID: &none}))
	if d := must(s.Status(ctx)).Deposits[0]; d.Reason != ReasonNoService {
		t.Fatalf("after removing the provider %+v", d)
	}
	must(s.SetSettings(ctx, SettingsUpdate{ProviderID: &id}))
	must(s.StampPending(ctx, session(s)))
	if d := must(s.Status(ctx)).Deposits[0]; d.State != StateStamped {
		t.Fatalf("after Jetzt nachholen %+v", d)
	}
}

// A folder previewed in one archive is never archived into another one
// opened meanwhile, not even into a copy of the same archive (same ID).
func TestRealServiceArchiveBoundToPreview(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	a := filepath.Join(t.TempDir(), "A")
	must(s.CreateArchive(ctx, CreateArchiveRequest{Company: "Firma A", Dir: a}))
	s.wait()
	copyOfA := filepath.Join(t.TempDir(), "Sicherung")
	if err := os.CopyFS(copyOfA, os.DirFS(a)); err != nil {
		t.Fatal(err)
	}
	pv := must(s.Preview(ctx, testenv.Demo(testenv.Incoming)))
	st := must(s.OpenArchive(ctx, copyOfA))
	s.wait()
	if st.Session == pv.Session {
		t.Fatalf("same session after opening the copy: %d", st.Session)
	}
	_, err := s.Archive(ctx, ArchiveOptions{Folder: pv.Folder, Name: pv.SuggestedName, Belegjahr: 2026, RetentionYears: 7, Session: pv.Session})
	if !errors.Is(err, errSwitched) {
		t.Fatalf("archive after switch: %v", err)
	}
	if st := must(s.Status(ctx)); len(st.Deposits) != 0 {
		t.Fatalf("copy changed: %+v", st.Deposits)
	}
	// Export and lock takeover, too, only for the session they were asked in.
	if _, err := s.Export(ctx, ExportOptions{Session: pv.Session, From: 2026, To: 2026, Target: filepath.Join(t.TempDir(), "Export")}); !errors.Is(err, errSwitched) {
		t.Fatalf("export after switch: %v", err)
	}
	if err := s.TakeOver(ctx, pv.Session); !errors.Is(err, errSwitched) {
		t.Fatalf("takeover after switch: %v", err)
	}
	if a, b := must(s.Status(ctx)), must(s.Status(ctx)); b.Rev <= a.Rev {
		t.Fatalf("status revision %d then %d", a.Rev, b.Rev)
	}
	// Recent: the other folder, not the open one.
	if st := must(s.Status(ctx)); len(st.Recent) != 1 || st.Recent[0].Dir != a {
		t.Fatalf("recent %+v", st.Recent)
	}
	// A folder that is no archive (a backup's parent folder) gets the hint.
	if _, err := s.OpenArchive(ctx, filepath.Dir(copyOfA)); !errors.Is(err, errNotArchive) {
		t.Fatalf("open parent: %v", err)
	}
}

// Opening never stamps: Status.Due says what a run would do, counted only
// when it could work (credentials), and only "Jetzt holen" (StampPending)
// fetches it. Archiving stamps only its own Ablage, never the backlog.
func TestRealServiceAsksBeforeStamping(t *testing.T) {
	e := testenv.New(t)
	e.SetMode(testenv.Offline)
	e.Add(testenv.Incoming, archive.Options{})
	e.Stamp()
	e.SetMode(testenv.Normal)
	requests := e.TSA.Requests()

	s := newTestService(t, e)
	ctx := t.Context()
	st := must(s.OpenArchive(ctx, e.Dir))
	s.wait()
	if st := must(s.Status(ctx)); st.Busy != "" || st.Deposits[0].State != StatePending || st.Due != (Due{}) {
		t.Fatalf("without credentials nothing is due: %s", testenv.JSON(st))
	}
	if st.Session == 0 {
		t.Fatal("no session")
	}
	// Credentials saved: now a run could work.
	must(s.SetCredentials(ctx, Credentials{User: testenv.User, Password: testenv.Password}))
	if st := must(s.Status(ctx)); st.Due != (Due{Paid: 1}) {
		t.Fatalf("due after credentials %+v", st.Due)
	}
	// Reopening (program start) asks again, and still stamps nothing.
	st = must(s.OpenArchive(ctx, e.Dir))
	s.wait()
	if st := must(s.Status(ctx)); st.Due != (Due{Paid: 1}) || st.Deposits[0].State != StatePending || e.TSA.Requests() != requests {
		t.Fatalf("on open %s requests %d", testenv.JSON(st), e.TSA.Requests()-requests)
	}

	// "Später", then archiving: exactly one request, for the new Ablage.
	res := must(s.Archive(ctx, ArchiveOptions{Session: session(s), Folder: testenv.Demo(testenv.Outgoing), Name: testenv.Outgoing, Belegjahr: 2026, RetentionYears: 7}))
	if res.Deposit.State != StateStamped || e.TSA.Requests() != requests+1 {
		t.Fatalf("archive %+v requests %d", res.Deposit, e.TSA.Requests()-requests)
	}
	if st := must(s.Status(ctx)); st.Due != (Due{Paid: 1}) {
		t.Fatalf("backlog after archiving %+v", st.Due)
	}

	// "Jetzt holen" for an archive no longer open is refused.
	if _, err := s.StampPending(ctx, session(s)-1); !errors.Is(err, errSwitched) {
		t.Fatalf("stale session: %v", err)
	}
	if r := must(s.StampPending(ctx, session(s))); r.Stamped != 1 {
		t.Fatalf("stamp %+v", r)
	}
	if st := must(s.Status(ctx)); st.Due != (Due{}) || st.Counts.Pending != 0 {
		t.Fatalf("after stamping %s", testenv.JSON(st))
	}
}

// Only providers proven in real use are offered; an archive set up for a
// hidden one keeps it, visible and with editable credentials.
func TestRealServiceOffersOnlyProvenProviders(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	must(s.OpenArchive(ctx, e.Dir))
	s.wait()
	ids := func() []string {
		var out []string
		for _, p := range must(s.Providers(ctx)) {
			out = append(out, p.ID)
		}
		return out
	}
	if got := ids(); !slices.Contains(got, "mensatek") || slices.Contains(got, "disig") || slices.Contains(got, "namirial") {
		t.Fatalf("offered %v", got)
	}
	disig := "disig"
	if _, err := s.SetSettings(ctx, SettingsUpdate{ProviderID: &disig}); !errors.Is(err, errNotOffered) {
		t.Fatalf("hidden provider chosen: %v", err)
	}
	// Set up before it was hidden (e.g. with the CLI of 1.0.1).
	if err := e.Archive.SetProvider(&disig); err != nil {
		t.Fatal(err)
	}
	must(s.OpenArchive(ctx, e.Dir))
	s.wait()
	if got := ids(); !slices.Contains(got, "disig") || slices.Contains(got, "namirial") {
		t.Fatalf("offered with disig set up %v", got)
	}
	if st := must(s.SetCredentials(ctx, Credentials{URL: "https://tsa.example.invalid/x"})); st.ProviderID != "disig" || st.Credentials.URL == "" {
		t.Fatalf("credentials of the current provider: %+v", st)
	}
	if _, err := s.SetSettings(ctx, SettingsUpdate{ProviderID: &disig}); err != nil {
		t.Fatalf("keeping the current provider: %v", err)
	}
}

// Another PC's lock shows on the main screen right after opening, not only
// at the first write; once released, "Archiv kontrollieren" clears it.
func TestRealServiceShowsLockOnOpen(t *testing.T) {
	e := testenv.New(t)
	foreign := `{"host":"BUCHHALTUNG-PC","pid":4242,"since":"2026-11-18T08:00:00Z","user":"anna"}`
	lock := filepath.Join(e.Dir, ".lock")
	if err := os.WriteFile(lock, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newTestService(t, e)
	ctx := t.Context()
	if st := must(s.OpenArchive(ctx, e.Dir)); st.Lock == nil || st.Lock.Host != "BUCHHALTUNG-PC" {
		t.Fatalf("lock not in the first status: %+v", st.Lock)
	}
	s.wait()
	if st := must(s.Status(ctx)); st.Lock == nil || st.Lock.Host != "BUCHHALTUNG-PC" {
		t.Fatalf("lock not shown after opening: %+v", st.Lock)
	}
	if b, err := os.ReadFile(lock); err != nil || string(b) != foreign {
		t.Fatalf("the check changed the lock: %q %v", b, err)
	}
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	must(s.Verify(ctx))
	if st := must(s.Status(ctx)); st.Lock != nil {
		t.Fatalf("released lock still shown: %+v", st.Lock)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the check left a lock: %v", err)
	}
}

// denyWrite makes opening these folders for writing fail, like a
// write-protected share or stick.
func denyWrite(s *realService, dirs ...string) {
	s.sys.openWrite = func(dir string) (*archive.Archive, error) {
		for _, d := range dirs {
			if strings.EqualFold(filepath.Clean(d), filepath.Clean(dir)) {
				return nil, &fs.PathError{Op: "open", Path: filepath.Join(dir, ".lock"), Err: fs.ErrPermission}
			}
		}
		return archive.Open(dir)
	}
}
