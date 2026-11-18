package stamp

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// writeOnce creates path with data the way evidence is written (spec §4
// step 5): a temp file whose name ends in ".tmp" (archive recovery deletes
// those), fsync, then a rename that never replaces an existing file. An
// existing file with the same content counts as written.
//
// internal/tsa has the same writer for its own evidence files but does not
// export it; this one is used for n.provider only.
func writeOnce(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameNoReplace(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		if errors.Is(err, fs.ErrExist) {
			if old, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(old, data) {
				return nil
			}
			return fmt.Errorf("Nachweisdatei existiert bereits: %s", path)
		}
		return err
	}
	return nil
}
