//go:build testtsa

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
)

func zips(t *testing.T, dir string) []string {
	t.Helper()
	m, err := filepath.Glob(filepath.Join(dir, "*.zip"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// Code review finding 1: an explicit read-only open is never replaced by
// the remembered default archive (opened writable on the first Status).
func TestExplicitOpenBeatsRememberedDefault(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	if err := archive.RememberDefault(e.Dir); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "Sicherung")
	if _, err := archive.Init(backup, "Sicherung GmbH"); err != nil {
		t.Fatal(err)
	}
	st := must(s.OpenArchive(ctx, backup, true))
	s.wait()
	if st.Archive.Dir != backup || !st.Archive.ReadOnly || st.Busy == busyStamp {
		t.Fatalf("explicit read-only open returned %+v", st.Archive)
	}
	if st := must(s.Status(ctx)); st.Archive.Dir != backup || !st.Archive.ReadOnly {
		t.Fatalf("Status replaced the opened archive: %+v", st.Archive)
	}
	// Closing is a choice too: the default does not come back.
	if err := s.CloseArchive(ctx); err != nil {
		t.Fatal(err)
	}
	if st := must(s.Status(ctx)); st.HasArchive {
		t.Fatalf("default archive reopened after close: %+v", st.Archive)
	}
}

// Code review finding 2: a writer queued behind another one never commits
// into an archive that was reopened read-only meanwhile.
func TestQueuedWriterRespectsReopen(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	ctx := t.Context()
	must(s.OpenArchive(ctx, e.Dir, false))
	s.wait()

	s.write.Lock() // another writer is busy
	errc := make(chan error, 1)
	go func() {
		_, err := s.Archive(ctx, ArchiveOptions{Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7})
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond) // let Archive queue on the writer slot
	opened := make(chan error, 1)
	go func() {
		_, err := s.OpenArchive(ctx, e.Dir, true)
		opened <- err
	}()
	// Wait until the reopen has cancelled the old generation, then let the
	// queued writers run.
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		done := s.genCtx.Err() != nil
		s.mu.Unlock()
		if done || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.write.Unlock()
	if err := <-opened; err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err == nil {
		t.Fatal("queued Archive succeeded after the reopen")
	}
	s.wait()
	if z := zips(t, e.Dir); len(z) != 0 {
		t.Fatalf("committed after reopen read-only: %v", z)
	}
	if st := must(s.Status(ctx)); !st.Archive.ReadOnly {
		t.Fatal("not read-only")
	}
}

// Code review finding 3: a cancellation after the commit still reports the
// committed Ablage, so the UI never offers to archive it again.
func TestCancelAfterCommitReportsCommitted(t *testing.T) {
	e := testenv.New(t)
	s := newTestService(t, e)
	if err := s.sys.creds.Set(e.Archive.Config.ArchiveID, e.Provider.ID, creds.Credential{User: testenv.User, Password: testenv.Password}); err != nil {
		t.Fatal(err)
	}
	must(s.OpenArchive(t.Context(), e.Dir, false))
	s.wait()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The request reaches the TSA only after the commit; cancel there.
	e.TSA.SetReply(func(w http.ResponseWriter, r *http.Request) bool {
		cancel()
		if hj, ok := w.(http.Hijacker); ok {
			if c, _, err := hj.Hijack(); err == nil {
				c.Close()
			}
		}
		return true
	})
	res, err := s.Archive(ctx, ArchiveOptions{Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7})
	if err != nil {
		t.Fatalf("error after the commit: %v", err)
	}
	if !res.Committed || res.Deposit.Seq != 1 || res.Deposit.State == StateStamped || !strings.Contains(res.Warning, "nicht erneut archivieren") {
		t.Fatalf("result %+v", res)
	}
	if z := zips(t, e.Dir); len(z) != 1 {
		t.Fatalf("zips %v", z)
	}
}

// Code review finding 5: the Archivprotokoll never goes into any archive,
// not only the one that is open, also not through a junction.
func TestProtocolNeverIntoAnotherArchive(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	s := newTestService(t, e)
	ctx := t.Context()
	must(s.OpenArchive(ctx, e.Dir, true))
	s.wait()
	other := filepath.Join(t.TempDir(), "Archiv B")
	if _, err := archive.Init(other, "B GmbH"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(other, "p.pdf")); !errors.Is(err, errIntoArchive) {
		t.Fatalf("into archive B: %v", err)
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(other, "Unterordner", "p.pdf")); err == nil {
		t.Fatal("into a folder of archive B accepted")
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", alias, other).CombinedOutput(); err != nil {
		t.Fatalf("mklink: %v %s", err, out)
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(alias, "p.pdf")); !errors.Is(err, errIntoArchive) {
		t.Fatalf("through a junction to archive B: %v", err)
	}
	if _, err := os.Stat(filepath.Join(other, "p.pdf")); err == nil {
		t.Fatal("written")
	}
	if err := s.SaveProtocol(ctx, 1, filepath.Join(t.TempDir(), "p.pdf")); err != nil {
		t.Fatal(err)
	}
}

// Code review finding 8: Ablagen that stamping refuses are reported, not
// answered with "Keine fehlenden Zeitstempel".
func TestStampPendingReportsRefusedAblage(t *testing.T) {
	e := testenv.New(t)
	e.SetMode(testenv.Offline)
	e.Add(testenv.Incoming, archive.Options{})
	e.Stamp()
	e.SetMode(testenv.Normal)
	zip := zips(t, e.Dir)[0]
	os.Chmod(zip, 0o644)
	b, _ := os.ReadFile(zip)
	b[len(b)/2] ^= 0xff
	os.WriteFile(zip, b, 0o644)

	s := newTestService(t, e)
	if err := s.sys.creds.Set(e.Archive.Config.ArchiveID, e.Provider.ID, creds.Credential{User: testenv.User, Password: testenv.Password}); err != nil {
		t.Fatal(err)
	}
	must(s.OpenArchive(t.Context(), e.Dir, false))
	s.wait()
	r := must(s.StampPending(t.Context()))
	if r.Failed != 1 || r.Stamped != 0 || len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "0001") {
		t.Fatalf("%+v", r)
	}
}

// Code review 2 finding 1: a writer requested while archive A was current
// never runs against archive B opened (writable) while it waited for the
// writer slot, whichever of the two queued first. Covers Archive,
// SetSettings and SetCredentials.
func TestQueuedWriterNeverIntoSwitchedArchive(t *testing.T) {
	ops := []struct {
		name string
		run  func(s *realService, ctx context.Context) error
	}{
		{"Archive", func(s *realService, ctx context.Context) error {
			_, err := s.Archive(ctx, ArchiveOptions{Folder: testenv.Demo(testenv.Incoming), Name: testenv.Incoming, Belegjahr: 2026, RetentionYears: 7})
			return err
		}},
		{"SetSettings", func(s *realService, ctx context.Context) error {
			name := "Falsche Firma GmbH"
			_, err := s.SetSettings(ctx, SettingsUpdate{Company: &name})
			return err
		}},
		{"SetCredentials", func(s *realService, ctx context.Context) error {
			_, err := s.SetCredentials(ctx, Credentials{User: "falsch", Password: "falsch"})
			return err
		}},
	}
	for _, op := range ops {
		for _, openFirst := range []bool{true, false} {
			order := "writer queued first"
			if openFirst {
				order = "open queued first"
			}
			t.Run(op.name+"/"+order, func(t *testing.T) {
				e := testenv.New(t)
				s := newTestService(t, e)
				ctx := t.Context()
				other := filepath.Join(t.TempDir(), "Archiv B")
				b, err := archive.Init(other, "B GmbH")
				if err != nil {
					t.Fatal(err)
				}
				if err := b.SetProvider(&e.Provider.ID); err != nil {
					t.Fatal(err)
				}
				must(s.OpenArchive(ctx, e.Dir, false))
				s.wait()

				s.write.Lock() // another writer is busy
				opened := make(chan error, 1)
				openB := func() {
					go func() {
						_, err := s.OpenArchive(ctx, other, false)
						opened <- err
					}()
					// Wait until the open has ended A's generation, i.e. it
					// queues on the slot.
					deadline := time.Now().Add(10 * time.Second)
					for {
						s.mu.Lock()
						done := s.genCtx.Err() != nil
						s.mu.Unlock()
						if done || time.Now().After(deadline) {
							break
						}
						time.Sleep(5 * time.Millisecond)
					}
					time.Sleep(50 * time.Millisecond)
				}
				if openFirst {
					openB()
				}
				errc := make(chan error, 1)
				go func() { errc <- op.run(s, ctx) }()
				time.Sleep(50 * time.Millisecond) // let the writer queue on the slot
				if !openFirst {
					openB()
				}
				s.write.Unlock()
				if err := <-opened; err != nil {
					t.Fatal(err)
				}
				err = <-errc
				if err == nil {
					t.Fatal("queued writer succeeded after the switch to archive B")
				}
				if !errors.Is(err, errSwitched) && !errors.Is(err, errReopened) {
					t.Fatalf("error %v", err)
				}
				if openFirst && !strings.Contains(err.Error(), "inzwischen gewechselt") {
					t.Fatalf("error %q, want „Archiv wurde inzwischen gewechselt“", err)
				}
				s.wait()
				if st := must(s.Status(ctx)); st.Archive.Dir != other || st.Archive.ReadOnly {
					t.Fatalf("not switched to B: %+v", st.Archive)
				}
				for _, dir := range []string{e.Dir, other} {
					if z := zips(t, dir); len(z) != 0 {
						t.Fatalf("committed into %s: %v", dir, z)
					}
					ar, err := archive.OpenReadOnly(dir)
					if err != nil {
						t.Fatal(err)
					}
					if ar.Config.Company == "Falsche Firma GmbH" {
						t.Fatalf("company of %s changed", dir)
					}
					if c, err := s.sys.creds.Get(ar.Config.ArchiveID, e.Provider.ID); err == nil && c.User == "falsch" {
						t.Fatalf("credentials of %s changed", dir)
					}
				}
			})
		}
	}
}

// corruptTagLine flips one hex digit of the tagmanifest line for name in
// the Ablage ZIP, keeping every other byte and the ZIP layout (Stored,
// no data descriptors) as the archive wrote it.
func corruptTagLine(t *testing.T, path, name string) {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	found := false
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		rc.Close()
		fh := f.FileHeader
		if strings.HasSuffix(fh.Name, "/tagmanifest-sha256.txt") {
			lines := strings.SplitAfter(string(data), "\n")
			for i, l := range lines {
				if strings.HasSuffix(strings.TrimRight(l, "\r\n"), "  "+name) {
					d := "0"
					if l[0] == '0' {
						d = "1"
					}
					lines[i] = d + l[1:]
					found = true
				}
			}
			data = []byte(strings.Join(lines, ""))
			fh.CRC32 = crc32.ChecksumIEEE(data)
		}
		w, err := zw.CreateRaw(&fh)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr.Close()
	if !found {
		t.Fatalf("no tagmanifest line for %s in %s", name, path)
	}
	os.Chmod(path, 0o644)
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Code review 2 finding 2: a successor whose tagmanifest line for
// chain.json is wrong fails tag integrity (spec §6 step 2); its chain.json
// still names the predecessor's Prüfwert, but that is no verified link.
func TestPredecessorNotLinkedWhenTagsBroken(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	e.Add(testenv.Outgoing, archive.Options{})
	s := newTestService(t, e)
	ctx := t.Context()
	must(s.OpenArchive(ctx, e.Dir, true))
	s.wait()
	if r := must(s.VerifyDeposit(ctx, 2)); !r.PredecessorLinked {
		t.Fatalf("intact archive not linked: %+v", r)
	}

	z := zips(t, e.Dir)
	if len(z) != 2 {
		t.Fatalf("zips %v", z)
	}
	slices.Sort(z)
	corruptTagLine(t, z[1], "chain.json")
	r := must(s.VerifyDeposit(ctx, 2))
	if r.Deposit.Seq != 2 || r.Content == ContentUnchanged {
		t.Fatalf("corrupted tagmanifest not detected: %+v", r)
	}
	if !r.PredecessorPresent || r.PredecessorLinked {
		t.Fatalf("untrusted chain.json reported as linked: %+v", r)
	}
}
