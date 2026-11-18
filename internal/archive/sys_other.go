//go:build !windows

package archive

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

// renameNoReplace links the target (which fails if it exists), removes the
// source and syncs the directory.
func renameNoReplace(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	if err := os.Remove(from); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
