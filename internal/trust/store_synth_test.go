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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/trust/trusttest"
)

// synthFixture returns a trusttest fixture whose LOTL signer is pinned for
// the duration of the test.
func synthFixture(t *testing.T) *trusttest.Fixture {
	t.Helper()
	fx := trusttest.New(t, "ZZ")
	pins := lotlPins
	lotlPins = []*x509.Certificate{fx.LOTLSigner}
	t.Cleanup(func() { lotlPins = pins })
	return fx
}

// version is one signed LOTL and national list of a fixture.
type version struct{ lotl, nat []byte }

func publishVersion(t *testing.T, fx *trusttest.Fixture, seq int, issue time.Time) version {
	t.Helper()
	fx.PublishSequence(t, seq, issue)
	return version{fx.Web.Get(trusttest.LOTLURL), fx.Web.Get(fx.NationalURL)}
}

func writeFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func hasProblem(s *Store, parts ...string) bool {
	for _, p := range s.Problems() {
		ok := true
		for _, part := range parts {
			ok = ok && strings.Contains(p.Error(), part)
		}
		if ok {
			return true
		}
	}
	return false
}

// TestOpenTrustsSignedSequenceNotFileName: the sequence in a cache file name
// is unsigned. An older signed list renamed to a huge sequence must not
// outrank the newest one, and the mismatch is a problem.
func TestOpenTrustsSignedSequenceNotFileName(t *testing.T) {
	fx := synthFixture(t)
	base := testNow.AddDate(0, -3, 0)
	v1 := publishVersion(t, fx, 1, base)
	v2 := publishVersion(t, fx, 2, base.AddDate(0, 1, 0))
	dir := t.TempDir()
	writeFiles(t, dir, map[string][]byte{
		"EU-2.xml":         v2.lotl,
		"ZZ-2.xml":         v2.nat,
		"ZZ-999999999.xml": v1.nat,
		"EU-999999999.xml": v1.lotl,
	})
	s := testStore(t, nil, dir)
	if l := s.list("ZZ"); l == nil || l.Sequence != 2 {
		t.Fatalf("ZZ = %s, want sequence 2", describe(l))
	}
	if l := s.Lists()[0]; l.Sequence != 2 {
		t.Fatalf("LOTL sequence %d, want 2", l.Sequence)
	}
	if !hasProblem(s, "ZZ-999999999.xml") || !hasProblem(s, "EU-999999999.xml") {
		t.Errorf("renamed lists not reported: %v", s.Problems())
	}
}

// TestOpenSameSequenceTakesLaterIssueDate: two different signed lists with the
// same sequence number. The later issue date wins whatever the file names say,
// and the conflict is reported.
func TestOpenSameSequenceTakesLaterIssueDate(t *testing.T) {
	fx := synthFixture(t)
	base := testNow.AddDate(0, -2, 0)
	early := publishVersion(t, fx, 3, base)
	late := publishVersion(t, fx, 3, base.Add(24*time.Hour))
	for name, files := range map[string]map[string][]byte{
		"later in base name": {"EU-3.xml": late.lotl, "ZZ-3.xml": late.nat, "ZZ-3-2.xml": early.nat},
		"later in suffix":    {"EU-3.xml": early.lotl, "EU-3-2.xml": late.lotl, "ZZ-3.xml": early.nat, "ZZ-3-2.xml": late.nat},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFiles(t, dir, files)
			s := testStore(t, nil, dir)
			l := s.list("ZZ")
			if l == nil || !l.IssueDate.Equal(base.Add(24*time.Hour)) {
				t.Fatalf("ZZ = %s, want the later list", describe(l))
			}
			if !hasProblem(s, "ZZ-3.xml", "ZZ-3-2.xml") {
				t.Errorf("conflict not reported: %v", s.Problems())
			}
		})
	}
	// The same list in two places is no conflict.
	dir, other := t.TempDir(), t.TempDir()
	writeFiles(t, dir, map[string][]byte{"EU-3.xml": late.lotl, "ZZ-3.xml": late.nat})
	writeFiles(t, other, map[string][]byte{"ZZ-3.xml": late.nat})
	if s := testStore(t, nil, dir, other); len(s.Problems()) != 0 {
		t.Errorf("problems: %v", s.Problems())
	}
}

// gatedWeb serves a fixed LOTL and national list versions in request order.
// The first national request blocks until release is closed.
type gatedWeb struct {
	lotl     []byte
	natURL   string
	nat      [][]byte // response of the 1st, 2nd, ... national request
	requests atomic.Int32
	waiting  chan struct{} // closed when the first national request blocks
	release  chan struct{}
}

func (w *gatedWeb) RoundTrip(r *http.Request) (*http.Response, error) {
	body := w.lotl
	if r.URL.String() == w.natURL {
		n := int(w.requests.Add(1))
		if n == 1 {
			close(w.waiting)
			<-w.release
		}
		body = w.nat[min(n, len(w.nat))-1]
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

// TestConcurrentRefreshKeepsNewestList: refresh A fetches list 2 but its
// response is delayed; refresh B meanwhile installs list 3. A's late, older
// list must not replace list 3.
func TestConcurrentRefreshKeepsNewestList(t *testing.T) {
	fx := synthFixture(t)
	v2 := publishVersion(t, fx, 2, testNow.Add(-48*time.Hour))
	v3 := publishVersion(t, fx, 3, testNow.Add(-24*time.Hour))
	web := &gatedWeb{lotl: v3.lotl, natURL: fx.NationalURL, nat: [][]byte{v2.nat, v3.nat},
		waiting: make(chan struct{}), release: make(chan struct{})}
	dir := t.TempDir()
	s, err := open(config{writeDir: dir, readDirs: []string{dir}, client: &http.Client{Transport: web}, now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	errA := make(chan error, 1)
	go func() { errA <- s.Refresh(context.Background(), "ZZ") }()
	<-web.waiting
	if err := s.Refresh(context.Background(), "ZZ"); err != nil {
		t.Fatal(err)
	}
	close(web.release)
	err = <-errA
	if l := s.list("ZZ"); l == nil || l.Sequence != 3 {
		t.Fatalf("ZZ = %s after the delayed refresh, want sequence 3", describe(l))
	}
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Errorf("delayed refresh: err = %v, want rollback error", err)
	}
}

// TestConcurrentSaveKeepsEveryVersion: different lists with the same
// sequence saved at the same time each get their own file (-2, -3, ...);
// none replaces another.
func TestConcurrentSaveKeepsEveryVersion(t *testing.T) {
	const writers = 8
	for round := range 20 {
		dir := t.TempDir()
		s := &Store{cfg: config{writeDir: dir}}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range writers {
			wg.Go(func() {
				<-start
				if err := s.save(&List{Territory: "ZZ", Sequence: 7, Raw: fmt.Appendf(nil, "version %d", i)}); err != nil {
					t.Errorf("round %d, writer %d: %v", round, i, err)
				}
			})
		}
		close(start)
		wg.Wait()
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, e := range entries {
			if !fileName.MatchString(e.Name()) {
				t.Fatalf("round %d: stray file %s", round, e.Name())
			}
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			seen[string(b)] = true
		}
		if len(seen) != writers || len(entries) != writers {
			t.Fatalf("round %d: %d files with %d distinct versions, want %d", round, len(entries), len(seen), writers)
		}
	}
}

func describe(l *List) string {
	if l == nil {
		return "none"
	}
	return fmt.Sprintf("%s-%d issued %s", l.Territory, l.Sequence, l.IssueDate.Format(time.RFC3339))
}
