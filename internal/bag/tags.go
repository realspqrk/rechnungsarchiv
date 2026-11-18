package bag

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Tag file names (spec §3).
const (
	fileBagIt       = "bagit.txt"
	fileBagInfo     = "bag-info.txt"
	fileManifest    = "manifest-sha256.txt"
	fileChain       = "chain.json"
	fileFiles       = "files.json"
	fileSkipped     = "skipped.txt"
	fileVerify      = "VERIFY.txt"
	fileTagManifest = "tagmanifest-sha256.txt"
)

// tagManifestOrder is the fixed order of tagmanifest-sha256.txt.
var tagManifestOrder = []string{fileBagIt, fileBagInfo, fileChain, fileFiles, fileManifest, fileSkipped, fileVerify}

// FormatVersion is the "format" number of chain.json and rechnungsarchiv.json.
const FormatVersion = 1

const bagItContent = "BagIt-Version: 1.0\nTag-File-Character-Encoding: UTF-8\n"

// BagInfo holds bag-info.txt (spec §3), labels in their fixed order.
type BagInfo struct {
	SourceOrganization string
	BaggingDate        string // YYYY-MM-DD, local date, an assertion
	PayloadOxum        string // <octets>.<streams>
	ExternalIdentifier string // <archive_id>/<sequence>
	Belegjahr          int
	RetentionYears     int    // Aufbewahrung-Jahre
	RetentionUntil     string // Aufbewahrung-Bis, YYYY-12-31
	ArchivedBy         string // user@host
	ToolVersion        string // Rechnungsarchiv-Version
	SealNonce          string // 32 lowercase hex
}

var bagInfoLabels = []string{
	"Source-Organization", "Bagging-Date", "Payload-Oxum", "External-Identifier", "Belegjahr",
	"Aufbewahrung-Jahre", "Aufbewahrung-Bis", "Archiviert-Von", "Rechnungsarchiv-Version", "Seal-Nonce",
}

func (bi *BagInfo) values() []string {
	return []string{bi.SourceOrganization, bi.BaggingDate, bi.PayloadOxum, bi.ExternalIdentifier,
		strconv.Itoa(bi.Belegjahr), strconv.Itoa(bi.RetentionYears), bi.RetentionUntil, bi.ArchivedBy,
		bi.ToolVersion, bi.SealNonce}
}

func (bi *BagInfo) render() ([]byte, error) {
	var b bytes.Buffer
	for i, v := range bi.values() {
		if v == "" || strings.ContainsAny(v, "\r\n") || strings.TrimSpace(v) != v {
			return nil, fmt.Errorf("bag-info: invalid value for %s", bagInfoLabels[i])
		}
		fmt.Fprintf(&b, "%s: %s\n", bagInfoLabels[i], v)
	}
	return b.Bytes(), nil
}

var (
	reDate  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	reOxum  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
	reNonce = regexp.MustCompile(`^[0-9a-f]{32}$`)
	reHex   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	reYear  = regexp.MustCompile(`^[1-9]\d{3}$`)
	reInt   = regexp.MustCompile(`^[1-9]\d{0,2}$`)
)

func parseBagInfo(data []byte) (*BagInfo, error) {
	lines, err := splitLines(data)
	if err != nil {
		return nil, err
	}
	if len(lines) != len(bagInfoLabels) {
		return nil, fmt.Errorf("bag-info.txt: %d Zeilen statt %d", len(lines), len(bagInfoLabels))
	}
	v := make([]string, len(lines))
	for i, l := range lines {
		label, val, ok := strings.Cut(l, ": ")
		if !ok || label != bagInfoLabels[i] || val == "" || strings.TrimSpace(val) != val {
			return nil, fmt.Errorf("bag-info.txt: Zeile %d ungültig (erwartet %s)", i+1, bagInfoLabels[i])
		}
		v[i] = val
	}
	if !reDate.MatchString(v[1]) || !reOxum.MatchString(v[2]) || !reYear.MatchString(v[4]) ||
		!reInt.MatchString(v[5]) || !reDate.MatchString(v[6]) || !reNonce.MatchString(v[9]) {
		return nil, fmt.Errorf("bag-info.txt: Wert im falschen Format")
	}
	bi := &BagInfo{SourceOrganization: v[0], BaggingDate: v[1], PayloadOxum: v[2], ExternalIdentifier: v[3],
		RetentionUntil: v[6], ArchivedBy: v[7], ToolVersion: v[8], SealNonce: v[9]}
	bi.Belegjahr, _ = strconv.Atoi(v[4])
	bi.RetentionYears, _ = strconv.Atoi(v[5])
	if bi.RetentionUntil != RetentionUntil(bi.Belegjahr, bi.RetentionYears) {
		return nil, fmt.Errorf("bag-info.txt: Aufbewahrung-Bis passt nicht zu Belegjahr und Aufbewahrung-Jahre")
	}
	return bi, nil
}

// RetentionUntil is 31.12. of Belegjahr + years (ADR-0006), as YYYY-MM-DD.
func RetentionUntil(belegjahr, years int) string {
	return fmt.Sprintf("%04d-12-31", belegjahr+years)
}

// Chain is chain.json (spec §3).
type Chain struct {
	ArchiveID string  `json:"archive_id"`
	Format    int     `json:"format"`
	Previous  *string `json:"previous"` // "sha256:<hex>" or nil for the first Ablage
	Sequence  int     `json:"sequence"`
}

func (c *Chain) render() ([]byte, error) {
	return CanonicalJSON(Obj{"archive_id": c.ArchiveID, "format": c.Format, "previous": c.Previous, "sequence": c.Sequence})
}

func parseChain(data []byte) (*Chain, error) {
	var c Chain
	if err := decodeCanonical(data, &c, func() ([]byte, error) { return c.render() }); err != nil {
		return nil, fmt.Errorf("chain.json: %v", err)
	}
	if c.Format != FormatVersion || c.Sequence < 1 || c.ArchiveID == "" {
		return nil, fmt.Errorf("chain.json: ungültige Werte")
	}
	if c.Previous != nil && !isRoot(*c.Previous) {
		return nil, fmt.Errorf("chain.json: previous ungültig")
	}
	if (c.Previous == nil) != (c.Sequence == 1) {
		return nil, fmt.Errorf("chain.json: previous passt nicht zur Nummer")
	}
	return &c, nil
}

// FileEntry is one element of files.json (spec §3).
type FileEntry struct {
	MTime  string `json:"mtime"` // RFC 3339, UTC
	Path   string `json:"path"`  // "data/…"
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func renderFiles(fs []FileEntry) ([]byte, error) {
	arr := make([]any, len(fs))
	for i, f := range fs {
		arr[i] = Obj{"mtime": f.MTime, "path": f.Path, "sha256": f.SHA256, "size": f.Size}
	}
	return CanonicalJSON(arr)
}

func parseFiles(data []byte) ([]FileEntry, error) {
	var fs []FileEntry
	if err := decodeCanonical(data, &fs, func() ([]byte, error) { return renderFiles(fs) }); err != nil {
		return nil, fmt.Errorf("files.json: %v", err)
	}
	for i, f := range fs {
		if !reHex.MatchString(f.SHA256) || f.Size < 0 || !strings.HasPrefix(f.Path, "data/") {
			return nil, fmt.Errorf("files.json: Eintrag %d ungültig", i+1)
		}
		if i > 0 && fs[i-1].Path >= f.Path {
			return nil, fmt.Errorf("files.json: nicht sortiert")
		}
	}
	return fs, nil
}

// ManifestEntry is one line of manifest-sha256.txt; Path includes "data/".
type ManifestEntry struct {
	SHA256 string
	Path   string
}

func renderManifest(es []ManifestEntry) []byte {
	var b bytes.Buffer
	for _, e := range es {
		fmt.Fprintf(&b, "%s  %s\n", e.SHA256, encodeBagPath(e.Path))
	}
	return b.Bytes()
}

func parseManifest(data []byte) ([]ManifestEntry, error) {
	lines, err := splitLines(data)
	if err != nil {
		return nil, err
	}
	coll := newCollisions()
	es := make([]ManifestEntry, 0, len(lines))
	for i, l := range lines {
		h, p, ok := strings.Cut(l, "  ")
		if !ok || !reHex.MatchString(h) {
			return nil, fmt.Errorf("manifest-sha256.txt: Zeile %d ungültig", i+1)
		}
		p, err := decodeBagPath(p)
		if err != nil || !strings.HasPrefix(p, "data/") || validatePath(p) != nil {
			return nil, fmt.Errorf("manifest-sha256.txt: Zeile %d: ungültiger Pfad", i+1)
		}
		if i > 0 && es[i-1].Path >= p {
			return nil, fmt.Errorf("manifest-sha256.txt: nicht sortiert oder doppelt")
		}
		if err := coll.add(p, false); err != nil {
			return nil, fmt.Errorf("manifest-sha256.txt: %v", err)
		}
		es = append(es, ManifestEntry{SHA256: h, Path: p})
	}
	return es, nil
}

func renderSkipped(paths []string) []byte {
	var b bytes.Buffer
	for _, p := range paths {
		fmt.Fprintf(&b, "system\t%s\n", encodeBagPath(p))
	}
	return b.Bytes()
}

func parseSkipped(data []byte) ([]string, error) {
	lines, err := splitLines(data)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		p, ok := strings.CutPrefix(l, "system\t")
		if !ok || p == "" {
			return nil, fmt.Errorf("skipped.txt: Zeile %d ungültig", i+1)
		}
		p, err := decodeBagPath(p)
		if err != nil {
			return nil, fmt.Errorf("skipped.txt: Zeile %d ungültig", i+1)
		}
		out = append(out, p)
	}
	return out, nil
}

// tagManifest renders tagmanifest-sha256.txt for the given tag file bytes.
func renderTagManifest(tags map[string][]byte) []byte {
	var b bytes.Buffer
	for _, n := range tagManifestOrder {
		sum := sha256.Sum256(tags[n])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), n)
	}
	return b.Bytes()
}

// RootOf returns the Prüfwert for tagmanifest bytes: "sha256:<hex>".
func RootOf(tagManifest []byte) string {
	sum := sha256.Sum256(tagManifest)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func isRoot(s string) bool {
	h, ok := strings.CutPrefix(s, "sha256:")
	return ok && reHex.MatchString(h)
}

// splitLines splits LF-terminated UTF-8 text without BOM or CR. Empty input
// yields no lines.
func splitLines(data []byte) ([]string, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) || bytes.IndexByte(data, '\r') >= 0 || data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("Textformat ungültig (BOM, CR oder fehlender Zeilenumbruch am Ende)")
	}
	lines := strings.Split(string(data[:len(data)-1]), "\n")
	for _, l := range lines {
		if l == "" {
			return nil, fmt.Errorf("Textformat ungültig (leere Zeile)")
		}
	}
	return lines, nil
}

// verifyText is VERIFY.txt (fixed, DE/EN).
const verifyText = `Rechnungsarchiv - manuelle Kontrolle / manual verification

DEUTSCH
Dieser Ordner ist ein BagIt-Paket (RFC 8493). Die Originaldateien liegen
unverändert im Unterordner data/.
1. Dateien kontrollieren:    sha256sum -c manifest-sha256.txt
2. Metadaten kontrollieren:  sha256sum -c tagmanifest-sha256.txt
3. Prüfwert berechnen:       sha256sum tagmanifest-sha256.txt
   Der Prüfwert ist "sha256:" gefolgt von diesem Hashwert.
4. Zeitstempel kontrollieren (Ordner <Ablage>.nachweis neben der ZIP-Datei):
   openssl ts -verify -digest <Hashwert aus Schritt 3> -in 1.tsr
   mit der Zertifikatskette aus 1.chain.pem.
chain.json verbindet diese Ablage mit der vorigen Ablage ("previous" ist
deren Prüfwert). files.json enthält die ursprünglichen Änderungsdaten,
skipped.txt die nicht archivierten Systemdateien.
Der Zeitstempel belegt, dass diese Dateien spätestens zu seinem Zeitpunkt
in dieser Form vorlagen. Er belegt nicht, dass sie echt oder vollständig sind.

ENGLISH
This folder is a BagIt bag (RFC 8493). The original files are stored
unchanged in the data/ subfolder.
1. Check the files:          sha256sum -c manifest-sha256.txt
2. Check the metadata:       sha256sum -c tagmanifest-sha256.txt
3. Compute the root value:   sha256sum tagmanifest-sha256.txt
   The root value (Prüfwert) is "sha256:" followed by this hash.
4. Check the timestamp (folder <Ablage>.nachweis next to the ZIP file):
   openssl ts -verify -digest <hash from step 3> -in 1.tsr
   using the certificate chain from 1.chain.pem.
chain.json links this Ablage to the previous one ("previous" is its root
value). files.json holds the original modification times, skipped.txt the
system files that were not archived.
The timestamp proves that these files existed in this form no later than
its time. It does not prove that they are genuine or complete.
`
