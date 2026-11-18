package export

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var errInside = errors.New("Der Exportordner darf nicht im Archiv liegen (und das Archiv nicht im Exportordner)")

// guard keeps the export out of the source archive (spec §8: the export
// never writes into it). Paths are compared where they physically lead,
// so a junction or symbolic link cannot smuggle the target into the
// source, and the target path may not pass through any link at all.
type guard struct{ src, dst string }

// checkTarget refuses a target inside the source archive (or the other way
// round), a target behind a link or junction, and a target that already
// has content.
func checkTarget(src, dst string) (*guard, error) {
	g := &guard{src: src, dst: dst}
	if err := g.check(); err != nil {
		return nil, err
	}
	des, err := os.ReadDir(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return g, nil
	}
	if err != nil {
		return nil, err
	}
	if len(des) > 0 {
		return nil, errors.New("Der Exportordner ist nicht leer")
	}
	return g, nil
}

// check runs the containment checks. Run it again right before writing.
func (g *guard) check() error {
	as, err1 := filepath.Abs(g.src)
	ad, err2 := filepath.Abs(g.dst)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if within(ad, as) || within(as, ad) {
		return errInside
	}
	if err := noLinks(ad); err != nil {
		return err
	}
	ps, err1 := physical(as)
	pd, err2 := physical(ad)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if within(pd, ps) || within(ps, pd) {
		return errInside
	}
	return nil
}

func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// physical resolves the deepest existing ancestor of the absolute path p
// through links and junctions and appends the rest.
func physical(p string) (string, error) {
	cur := p
	var rest []string
	for {
		_, err := os.Stat(cur)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, nil
		}
		rest = append(rest, filepath.Base(cur))
		cur = parent
	}
	real, err := finalPath(cur)
	if err != nil {
		return "", err
	}
	for i := len(rest) - 1; i >= 0; i-- {
		real = filepath.Join(real, rest[i])
	}
	return real, nil
}

func errLink(p string) error {
	return fmt.Errorf("Der Exportordner liegt hinter einer Verknüpfung (Junction oder symbolischer Link): %s. Bitte einen gewöhnlichen Ordner wählen", p)
}

// mkNew creates a folder inside the new export. It must not exist yet, so
// nothing planted there (a junction) is followed.
func mkNew(dir string) error {
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(dir); err != nil || isLink(fi) {
		return errLink(dir)
	}
	return nil
}
