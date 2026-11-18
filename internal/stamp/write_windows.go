//go:build windows

package stamp

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// renameNoReplace uses MoveFileEx without MOVEFILE_REPLACE_EXISTING:
// os.Rename replaces an existing target on Windows.
func renameNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(longPath(from))
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(longPath(to))
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(f, t, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}
	return nil
}

// longPath adds the \\?\ prefix raw Win32 calls need beyond MAX_PATH.
func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil || len(abs) < 240 || strings.HasPrefix(abs, `\`) {
		return p
	}
	return `\\?\` + abs
}
