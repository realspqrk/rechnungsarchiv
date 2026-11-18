//go:build !windows

// Command rechnungsarchiv-gui is the Windows launcher of the window
// (ADR-0008). Elsewhere the main program is used directly.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "rechnungsarchiv-gui ist nur der Starter für Windows; bitte rechnungsarchiv gui verwenden.")
	os.Exit(2)
}
