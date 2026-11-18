//go:build windows

package archive

import (
	"errors"
	"io"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openLock opens (or creates) .lock for writing with a share mode that lets
// others only read it: no second writer and no deleter can open it while
// this handle lives, and Windows drops the handle when the process dies.
// Reparse points are opened as themselves and refused, never followed.
func openLock(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE|windows.DELETE,
		windows.FILE_SHARE_READ, nil, windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	switch {
	case errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		return nil, errBusy
	case err != nil:
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		windows.CloseHandle(h)
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	if fi.FileAttributes&(windows.FILE_ATTRIBUTE_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY) != 0 {
		windows.CloseHandle(h)
		return nil, errLockNotFile(path)
	}
	return os.NewFile(uintptr(h), path), nil
}

// lockRetryable reports an open error that may be the short window in
// which a releasing writer's .lock is pending deletion.
func lockRetryable(err error) bool { return errors.Is(err, windows.ERROR_ACCESS_DENIED) }

// readLockFile reads .lock while another process may hold it. The share
// mode must admit the holder's write and delete access.
func readLockFile(path string) ([]byte, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxLockBytes))
}

// removeLock deletes .lock through the held handle, then closes it. The
// name is gone before the OS lock is released, so the delete can never hit
// a successor's lock. POSIX semantics remove the name at once; the classic
// fallback leaves it pending until the close right after.
func removeLock(f *os.File, path string) error {
	h := windows.Handle(f.Fd())
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	err := windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags)))
	if err != nil {
		del := byte(1) // FILE_DISPOSITION_INFO.DeleteFile (BOOLEAN)
		err = windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, &del, 1)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

func closeLock(f *os.File) { f.Close() }
