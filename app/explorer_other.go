//go:build !windows

package app

import "os/exec"

// selectInExplorer exists so the package builds elsewhere; the GUI runs on
// Windows only.
func selectInExplorer(path string) *exec.Cmd { return exec.Command("explorer.exe", "/select,"+path) }
