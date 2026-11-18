//go:build windows

package export

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// volumeNameDOS asks GetFinalPathNameByHandle for a drive-letter path (x/sys
// names only the FILE_NAME_* flags).
const volumeNameDOS = 0x0

// finalPath is where p physically leads (GetFinalPathNameByHandle).
// Go's EvalSymlinks does not follow junctions reliably.
func finalPath(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(longPath(p))
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", &os.PathError{Op: "open", Path: p, Err: err}
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameDOS)
	if err != nil {
		return "", &os.PathError{Op: "GetFinalPathNameByHandle", Path: p, Err: err}
	}
	s := windows.UTF16ToString(buf[:n])
	switch {
	case strings.HasPrefix(s, `\\?\UNC\`):
		s = `\\` + s[len(`\\?\UNC\`):]
	case strings.HasPrefix(s, `\\?\`):
		s = s[len(`\\?\`):]
	}
	return s, nil
}

// noLinks refuses a reparse point (junction, symbolic link, mount point …)
// anywhere in the existing part of the absolute path p.
func noLinks(p string) error {
	vol := filepath.VolumeName(p)
	for cur := p; len(cur) > len(vol)+1; cur = filepath.Dir(cur) {
		fi, err := os.Lstat(cur)
		if err != nil {
			continue // not created yet
		}
		if isLink(fi) {
			return errLink(cur)
		}
	}
	return nil
}

func isLink(fi fs.FileInfo) bool {
	if fi.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func longPath(p string) string {
	if len(p) < 240 || strings.HasPrefix(p, `\`) {
		return p
	}
	return `\\?\` + p
}
