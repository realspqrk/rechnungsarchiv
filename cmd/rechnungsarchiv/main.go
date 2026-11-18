// Command rechnungsarchiv is the one program of Rechnungsarchiv (ADR-0008):
// the command line for scripts and AI agents (docs/cli.md) and, with
// `rechnungsarchiv gui` or when started from Explorer without arguments,
// the window (package app). It is also the standalone verifier shipped in
// every export. It only parses arguments and renders the results of
// internal/; it has no logic of its own.
package main

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/term"
)

func main() {
	args := os.Args[1:]
	stdoutTTY := term.IsTerminal(int(os.Stdout.Fd()))
	if opensWindow(args, startedAlone(stdoutTTY)) {
		// Explorer or Start menu: the window, as `gui` does.
		args = []string{"gui"}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, args, ioEnv{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		stdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		stdoutTTY: stdoutTTY,
		readPassword: func() ([]byte, error) {
			return term.ReadPassword(int(os.Stdin.Fd()))
		},
	})
	stop()
	os.Exit(code)
}
