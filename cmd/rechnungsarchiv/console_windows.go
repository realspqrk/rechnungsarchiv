package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// x/sys/windows has neither GetConsoleProcessList nor FreeConsole.
var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

// osConsoleAlone reports whether this process is the only one attached to
// its console: Windows created the console for it (Explorer, Start menu,
// a shortcut, the launcher's CREATE_NO_WINDOW). In a terminal the shell is
// attached too; without any console the count is 0.
func osConsoleAlone() bool {
	var pids [2]uint32 // a count above the buffer size is returned as is
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return n == 1
}

// osFreeConsole detaches from the console; Windows closes its window once
// no process is attached.
func osFreeConsole() { procFreeConsole.Call() }

// osAlert shows msg in a message box (errors of a launch without console).
func osAlert(msg string) {
	text, err1 := windows.UTF16PtrFromString(msg)
	title, err2 := windows.UTF16PtrFromString("Rechnungsarchiv")
	if err1 != nil || err2 != nil {
		return
	}
	windows.MessageBox(0, text, title, windows.MB_OK|windows.MB_ICONERROR)
}
