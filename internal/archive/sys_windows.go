//go:build windows

package archive

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// renameNoReplace renames without replacing an existing target. os.Rename
// replaces on Windows (MOVEFILE_REPLACE_EXISTING), so call MoveFileEx
// without that flag.
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

func longPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil || len(abs) < 240 || strings.HasPrefix(abs, `\`) {
		return p
	}
	return `\?\` + abs
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED // exists, but not ours to query
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	const stillActive = 259
	return code == stillActive
}
