package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

const demo = "../../testdata/demo"

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// setup creates an archive and a private copy of the demo folders.
func setup(t *testing.T) (*Archive, string) {
	t.Helper()
	root := t.TempDir()
	in := filepath.Join(root, "eingang")
	copyDir(t, demo, in)
	a, err := Init(filepath.Join(root, "Archiv"), "Alpenblick Tischlerei GmbH")
	if err != nil {
		t.Fatal(err)
	}
	return a, in
}

func TestInitArchiveAndChain(t *testing.T) {
	a, in := setup(t)
	cfg, _ := os.ReadFile(filepath.Join(a.Dir, configFile))
	want := fmt.Sprintf(`{"archive_id":"%s","company":"Alpenblick Tischlerei GmbH","format":1,"retention_default":7,"tsa":null}`, a.Config.ArchiveID)
	if string(cfg) != want || len(a.Config.ArchiveID) != 36 || a.Config.ArchiveID[14] != '4' {
		t.Fatalf("config %s", cfg)
	}
	if _, err := Init(a.Dir, "Noch eine GmbH"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Init: %v", err)
	}

	d1, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := a.Archive(filepath.Join(in, "2026-10 Ausgangsrechnungen"), Options{RetentionYears: 10})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(d1.Path) != "0001_2026-10 Eingangsrechnungen.zip" || filepath.Base(d2.Path) != "0002_2026-10 Ausgangsrechnungen.zip" {
		t.Fatalf("names %s, %s", d1.Path, d2.Path)
	}
	for _, d := range []*Deposit{d1, d2} {
		if fi, err := os.Stat(d.EvidenceDir); err != nil || !fi.IsDir() {
			t.Fatalf("no .nachweis for %s", d.Path)
		}
		if fi, _ := os.Stat(d.Path); fi.Mode().Perm()&0o200 != 0 {
			t.Fatalf("%s is not read-only", d.Path)
		}
		if r, err := bag.Check(d.Path); err != nil || r.Status != bag.OK {
			t.Fatalf("check %s: %v %v", d.Path, err, r.Problems)
		}
	}
	info := d2.Bag.Info
	if info.Belegjahr != 2026 || info.RetentionYears != 10 || info.RetentionUntil != "2036-12-31" ||
		info.ExternalIdentifier != a.Config.ArchiveID+"/2" || !strings.Contains(info.ArchivedBy, "@") {
		t.Fatalf("bag-info %+v", info)
	}
	if d1.Bag.Info.RetentionUntil != "2033-12-31" {
		t.Fatalf("default retention: %s", d1.Bag.Info.RetentionUntil)
	}

	// The chain, as read back from disk.
	ds, err := a.Deposits()
	if err != nil || len(ds) != 2 {
		t.Fatalf("deposits %v %v", ds, err)
	}
	c1, c2 := ds[0].Bag.Chain, ds[1].Bag.Chain
	if c1.Sequence != 1 || c1.Previous != nil || c2.Sequence != 2 || c2.Previous == nil || *c2.Previous != ds[0].Bag.Root {
		t.Fatalf("chain %+v %+v", c1, c2)
	}
	if ds[0].Bag.Root == ds[1].Bag.Root || len(a.lockLeft(t)) != 0 {
		t.Fatal("roots equal or lock left behind")
	}

	// Same folder again: archived again (no deduplication), but the
	// preview reports every file as already known.
	p, err := a.Preview(filepath.Join(in, "2026-10 Eingangsrechnungen"))
	if err != nil || len(p.Known) != 9 || p.Belegjahr != 2026 || !slices.Equal(p.Years, []int{time.Now().Year()}) {
		t.Fatalf("preview %+v %v", p, err)
	}
	d3, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{Name: "2026-10 Eingangsrechnungen nochmals"})
	if err != nil || d3.Sequence != 3 || len(d3.Bag.Manifest) != 9 {
		t.Fatalf("re-archive: %v", err)
	}
}

func (a *Archive) lockLeft(t *testing.T) string {
	data, _ := os.ReadFile(a.lockPath())
	return string(data)
}

func TestArchiveOptions(t *testing.T) {
	a, in := setup(t)
	plain := filepath.Join(in, "Rechnungen")
	os.Rename(filepath.Join(in, "2026-10 Ausgangsrechnungen"), plain)
	if _, err := a.Archive(plain, Options{}); !errors.Is(err, ErrNeedBelegjahr) {
		t.Fatalf("missing Belegjahr: %v", err)
	}
	if _, err := a.Archive(plain, Options{Belegjahr: 2026, RetentionYears: 9}); err == nil {
		t.Fatal("retention 9 accepted")
	}
	if _, err := a.Archive(plain, Options{Belegjahr: 2026, Name: "Oktober."}); err == nil {
		t.Fatal("name with trailing dot accepted")
	}
	if _, err := a.Archive(a.Dir, Options{Belegjahr: 2026}); err == nil {
		t.Fatal("archived the archive into itself")
	}
	d, err := a.Archive(plain, Options{Belegjahr: 2025, RetentionYears: 22})
	if err != nil || d.Bag.Info.RetentionUntil != "2047-12-31" {
		t.Fatalf("%v", err)
	}
	if BelegjahrFromName("Q3 2025 Belege") != 2025 || BelegjahrFromName("Rechnungen 12345") != 0 {
		t.Fatal("BelegjahrFromName")
	}
}

func TestLock(t *testing.T) {
	a, in := setup(t)
	folder := filepath.Join(in, "2026-10 Ausgangsrechnungen")

	// Another PC holds the lock: refused, with host and time for the UI.
	foreign := `{"host":"BUCHHALTUNG-PC","pid":4242,"since":"2026-11-18T08:00:00Z","user":"anna"}`
	os.WriteFile(a.lockPath(), []byte(foreign), 0o644)
	_, err := a.Archive(folder, Options{})
	var le *LockedError
	if !errors.As(err, &le) || le.Host != "BUCHHALTUNG-PC" || le.User != "anna" || !strings.Contains(err.Error(), "BUCHHALTUNG-PC") {
		t.Fatalf("got %v", err)
	}
	if a.lockLeft(t) != foreign {
		t.Fatal("foreign lock was touched")
	}
	// After confirmation the UI takes it over.
	if err := a.TakeOver(le); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Archive(folder, Options{}); err != nil {
		t.Fatal(err)
	}

	// A lock from a dead process on this PC is taken over silently.
	dead := fmt.Sprintf(`{"host":%q,"pid":2147483000,"since":"2026-11-18T08:00:00Z","user":"x"}`, a.host)
	os.WriteFile(a.lockPath(), []byte(dead), 0o644)
	if _, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{}); err != nil {
		t.Fatalf("stale lock not taken over: %v", err)
	}

	// A live process on this PC (ourselves, as another writer) blocks.
	alive := fmt.Sprintf(`{"host":%q,"pid":%d,"since":"2026-11-18T08:00:00Z","user":"x"}`, a.host, os.Getpid())
	os.WriteFile(a.lockPath(), []byte(alive), 0o644)
	if _, err := a.Archive(folder, Options{Name: "nochmal 2026"}); !errors.As(err, &le) {
		t.Fatalf("live lock ignored: %v", err)
	}
	// TakeOver refuses a lock that changed after the user saw it.
	os.WriteFile(a.lockPath(), []byte(foreign), 0o644)
	if err := a.TakeOver(le); err == nil {
		t.Fatal("took over a changed lock")
	}
}

func TestRecoveryAndNoReplace(t *testing.T) {
	a, in := setup(t)
	if _, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{}); err != nil {
		t.Fatal(err)
	}
	leftovers := []string{"0002_halb.zip.tmp", "rechnungsarchiv.json.tmp", "0001_2026-10 Eingangsrechnungen.nachweis/1.tsr.tmp"}
	for _, l := range leftovers {
		os.WriteFile(filepath.Join(a.Dir, l), []byte("crash"), 0o644)
	}
	// While a writer holds the lock, recovery leaves its temp files alone.
	os.WriteFile(a.lockPath(), []byte(`{"host":"anderer-pc","pid":1,"since":"2026-11-18T08:00:00Z","user":"x"}`), 0o644)
	if _, err := Open(a.Dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.Dir, leftovers[0])); err != nil {
		t.Fatal("tmp removed under a foreign lock")
	}
	os.Remove(a.lockPath())
	if _, err := Open(a.Dir); err != nil {
		t.Fatal(err)
	}
	for _, l := range leftovers {
		if _, err := os.Stat(filepath.Join(a.Dir, l)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s not cleaned up", l)
		}
	}
	if _, err := os.Stat(a.lockPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("recovery left the lock behind")
	}

	// The commit rename never replaces an existing file.
	src, dst := filepath.Join(a.Dir, "x.tmp"), filepath.Join(a.Dir, "ziel.txt")
	os.WriteFile(src, []byte("neu"), 0o644)
	os.WriteFile(dst, []byte("alt"), 0o644)
	if ok, err := renameNoReplace(src, dst); ok || !errors.Is(err, os.ErrExist) {
		t.Fatalf("rename replaced or failed oddly: %v", err)
	}
	if data, _ := os.ReadFile(dst); string(data) != "alt" {
		t.Fatal("target overwritten")
	}
}

// snapshot records every path with size, mtime and mode.
func snapshot(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s %d %d %v", p, fi.Size(), fi.ModTime().UnixNano(), fi.Mode()))
		return nil
	})
	sort.Strings(out)
	return out
}

func TestReadOnlyWritesNothing(t *testing.T) {
	a, in := setup(t)
	if _, err := a.Archive(filepath.Join(in, "2026-10 Eingangsrechnungen"), Options{}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(a.Dir, "0002_halb.zip.tmp"), []byte("crash"), 0o644)
	before := snapshot(t, a.Dir)

	ro, err := OpenReadOnly(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	ds, err := ro.Deposits()
	if err != nil || len(ds) != 1 {
		t.Fatalf("%v %v", ds, err)
	}
	if r, err := bag.Check(ds[0].Path); err != nil || r.Status != bag.OK {
		t.Fatalf("%v %v", err, r)
	}
	if _, err := ro.Preview(filepath.Join(in, "2026-10 Ausgangsrechnungen")); err != nil {
		t.Fatal(err)
	}
	if _, err := ro.Archive(filepath.Join(in, "2026-10 Ausgangsrechnungen"), Options{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("archive in read-only mode: %v", err)
	}
	if err := ro.TakeOver(&LockedError{}); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("takeover in read-only mode: %v", err)
	}
	if after := snapshot(t, a.Dir); !slices.Equal(before, after) {
		t.Fatalf("read-only mode changed the archive:\n%v\n%v", before, after)
	}
}
