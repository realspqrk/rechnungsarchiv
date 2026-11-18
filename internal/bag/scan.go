package bag

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/text/unicode/norm"
)

// Input is a scanned input folder, ready to be written as a bag.
type Input struct {
	Root    string      // the scanned folder
	Files   []InputFile // sorted by Path
	Skipped []string    // input-relative paths of system junk, sorted
}

// InputFile is one payload file. Path is relative to the input folder,
// slash-separated and NFC-normalized; OSPath is where it is read from.
type InputFile struct {
	Path    string
	OSPath  string
	Size    int64
	ModTime time.Time
}

// Scan walks dir and applies the payload rules of spec §3. It fails on the
// first rule violation; empty directories are ignored and junk is skipped.
func Scan(dir string) (*Input, error) {
	in := &Input{Root: dir}
	coll := newCollisions()
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = norm.NFC.String(filepath.ToSlash(rel))
		if !d.IsDir() && isJunk(rel) {
			in.Skipped = append(in.Skipped, rel)
			return nil
		}
		// Validate the name first: Windows cannot even stat some invalid names.
		if err := validatePath(rel); err != nil {
			return err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return err
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrLink, rel)
		}
		if err := checkAttrs(fi); err != nil {
			return fmt.Errorf("%w: %s", err, rel)
		}
		if !fi.IsDir() && !fi.Mode().IsRegular() {
			return fmt.Errorf("%w: %s (keine normale Datei)", ErrLink, rel)
		}
		if fi.IsDir() {
			return nil // directories only matter through their files
		}
		if err := coll.add("data/"+rel, false); err != nil {
			return err
		}
		in.Files = append(in.Files, InputFile{Path: rel, OSPath: p, Size: fi.Size(), ModTime: fi.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(in.Files) == 0 {
		return nil, ErrEmpty
	}
	sort.Slice(in.Files, func(i, j int) bool { return in.Files[i].Path < in.Files[j].Path })
	sort.Strings(in.Skipped)
	return in, nil
}
