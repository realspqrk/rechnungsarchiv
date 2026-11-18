//go:build !windows

package export

import (
	"io/fs"
	"os"
	"path/filepath"
)

func finalPath(p string) (string, error) { return filepath.EvalSymlinks(p) }

// noLinks refuses a target that is itself a symbolic link. Links further
// up (/var on macOS) are resolved by physical instead.
func noLinks(p string) error {
	if fi, err := os.Lstat(p); err == nil && isLink(fi) {
		return errLink(p)
	}
	return nil
}

func isLink(fi fs.FileInfo) bool { return fi.Mode()&fs.ModeSymlink != 0 }
