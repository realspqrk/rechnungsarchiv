package tsa

import (
	"errors"
	"fmt"
	"io/fs"

	"golang.org/x/sys/windows"
)

// renameNoReplace moves from to to and fails if to exists. os.Rename on
// Windows replaces (MOVEFILE_REPLACE_EXISTING); MoveFileEx without that
// flag does not.
func renameNoReplace(from, to string) error {
	f, err := windows.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := windows.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(f, t, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return fmt.Errorf("%s: %w", to, fs.ErrExist)
	}
	return err
}
