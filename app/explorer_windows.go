package app

import (
	"os/exec"
	"syscall"
)

// selectInExplorer returns the command that opens Explorer with path
// selected. Explorer parses its own command line: Go would quote the whole
// argument ("/select,D:\a b\x.zip") when the path has a space, and Explorer
// then ignores it and opens the Documents folder. Only the path may be
// quoted, so the command line is set verbatim. Windows paths cannot
// contain '"'.
func selectInExplorer(path string) *exec.Cmd {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd
}
