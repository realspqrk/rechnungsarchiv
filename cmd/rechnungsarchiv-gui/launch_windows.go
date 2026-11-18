// Command rechnungsarchiv-gui is the Windows launcher of the window
// (ADR-0008). Built with -H windowsgui, it has no console, so starting it
// from a shortcut or the Start menu flashes no console window. It only
// starts `rechnungsarchiv.exe gui <args…>` from its own folder with
// CREATE_NO_WINDOW and exits. It has no logic of its own and does not
// contain the GUI.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// mainExe is the one program for CLI and GUI, next to the launcher.
const mainExe = "rechnungsarchiv.exe"

func main() {
	self, err := os.Executable()
	if err == nil {
		var exe string
		if exe, err = target(self); err == nil {
			err = start(exe, os.Args[1:])
		}
	}
	if err != nil {
		alert(err.Error())
		os.Exit(1)
	}
}

// target is rechnungsarchiv.exe in the folder of the launcher self.
func target(self string) (string, error) {
	dir := filepath.Dir(self)
	exe := filepath.Join(dir, mainExe)
	if fi, err := os.Stat(exe); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s fehlt im Programmordner (%s). Bitte Rechnungsarchiv neu installieren.", mainExe, dir)
	}
	return exe, nil
}

// commandLine is the Windows command line `"<exe>" gui <args…>`, each
// argument quoted for CommandLineToArgvW.
func commandLine(exe string, args []string) string {
	parts := []string{syscall.EscapeArg(exe), "gui"}
	for _, a := range args {
		parts = append(parts, syscall.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}

// start runs exe without a console window and does not wait for it. It
// inherits the working folder, so relative paths keep their meaning.
func start(exe string, args []string) error {
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:       commandLine(exe, args),
		CreationFlags: windows.CREATE_NO_WINDOW,
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s konnte nicht gestartet werden: %v", exe, err)
	}
	return cmd.Process.Release()
}

func alert(msg string) {
	text, err1 := windows.UTF16PtrFromString(msg)
	title, err2 := windows.UTF16PtrFromString("Rechnungsarchiv")
	if err1 == nil && err2 == nil {
		windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
	}
}
