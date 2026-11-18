package trust

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/x509"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

const (
	// DirName is the list cache inside an archive (spec §1).
	DirName = "vertrauenslisten"
	// LOTLURL is where the Commission publishes the LOTL.
	LOTLURL = "https://ec.europa.eu/tools/lotl/eu-lotl.xml"
)

// bootstrapFS is the verified snapshot shipped with the release (spec §7). It
// is verified on every start like any cached list.
//
//go:embed bootstrap/*.xml.gz
var bootstrapFS embed.FS

// fileName matches cached lists: <CC>-<sequence>.xml, or <CC>-<sequence>-<n>.xml
// for a differing list that reuses a sequence number.
var fileName = regexp.MustCompile(`^([A-Z]{2})-([0-9]{1,9})(?:-[0-9]{1,3})?\.xml(\.gz)?$`)

// Store holds the newest verified LOTL and national lists. It is safe for
// concurrent use.
type Store struct {
	cfg config
	now func() time.Time

	mu       sync.Mutex
	lotl     *List
	lists    map[string]*List // territory -> newest verified national list
	problems []error
}

type config struct {
	writeDir  string   // where refreshed lists are stored
	readDirs  []string // caches to load, besides the bootstrap
	bootstrap fs.FS    // nil: none (tests)
	client    *http.Client
	now       func() time.Time
}

// Open loads the bootstrap snapshot and the archive's list cache
// (<archiveDir>/vertrauenslisten). Refresh stores new lists there.
func Open(archiveDir string) (*Store, error) {
	dir := filepath.Join(archiveDir, DirName)
	return open(config{writeDir: dir, readDirs: []string{dir}, bootstrap: bootstrapFS})
}

// OpenReadOnly is Open for read-only mode (spec §9): it reads the archive's
// cache but stores refreshed lists in a cache outside the archive.
func OpenReadOnly(archiveDir string) (*Store, error) {
	tmp := filepath.Join(os.TempDir(), "rechnungsarchiv", DirName)
	return open(config{
		writeDir:  tmp,
		readDirs:  []string{filepath.Join(archiveDir, DirName), tmp},
		bootstrap: bootstrapFS,
	})
}

func open(cfg config) (*Store, error) {
	if cfg.client == nil {
		cfg.client = &http.Client{Timeout: 2 * time.Minute}
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	s := &Store{cfg: cfg, now: cfg.now, lists: map[string]*List{}}
	cands, err := s.candidates()
	if err != nil {
		return nil, err
	}
	s.load(cands)
	return s, nil
}

// Qualify applies spec §6 step 6. path is the timestamp signer certificate
// followed by its validated chain. territory is the trusted list of the
// provider that issued the token, or "" if unknown (see qualify).
func (s *Store) Qualify(path []*x509.Certificate, genTime time.Time, territory string) Decision {
	return qualify(s.Lists(), path, genTime, territory)
}

// Lists returns the newest verified national lists, sorted by territory,
// after the LOTL. It is empty if no LOTL could be verified.
func (s *Store) Lists() []*List {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lotl == nil {
		return nil
	}
	out := []*List{s.lotl}
	for _, cc := range slices.Sorted(maps.Keys(s.lists)) {
		out = append(out, s.lists[cc])
	}
	return out
}

// Problems lists cached files that failed verification, or whose name does
// not match their signed sequence, and were skipped, plus different signed
// lists that share a sequence number.
func (s *Store) Problems() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.problems)
}

func (s *Store) list(territory string) *List {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lists[territory]
}

// Refresh downloads the LOTL and the national lists of the given territories,
// verifies them, stores new versions and marks them as fetched online. It
// keeps going after a failure and returns all errors joined. If the LOTL
// cannot be fetched, the cached one supplies the pointers.
func (s *Store) Refresh(ctx context.Context, territories ...string) error {
	var errs []error
	if err := s.refreshLOTL(ctx); err != nil {
		errs = append(errs, err)
	}
	for _, cc := range territories {
		if err := s.refreshNational(ctx, cc); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Store) refreshLOTL(ctx context.Context) error {
	raw, err := s.fetch(ctx, LOTLURL)
	if err != nil {
		return fmt.Errorf("LOTL: %w", err)
	}
	l, err := verifyLOTL(raw, lotlPins)
	if err != nil {
		return err
	}
	return s.accept(l, func() *List { return s.lotl }, func() { s.lotl = l })
}

func (s *Store) refreshNational(ctx context.Context, cc string) error {
	s.mu.Lock()
	lotl := s.lotl
	s.mu.Unlock()
	if lotl == nil {
		return fmt.Errorf("%s: no verified LOTL", cc)
	}
	p, ok := lotl.pointer(cc)
	if !ok {
		return fmt.Errorf("%s: LOTL %d has no pointer to an XML list", cc, lotl.Sequence)
	}
	raw, err := s.fetch(ctx, p.Location)
	if err != nil {
		return fmt.Errorf("%s: %w", cc, err)
	}
	l, err := verifyNational(raw, lotl, cc)
	if err != nil {
		return err
	}
	return s.accept(l, func() *List { return s.lists[cc] }, func() { s.lists[cc] = l })
}

// accept stores a list fetched and verified online and puts it in use. It
// counts as online evidence only if it is not older than the list in use
// (rollback) and not past its NextUpdate (stale or replayed).
//
// current returns the list in use and is called with s.mu held. The rollback
// check is repeated under the same lock as use: a concurrent Refresh may have
// installed a newer list while this one was being fetched or saved.
func (s *Store) accept(l *List, current func() *List, use func()) error {
	s.mu.Lock()
	err := rollback(l, current())
	s.mu.Unlock()
	if err != nil {
		return err
	}
	if err := s.save(l); err != nil {
		return err
	}
	now := s.now()
	var stale error
	if !l.NextUpdate.IsZero() && now.After(l.NextUpdate) {
		stale = fmt.Errorf("%s: list %d is past its NextUpdate %s", l.Territory, l.Sequence, l.NextUpdate.Format(time.RFC3339))
	} else {
		l.fetchedOnline = now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := rollback(l, current()); err != nil {
		return err
	}
	use()
	return stale
}

// rollback reports an error if l is older than cur: a lower signed sequence,
// or the same sequence with an earlier signed issue date (the order load uses).
func rollback(l, cur *List) error {
	switch {
	case cur == nil:
		return nil
	case l.Sequence < cur.Sequence:
		return fmt.Errorf("%s: server sent list %d, older than known list %d", l.Territory, l.Sequence, cur.Sequence)
	case l.Sequence == cur.Sequence && l.IssueDate.Before(cur.IssueDate):
		return fmt.Errorf("%s: server sent list %d issued %s, older than the known list %d issued %s", l.Territory,
			l.Sequence, l.IssueDate.Format(time.RFC3339), cur.Sequence, cur.IssueDate.Format(time.RFC3339))
	}
	return nil
}

func (s *Store) fetch(ctx context.Context, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return nil, fmt.Errorf("refusing URL %q", url)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "rechnungsarchiv")
	resp, err := s.cfg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return readLimited(resp.Body)
}

func readLimited(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxListSize+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxListSize {
		return nil, fmt.Errorf("list larger than %d bytes", maxListSize)
	}
	return b, nil
}

// save writes the exact signed bytes as <CC>-<sequence>.xml with
// tsa.WriteNew (temp file, fsync, no-replace rename). Existing files are never
// overwritten, also not by a concurrent save: identical bytes are kept, a
// different list with the same sequence gets the next free -2, -3, … suffix.
func (s *Store) save(l *List) error {
	dir := s.cfg.writeDir
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for n := 1; n < 1000; n++ {
		name := fmt.Sprintf("%s-%d.xml", l.Territory, l.Sequence)
		if n > 1 {
			name = fmt.Sprintf("%s-%d-%d.xml", l.Territory, l.Sequence, n)
		}
		target := filepath.Join(dir, name)
		// Fast path for the usual refresh of an unchanged list. Any read
		// error (missing, or on Windows a sharing violation while a
		// concurrent save publishes this name) leaves the decision to
		// WriteNew, which never replaces.
		if existing, err := os.ReadFile(target); err == nil {
			if bytes.Equal(existing, l.Raw) {
				return nil
			}
			continue
		}
		err := tsa.WriteNew(target, l.Raw)
		if !errors.Is(err, tsa.ErrExists) {
			return err // nil: written now, or the same bytes got there first
		}
	}
	return fmt.Errorf("%s-%d: too many versions", l.Territory, l.Sequence)
}

// candidate is a list file found in the bootstrap or a cache directory.
type candidate struct {
	origin    string // for error messages
	territory string // from the file name; the verified content decides
	sequence  int    // from the file name; must equal the signed sequence
	read      func() ([]byte, error)
}

func (s *Store) candidates() ([]candidate, error) {
	var out []candidate
	if s.cfg.bootstrap != nil {
		names, err := fs.Glob(s.cfg.bootstrap, "bootstrap/*.xml.gz")
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			c, ok := parseName(path.Base(name))
			if !ok {
				continue
			}
			c.origin = "bootstrap " + path.Base(name)
			c.read = func() ([]byte, error) { return readGzip(s.cfg.bootstrap, name) }
			out = append(out, c)
		}
	}
	for _, dir := range s.cfg.readDirs {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			c, ok := parseName(e.Name())
			if !ok || !e.Type().IsRegular() || path.Ext(e.Name()) != ".xml" {
				continue
			}
			p := filepath.Join(dir, e.Name())
			c.origin = p
			c.read = func() ([]byte, error) { return readFile(p) }
			out = append(out, c)
		}
	}
	return out, nil
}

func parseName(name string) (candidate, bool) {
	m := fileName.FindStringSubmatch(name)
	if m == nil {
		return candidate{}, false
	}
	seq, _ := strconv.Atoi(m[2])
	return candidate{territory: m[1], sequence: seq}, true
}

// load selects, per territory, the verified list with the highest signed
// sequence and, among different lists with that sequence, the latest signed
// issue date. National lists are checked against the selected LOTL.
func (s *Store) load(cands []candidate) {
	byTerritory := map[string][]candidate{}
	for _, c := range cands {
		byTerritory[c.territory] = append(byTerritory[c.territory], c)
	}
	s.lotl = s.pick(byTerritory[territoryLOTL], func(raw []byte) (*List, error) { return verifyLOTL(raw, lotlPins) })
	if s.lotl == nil {
		return
	}
	for _, cc := range slices.Sorted(maps.Keys(byTerritory)) {
		if cc == territoryLOTL {
			continue
		}
		if l := s.pick(byTerritory[cc], func(raw []byte) (*List, error) { return verifyNational(raw, s.lotl, cc) }); l != nil {
			s.lists[cc] = l
		}
	}
}

// pick verifies the candidates of one territory and returns the newest list.
//
// File names are unsigned: a candidate counts only if the sequence in its
// name equals the signed TSLSequenceNumber, else it is a problem. That makes
// the name a safe upper bound, so candidates are tried by descending name
// sequence and the search stops below the best signed sequence found. Every
// candidate with that sequence is verified: of different lists, the later
// signed ListIssueDateTime wins and the conflict is a problem.
func (s *Store) pick(cands []candidate, verify func([]byte) (*List, error)) *List {
	cands = slices.Clone(cands)
	slices.SortStableFunc(cands, func(a, b candidate) int { return cmp.Compare(b.sequence, a.sequence) })
	var best *List
	var bestOrigin string
	for _, c := range cands {
		if best != nil && c.sequence < best.Sequence {
			break
		}
		raw, err := c.read()
		var l *List
		if err == nil {
			l, err = verify(raw)
		}
		if err == nil && l.Sequence != c.sequence {
			err = fmt.Errorf("file name says sequence %d, the signed list has %d", c.sequence, l.Sequence)
		}
		switch {
		case err != nil:
			s.problems = append(s.problems, fmt.Errorf("%s: %w", c.origin, err))
		case best == nil:
			best, bestOrigin = l, c.origin
		case !bytes.Equal(l.Raw, best.Raw):
			older, newer := c.origin, bestOrigin
			if l.IssueDate.After(best.IssueDate) {
				older, newer = bestOrigin, c.origin
				best, bestOrigin = l, c.origin
			}
			s.problems = append(s.problems, fmt.Errorf("%s and %s: different signed lists with sequence %d; using %s (issued %s)",
				older, newer, l.Sequence, newer, best.IssueDate.Format(time.RFC3339)))
		}
	}
	return best
}

func readFile(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readLimited(f)
}

func readGzip(fsys fs.FS, name string) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	return readLimited(zr)
}
