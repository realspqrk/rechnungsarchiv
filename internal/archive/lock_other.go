//go:build !windows

package archive

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// openLock opens (or creates) .lock and takes an exclusive flock(2) on it,
// which the kernel drops when the process dies. A releasing writer unlinks
// the name while still holding the flock, so after locking, check that our
// inode is still the one at path; otherwise retry with the new file.
// Symlinks are never followed.
func openLock(path string) (*os.File, error) {
	for i := 0; i < 100; i++ {
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o644)
		if err != nil {
			return nil, err
		}
		if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			f.Close()
			if errors.Is(err, syscall.EWOULDBLOCK) {
				return nil, errBusy
			}
			return nil, &os.PathError{Op: "flock", Path: path, Err: err}
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, err
		}
		if !fi.Mode().IsRegular() {
			f.Close()
			return nil, errLockNotFile(path)
		}
		if cur, err := os.Lstat(path); err == nil && os.SameFile(fi, cur) {
			return f, nil
		}
		f.Close() // unlinked by its previous holder meanwhile
	}
	return nil, errBusy
}

func lockRetryable(error) bool { return false }

func readLockFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxLockBytes))
}

// removeLock unlinks .lock while the flock is still held, then closes.
func removeLock(f *os.File, path string) error {
	err := os.Remove(path)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func closeLock(f *os.File) { f.Close() }
