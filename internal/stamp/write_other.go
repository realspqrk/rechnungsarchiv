//go:build !windows

package stamp

import "os"

// renameNoReplace links the new name (which fails if it exists), then
// removes the temp name.
func renameNoReplace(from, to string) error {
	if err := os.Link(from, to); err != nil {
		return err
	}
	return os.Remove(from)
}
