package bag

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// Input errors. Messages are German because the UI shows them as-is.
var (
	ErrInvalidName = errors.New("ungültiger Dateiname")
	ErrCollision   = errors.New("Dateinamen-Konflikt")
	ErrLink        = errors.New("Verknüpfung nicht erlaubt")
	ErrPlaceholder = errors.New("Datei ist nur in der Cloud, nicht lokal gespeichert")
	ErrChanged     = errors.New("Datei wurde während des Archivierens verändert")
	ErrEmpty       = errors.New("Ordner enthält keine Dateien")
)

const (
	maxNameBytes      = 1024 // whole relative path, UTF-8 bytes
	maxComponentUTF16 = 255
)

// ValidateComponent checks one path component against the Windows naming
// rules (spec §3): reserved names, trailing dot or space, forbidden
// characters. It returns a short German reason, or "" if the name is fine.
func ValidateComponent(c string) string {
	if c == "" || c == "." || c == ".." {
		return "leerer oder relativer Namensteil"
	}
	if !utf8.ValidString(c) {
		return "keine gültige UTF-8-Kodierung"
	}
	if len(utf16.Encode([]rune(c))) > maxComponentUTF16 {
		return "Name zu lang"
	}
	for _, r := range c {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return fmt.Sprintf("unerlaubtes Zeichen %q", r)
		}
	}
	if strings.HasSuffix(c, ".") || strings.HasSuffix(c, " ") {
		return "endet mit Punkt oder Leerzeichen"
	}
	stem := c
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL":
		return "unter Windows reservierter Name"
	}
	if len(stem) >= 4 && (strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT")) {
		if d := stem[3:]; len(d) == 1 && d[0] >= '0' && d[0] <= '9' || d == "¹" || d == "²" || d == "³" {
			return "unter Windows reservierter Name"
		}
	}
	return ""
}

// validatePath checks a relative slash-separated path: NFC, no absolute
// paths, no "..", no backslashes, every component valid on Windows.
func validatePath(p string) error {
	if p == "" || len(p) > maxNameBytes {
		return fmt.Errorf("%w: %q (Länge)", ErrInvalidName, p)
	}
	if !utf8.ValidString(p) {
		return fmt.Errorf("%w: %q (keine gültige UTF-8-Kodierung)", ErrInvalidName, p)
	}
	if !norm.NFC.IsNormalString(p) {
		return fmt.Errorf("%w: %q (nicht NFC-normalisiert)", ErrInvalidName, p)
	}
	if strings.HasPrefix(p, "/") || strings.Contains(p, `\`) {
		return fmt.Errorf("%w: %q (absoluter Pfad oder Backslash)", ErrInvalidName, p)
	}
	for _, c := range strings.Split(p, "/") {
		if why := ValidateComponent(c); why != "" {
			return fmt.Errorf("%w: %q (%s)", ErrInvalidName, p, why)
		}
	}
	return nil
}

var folder = cases.Fold()

// foldKey is the identity used for duplicate detection: NFC + Unicode case fold.
func foldKey(p string) string {
	return norm.NFC.String(folder.String(norm.NFC.String(p)))
}

// collisions detects names that collide after NFC + case fold, including a
// file colliding with a directory prefix of another path. isDir marks
// directory entries (allowed to repeat as prefixes).
type collisions struct {
	seen map[string]collisionEntry
}

type collisionEntry struct {
	name string
	dir  bool
}

func newCollisions() *collisions { return &collisions{seen: map[string]collisionEntry{}} }

func (c *collisions) add(p string, isDir bool) error {
	parts := strings.Split(p, "/")
	for i := 1; i <= len(parts); i++ {
		sub := strings.Join(parts[:i], "/")
		dir := i < len(parts) || isDir
		k := foldKey(sub)
		if prev, ok := c.seen[k]; ok {
			if prev.dir && dir && prev.name == sub {
				continue
			}
			return fmt.Errorf("%w: %q und %q (gleicher Name nach Unicode-Normalisierung und Groß-/Kleinschreibung)", ErrCollision, prev.name, sub)
		}
		c.seen[k] = collisionEntry{name: sub, dir: dir}
	}
	return nil
}

// isJunk reports system files that are never archived (spec §3 skipped.txt).
func isJunk(rel string) bool {
	b := path.Base(rel)
	switch {
	case strings.EqualFold(b, "Thumbs.db"), strings.EqualFold(b, "desktop.ini"), b == ".DS_Store":
		return true
	}
	return strings.HasPrefix(b, "~$")
}

// encodeBagPath percent-encodes '%', CR and LF as BagIt (RFC 8493 §2.1.3) requires.
func encodeBagPath(p string) string {
	if !strings.ContainsAny(p, "%\r\n") {
		return p
	}
	r := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return r.Replace(p)
}

func decodeBagPath(p string) (string, error) {
	if !strings.Contains(p, "%") {
		return p, nil
	}
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		if p[i] != '%' {
			b.WriteByte(p[i])
			continue
		}
		if i+2 >= len(p) {
			return "", fmt.Errorf("bad percent-encoding")
		}
		switch strings.ToUpper(p[i+1 : i+3]) {
		case "25":
			b.WriteByte('%')
		case "0D":
			b.WriteByte('\r')
		case "0A":
			b.WriteByte('\n')
		default:
			return "", fmt.Errorf("bad percent-encoding")
		}
		i += 2
	}
	return b.String(), nil
}
