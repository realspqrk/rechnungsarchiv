package trust

import (
	"bytes"
	"context"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeWeb serves fixture bytes by URL instead of touching the network.
type fakeWeb map[string][]byte

func (f fakeWeb) RoundTrip(r *http.Request) (*http.Response, error) {
	body, ok := f[r.URL.String()]
	status := http.StatusOK
	if !ok {
		status, body = http.StatusNotFound, []byte("not found")
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

func fixtureWeb(t *testing.T) fakeWeb {
	return fakeWeb{
		LOTLURL: readFixture(t, fixtureLOTL),
		"https://www.signatur.rtr.at/vertrauensliste.xml": readFixture(t, fixtureAT),
		"http://tl.nbu.gov.sk/kca/tsl/tsl.xml":            readFixture(t, fixtureSK),
	}
}

var testNow = time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)

// testStore opens a store without the bootstrap snapshot, so only the given
// directories count.
func testStore(t *testing.T, web fakeWeb, writeDir string, readDirs ...string) *Store {
	t.Helper()
	s, err := open(config{
		writeDir: writeDir,
		readDirs: append([]string{writeDir}, readDirs...),
		client:   &http.Client{Transport: web},
		now:      func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func copyFixture(t *testing.T, name, dir, as string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, as), readFixture(t, name), 0o644); err != nil {
		t.Fatal(err)
	}
}

func disig198(t *testing.T) []*x509.Certificate {
	return certsOf(t, fixtureNational(t, fixtureSK, "SK"), "(198) CA Disig Qualified TSA 1")
}

func TestOpenEmbeddedBootstrap(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Problems()) > 0 {
		t.Fatalf("problems: %v", s.Problems())
	}
	got := map[string]bool{}
	for _, l := range s.Lists() {
		got[l.Territory] = true
		if _, online := l.FetchedOnline(); online {
			t.Errorf("%s: bootstrap list marked as fetched online", l.Territory)
		}
	}
	for _, cc := range []string{"EU", "AT", "ES", "SK", "IT", "FR"} {
		if !got[cc] {
			t.Errorf("bootstrap lacks %s", cc)
		}
	}
}

func TestOpenUsesNewestVerifiedList(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, fixtureLOTL, dir, "EU-398.xml")
	copyFixture(t, fixtureAT, dir, "AT-75.xml")
	// A newer-looking but forged AT list must be skipped (fail closed).
	forged := bytes.Replace(readFixture(t, fixtureAT), []byte("<TSLSequenceNumber>75<"), []byte("<TSLSequenceNumber>99<"), 1)
	if err := os.WriteFile(filepath.Join(dir, "AT-99.xml"), forged, 0o644); err != nil {
		t.Fatal(err)
	}
	// Unrelated files are ignored.
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	s := testStore(t, nil, dir)
	at := s.list("AT")
	if at == nil || at.Sequence != 75 {
		t.Fatalf("AT = %+v, want sequence 75", at)
	}
	if len(s.Problems()) != 1 || !strings.Contains(s.Problems()[0].Error(), "AT-99.xml") {
		t.Errorf("problems = %v", s.Problems())
	}
}

func TestOpenWithoutVerifiableLOTL(t *testing.T) {
	dir := t.TempDir()
	lotl := bytes.Clone(readFixture(t, fixtureLOTL))
	i := bytes.Index(lotl, []byte("https://www.signatur.rtr.at"))
	copy(lotl[i:], "https://www.signatur.rtr.ax")
	os.WriteFile(filepath.Join(dir, "EU-398.xml"), lotl, 0o644)
	copyFixture(t, fixtureSK, dir, "SK-146.xml")

	s := testStore(t, nil, dir)
	if len(s.Lists()) != 0 {
		t.Fatalf("lists without a verified LOTL: %d", len(s.Lists()))
	}
	if d := s.Qualify(disig198(t), mustTime(t, "2026-01-01T00:00:00Z"), ""); d.Result != Unconfirmed {
		t.Fatalf("got %v", d.Result)
	}
}

func TestRefreshStoresExactBytesAndCountsAsOnline(t *testing.T) {
	archive := t.TempDir()
	dir := filepath.Join(archive, DirName)
	s := testStore(t, fixtureWeb(t), dir)
	if err := s.Refresh(context.Background(), "AT", "SK"); err != nil {
		t.Fatal(err)
	}
	for file, fixture := range map[string]string{"EU-398.xml": fixtureLOTL, "AT-75.xml": fixtureAT, "SK-146.xml": fixtureSK} {
		got, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, readFixture(t, fixture)) {
			t.Errorf("%s: stored bytes differ from the signed list", file)
		}
	}

	// genTime after SK's issue date: only the online fetch makes it decidable.
	genTime := mustTime(t, "2026-11-10T08:00:00Z")
	d := s.Qualify(disig198(t), genTime, "")
	if d.Result != Qualified || !d.FetchedOnline || d.Territory != "SK" {
		t.Fatalf("after refresh: %+v", d)
	}

	// The same lists from the cache are no evidence for that genTime.
	cached := testStore(t, nil, dir)
	if d := cached.Qualify(disig198(t), genTime, ""); d.Result != Unconfirmed {
		t.Fatalf("from cache: %v, want unbestätigt", d.Result)
	}

	// A second refresh adds no duplicate files.
	if err := s.Refresh(context.Background(), "AT", "SK"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 3 {
		t.Errorf("cache has %d files, want 3", len(entries))
	}
}

func TestRefreshRejectsTamperedList(t *testing.T) {
	dir := t.TempDir()
	web := fixtureWeb(t)
	sk := bytes.Clone(web["http://tl.nbu.gov.sk/kca/tsl/tsl.xml"])
	i := bytes.Index(sk, []byte("Disig"))
	sk[i] = 'X'
	web["http://tl.nbu.gov.sk/kca/tsl/tsl.xml"] = sk

	s := testStore(t, web, dir)
	err := s.Refresh(context.Background(), "AT", "SK")
	if err == nil || !strings.Contains(err.Error(), "SK") {
		t.Fatalf("err = %v, want SK failure", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SK-146.xml")); !os.IsNotExist(err) {
		t.Error("tampered list was stored")
	}
	if s.list("AT") == nil {
		t.Error("AT must still be refreshed")
	}
	if s.list("SK") != nil {
		t.Error("tampered SK list in use")
	}
}

func TestRefreshRejectsRollback(t *testing.T) {
	dir := t.TempDir()
	s := testStore(t, fixtureWeb(t), dir)
	if err := s.Refresh(context.Background(), "AT"); err != nil {
		t.Fatal(err)
	}
	s.lists["AT"].Sequence = 76 // pretend a newer AT list is already known
	s.lists["AT"].fetchedOnline = time.Time{}
	if err := s.Refresh(context.Background(), "AT"); err == nil {
		t.Fatal("older list accepted over a newer one")
	}
	if _, online := s.list("AT").FetchedOnline(); online {
		t.Error("rolled-back list counts as online evidence")
	}
}

func TestRefreshExpiredListIsNotOnlineEvidence(t *testing.T) {
	dir := t.TempDir()
	s := testStore(t, fixtureWeb(t), dir)
	s.now = func() time.Time { return mustTime(t, "2026-12-20T00:00:00Z") } // after AT NextUpdate 2026-12-15
	err := s.Refresh(context.Background(), "AT")
	if err == nil || !strings.Contains(err.Error(), "NextUpdate") {
		t.Fatalf("err = %v, want expired-list error", err)
	}
	if _, online := s.list("AT").FetchedOnline(); online {
		t.Error("expired list counts as online evidence")
	}
	if _, err := os.Stat(filepath.Join(dir, "AT-75.xml")); err != nil {
		t.Error("verified list must still be kept:", err)
	}
}

func TestRefreshKeepsDifferingVersionWithSameSequence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AT-75.xml"), []byte("older bytes"), 0o644)
	s := testStore(t, fixtureWeb(t), dir)
	if err := s.Refresh(context.Background(), "AT"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "AT-75.xml")); string(b) != "older bytes" {
		t.Error("existing version was overwritten")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "AT-75-2.xml")); !bytes.Equal(b, readFixture(t, fixtureAT)) {
		t.Error("new version not stored as AT-75-2.xml")
	}
}

func TestReadOnlyNeverWritesIntoArchive(t *testing.T) {
	archive := t.TempDir()
	copyFixture(t, fixtureSK, filepath.Join(archive, DirName), "SK-146.xml")
	before := snapshot(t, archive)

	tmp := t.TempDir()
	s := testStore(t, fixtureWeb(t), tmp, filepath.Join(archive, DirName))
	if err := s.Refresh(context.Background(), "AT", "SK"); err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, archive); after != before {
		t.Fatalf("archive changed:\n%s\n->\n%s", before, after)
	}
	if _, err := os.Stat(filepath.Join(tmp, "AT-75.xml")); err != nil {
		t.Error("refreshed list not cached in temp dir:", err)
	}
}

func TestOpenReadOnlyUsesTempCache(t *testing.T) {
	archive := t.TempDir()
	s, err := OpenReadOnly(archive)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(s.cfg.writeDir, archive) {
		t.Fatalf("read-only store writes into the archive: %s", s.cfg.writeDir)
	}
	if _, err := os.Stat(filepath.Join(archive, DirName)); !os.IsNotExist(err) {
		t.Error("OpenReadOnly created the archive's list folder")
	}
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			fmt.Fprintf(&b, "%s/\n", p) // NTFS updates directory times lazily
			return nil
		}
		info, _ := d.Info()
		fmt.Fprintf(&b, "%s %d %s\n", p, info.Size(), info.ModTime())
		return nil
	})
	return b.String()
}
