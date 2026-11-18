package tsa

import (
	"context"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"golang.org/x/crypto/ocsp"
)

func ocspTmpl(c *x509.Certificate, this, next time.Time) ocsp.Response {
	return ocsp.Response{Status: ocsp.Good, SerialNumber: c.SerialNumber, ThisUpdate: this, NextUpdate: next}
}

func TestCheckOCSP(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	p, tsu := e.pki, e.pki.TSU.Cert
	gen := time.Now().Truncate(time.Second)
	evil := attacker(t)
	noEKU := p.Inter.Issue(t, tsatest.CertOptions{CN: "Not a responder"})

	check := func(raw []byte) (*Revocation, error) {
		return CheckRevocation(KindOCSP, raw, tsu, p.Inter.Cert, gen)
	}
	t.Run("delegated responder, good, covering", func(t *testing.T) {
		r := must(check(tsatest.SignOCSP(p.Inter, p.Responder, ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD)))))
		if !r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("signed by CA itself", func(t *testing.T) {
		r := must(check(tsatest.SignOCSP(p.Inter, p.Inter, ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD)))))
		if !r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("produced within 24h after genTime", func(t *testing.T) {
		r := must(check(tsatest.SignOCSP(p.Inter, p.Responder, ocspTmpl(tsu, gen.Add(2*hourD), time.Time{}))))
		if !r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("foreign responder", func(t *testing.T) {
		_, err := check(tsatest.SignOCSP(p.Inter, evil.Responder, ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD))))
		if err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("responder without OCSPSigning", func(t *testing.T) {
		_, err := check(tsatest.SignOCSP(p.Inter, noEKU, ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD))))
		if err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("certID of another issuer", func(t *testing.T) {
		// Signed by the right CA, but the certID names a different issuer.
		raw, err := ocsp.CreateResponse(evil.Inter.Cert, p.Inter.Cert, ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD)), p.Inter.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := check(raw); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("other serial", func(t *testing.T) {
		if _, err := check(tsatest.SignOCSP(p.Inter, p.Responder, ocspTmpl(p.Responder.Cert, gen.Add(-hourD), gen.Add(hourD)))); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("stale: ends before genTime", func(t *testing.T) {
		r := must(check(tsatest.SignOCSP(p.Inter, p.Responder, ocspTmpl(tsu, gen.Add(-48*hourD), gen.Add(-24*hourD)))))
		if r.Covers || r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("stale: more than 24h after genTime", func(t *testing.T) {
		r := must(check(tsatest.SignOCSP(p.Inter, p.Responder, ocspTmpl(tsu, gen.Add(25*hourD), gen.Add(48*hourD)))))
		if r.Covers || r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	for _, tc := range []struct {
		name   string
		reason int
		kc     bool
	}{{"hold", ocsp.CertificateHold, false}, {"keyCompromise", ocsp.KeyCompromise, true}, {"unspecified", ocsp.Unspecified, true}, {"superseded", ocsp.Superseded, false}} {
		t.Run("revoked "+tc.name, func(t *testing.T) {
			tmpl := ocspTmpl(tsu, gen.Add(-hourD), gen.Add(hourD))
			tmpl.Status, tmpl.RevocationReason, tmpl.RevokedAt = ocsp.Revoked, tc.reason, gen.Add(-2*hourD)
			r := must(check(tsatest.SignOCSP(p.Inter, p.Responder, tmpl)))
			if r.OK() || r.Status != StatusRevoked || r.Reason != tc.reason || r.KeyCompromise() != tc.kc {
				t.Fatalf("%+v", r)
			}
		})
	}
}

func TestCheckCRL(t *testing.T) {
	e := newEnv(t, tsatest.PKIOptions{})
	p := e.pki
	inter := p.Inter.Cert
	gen := time.Now().Truncate(time.Second)
	check := func(raw []byte) (*Revocation, error) {
		return CheckRevocation(KindCRL, raw, inter, p.Root.Cert, gen)
	}
	ext := func(oid asn1.ObjectIdentifier, v any) []pkix.Extension {
		b, err := asn1.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return []pkix.Extension{{Id: oid, Critical: true, Value: b}}
	}

	t.Run("not listed, covering", func(t *testing.T) {
		r := must(check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), nil)))
		if !r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("matching IDP", func(t *testing.T) {
		idp := tagged0(tagged0(uri(inter.CRLDistributionPoints[0])))
		r := must(check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), []pkix.Extension{{Id: oidIssuingDistPt, Critical: true, Value: seq(idp)}})))
		if !r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("IDP for another partition", func(t *testing.T) {
		idp := tagged0(tagged0(uri("http://example.invalid/part2.crl")))
		if _, err := check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), []pkix.Extension{{Id: oidIssuingDistPt, Critical: true, Value: seq(idp)}})); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("IDP only user certs", func(t *testing.T) {
		v := []byte{0x81, 0x01, 0xff} // [1] onlyContainsUserCerts TRUE
		if _, err := check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), []pkix.Extension{{Id: oidIssuingDistPt, Critical: true, Value: seq(v)}})); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("delta CRL", func(t *testing.T) {
		if _, err := check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), ext(oidDeltaCRL, 1))); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("signed by another CA", func(t *testing.T) {
		if _, err := check(p.CRL(p.Inter, gen.Add(-hourD), gen.Add(hourD), nil)); err == nil {
			t.Fatal("accepted")
		} else {
			t.Log(err)
		}
	})
	t.Run("same issuer name, foreign key", func(t *testing.T) {
		ev := attacker(t)
		_, err := check(ev.CRL(ev.Root, gen.Add(-hourD), gen.Add(hourD), nil))
		if err == nil {
			t.Fatal("accepted")
		}
		t.Log(err)
	})
	t.Run("stale", func(t *testing.T) {
		r := must(check(p.CRL(p.Root, gen.Add(-72*hourD), gen.Add(-48*hourD), nil)))
		if r.OK() {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("CRL after the certificate expired", func(t *testing.T) {
		short := p.Inter.Issue(t, tsatest.CertOptions{CN: "short", NotAfter: gen.Add(30 * time.Minute)})
		raw := p.CRL(p.Inter, gen.Add(hourD), gen.Add(48*hourD), nil)
		r := must(CheckRevocation(KindCRL, raw, short.Cert, p.Inter.Cert, gen))
		if r.Covers {
			t.Fatal("expired certificate dropped off the CRL counts as not listed")
		}
	})
	t.Run("revoked keyCompromise", func(t *testing.T) { // last: Revoke has no undo
		p.Revoke(inter, ReasonKeyCompromise)
		r := must(check(p.CRL(p.Root, gen.Add(-hourD), gen.Add(hourD), nil)))
		if r.OK() || !r.KeyCompromise() {
			t.Fatalf("%+v", r)
		}
	})
}

const hourD = time.Hour

func tagged0(b []byte) []byte { return append([]byte{0xa0, byte(len(b))}, b...) }
func uri(s string) []byte     { return append([]byte{0x86, byte(len(s))}, s...) }
func seq(b []byte) []byte     { return append([]byte{0x30, byte(len(b))}, b...) }

func TestCollectAndValidate(t *testing.T) {
	ctx := context.Background()
	t.Run("happy path: OCSP for TSU, CRL for intermediate", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		tok := must(e.verify(t, nil))
		var f Fetcher
		m, checks, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		if len(checks) != 2 || checks[0].Revocation.Kind != KindOCSP || checks[1].Revocation.Kind != KindCRL {
			t.Fatalf("checks %+v", checks)
		}
		if len(m.Chain) != 3 || len(m.Evidence) != 2 {
			t.Fatalf("material: %d certs, %d evidence", len(m.Chain), len(m.Evidence))
		}
		if e.pki.Hits["/ocsp/inter"] != 1 || e.pki.Hits["/crl/root.crl"] != 1 {
			t.Fatalf("hits %v", e.pki.Hits)
		}
	})
	t.Run("anchored at TSU still needs its evidence", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		o := e.opts()
		o.Anchors = []*x509.Certificate{e.pki.TSU.Cert}
		tok := must(VerifyResponse(tsatest.Response(0, "", e.token(t, nil)), o))
		var f Fetcher
		m, checks, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		if len(checks) != 1 || len(m.Chain) != 2 {
			t.Fatalf("%d checks, %d certs", len(checks), len(m.Chain))
		}
	})
	// Real-world proof (Mensatek): the trusted list names the TSU itself as
	// the service identity and the token carries only the TSU certificate.
	// Its issuer is needed for the revocation evidence and must come from
	// the AIA caIssuers URL.
	onlySigner := func(e *env) *Token {
		o := e.opts()
		o.Anchors = []*x509.Certificate{e.pki.TSU.Cert}
		raw := e.token(t, func(p *tsatest.TokenParams) { p.Certs = []*x509.Certificate{e.pki.TSU.Cert} })
		return must(VerifyResponse(tsatest.Response(0, "", raw), o))
	}
	t.Run("anchored at TSU, issuer only via AIA", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		tok := onlySigner(e)
		var f Fetcher
		m, checks, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		if len(checks) != 1 || len(m.Chain) != 2 || !m.Chain[1].Equal(e.pki.Inter.Cert) || e.pki.Hits["/inter.cer"] != 1 {
			t.Fatalf("%d checks, %d certs, hits %v", len(checks), len(m.Chain), e.pki.Hits)
		}
		if _, err := ValidateMaterial(tok, m); err != nil {
			t.Fatalf("stored material does not validate offline: %v", err)
		}
	})
	t.Run("anchored at TSU, AIA serves a foreign certificate", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{
			TSU: func(o *tsatest.CertOptions) {
				o.Issuers = []string{strings.Replace(o.Issuers[0], "/inter.cer", "/root.cer", 1)}
			},
		})
		tok := onlySigner(e)
		var f Fetcher
		if _, _, err := f.Collect(ctx, tok); !errors.Is(err, ErrMaterial) {
			t.Fatalf("foreign issuer accepted: %v", err)
		}
		if e.pki.Hits["/root.cer"] != 1 {
			t.Fatalf("foreign certificate not served: %v", e.pki.Hits)
		}
	})
	t.Run("OCSP fails, CRL fallback", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		e.pki.OCSPHook = func(*ocsp.Request) []byte { return []byte("garbage") }
		tok := must(e.verify(t, nil))
		var f Fetcher
		_, checks, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		if checks[0].Revocation.Kind != KindCRL {
			t.Fatal("no CRL fallback")
		}
	})
	t.Run("TSU revoked keyCompromise", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		e.pki.Revoke(e.pki.TSU.Cert, ReasonKeyCompromise)
		tok := must(e.verify(t, nil))
		var f Fetcher
		_, checks, err := f.Collect(ctx, tok)
		if !errors.Is(err, ErrMaterial) {
			t.Fatalf("err %v", err)
		}
		if !checks[0].Revocation.KeyCompromise() {
			t.Fatalf("%+v", checks[0].Revocation)
		}
	})
	t.Run("hold on intermediate", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		e.pki.Revoke(e.pki.Inter.Cert, ReasonCertificateHold)
		tok := must(e.verify(t, nil))
		var f Fetcher
		if _, _, err := f.Collect(ctx, tok); !errors.Is(err, ErrMaterial) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("missing evidence offline", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		tok := must(e.verify(t, nil))
		var f Fetcher
		m, _, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		delete(m.Evidence, CertKey(e.pki.Inter.Cert))
		if _, err := ValidateMaterial(tok, m); !errors.Is(err, ErrMaterial) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("evidence of one cert filed under another", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		tok := must(e.verify(t, nil))
		var f Fetcher
		m, _, err := f.Collect(ctx, tok)
		if err != nil {
			t.Fatal(err)
		}
		k1, k2 := CertKey(e.pki.TSU.Cert), CertKey(e.pki.Inter.Cert)
		m.Evidence[k1], m.Evidence[k2] = m.Evidence[k2], m.Evidence[k1]
		if _, err := ValidateMaterial(tok, m); !errors.Is(err, ErrMaterial) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("AIA completes a missing intermediate", func(t *testing.T) {
		e := newEnv(t, tsatest.PKIOptions{})
		tsr := tsatest.Response(0, "", e.token(t, func(p *tsatest.TokenParams) { p.Certs = []*x509.Certificate{e.pki.TSU.Cert} }))
		var f Fetcher
		tok, err := f.Accept(ctx, tsr, e.opts())
		if err != nil {
			t.Fatal(err)
		}
		if len(tok.Chain) != 3 || e.pki.Hits["/inter.cer"] != 1 {
			t.Fatalf("chain %d, hits %v", len(tok.Chain), e.pki.Hits)
		}
	})
}
