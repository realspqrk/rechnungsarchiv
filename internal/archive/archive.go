// Package archive manages the archive folder (spec §1, §4): configuration,
// the writer lock, numbering, the commit order and crash recovery.
package archive

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

// Version is written to bag-info.txt as Rechnungsarchiv-Version.
var Version = "0.1.0-dev"

const configFile = "rechnungsarchiv.json"

var (
	ErrNoArchive = errors.New("kein Archiv: rechnungsarchiv.json fehlt")
	ErrExists    = errors.New("hier besteht bereits ein Archiv")
	ErrReadOnly  = errors.New("Archiv ist nur zum Kontrollieren geöffnet")
)

// Config is rechnungsarchiv.json (spec §1).
type Config struct {
	ArchiveID        string  `json:"archive_id"`
	Company          string  `json:"company"`
	Format           int     `json:"format"`
	RetentionDefault int     `json:"retention_default"`
	TSA              *string `json:"tsa"` // provider id, nil if none is set up
}

func (c *Config) render() ([]byte, error) {
	return bag.CanonicalJSON(bag.Obj{"archive_id": c.ArchiveID, "company": c.Company, "format": c.Format,
		"retention_default": c.RetentionDefault, "tsa": c.TSA})
}

// Archive is an opened archive folder.
type Archive struct {
	Dir    string
	Config Config

	readOnly bool
	// Hooks for tests; set by open.
	now   func() time.Time
	user  string
	host  string
	pid   int
	nonce func() (string, error)
}

// Deposit is one Ablage found in the archive folder.
type Deposit struct {
	Sequence    int    // from the file name
	Name        string // folder name part of "<NNNN>_<Name>.zip"
	Path        string // the .zip
	EvidenceDir string // "<NNNN>_<Name>.nachweis"
	Bag         *bag.Bag
	Err         error // the ZIP could not be read; Bag is nil
}

// Init creates a new archive in dir (created if missing).
func Init(dir, company string) (*Archive, error) {
	company = strings.TrimSpace(company)
	if company == "" || strings.ContainsAny(company, "\r\n\t") {
		return nil, errors.New("Firmenname fehlt oder enthält Zeilenumbrüche")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, configFile)); err == nil {
		return nil, ErrExists
	}
	id, err := uuidV4()
	if err != nil {
		return nil, err
	}
	cfg := Config{ArchiveID: id, Company: company, Format: bag.FormatVersion, RetentionDefault: 7}
	data, err := cfg.render()
	if err != nil {
		return nil, err
	}
	os.Remove(filepath.Join(dir, configFile+".tmp")) // from a crashed Init
	if err := writeNew(filepath.Join(dir, configFile), data); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrExists
		}
		return nil, err
	}
	return Open(dir)
}

// Open opens an archive for writing and runs crash recovery (spec §4)
// unless another writer currently holds the lock.
func Open(dir string) (*Archive, error) {
	a, err := open(dir, false)
	if err != nil {
		return nil, err
	}
	if err := a.recover(); err != nil {
		return nil, err
	}
	return a, nil
}

// OpenReadOnly opens an archive for checking only (spec §9): it never
// writes, locks or deletes anything.
func OpenReadOnly(dir string) (*Archive, error) { return open(dir, true) }

// ReadOnly reports whether the archive was opened with OpenReadOnly.
func (a *Archive) ReadOnly() bool { return a.readOnly }

func open(dir string, readOnly bool) (*Archive, error) {
	data, err := os.ReadFile(filepath.Join(dir, configFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNoArchive
	}
	if err != nil {
		return nil, err
	}
	a := &Archive{Dir: dir, readOnly: readOnly, now: time.Now, pid: os.Getpid(), nonce: randomHex16}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(&a.Config); err != nil {
		return nil, fmt.Errorf("rechnungsarchiv.json ist beschädigt: %v", err)
	}
	if a.Config.Format != bag.FormatVersion {
		return nil, fmt.Errorf("rechnungsarchiv.json: Format %d wird nicht unterstützt", a.Config.Format)
	}
	if a.Config.ArchiveID == "" || !validRetention(a.Config.RetentionDefault) {
		return nil, errors.New("rechnungsarchiv.json ist unvollständig")
	}
	a.user, a.host = identity()
	return a, nil
}

// recover deletes leftover *.tmp files (spec §4) while holding the lock.
func (a *Archive) recover() error {
	release, err := a.lock()
	var le *LockedError
	if errors.As(err, &le) {
		return nil // an active writer owns the temp files
	}
	if err != nil {
		return err
	}
	defer release()
	for _, pattern := range []string{"*.tmp", "*.nachweis/*.tmp"} {
		matches, err := filepath.Glob(filepath.Join(a.Dir, pattern))
		if err != nil {
			return err
		}
		for _, m := range matches {
			if err := os.Remove(m); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

var reDeposit = regexp.MustCompile(`^(\d{4,})_(.+)\.zip$`)

// Deposits lists the Ablagen, sorted by sequence and name. Unreadable ZIPs
// are listed with Err set.
func (a *Archive) Deposits() ([]Deposit, error) {
	des, err := os.ReadDir(a.Dir)
	if err != nil {
		return nil, err
	}
	var out []Deposit
	for _, de := range des {
		m := reDeposit.FindStringSubmatch(de.Name())
		if m == nil || de.IsDir() {
			continue
		}
		seq, _ := strconv.Atoi(m[1])
		d := Deposit{Sequence: seq, Name: m[2], Path: filepath.Join(a.Dir, de.Name()),
			EvidenceDir: filepath.Join(a.Dir, strings.TrimSuffix(de.Name(), ".zip")+".nachweis")}
		d.Bag, d.Err = bag.Inspect(d.Path)
		if d.Err == nil && d.Bag.Chain.Sequence != seq {
			d.Err = fmt.Errorf("%s: Nummer im Dateinamen passt nicht zu chain.json", de.Name())
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sequence != out[j].Sequence {
			return out[i].Sequence < out[j].Sequence
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// next returns the sequence number and predecessor for a new Ablage.
func (a *Archive) next() (int, *string, error) {
	ds, err := a.Deposits()
	if err != nil {
		return 0, nil, err
	}
	if len(ds) == 0 {
		return 1, nil, nil
	}
	last := ds[len(ds)-1]
	if len(ds) > 1 && ds[len(ds)-2].Sequence == last.Sequence {
		return 0, nil, fmt.Errorf("Nummer doppelt vergeben: Ablage %04d", last.Sequence)
	}
	if last.Err != nil {
		return 0, nil, fmt.Errorf("letzte Ablage ist nicht lesbar: %w", last.Err)
	}
	if last.Bag.Chain.ArchiveID != a.Config.ArchiveID {
		return 0, nil, fmt.Errorf("Ablage %04d gehört zu einem anderen Archiv", last.Sequence)
	}
	root := last.Bag.Root
	return last.Sequence + 1, &root, nil
}

func validRetention(n int) bool { return n == 7 || n == 10 || n == 12 || n == 22 }

func identity() (string, string) {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unbekannt"
	}
	name := "unbekannt"
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
		if i := strings.LastIndexByte(name, '\\'); i >= 0 {
			name = name[i+1:] // DOMAIN\user on Windows
		}
	}
	return name, host
}

func randomHex16() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func uuidV4() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// writeNew writes data to path via path.tmp, fsync and a no-replace rename.
func writeNew(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameNoReplace(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
