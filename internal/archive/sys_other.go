//go:build !windows

package archive

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// renameNoReplace links the target (which fails if it exists), removes the
// source and syncs the directory. Once the link exists, the target is
// published: published is then true even if a later step fails, and err
// reports that cleanup or durability problem.
func renameNoReplace(from, to string) (published bool, err error) {
	if err := os.Link(from, to); err != nil {
		return false, err
	}
	var errs []error
	if err := os.Remove(from); err != nil {
		errs = append(errs, err)
	}
	if err := syncDir(filepath.Dir(to)); err != nil {
		errs = append(errs, fmt.Errorf("Verzeichnis nicht synchronisiert: %w", err))
	}
	return true, errors.Join(errs...)
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// isLink reports symlinks (as returned by Lstat).
func isLink(fi fs.FileInfo) bool { return fi.Mode()&fs.ModeSymlink != 0 }

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
