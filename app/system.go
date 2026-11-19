package app

import (
	"crypto/x509"
	"os"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
)

// system is what the real service uses from the outside world, like the
// CLI's (cmd/rechnungsarchiv/deps.go). Release builds use the real stores;
// a build with the testtsa tag can point them at the test PKI
// (testhook_testtsa.go), and tests inject their own.
type system struct {
	creds creds.Store
	// openWrite opens an archive for writing; nil: archive.Open. Tests
	// simulate a folder this PC cannot write to.
	openWrite func(dir string) (*archive.Archive, error)
	// trust opens an archive's trusted lists in normal mode (refreshed
	// lists are stored in the archive).
	trust func(archiveDir string) (*trust.Store, error)
	// trustRead opens them read-only (spec §9): refreshed lists go to a
	// cache outside the archive.
	trustRead func(dir string) (*trust.Store, error)
	// trustExport opens the lists inside a new export; nil: export default.
	trustExport func(exportDir string) (*trust.Store, error)
	rootCAs     *x509.CertPool // TLS roots for the provider; nil: system roots
	// verifier is the program bundled into every export as
	// rechnungsarchiv.exe. Nil: the export default, os.Executable, which is
	// right because the GUI runs inside the one binary that is also the CLI
	// (ADR-0008). Tests inject a stand-in.
	verifier func() (string, error)
	// home is where new archives and exports are suggested.
	home func() (string, error)
	// debug enables the frontend's screenshot hooks (test environment only).
	debug bool
}

// sys is the release system; testhook_testtsa.go may replace parts of it.
var sys = system{
	creds:     creds.System(),
	trust:     trust.Open,
	trustRead: trust.OpenReadOnly,
	home:      os.UserHomeDir,
}
