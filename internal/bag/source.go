package bag

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// srcFile is one file of a bag, from a ZIP entry or an extracted directory.
type srcFile struct {
	rel  string // relative to the bag directory, NFC, slash-separated
	size int64
	open func() (io.ReadCloser, error)
	crc  *uint32 // ZIP only: CRC-32 from the central directory
}

// source is an opened bag: a profile ZIP or an extracted bag directory.
type source struct {
	dir       string // name of the bag directory
	files     map[string]*srcFile
	extracted bool // an extracted directory, not a ZIP
	closer    io.Closer
}

func (s *source) Close() error {
	if s.closer != nil {
		return s.closer.Close()
	}
	return nil
}

var reTopDir = regexp.MustCompile(`^\d{4,}_`)

// openSource opens path as a ZIP file or, if it is a directory, as an
// extracted bag. Profile violations are returned as *formatError.
func openSource(path string) (*source, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		return openDir(path)
	}
	return openZip(path, fi.Size())
}

func openZip(path string, size int64) (*source, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	src, err := zipSource(f, size)
	if err != nil {
		f.Close()
		return nil, err
	}
	src.closer = f
	return src, nil
}

func zipSource(f *os.File, size int64) (*source, error) {
	entries, err := parseZip(f, size)
	if err != nil {
		return nil, err
	}
	src := &source{files: map[string]*srcFile{}}
	coll := newCollisions()
	var total int64
	rank := 0 // entry order (spec §2): bagit.txt, payload, other tags, tagmanifest
	for _, e := range entries {
		top, rel, ok := strings.Cut(e.name, "/")
		if !ok || top == "" {
			return nil, bad("Eintrag %q liegt außerhalb des Ablage-Ordners", e.name)
		}
		if src.dir == "" {
			if !reTopDir.MatchString(top) || ValidateComponent(top) != "" {
				return nil, bad("Ablage-Ordner %q hat keinen gültigen Namen", top)
			}
			src.dir = top
		} else if top != src.dir {
			return nil, bad("mehr als ein Hauptordner (%q, %q)", src.dir, top)
		}
		if e.dir {
			rel = strings.TrimSuffix(rel, "/")
			if rel == "" {
				continue
			}
		}
		if err := validatePath(rel); err != nil {
			return nil, bad("%v", err)
		}
		if err := coll.add(rel, e.dir); err != nil {
			return nil, bad("doppelter Eintrag: %v", err)
		}
		if e.dir {
			continue
		}
		r := entryRank(rel)
		if r < rank || r == 0 && len(src.files) > 0 || rank == 3 {
			return nil, bad("Reihenfolge der Einträge verletzt bei %q (erwartet: bagit.txt, Nutzdaten, Steuerdateien, tagmanifest-sha256.txt zuletzt)", e.name)
		}
		rank = r
		if strings.HasPrefix(rel, "data/") && !addBounded(&total, int64(e.size), totalLimit) {
			return nil, bad("Nutzdaten zu groß (höchstens %d GiB)", MaxTotalBytes>>30)
		}
		off, n, crc := int64(e.dataOff), int64(e.size), e.crc
		src.files[rel] = &srcFile{rel: rel, size: n, crc: &crc, open: func() (io.ReadCloser, error) {
			return io.NopCloser(io.NewSectionReader(f, off, n)), nil
		}}
	}
	return src, nil
}

// entryRank is the position class of a ZIP entry in the required order.
func entryRank(rel string) int {
	switch {
	case rel == fileBagIt:
		return 0
	case strings.HasPrefix(rel, "data/"):
		return 1
	case rel == fileTagManifest:
		return 3
	}
	return 2
}

func openDir(path string) (*source, error) {
	// Accept a folder that only wraps the bag, as "Extract all" creates it.
	if _, err := os.Stat(filepath.Join(path, fileBagIt)); err != nil {
		if des, err := os.ReadDir(path); err == nil && len(des) == 1 && des[0].IsDir() {
			inner := filepath.Join(path, des[0].Name())
			if _, err := os.Stat(filepath.Join(inner, fileBagIt)); err == nil {
				path = inner
			}
		}
	}
	src := &source{dir: filepath.Base(path), files: map[string]*srcFile{}, extracted: true}
	coll := newCollisions()
	var total int64
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == path {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			return err
		}
		rel = norm.NFC.String(filepath.ToSlash(rel))
		if err := validatePath(rel); err != nil {
			return &formatError{err.Error()}
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 || checkAttrs(fi) != nil || !fi.IsDir() && !fi.Mode().IsRegular() {
			return &formatError{fmt.Sprintf("Verknüpfung oder Sonderdatei nicht erlaubt: %s", rel)}
		}
		if len(src.files) >= maxEntries {
			return &formatError{"zu viele Dateien"}
		}
		if err := coll.add(rel, fi.IsDir()); err != nil {
			return &formatError{err.Error()}
		}
		if fi.IsDir() {
			return nil
		}
		if strings.HasPrefix(rel, "data/") && !addBounded(&total, fi.Size(), totalLimit) {
			return &formatError{fmt.Sprintf("Nutzdaten zu groß (höchstens %d GiB)", MaxTotalBytes>>30)}
		}
		osPath := p
		src.files[rel] = &srcFile{rel: rel, size: fi.Size(), open: func() (io.ReadCloser, error) { return os.Open(osPath) }}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return src, nil
}

// read reads a whole small file with a size bound. crcOK is false if a ZIP
// entry does not match its CRC-32.
func (s *source) read(rel string, limit int64) (data []byte, crcOK bool, err error) {
	f := s.files[rel]
	if f.size > limit {
		return nil, false, &formatError{fmt.Sprintf("%s ist zu groß", rel)}
	}
	rc, err := f.open()
	if err != nil {
		return nil, false, err
	}
	defer rc.Close()
	data, err = io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) != f.size {
		return nil, false, fmt.Errorf("%s: Größe hat sich beim Lesen geändert", rel)
	}
	return data, f.crc == nil || crc32.ChecksumIEEE(data) == *f.crc, nil
}

// hash streams a file and returns its SHA-256 hex and whether the CRC-32
// (ZIP only) matched.
func (s *source) hash(rel string) (string, bool, error) {
	f := s.files[rel]
	rc, err := f.open()
	if err != nil {
		return "", false, err
	}
	defer rc.Close()
	h, c := sha256.New(), crc32.NewIEEE()
	n, err := io.Copy(io.MultiWriter(h, c), io.LimitReader(rc, f.size+1))
	if err != nil {
		return "", false, err
	}
	if n != f.size {
		return "", false, fmt.Errorf("%s: Größe hat sich beim Lesen geändert", rel)
	}
	return hex.EncodeToString(h.Sum(nil)), f.crc == nil || c.Sum32() == *f.crc, nil
}

func (s *source) sorted() []string {
	out := make([]string, 0, len(s.files))
	for k := range s.files {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
