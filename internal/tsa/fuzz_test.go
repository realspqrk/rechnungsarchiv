package tsa

import (
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

// Malformed input must never panic or verify. Seeds: a valid response,
// variants, and garbage. Run longer with: go test -fuzz FuzzVerifyResponse
func FuzzVerifyResponse(f *testing.F) {
	e := newEnv(f, tsatest.PKIOptions{})
	valid := tsatest.Response(0, "", e.token(f, nil))
	f.Add(valid)
	f.Add(tsatest.Response(0, "", e.token(f, func(p *tsatest.TokenParams) { p.CertIDv1 = true; p.Accuracy = &tsatest.Accuracy{Millis: 1} })))
	f.Add(tsatest.Response(2, "rejected", nil))
	f.Add(valid[:len(valid)/2])
	f.Add([]byte{0x30, 0x80, 0x00, 0x00})
	f.Add([]byte{})
	opts := e.opts()
	f.Fuzz(func(t *testing.T, b []byte) {
		tok, err := VerifyResponse(b, opts)
		if err == nil && string(b) != string(valid) {
			// Only the honest token may verify: any other accepted input
			// must at least carry the same signed TSTInfo.
			if !tok.Signer.Equal(e.pki.TSU.Cert) || tok.Nonce.Cmp(e.req.Nonce) != 0 {
				t.Fatalf("forged input verified")
			}
		}
		ParseRequest(b)
	})
}

func FuzzCheckRevocation(f *testing.F) {
	e := newEnv(f, tsatest.PKIOptions{})
	p := e.pki
	now := time.Now()
	f.Add(true, p.OCSP(p.TSU.Cert.SerialNumber, p.Inter, p.Responder, now.Add(-time.Hour), now.Add(time.Hour)))
	f.Add(false, p.CRL(p.Inter, now.Add(-time.Hour), now.Add(time.Hour), nil))
	f.Add(true, []byte{0x30, 0x03, 0x0a, 0x01, 0x00})
	f.Fuzz(func(t *testing.T, ocsp bool, b []byte) {
		kind := KindCRL
		if ocsp {
			kind = KindOCSP
		}
		CheckRevocation(kind, b, p.TSU.Cert, p.Inter.Cert, now)
		ParsePEM(b)
		parseCertsLoose(b)
	})
}
