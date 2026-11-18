package trust

import (
	"crypto/sha256"
	"crypto/x509"
)

// Anchors returns the ServiceDigitalIdentity certificates (current and, where
// a history entry carries any, historic) of every service that was QTST at
// some time, in the newest verified national lists of all territories: the
// trust anchors for path validation (spec §5 item 7). The set is not limited
// to one provider, because providers may route requests to another QTSP
// (Mensatek → FNMT). Whether the service was qualified at genTime is decided
// separately by Qualify.
func (s *Store) Anchors() []*x509.Certificate {
	return anchorCerts(s.Lists())
}

func anchorCerts(lists []*List) []*x509.Certificate {
	var out []*x509.Certificate
	seen := map[[32]byte]bool{}
	for _, l := range lists {
		for _, svc := range l.Services {
			certs := svc.Certs
			for _, iv := range svc.History {
				certs = append(certs[:len(certs):len(certs)], iv.Certs...)
			}
			for _, c := range certs {
				k := sha256.Sum256(c.Raw)
				if !seen[k] {
					seen[k] = true
					out = append(out, c)
				}
			}
		}
	}
	return out
}
