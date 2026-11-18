package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

// Options for Archive. Zero values mean "use the default".
type Options struct {
	Name           string // Ablage name; default: the folder name
	Belegjahr      int    // default: the year in the folder name
	RetentionYears int    // 7, 10, 12 or 22; default: Config.RetentionDefault
}

var ErrNeedBelegjahr = errors.New("Belegjahr fehlt: der Ordnername enthält keine Jahreszahl, bitte angeben")

var reYear = regexp.MustCompile(`(?:^|\D)((?:19|20)\d{2})(?:\D|$)`)

// BelegjahrFromName returns the first plausible year in a folder name
// ("2026-11 Eingangsrechnungen" → 2026), or 0.
func BelegjahrFromName(name string) int {
	m := reYear.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	y, _ := strconv.Atoi(m[1])
	return y
}

// Archive commits folder as a new Ablage, following spec §4 exactly:
// lock → write .zip.tmp → fsync → re-open and check fully → no-replace
// rename (the commit) → read-only attribute → create .nachweis/ → unlock.
// If the returned Deposit is non-nil, the Ablage was committed even if err
// reports a later problem.
func (a *Archive) Archive(folder string, opts Options) (*Deposit, error) {
	if a.readOnly {
		return nil, ErrReadOnly
	}
	name := opts.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(folder))
	}
	// Check the name and the longest file name derived from it.
	why := bag.ValidateComponent(name)
	if why == "" {
		why = bag.ValidateComponent("0000_" + name + ".nachweis")
	}
	if why != "" {
		return nil, fmt.Errorf("Name der Ablage ungültig: %q (%s)", name, why)
	}
	year := opts.Belegjahr
	if year == 0 {
		year = BelegjahrFromName(name)
	}
	if year == 0 {
		return nil, ErrNeedBelegjahr
	}
	if year < 1900 || year > 2999 {
		return nil, fmt.Errorf("Belegjahr %d ist ungültig", year)
	}
	years := opts.RetentionYears
	if years == 0 {
		years = a.Config.RetentionDefault
	}
	if !validRetention(years) {
		return nil, errors.New("Aufbewahrungsfrist muss 7, 10, 12 oder 22 Jahre betragen")
	}
	if err := a.notInside(folder); err != nil {
		return nil, err
	}
	in, err := bag.Scan(folder)
	if err != nil {
		return nil, err
	}

	release, err := a.lock() // step 1
	if err != nil {
		return nil, err
	}
	defer release() // step 6
	seq, prev, err := a.next()
	if err != nil {
		return nil, err
	}
	nonce, err := a.nonce()
	if err != nil {
		return nil, err
	}
	meta := bag.Meta{
		Company: a.Config.Company, BaggingDate: a.now().Format("2006-01-02"),
		ArchiveID: a.Config.ArchiveID, Sequence: seq, Previous: prev,
		Belegjahr: year, RetentionYears: years, ArchivedBy: a.user + "@" + a.host,
		ToolVersion: Version, SealNonce: nonce,
	}
	base := bag.DirName(seq, name)
	final := filepath.Join(a.Dir, base+".zip")
	tmp := final + ".tmp"
	if _, err := os.Lstat(final); err == nil {
		return nil, fmt.Errorf("%s existiert bereits", filepath.Base(final))
	}

	written, err := writeTmp(tmp, name, in, meta) // step 2
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	res, err := bag.Check(tmp) // step 3
	if err == nil && (res.Status != bag.OK || res.Bag.Root != written.Root) {
		err = fmt.Errorf("Kontrolle der neuen Ablage fehlgeschlagen: %s", firstProblem(res))
	}
	if err != nil {
		os.Remove(tmp)
		return nil, err
	}
	if err := renameNoReplace(tmp, final); err != nil { // step 4: the commit
		os.Remove(tmp)
		return nil, fmt.Errorf("Ablage konnte nicht gespeichert werden: %w", err)
	}
	dep := &Deposit{Sequence: seq, Name: name, Path: final, EvidenceDir: filepath.Join(a.Dir, base+".nachweis"), Bag: res.Bag}
	if err := os.Chmod(final, 0o444); err != nil {
		return dep, fmt.Errorf("Ablage gespeichert, Schreibschutz nicht gesetzt: %w", err)
	}
	if err := os.Mkdir(dep.EvidenceDir, 0o755); err != nil && !errors.Is(err, os.ErrExist) { // step 5
		return dep, fmt.Errorf("Ablage gespeichert, Nachweis-Ordner nicht angelegt: %w", err)
	}
	return dep, nil
}

func writeTmp(tmp, name string, in *bag.Input, meta bag.Meta) (*bag.Written, error) {
	f, err := os.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, err
	}
	w, err := bag.Write(f, name, in, meta)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return w, err
}

func firstProblem(r *bag.Result) string {
	if len(r.Problems) == 0 {
		return "Prüfwert weicht ab"
	}
	return r.Problems[0].Msg
}

// notInside refuses to archive the archive folder itself, a folder inside
// it, or a folder that contains the archive.
func (a *Archive) notInside(folder string) error {
	f, err1 := filepath.Abs(folder)
	d, err2 := filepath.Abs(a.Dir)
	if err1 != nil || err2 != nil {
		return errors.Join(err1, err2)
	}
	within := func(child, parent string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	if within(f, d) || within(d, f) {
		return errors.New("der Ordner darf nicht im Archiv liegen und das Archiv nicht enthalten")
	}
	return nil
}

// Preview is what the UI shows before "Ordner archivieren". It is purely
// informational and never changes the format (spec §3: no deduplication).
type Preview struct {
	Input     *bag.Input
	Name      string   // default Ablage name
	Belegjahr int      // from the folder name, 0 if none
	Known     []string // input paths whose content is already in an earlier Ablage
	Years     []int    // distinct modification years; more than one deserves a hint
}

// Preview scans folder and compares it with the existing Ablagen. It reads
// files but writes nothing, so it also works in read-only mode.
func (a *Archive) Preview(folder string) (*Preview, error) {
	in, err := bag.Scan(folder)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(filepath.Clean(folder))
	p := &Preview{Input: in, Name: name, Belegjahr: BelegjahrFromName(name)}
	ds, err := a.Deposits()
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, d := range ds {
		if d.Bag != nil {
			for _, m := range d.Bag.Manifest {
				have[m.SHA256] = true
			}
		}
	}
	seenYear := map[int]bool{}
	for _, f := range in.Files {
		if y := f.ModTime.Year(); !seenYear[y] {
			seenYear[y] = true
			p.Years = append(p.Years, y)
		}
		if len(have) == 0 {
			continue
		}
		sum, err := fileHash(f.OSPath)
		if err != nil {
			return nil, err
		}
		if have[sum] {
			p.Known = append(p.Known, f.Path)
		}
	}
	sort.Ints(p.Years)
	return p, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
