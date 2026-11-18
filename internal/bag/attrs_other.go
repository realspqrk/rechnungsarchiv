//go:build !windows

package bag

import "io/fs"

// checkAttrs has nothing to check outside Windows; symlinks are caught by
// the file mode.
func checkAttrs(fs.FileInfo) error { return nil }
