//go:build !windows

package tsa

import (
	"os"
	"path/filepath"
)

// renameNoReplace links from to to (link fails with EEXIST if to exists),
// removes from and syncs the directory.
func renameNoReplace(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	os.Remove(from)
	d, err := os.Open(filepath.Dir(to))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
