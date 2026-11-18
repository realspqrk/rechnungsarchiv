package main

import (
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
)

// system is what the real service uses from the outside world, like the
// CLI's (cmd/rechnungsarchiv/deps.go). Release builds use the real stores;
// a build with the testtsa tag can point them at the test PKI
// (testhook_testtsa.go), and tests inject their own.
type system struct {
	creds creds.Store
	// trust opens an archive's trusted lists in normal mode (refreshed
	// lists are stored in the archive).
	trust func(archiveDir string) (*trust.Store, error)
	// trustRead opens them read-only (spec §9): refreshed lists go to a
	// cache outside the archive.
	trustRead func(dir string) (*trust.Store, error)
	// trustExport opens the lists inside a new export; nil: export default.
	trustExport func(exportDir string) (*trust.Store, error)
	rootCAs     *x509.CertPool // TLS roots for the provider; nil: system roots
	// verifier is the command-line program bundled into every export.
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
	verifier:  bundledVerifier,
	home:      os.UserHomeDir,
}

// bundledVerifier is rechnungsarchiv.exe next to the GUI executable: the
// export ships the CLI (the standalone verifier), never the GUI itself.
func bundledVerifier() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(self), export.ExeName)
	if fi, err := os.Stat(p); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s fehlt neben dem Programm (%s); bitte das Programm neu installieren", export.ExeName, filepath.Dir(self))
	}
	if same, _ := sameFile(p, self); same {
		return "", errors.New(export.ExeName + " ist das Programm selbst, nicht das Prüfprogramm")
	}
	return p, nil
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(fa, fb), nil
}
