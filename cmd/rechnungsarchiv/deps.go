package main

import (
	"crypto/x509"

	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
)

// system is what the commands use from the outside world. Release builds
// use the real stores; a build with the testtsa tag can point them at the
// test PKI (testhook_testtsa.go).
type system struct {
	creds func() creds.Store
	// trust opens an archive's trusted lists in normal mode (refreshed
	// lists are stored in the archive).
	trust func(archiveDir string) (*trust.Store, error)
	// trustRead opens them read-only (spec §9): refreshed lists go to a
	// cache outside the archive.
	trustRead func(dir string) (*trust.Store, error)
	// trustExport opens the lists inside a new export; nil: export default.
	trustExport func(exportDir string) (*trust.Store, error)
	rootCAs     *x509.CertPool // TLS roots for the provider; nil: system roots
}

var sys = system{creds: creds.System, trust: trust.Open, trustRead: trust.OpenReadOnly}
