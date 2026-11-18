package trust

import (
	"crypto/x509"
	"embed"
	"encoding/pem"
	"fmt"
	"io/fs"
)

// The LOTL signing certificates are pinned in the release (ADR-0004). They are
// the certificates of the LOTL's self-pointer (OtherTSLPointer to
// eu-lotl.xml), which the Commission also publishes in the Official Journal.
// A LOTL signed by any other certificate is rejected (fail closed). See the
// release playbook for refreshing them.
//
//go:embed pins/*.pem
var pinFS embed.FS

var lotlPins = mustLoadPins(pinFS)

func mustLoadPins(fsys fs.FS) []*x509.Certificate {
	certs, err := loadPins(fsys)
	if err != nil {
		panic(err)
	}
	return certs
}

func loadPins(fsys fs.FS) ([]*x509.Certificate, error) {
	names, err := fs.Glob(fsys, "pins/*.pem")
	if err != nil {
		return nil, err
	}
	var certs []*x509.Certificate
	for _, name := range names {
		b, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		block, _ := pem.Decode(b)
		if block == nil || block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("%s: no certificate", name)
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("no pinned LOTL certificates")
	}
	return certs, nil
}
