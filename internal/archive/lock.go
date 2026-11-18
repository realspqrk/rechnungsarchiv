package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

const lockFile = ".lock"

// LockInfo is the content of .lock (spec §1).
type LockInfo struct {
	Host  string `json:"host"`
	User  string `json:"user"`
	PID   int    `json:"pid"`
	Since string `json:"since"` // RFC 3339
}

// LockedError means another writer holds the archive. The UI shows Host,
// User and Since and may offer TakeOver after the user confirms.
type LockedError struct {
	LockInfo
	raw []byte // exact lock content, so TakeOver removes only this lock
}

func (e *LockedError) Error() string {
	since := e.Since
	if t, err := time.Parse(time.RFC3339, e.Since); err == nil {
		since = t.Local().Format("02.01.2006 15:04")
	}
	return fmt.Sprintf("Archiv wird gerade auf %s von %s verwendet (seit %s)", e.Host, e.User, since)
}

func (a *Archive) lockPath() string { return filepath.Join(a.Dir, lockFile) }

// lock acquires .lock. A lock from a dead process on this host is taken
// over silently; any other existing lock yields *LockedError.
func (a *Archive) lock() (release func(), err error) {
	if a.readOnly {
		return nil, ErrReadOnly
	}
	info := LockInfo{Host: a.host, User: a.user, PID: a.pid, Since: a.now().UTC().Format(time.RFC3339)}
	mine, err := bag.CanonicalJSON(bag.Obj{"host": info.Host, "user": info.User, "pid": info.PID, "since": info.Since})
	if err != nil {
		return nil, err
	}
	for attempt := 0; ; attempt++ {
		f, err := os.OpenFile(a.lockPath(), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, err = f.Write(mine)
			if err == nil {
				err = f.Sync()
			}
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				os.Remove(a.lockPath())
				return nil, err
			}
			return func() { a.removeLockIf(mine) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		held, err := a.readLock()
		if err != nil {
			return nil, err
		}
		if attempt == 0 && held.Host == a.host && held.PID != a.pid && !pidAlive(held.PID) {
			if err := a.removeLockIf(held.raw); err != nil {
				return nil, err
			}
			continue // stale lock from a crashed process on this PC
		}
		return nil, held
	}
}

// readLock returns the current lock as *LockedError. An unreadable lock is
// reported with host "unbekannt" so the UI can still offer a takeover.
func (a *Archive) readLock() (*LockedError, error) {
	raw, err := os.ReadFile(a.lockPath())
	if err != nil {
		return nil, err
	}
	le := &LockedError{raw: raw}
	if json.Unmarshal(raw, &le.LockInfo) != nil || le.Host == "" {
		le.LockInfo = LockInfo{Host: "unbekannt", User: "unbekannt"}
	}
	return le, nil
}

// removeLockIf deletes .lock only if it still has exactly the given content.
func (a *Archive) removeLockIf(content []byte) error {
	raw, err := os.ReadFile(a.lockPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, content) {
		return errors.New("Sperre wurde inzwischen von einem anderen Programm übernommen")
	}
	return os.Remove(a.lockPath())
}

// TakeOver removes the lock the user saw in seen, after the user confirmed
// (spec §1: a lock from another host needs confirmation). It refuses if the
// lock changed in the meantime.
func (a *Archive) TakeOver(seen *LockedError) error {
	if a.readOnly {
		return ErrReadOnly
	}
	return a.removeLockIf(seen.raw)
}
