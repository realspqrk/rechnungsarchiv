//go:build !windows

package main

// Outside Windows a run without arguments is always the CLI; the window
// opens with `rechnungsarchiv gui`, and errors go to stderr.
func osConsoleAlone() bool { return false }
func osFreeConsole()       {}
func osAlert(string)       {}
