package tsa

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// renameNoReplace moves from to to and fails if to exists. os.Rename on
// Windows replaces (MOVEFILE_REPLACE_EXISTING); MoveFileEx without that
// flag does not.
func renameNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(f, t, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return fmt.Errorf("%s: %w", to, fs.ErrExist)
	}
	return err
}

// renameReplace moves from to to, atomically replacing an existing to.
func renameReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

// longPath adds the \\?\ prefix to long absolute paths. Raw Windows calls,
// unlike os.*, get no automatic long-path handling.
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil || len(abs) < 240 || strings.HasPrefix(abs, `\`) {
		return p
	}
	return `\\?\` + abs
}
