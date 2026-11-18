package bag

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Parsing limits (spec §2: a verifier MUST bound its parsing).
const (
	maxEntries   = 200_000
	maxCDBytes   = 128 << 20
	maxEOCDBytes = 22 + max16
	maxEntryName = 2048 // top dir + "data/" + payload path
)

// MaxTotalBytes is the largest payload an Ablage may hold: the sum of all
// payload file sizes, 64 GiB. Scan and Write refuse larger input; Check
// treats a larger ZIP or extracted bag as Ungültig before hashing anything.
const MaxTotalBytes = 64 << 30

// totalLimit is MaxTotalBytes; tests lower it.
var totalLimit int64 = MaxTotalBytes

var errTooLarge = fmt.Errorf("Ordner zu groß: höchstens %d GiB pro Ablage", MaxTotalBytes>>30)

// addBounded adds n to *total if the sum stays within [0, limit], without
// overflowing, and reports whether it did.
func addBounded(total *int64, n, limit int64) bool {
	if n < 0 || *total > limit || n > limit-*total {
		return false
	}
	*total += n
	return true
}

// maxZipBytes bounds the size of an Ablage ZIP: the payload limit plus the
// most that tag files, local and central headers and the end records add.
func maxZipBytes() int64 {
	perEntry := int64(30 + maxEntryName + 20 + 46 + maxEntryName + 28)
	return totalLimit + int64(len(tagFiles))*maxTagBytes + maxEntries*perEntry + 22 + 56 + 20
}

type zipEntry struct {
	name      string
	size      uint64
	crc       uint32
	headerOff uint64
	dataOff   uint64
	dir       bool
}

// formatError is a violation of the ZIP profile (spec §2) or of the bag
// structure → Ungültig.
type formatError struct{ msg string }

func (e *formatError) Error() string { return e.msg }

func bad(format string, a ...any) error {
	return &formatError{"ZIP-Format verletzt: " + fmt.Sprintf(format, a...)}
}

// parseZip reads the central directory of a profile ZIP and cross-checks
// every local header. It does not read entry data.
func parseZip(r io.ReaderAt, size int64) ([]zipEntry, error) {
	le := binary.LittleEndian
	if size < 22 {
		return nil, bad("Datei zu klein")
	}
	if size > maxZipBytes() {
		return nil, bad("Datei zu groß für eine Ablage")
	}
	tail := min(size, maxEOCDBytes)
	buf := make([]byte, tail)
	if _, err := r.ReadAt(buf, size-tail); err != nil {
		return nil, err
	}
	// No archive comment: the end record is the last 22 bytes.
	eocd := len(buf) - 22
	if le.Uint32(buf[eocd:]) != sigEOCD || le.Uint16(buf[eocd+20:]) != 0 {
		for i := len(buf) - 22; i >= 0; i-- {
			if le.Uint32(buf[i:]) == sigEOCD && i+22+int(le.Uint16(buf[i+20:])) == len(buf) {
				return nil, bad("Archivkommentar nicht erlaubt")
			}
		}
		return nil, bad("Verzeichnisende nicht gefunden")
	}
	e := buf[eocd:]
	eocdPos := uint64(size-tail) + uint64(eocd)
	if le.Uint16(e[4:]) != 0 || le.Uint16(e[6:]) != 0 {
		return nil, bad("mehrteiliges ZIP")
	}
	nDisk, n, cdSize, cdOff := uint64(le.Uint16(e[8:])), uint64(le.Uint16(e[10:])), uint64(le.Uint32(e[12:])), uint64(le.Uint32(e[16:]))
	cdEnd := eocdPos
	if n == max16 || nDisk == max16 || cdSize == max32 || cdOff == max32 {
		if eocdPos < 20+56 {
			return nil, bad("zip64-Ende fehlt")
		}
		var loc [20]byte
		if _, err := r.ReadAt(loc[:], int64(eocdPos-20)); err != nil {
			return nil, err
		}
		if le.Uint32(loc[0:]) != sigZip64Loc || le.Uint32(loc[4:]) != 0 || le.Uint32(loc[16:]) != 1 {
			return nil, bad("zip64-Locator ungültig")
		}
		z64Pos := le.Uint64(loc[8:])
		if z64Pos != eocdPos-20-56 {
			return nil, bad("zip64-Ende an falscher Stelle")
		}
		var z [56]byte
		if _, err := r.ReadAt(z[:], int64(z64Pos)); err != nil {
			return nil, err
		}
		if le.Uint32(z[0:]) != sigZip64EOCD || le.Uint64(z[4:]) != 44 || le.Uint32(z[16:]) != 0 || le.Uint32(z[20:]) != 0 {
			return nil, bad("zip64-Ende ungültig")
		}
		nDisk, n, cdSize, cdOff = le.Uint64(z[24:]), le.Uint64(z[32:]), le.Uint64(z[40:]), le.Uint64(z[48:])
		if n < max16 && cdSize < max32 && cdOff < max32 {
			return nil, bad("zip64 ohne Notwendigkeit")
		}
		cdEnd = z64Pos
	}
	if nDisk != n {
		return nil, bad("mehrteiliges ZIP")
	}
	if n == 0 || n > maxEntries {
		return nil, bad("Anzahl der Einträge %d außerhalb der Grenzen", n)
	}
	if cdSize > maxCDBytes || cdOff+cdSize != cdEnd {
		return nil, bad("Zentralverzeichnis ungültig")
	}
	cd := make([]byte, cdSize)
	if _, err := r.ReadAt(cd, int64(cdOff)); err != nil {
		return nil, err
	}
	entries := make([]zipEntry, 0, n)
	p := cd
	for i := uint64(0); i < n; i++ {
		if len(p) < 46 || le.Uint32(p) != sigCentral {
			return nil, bad("Zentralverzeichnis-Eintrag %d ungültig", i)
		}
		verMade, verNeed, flags, method := le.Uint16(p[4:]), le.Uint16(p[6:]), le.Uint16(p[8:]), le.Uint16(p[10:])
		tm, dt, crc := le.Uint16(p[12:]), le.Uint16(p[14:]), le.Uint32(p[16:])
		csize, usize := uint64(le.Uint32(p[20:])), uint64(le.Uint32(p[24:]))
		nl, el, cl := int(le.Uint16(p[28:])), int(le.Uint16(p[30:])), int(le.Uint16(p[32:]))
		disk, ext, off := le.Uint16(p[34:]), le.Uint32(p[38:]), uint64(le.Uint32(p[42:]))
		if len(p) < 46+nl+el+cl {
			return nil, bad("Zentralverzeichnis abgeschnitten")
		}
		name := string(p[46 : 46+nl])
		extra := p[46+nl : 46+nl+el]
		p = p[46+nl+el+cl:]
		if flags&0x41 != 0 {
			return nil, bad("verschlüsselter Eintrag %q", name)
		}
		if flags != flagUTF8 {
			return nil, bad("unerlaubte Flags 0x%04x bei %q (erwartet: nur UTF-8)", flags, name)
		}
		if method != 0 {
			return nil, bad("Kompressionsmethode %d bei %q (erlaubt: nur Stored)", method, name)
		}
		if tm != dosTime0000 || dt != dosDate1980 {
			return nil, bad("Zeitangabe bei %q ist nicht 1980-01-01 00:00", name)
		}
		if cl != 0 || disk != 0 {
			return nil, bad("Kommentar oder Datenträgernummer bei %q", name)
		}
		if verMade>>8 == 3 && (ext>>16)&0xF000 == 0xA000 {
			return nil, bad("symbolische Verknüpfung %q", name)
		}
		if nl == 0 || nl > maxEntryName || !utf8.ValidString(name) {
			return nil, bad("ungültiger Name %q", name)
		}
		if !norm.NFC.IsNormalString(name) {
			return nil, bad("Name %q ist nicht NFC-normalisiert", name)
		}
		var err error
		usize, csize, off, err = parseZip64Extra(extra, usize, csize, off, true, name)
		if err != nil {
			return nil, err
		}
		if usize != csize {
			return nil, bad("Größenangaben bei %q widersprechen sich", name)
		}
		le2 := zipEntry{name: name, size: usize, crc: crc, headerOff: off, dir: strings.HasSuffix(name, "/")}
		if le2.dir && (usize != 0 || crc != 0) {
			return nil, bad("Verzeichniseintrag %q mit Inhalt", name)
		}
		dataOff, err := checkLocal(r, le2, verNeed, cdOff)
		if err != nil {
			return nil, err
		}
		le2.dataOff = dataOff
		entries = append(entries, le2)
	}
	if len(p) != 0 {
		return nil, bad("Daten hinter dem Zentralverzeichnis")
	}
	// The entries lie back to back in central-directory order, from the
	// first byte up to the central directory: no overlaps, no gaps, no
	// hidden data, and the physical order equals the central order.
	next := uint64(0)
	for _, e := range entries {
		if e.headerOff != next {
			return nil, bad("Einträge nicht lückenlos in der Reihenfolge des Zentralverzeichnisses (%q)", e.name)
		}
		next = e.dataOff + e.size
		if next < e.dataOff || next > cdOff {
			return nil, bad("Einträge überlappen (%q)", e.name)
		}
	}
	if next != cdOff {
		return nil, bad("Daten vor dem Zentralverzeichnis")
	}
	return entries, nil
}

// parseZip64Extra accepts no extra field, or exactly one zip64 field that
// holds precisely the values marked 0xFFFFFFFF, each of which must really
// need 64 bits ("zip64 only when needed").
func parseZip64Extra(extra []byte, usize, csize, off uint64, central bool, name string) (uint64, uint64, uint64, error) {
	le := binary.LittleEndian
	want := 0
	for _, v := range []uint64{usize, csize} {
		if v == max32 {
			want++
		}
	}
	if central && off == max32 {
		want++
	}
	if want == 0 {
		if len(extra) != 0 {
			return 0, 0, 0, bad("unerlaubtes Zusatzfeld bei %q", name)
		}
		return usize, csize, off, nil
	}
	if len(extra) != 4+8*want || le.Uint16(extra) != zip64ExtraTag || int(le.Uint16(extra[2:])) != 8*want {
		return 0, 0, 0, bad("unerlaubtes Zusatzfeld bei %q", name)
	}
	vals := extra[4:]
	next := func() uint64 {
		v := le.Uint64(vals)
		vals = vals[8:]
		if v < max32 {
			want = -1 // marks an unnecessary zip64 value
		}
		return v
	}
	if usize == max32 {
		usize = next()
	}
	if csize == max32 {
		csize = next()
	}
	if central && off == max32 {
		off = next()
	}
	if want < 0 {
		return 0, 0, 0, bad("zip64 ohne Notwendigkeit bei %q", name)
	}
	return usize, csize, off, nil
}

func checkLocal(r io.ReaderAt, e zipEntry, verNeed uint16, cdOff uint64) (uint64, error) {
	le := binary.LittleEndian
	if e.headerOff+30 > cdOff {
		return 0, bad("lokaler Kopf von %q außerhalb", e.name)
	}
	var h [30]byte
	if _, err := r.ReadAt(h[:], int64(e.headerOff)); err != nil {
		return 0, bad("lokaler Kopf von %q unlesbar", e.name)
	}
	nl, el := uint64(le.Uint16(h[26:])), uint64(le.Uint16(h[28:]))
	if le.Uint32(h[0:]) != sigLocal || le.Uint16(h[4:]) != verNeed || le.Uint16(h[6:]) != flagUTF8 ||
		le.Uint16(h[8:]) != 0 || le.Uint16(h[10:]) != dosTime0000 || le.Uint16(h[12:]) != dosDate1980 ||
		le.Uint32(h[14:]) != e.crc || nl != uint64(len(e.name)) {
		return 0, bad("lokaler Kopf von %q passt nicht zum Zentralverzeichnis", e.name)
	}
	if e.headerOff+30+nl+el > cdOff {
		return 0, bad("lokaler Kopf von %q außerhalb", e.name)
	}
	ne := make([]byte, nl+el)
	if _, err := r.ReadAt(ne, int64(e.headerOff+30)); err != nil {
		return 0, err
	}
	if !bytes.Equal(ne[:nl], []byte(e.name)) {
		return 0, bad("lokaler Name von %q weicht ab", e.name)
	}
	cs, us := uint64(le.Uint32(h[18:])), uint64(le.Uint32(h[22:]))
	us, cs, _, err := parseZip64Extra(ne[nl:], us, cs, 0, false, e.name)
	if err != nil {
		return 0, err
	}
	if us != e.size || cs != e.size {
		return 0, bad("lokale Größe von %q weicht ab", e.name)
	}
	return e.headerOff + 30 + nl + el, nil
}
