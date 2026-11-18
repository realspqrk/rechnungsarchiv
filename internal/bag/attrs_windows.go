//go:build windows

package bag

import (
	"io/fs"
	"syscall"
)

const (
	attrReparsePoint      = 0x400
	attrOffline           = 0x1000
	attrRecallOnOpen      = 0x40000
	attrRecallOnDataAcces = 0x400000
)

// checkAttrs rejects reparse points (symlinks, junctions, cloud files) and
// cloud placeholders (spec §1, §3).
func checkAttrs(fi fs.FileInfo) error {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return nil
	}
	a := d.FileAttributes
	if a&(attrOffline|attrRecallOnOpen|attrRecallOnDataAcces) != 0 {
		return ErrPlaceholder
	}
	if a&attrReparsePoint != 0 {
		return ErrLink
	}
	return nil
}
