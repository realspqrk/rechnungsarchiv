package archive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// Lock acquires the archive's writer lock (spec §1, §4 steps 1 and 6) and
// returns its release function. It is the same OS-level lock Archive takes;
// stamp.Options.Lock uses it. A lock held by another writer, or left by a
// crashed process on another PC, yields *LockedError.
func (a *Archive) Lock() (release func(), err error) { return a.lock() }

// LockHolder returns the content of .lock, or nil if there is none. It
// reads only, so it also works on a read-only archive. A lock left behind
// by a crash is returned too; Lock decides whether it may be taken over.
func (a *Archive) LockHolder() (*LockInfo, error) {
	raw, err := readLockFile(a.lockPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info := parseLock(raw).LockInfo
	return &info, nil
}

// NextSequence returns the number the next Ablage will get. It fails like
// Archive would (number assigned twice, last Ablage unreadable).
func (a *Archive) NextSequence() (int, error) {
	seq, _, err := a.next()
	return seq, err
}

// NameProblem says why name cannot be an Ablage name, or "" if it can. It
// checks the name and the longest file name derived from it.
func NameProblem(name string) string {
	why := bag.ValidateComponent(name)
	if why == "" {
		why = bag.ValidateComponent("0000_" + name + ".nachweis")
	}
	return why
}

// Resolve applies the defaults and the checks of Archive to opts without
// reading the folder or writing anything: the Ablage name (default: the
// folder name), the Belegjahr (default: the year in the name) and the
// retention (default: Config.RetentionDefault). It works in read-only mode,
// for a dry run.
func (a *Archive) Resolve(folder string, opts Options) (Options, error) {
	name := opts.Name
	if name == "" {
		name = filepath.Base(filepath.Clean(folder))
	}
	if why := NameProblem(name); why != "" {
		return Options{}, fmt.Errorf("Name der Ablage ungültig: %q (%s)", name, why)
	}
	year := opts.Belegjahr
	if year == 0 {
		year = BelegjahrFromName(name)
	}
	if year == 0 {
		return Options{}, ErrNeedBelegjahr
	}
	if year < 1900 || year > 2999 {
		return Options{}, fmt.Errorf("Belegjahr %d ist ungültig", year)
	}
	years := opts.RetentionYears
	if years == 0 {
		years = a.Config.RetentionDefault
	}
	if !validRetention(years) {
		return Options{}, ErrRetention
	}
	if err := a.notInside(folder); err != nil {
		return Options{}, err
	}
	return Options{Name: name, Belegjahr: year, RetentionYears: years}, nil
}

// ErrRetention means a retention period other than 7, 10, 12 or 22 years.
var ErrRetention = errors.New("Aufbewahrungsfrist muss 7, 10, 12 oder 22 Jahre betragen")

// SetProvider sets the timestamp provider in rechnungsarchiv.json (nil: none).
// The id must be one of tsa.Providers().
func (a *Archive) SetProvider(id *string) error {
	if id != nil {
		if _, ok := tsa.ProviderByID(*id); !ok {
			return fmt.Errorf("Zeitstempel-Anbieter %q ist nicht bekannt", *id)
		}
		v := *id
		id = &v
	}
	return a.updateConfig(func(c *Config) error { c.TSA = id; return nil })
}

// SetCompany changes the company name. It applies to new Ablagen only:
// existing ones keep the Source-Organization they were sealed with.
func (a *Archive) SetCompany(company string) error {
	company, err := checkCompany(company)
	if err != nil {
		return err
	}
	return a.updateConfig(func(c *Config) error { c.Company = company; return nil })
}

// SetRetentionDefault changes the default retention for new Ablagen (7,
// 10, 12 or 22 years).
func (a *Archive) SetRetentionDefault(years int) error {
	if !validRetention(years) {
		return ErrRetention
	}
	return a.updateConfig(func(c *Config) error { c.RetentionDefault = years; return nil })
}

func checkCompany(company string) (string, error) {
	company = strings.TrimSpace(company)
	if company == "" || strings.ContainsAny(company, "\r\n\t") {
		return "", errors.New("Firmenname fehlt oder enthält Zeilenumbrüche")
	}
	return company, nil
}

// updateConfig rewrites rechnungsarchiv.json under the writer lock. It
// re-reads the file first, so a change made by another writer in between is
// kept, and writes canonical JSON via a temp file, fsync and rename.
func (a *Archive) updateConfig(change func(*Config) error) error {
	if a.readOnly {
		return ErrReadOnly
	}
	release, err := a.lock()
	if err != nil {
		return err
	}
	defer release()
	cur, err := readConfig(a.Dir)
	if err != nil {
		return err
	}
	if cur.ArchiveID != a.Config.ArchiveID {
		return errors.New("rechnungsarchiv.json gehört inzwischen zu einem anderen Archiv")
	}
	if err := change(&cur); err != nil {
		return err
	}
	data, err := cur.render()
	if err != nil {
		return err
	}
	if err := replaceFile(filepath.Join(a.Dir, configFile), data); err != nil {
		return err
	}
	a.Config = cur
	return nil
}

// replaceFile writes data to path via path.tmp, fsync and a replacing
// rename. Only rechnungsarchiv.json is ever replaced; evidence never is.
func replaceFile(path string, data []byte) error {
	tmp := path + ".tmp"
	os.Remove(tmp) // left by a crash; we hold the lock
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
		// A reader that has the file open for a moment (Windows opens without
		// FILE_SHARE_DELETE) makes the rename fail briefly.
		for i := 0; ; i++ {
			if err = os.Rename(tmp, path); err == nil || i == 25 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
