package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

// The writer lock (spec §1) has two layers:
//
//   - The OS lock: the writer keeps .lock open with an exclusive OS-level
//     lock for as long as it holds the archive (Windows: a share mode that
//     admits no other writer or deleter; elsewhere: flock(2)). The OS
//     releases it when the process dies, and every acquisition, takeover
//     and release happens while holding it, so they are atomic.
//   - The JSON payload {host,user,pid,since}: what the UI shows, and what
//     decides whether a lock found without a live OS holder (a crash) may
//     be taken over silently or needs the user's confirmation.

const lockFile = ".lock"

// maxLockBytes bounds what is read from .lock.
const maxLockBytes = 64 << 10

// busyWait is how long acquisition retries while the OS lock is held but
// the payload still names a dead owner (or none): the holder is in the
// middle of a takeover and is about to write its own payload.
const busyWait = 2 * time.Second

// errBusy means another handle holds the OS lock.
var errBusy = errors.New("lock busy")

// hookTakeover, if set by a test, runs after the OS lock is held and the
// found payload was judged takeover-able, just before ours is written.
var hookTakeover func(*Archive)

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
	f, raw, err := a.grab()
	if err != nil {
		return nil, err
	}
	if len(raw) != 0 {
		if held := parseLock(raw); !a.staleOnThisHost(held) {
			closeLock(f)
			return nil, held
		}
		if hookTakeover != nil {
			hookTakeover(a) // stale lock from a crashed process on this PC
		}
	}
	if err := writeLock(f, mine); err != nil {
		removeLock(f, a.lockPath())
		return nil, err
	}
	path := a.lockPath()
	return func() { removeLock(f, path) }, nil
}

// staleOnThisHost reports a payload left by a dead process on this PC. A
// payload naming this very process is never stale: it can only come from
// another Archive value of ours or a forged file.
func (a *Archive) staleOnThisHost(held *LockedError) bool {
	return held.Host == a.host && held.PID != a.pid && !pidAlive(held.PID)
}

// grab takes the OS lock on .lock (creating the file if needed) and returns
// the payload found in it. If a live process holds the OS lock, it returns
// that holder's payload as *LockedError.
func (a *Archive) grab() (*os.File, []byte, error) {
	deadline := time.Now().Add(busyWait)
	for {
		f, err := openLock(a.lockPath())
		if err == nil {
			raw, err := io.ReadAll(io.LimitReader(f, maxLockBytes))
			if err != nil {
				closeLock(f)
				return nil, nil, err
			}
			return f, raw, nil
		}
		if lockRetryable(err) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if !errors.Is(err, errBusy) {
			return nil, nil, err
		}
		raw, rerr := readLockFile(a.lockPath())
		held := parseLock(raw)
		midTakeover := rerr != nil || len(raw) == 0 || a.staleOnThisHost(held)
		if midTakeover && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		return nil, nil, held
	}
}

func errLockNotFile(path string) error {
	return fmt.Errorf("%s ist keine normale Datei (Verknüpfung oder Ordner)", path)
}

func parseLock(raw []byte) *LockedError {
	le := &LockedError{raw: raw}
	if json.Unmarshal(raw, &le.LockInfo) != nil || le.Host == "" {
		le.LockInfo = LockInfo{Host: "unbekannt", User: "unbekannt"}
	}
	return le
}

// readLock returns the current lock as *LockedError. An unreadable lock is
// reported with host "unbekannt" so the UI can still offer a takeover.
func (a *Archive) readLock() (*LockedError, error) {
	raw, err := readLockFile(a.lockPath())
	if err != nil {
		return nil, err
	}
	return parseLock(raw), nil
}

func writeLock(f *os.File, content []byte) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt(content, 0); err != nil {
		return err
	}
	return f.Sync()
}

// TakeOver removes the lock the user saw in seen, after the user confirmed
// (spec §1: a lock from another host needs confirmation). It refuses if the
// lock changed in the meantime, and it can never remove the lock of a
// writer that is still running: that writer holds the OS lock, and
// TakeOver then returns its *LockedError.
func (a *Archive) TakeOver(seen *LockedError) error {
	if a.readOnly {
		return ErrReadOnly
	}
	f, raw, err := a.grab()
	if err != nil {
		return err
	}
	if len(raw) != 0 && !bytes.Equal(raw, seen.raw) {
		closeLock(f)
		return errors.New("Sperre wurde inzwischen von einem anderen Programm übernommen")
	}
	return removeLock(f, a.lockPath())
}
