package tsa

import (
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

func TestVerifyValidToken(t *testing.T) {
	for _, tc := range []struct {
		name string
		pki  tsatest.PKIOptions
		edit func(p *tsatest.TokenParams)
	}{
		{"ECDSA P-256", tsatest.PKIOptions{}, nil},
		{"RSA PKCS1v15 SHA-384", tsatest.PKIOptions{RSA: true}, func(p *tsatest.TokenParams) { p.Hash = crypto.SHA384 }},
		{"RSA PSS", tsatest.PKIOptions{RSA: true}, func(p *tsatest.TokenParams) { p.PSS = true }},
		{"ESSCertID v1", tsatest.PKIOptions{}, func(p *tsatest.TokenParams) { p.CertIDv1 = true }},
		{"ESSCertIDv2 SHA-512 without issuerSerial", tsatest.PKIOptions{}, func(p *tsatest.TokenParams) {
			p.CertIDHash, p.NoIssuerSerial = crypto.SHA512, true
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, tc.pki)
			genTime := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
			tok, err := e.verify(t, func(p *tsatest.TokenParams) {
				p.GenTime, p.Serial = genTime, big.NewInt(4711)
				if tc.edit != nil {
					tc.edit(p)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			if !tok.GenTime.Equal(genTime) || tok.Serial.Int64() != 4711 || !tok.Policy.Equal(tsatest.DefaultPolicy) {
				t.Errorf("fields: %v %v %v", tok.GenTime, tok.Serial, tok.Policy)
			}
			if tok.Accuracy != nil {
				t.Errorf("accuracy %v, want absent", *tok.Accuracy)
			}
			if !tok.Signer.Equal(e.pki.TSU.Cert) || len(tok.Chain) != 3 || !tok.Chain[2].Equal(e.pki.Root.Cert) {
				t.Errorf("signer/chain wrong: %d certs", len(tok.Chain))
			}
		})
	}
}

func TestVerifyAccuracy(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	tok, err := e.verify(t, func(p *tsatest.TokenParams) { p.Accuracy = &tsatest.Accuracy{Seconds: 1, Millis: 500} })
	if err != nil {
		t.Fatal(err)
	}
	if tok.Accuracy == nil || *tok.Accuracy != 1500*time.Millisecond {
		t.Fatalf("accuracy %v", tok.Accuracy)
	}
}

// attacker is a second, untrusted PKI with a perfectly valid-looking TSU.
func attacker(t *testing.T) *tsatest.PKI { return tsatest.NewPKI(t, tsatest.PKIOptions{}) }

func TestVerifyCertInjection(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	evil := attacker(t)

	t.Run("extra cert in bag is ignored", func(t *testing.T) {
		tok, err := e.verify(t, func(p *tsatest.TokenParams) {
			p.Certs = []*x509.Certificate{evil.TSU.Cert, e.pki.TSU.Cert, evil.Inter.Cert, e.pki.Inter.Cert}
		})
		if err != nil {
			t.Fatal(err)
		}
		if !tok.Signer.Equal(e.pki.TSU.Cert) {
			t.Fatal("wrong signer chosen")
		}
	})
	t.Run("attacker signs, ESSCertID names trusted TSU", func(t *testing.T) {
		_, err := e.verify(t, func(p *tsatest.TokenParams) {
			p.Signer, p.CertIDCert, p.SIDCert = evil.TSU, e.pki.TSU.Cert, e.pki.TSU.Cert
			p.Certs = []*x509.Certificate{evil.TSU.Cert, e.pki.TSU.Cert, e.pki.Inter.Cert}
		})
		wantItem(t, err, 2)
	})
	t.Run("attacker signs with own cert first in bag", func(t *testing.T) {
		_, err := e.verify(t, func(p *tsatest.TokenParams) {
			p.Signer = evil.TSU
			p.Certs = []*x509.Certificate{evil.TSU.Cert, e.pki.TSU.Cert, e.pki.Inter.Cert}
		})
		wantItem(t, err, 7)
	})
	t.Run("SignerIdentifier names a different cert than ESSCertID", func(t *testing.T) {
		_, err := e.verify(t, func(p *tsatest.TokenParams) {
			p.SIDCert = evil.TSU.Cert
			p.Certs = append(p.Certs, evil.TSU.Cert)
		})
		wantItem(t, err, 3)
	})
	t.Run("no ESSCertID", func(t *testing.T) {
		_, err := e.verify(t, func(p *tsatest.TokenParams) { p.NoCertID = true })
		wantItem(t, err, 3)
	})
	t.Run("ESSCertID names a sibling cert with the same key", func(t *testing.T) {
		// ESSCertID hash of the TSU, but signed (and SID) as another cert
		// with the same key: the hash binding rejects it.
		other := e.pki.NewTSU(t, func(o *tsatest.CertOptions) { o.Key = e.pki.TSU.Key })
		_, err := e.verify(t, func(p *tsatest.TokenParams) { p.CertIDCert = other.Cert })
		wantItem(t, err, 3)
	})
}

func TestVerifyRejects(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	otherKey := tsatest.ECKey(t)
	nonCritical := e.pki.NewTSU(t, func(o *tsatest.CertOptions) { o.EKUCritical = false })
	nonExclusive := e.pki.NewTSU(t, func(o *tsatest.CertOptions) {
		o.EKU = []asn1.ObjectIdentifier{tsatest.OIDTimeStamping, tsatest.OIDCodeSigning}
	})
	noEKU := e.pki.NewTSU(t, func(o *tsatest.CertOptions) { o.EKU = nil })
	weakRSA := e.pki.NewTSU(t, func(o *tsatest.CertOptions) { o.Key = tsatest.RSAKey(t, 1024) })
	withSigner := func(c *tsatest.Cert) func(p *tsatest.TokenParams) {
		return func(p *tsatest.TokenParams) {
			p.Signer = c
			p.Certs = []*x509.Certificate{c.Cert, e.pki.Inter.Cert}
		}
	}
	for _, tc := range []struct {
		name string
		item int
		edit func(p *tsatest.TokenParams)
	}{
		{"manipulated signed attribute", 2, func(p *tsatest.TokenParams) { p.TamperSignedAttrs = true }},
		{"messageDigest mismatch", 2, func(p *tsatest.TokenParams) { p.WrongMessageDigest = true }},
		{"forged signature", 2, func(p *tsatest.TokenParams) { p.SignKey = otherKey }},
		{"wrong imprint", 5, func(p *tsatest.TokenParams) { p.Imprint = make([]byte, 32) }},
		{"imprint not SHA-256", 5, func(p *tsatest.TokenParams) { p.ImprintAlg = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3} }},
		{"nonce mismatch", 5, func(p *tsatest.TokenParams) { p.Nonce = new(big.Int).Add(p.Nonce, big.NewInt(1)) }},
		{"nonce missing", 5, func(p *tsatest.TokenParams) { p.Nonce = nil }},
		{"policy mismatch", 5, func(p *tsatest.TokenParams) { p.Policy = asn1.ObjectIdentifier{1, 2, 3} }},
		{"EKU not critical", 4, withSigner(nonCritical)},
		{"EKU not exclusive", 4, withSigner(nonExclusive)},
		{"no EKU", 4, withSigner(noEKU)},
		{"SHA-1 digest", 6, func(p *tsatest.TokenParams) { p.Hash = crypto.SHA1 }},
		{"ESSCertIDv2 with SHA-1", 3, func(p *tsatest.TokenParams) { p.CertIDHash = crypto.SHA1 }},
		{"RSA-1024 signer", 6, withSigner(weakRSA)},
		{"genTime after TSU expiry", 7, func(p *tsatest.TokenParams) { p.GenTime = time.Now().Add(2 * 365 * 24 * time.Hour) }},
		{"genTime before TSU validity", 7, func(p *tsatest.TokenParams) { p.GenTime = time.Now().Add(-48 * time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.verify(t, tc.edit)
			wantItem(t, err, tc.item)
		})
	}
}

func TestVerifyReplay(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	old := tsatest.Response(0, "", e.token(t, nil))
	// A new request for the same PrÃ¼fwert must not accept the old answer.
	fresh := must(NewRequest(e.pw, nil))
	o := e.opts()
	o.Request = fresh
	_, err := VerifyResponse(old, o)
	wantItem(t, err, 5)
}

func TestVerifyPath(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	tsr := tsatest.Response(0, "", e.token(t, nil))
	verify := func(anchors []*x509.Certificate, extra ...*x509.Certificate) (*Token, error) {
		o := e.opts()
		o.Anchors, o.Intermediates = anchors, extra
		return VerifyResponse(tsr, o)
	}
	t.Run("anchored at TSU", func(t *testing.T) {
		tok := must(verify([]*x509.Certificate{e.pki.TSU.Cert}))
		if len(tok.Chain) != 1 {
			t.Fatalf("chain %d", len(tok.Chain))
		}
	})
	t.Run("anchored at CA", func(t *testing.T) {
		tok := must(verify([]*x509.Certificate{e.pki.Inter.Cert}))
		if len(tok.Chain) != 2 {
			t.Fatalf("chain %d", len(tok.Chain))
		}
	})
	t.Run("foreign anchor", func(t *testing.T) {
		_, err := verify([]*x509.Certificate{attacker(t).Root.Cert})
		wantItem(t, err, 7)
	})
	t.Run("no anchors", func(t *testing.T) {
		_, err := verify(nil)
		wantItem(t, err, 7)
	})
	t.Run("intermediate missing, supplied from chain.pem", func(t *testing.T) {
		tsr := tsatest.Response(0, "", e.token(t, func(p *tsatest.TokenParams) { p.Certs = []*x509.Certificate{e.pki.TSU.Cert} }))
		o := e.opts()
		if _, err := VerifyResponse(tsr, o); err == nil {
			t.Fatal("verified without intermediate")
		}
		o.Intermediates = []*x509.Certificate{e.pki.Inter.Cert}
		must(VerifyResponse(tsr, o))
	})
	t.Run("signer only in chain.pem", func(t *testing.T) {
		tsr := tsatest.Response(0, "", e.token(t, func(p *tsatest.TokenParams) { p.Certs = nil }))
		o := e.opts()
		o.Intermediates = []*x509.Certificate{e.pki.TSU.Cert, e.pki.Inter.Cert}
		must(VerifyResponse(tsr, o))
	})
	t.Run("TSU anchor still needs EKU", func(t *testing.T) {
		bad := e.pki.NewTSU(t, func(o *tsatest.CertOptions) { o.EKUCritical = false })
		tsr := tsatest.Response(0, "", e.token(t, func(p *tsatest.TokenParams) {
			p.Signer, p.Certs = bad, []*x509.Certificate{bad.Cert}
		}))
		o := e.opts()
		o.Anchors = []*x509.Certificate{bad.Cert}
		_, err := VerifyResponse(tsr, o)
		wantItem(t, err, 4)
	})
	t.Run("CA without keyCertSign", func(t *testing.T) {
		ca := e.pki.Root.Issue(t, tsatest.CertOptions{CN: "No CertSign CA", IsCA: true, KeyUsage: x509.KeyUsageDigitalSignature})
		tsu := ca.Issue(t, tsatest.CertOptions{CN: "TSU", EKU: []asn1.ObjectIdentifier{tsatest.OIDTimeStamping}, EKUCritical: true})
		tsr := tsatest.Response(0, "", e.token(t, func(p *tsatest.TokenParams) {
			p.Signer, p.Certs = tsu, []*x509.Certificate{tsu.Cert, ca.Cert}
		}))
		_, err := VerifyResponse(tsr, e.opts())
		wantItem(t, err, 7)
	})
}

func TestVerifyStatusRejection(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	_, err := VerifyResponse(tsatest.Response(2, "no credit", nil), e.opts())
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 2 {
		t.Fatalf("got %v", err)
	}
	_, err = VerifyResponse(tsatest.Response(0, "", nil), e.opts())
	wantItem(t, err, 1)
}

func TestParseGeneralizedTime(t *testing.T) {
	for in, want := range map[string]string{
		"20261118120000Z":     "2026-11-18T12:00:00Z",
		"20261118120000.5Z":   "2026-11-18T12:00:00.5Z",
		"20261118120000.500Z": "2026-11-18T12:00:00.5Z",
	} {
		got, err := parseGeneralizedTime(asn1.RawValue{Tag: asn1.TagGeneralizedTime, Bytes: []byte(in)})
		if err != nil || got.Format(time.RFC3339Nano) != want {
			t.Errorf("%s: %v %v", in, got, err)
		}
	}
	for _, in := range []string{"20261118120000+0100", "2026111812Z", "20261118120000"} {
		if _, err := parseGeneralizedTime(asn1.RawValue{Tag: asn1.TagGeneralizedTime, Bytes: []byte(in)}); err == nil {
			t.Errorf("%s accepted", in)
		}
	}
}
