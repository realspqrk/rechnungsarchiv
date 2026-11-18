//go:build online

package tsa

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"net/http"
	"testing"
	"time"
)

// TestOnlineInterop stamps against free public TSAs (not qualified, test
// only) and runs the real verifier on their RSA tokens. Trust anchors are
// the operators' published roots, fetched here.
func TestOnlineInterop(t *testing.T) {
	for _, tc := range []struct {
		name, url string
		roots     []string
	}{
		{"FreeTSA", "https://freetsa.org/tsr", []string{"https://freetsa.org/files/cacert.pem"}},
		{"DigiCert", "http://timestamp.digicert.com", []string{"https://cacerts.digicert.com/DigiCertTrustedRootG4.crt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			f := &Fetcher{HTTP: &http.Client{Timeout: 30 * time.Second}}
			var anchors []*x509.Certificate
			for _, u := range tc.roots {
				b, err := f.do(ctx, http.MethodGet, u, "", nil)
				if err != nil {
					t.Fatal(err)
				}
				anchors = append(anchors, parseCertsLoose(b)...)
			}
			if len(anchors) == 0 {
				t.Fatal("no root certificate")
			}
			req := must(NewRequest(sha256.Sum256([]byte("rechnungsarchiv online interop "+time.Now().String())), nil))
			c := &Client{URL: tc.url, Timeout: 30 * time.Second, allowHTTP: true}
			tsr, err := c.Send(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			tok, err := f.Accept(ctx, tsr, VerifyOptions{Request: req, Anchors: anchors})
			if err != nil {
				t.Fatal(err)
			}
			acc := "absent"
			if tok.Accuracy != nil {
				acc = tok.Accuracy.String()
			}
			t.Logf("genTime %s, accuracy %s, policy %v, serial %v, signer %q (%T), chain %d",
				tok.GenTime, acc, tok.Policy, tok.Serial, tok.Signer.Subject.CommonName, tok.Signer.PublicKey, len(tok.Chain))
			if d := time.Since(tok.GenTime); d < -5*time.Minute || d > 5*time.Minute {
				t.Errorf("genTime off by %v", d)
			}

			// Offline re-verification from what would be stored.
			again, err := VerifyResponse(tsr, VerifyOptions{Request: must(ParseRequest(req.DER)), Anchors: anchors, Intermediates: tok.Certs})
			if err != nil || !again.GenTime.Equal(tok.GenTime) {
				t.Fatalf("offline re-verification: %v", err)
			}

			// Validation material: reported, not required (these are not
			// qualified services and may publish no OCSP/CRL).
			_, checks, err := f.Collect(ctx, tok)
			for _, cc := range checks {
				if cc.Revocation != nil {
					t.Logf("revocation %s: %s status %d covers %v", cc.Cert.Subject.CommonName, cc.Revocation.Kind, cc.Revocation.Status, cc.Revocation.Covers)
				} else {
					t.Logf("revocation %s: %v", cc.Cert.Subject.CommonName, cc.Err)
				}
			}
			t.Logf("Collect: %v", err)
		})
	}
}
