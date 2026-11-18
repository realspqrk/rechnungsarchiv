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
// followed by its validated chain.
func (s *Store) Qualify(path []*x509.Certificate, genTime time.Time) Decision {
	return qualify(s.Lists(), path, genTime)
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

// Problems lists cached files that failed verification and were skipped.
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
	s.mu.Lock()
	cur := s.lotl
	s.mu.Unlock()
	return s.accept(l, cur, func() { s.lotl = l })
}

func (s *Store) refreshNational(ctx context.Context, cc string) error {
	s.mu.Lock()
	lotl, cur := s.lotl, s.lists[cc]
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
	return s.accept(l, cur, func() { s.lists[cc] = l })
}

// accept stores a list fetched and verified online and puts it in use. It
// counts as online evidence only if it is not older than the list in use
// (rollback) and not past its NextUpdate (stale or replayed).
func (s *Store) accept(l, cur *List, use func()) error {
	if cur != nil && l.Sequence < cur.Sequence {
		return fmt.Errorf("%s: server sent list %d, older than known list %d", l.Territory, l.Sequence, cur.Sequence)
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
	use()
	s.mu.Unlock()
	return stale
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

// save writes the exact signed bytes as <CC>-<sequence>.xml (temp file, fsync,
// rename). Existing files are never overwritten: identical bytes are kept, a
// different list with the same sequence gets a -2, -3, … suffix.
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
		existing, err := os.ReadFile(target)
		switch {
		case err == nil && bytes.Equal(existing, l.Raw):
			return nil
		case err == nil:
			continue
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
		return writeNew(target, l.Raw)
	}
	return fmt.Errorf("%s-%d: too many versions", l.Territory, l.Sequence)
}

func writeNew(target string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// candidate is a list file found in the bootstrap or a cache directory.
type candidate struct {
	origin    string // for error messages
	territory string // from the file name; the verified content decides
	sequence  int
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

// load verifies candidates newest first per territory and keeps the first
// that verifies. National lists are checked against the newest verified LOTL.
func (s *Store) load(cands []candidate) {
	slices.SortStableFunc(cands, func(a, b candidate) int { return cmp.Compare(b.sequence, a.sequence) })
	try := func(c candidate, verify func([]byte) (*List, error)) *List {
		raw, err := c.read()
		if err == nil {
			var l *List
			if l, err = verify(raw); err == nil {
				return l
			}
		}
		s.problems = append(s.problems, fmt.Errorf("%s: %w", c.origin, err))
		return nil
	}
	for _, c := range cands {
		if c.territory == territoryLOTL && s.lotl == nil {
			s.lotl = try(c, func(raw []byte) (*List, error) { return verifyLOTL(raw, lotlPins) })
		}
	}
	if s.lotl == nil {
		return
	}
	for _, c := range cands {
		if c.territory == territoryLOTL || s.lists[c.territory] != nil {
			continue
		}
		if l := try(c, func(raw []byte) (*List, error) { return verifyNational(raw, s.lotl, c.territory) }); l != nil {
			s.lists[c.territory] = l
		}
	}
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
