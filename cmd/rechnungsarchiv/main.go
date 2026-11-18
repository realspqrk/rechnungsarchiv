// Command rechnungsarchiv is the command line of Rechnungsarchiv for
// scripts and AI agents (ADR-0008, docs/cli.md). It is also the standalone
// verifier shipped in every export. It only parses arguments and renders
// the results of internal/; it has no logic of its own.
package main

import (
	"context"
	"os"
	"os/signal"

	"golang.org/x/term"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], ioEnv{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		stdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		stdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		readPassword: func() ([]byte, error) {
			return term.ReadPassword(int(os.Stdin.Fd()))
		},
	})
	stop()
	os.Exit(code)
}
