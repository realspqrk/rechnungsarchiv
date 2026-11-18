//go:build testtsa

package verify_test

import (
	"context"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
	"golang.org/x/crypto/ocsp"
)

// tsuStamped returns a stamped archive of the Mensatek kind: the trusted
// list names the TSU certificate itself, the token carries only the TSU,
// and its issuer came via AIA into an extended n.chain.pem.
func tsuStamped(t *testing.T) *testenv.Env {
	t.Helper()
	e := testenv.New(t)
	e.PublishListsNaming(time.Now().Add(-47*time.Hour), e.PKI.TSU.Cert)
	e.TSA.Chain = nil
	e.Add(testenv.Incoming, archive.Options{Name: "A", Belegjahr: 2026})
	for _, o := range e.Stamp().Outcomes {
		if !o.Stamped {
			t.Fatalf("not stamped: %+v", o)
		}
	}
	e.PublishListsNaming(time.Now().Add(time.Minute), e.PKI.TSU.Cert) // issued after genTime
	if err := e.Trust().Refresh(context.Background(), testenv.Territory); err != nil {
		t.Fatal(err)
	}
	return e
}

func ndir(e *testenv.Env) string { return filepath.Join(e.Dir, "0001_A.nachweis") }

func nachweis(e *testenv.Env, name string) string { return filepath.Join(ndir(e), name) }

func overwrite(t *testing.T, p string, b []byte) {
	t.Helper()
	os.Chmod(p, 0o644)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func wantNotGreen(t *testing.T, e *testenv.Env) {
	t.Helper()
	d := run(t, e, verify.Options{}).Deposits[0]
	if d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial || d.Proven != nil || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

// Security tests for an extended n.chain.pem (spec §5): what is appended
// is only a candidate; the path is still built against trusted-list
// anchors, an issuer must verify the signature, and evidence must be the
// issuer's for exactly that certificate.
func TestExtendedChainSecurity(t *testing.T) {
	t.Run("baseline", func(t *testing.T) {
		e := tsuStamped(t)
		d := run(t, e, verify.Options{}).Deposits[0]
		if d.Status != verify.StatusTimestamped || d.Qualification != verify.Qualified || len(d.Token.Chain) != 1 || len(d.Token.Revocation) != 1 {
			t.Fatalf("%s", testenv.JSON(d))
		}
	})
	t.Run("unrelated certificate appended", func(t *testing.T) {
		e := tsuStamped(t)
		alien := tsatest.SelfSigned(t, tsatest.CertOptions{CN: "Fremde CA"}).Cert
		other := e.PKI.NewTSU(t, nil).Cert // same issuer, another TSU
		m := &tsa.Material{Chain: []*x509.Certificate{e.PKI.TSU.Cert, e.PKI.Inter.Cert, alien, other}}
		if err := tsa.WriteMaterial(ndir(e), 1, m); err != nil {
			t.Fatalf("append-only extension refused: %v", err)
		}
		d := run(t, e, verify.Options{}).Deposits[0]
		if d.Status != verify.StatusTimestamped || d.Qualification != verify.Qualified || len(d.Token.Chain) != 1 || d.Token.Chain[0] != e.PKI.TSU.Cert.Subject.String() {
			t.Fatalf("appended certificates changed the decision: %s", testenv.JSON(d))
		}
	})

	// A look-alike issuer (same name, own key) with evidence it signed
	// itself: never accepted, whether it replaces the real issuer or is
	// stored before or after it.
	fake := func(t *testing.T, e *testenv.Env) *tsatest.Cert {
		f := tsatest.SelfSigned(t, tsatest.CertOptions{CN: e.PKI.Inter.Cert.Subject.CommonName})
		if string(f.Cert.RawSubject) != string(e.PKI.TSU.Cert.RawIssuer) {
			t.Fatal("look-alike does not match the issuer name")
		}
		now := time.Now()
		forged := tsatest.SignOCSP(f, f, ocsp.Response{Status: ocsp.Good, SerialNumber: e.PKI.TSU.Cert.SerialNumber, ThisUpdate: now.Add(-time.Hour), NextUpdate: now.Add(24 * time.Hour)})
		overwrite(t, nachweis(e, filepath.Join("1.rev", tsa.CertKey(e.PKI.TSU.Cert)+".ocsp")), forged)
		return f
	}
	for name, order := range map[string]func(e *testenv.Env, f *tsatest.Cert) []*x509.Certificate{
		"look-alike issuer replaces the real one": func(e *testenv.Env, f *tsatest.Cert) []*x509.Certificate {
			return []*x509.Certificate{e.PKI.TSU.Cert, f.Cert}
		},
		"look-alike issuer before the real one": func(e *testenv.Env, f *tsatest.Cert) []*x509.Certificate {
			return []*x509.Certificate{e.PKI.TSU.Cert, f.Cert, e.PKI.Inter.Cert}
		},
		"look-alike issuer appended": func(e *testenv.Env, f *tsatest.Cert) []*x509.Certificate {
			return []*x509.Certificate{e.PKI.TSU.Cert, e.PKI.Inter.Cert, f.Cert}
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := tsuStamped(t)
			f := fake(t, e)
			overwrite(t, nachweis(e, "1.chain.pem"), tsa.EncodePEM(order(e, f)))
			wantNotGreen(t, e)
		})
	}

	t.Run("replayed evidence of another certificate", func(t *testing.T) {
		e := tsuStamped(t)
		other := e.PKI.NewTSU(t, nil)
		now := time.Now()
		replay := e.PKI.OCSP(other.Cert.SerialNumber, e.PKI.Inter, e.PKI.Responder, now.Add(-time.Hour), now.Add(24*time.Hour))
		overwrite(t, nachweis(e, filepath.Join("1.rev", tsa.CertKey(e.PKI.TSU.Cert)+".ocsp")), replay)
		wantNotGreen(t, e)
	})
	t.Run("malformed chain.pem", func(t *testing.T) {
		e := tsuStamped(t)
		overwrite(t, nachweis(e, "1.chain.pem"), []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"))
		wantNotGreen(t, e)
		// The writer never replaces it either.
		err := tsa.WriteMaterial(ndir(e), 1, &tsa.Material{Chain: []*x509.Certificate{e.PKI.TSU.Cert, e.PKI.Inter.Cert}})
		if err == nil {
			t.Fatal("malformed chain.pem replaced")
		}
	})
	t.Run("stored prefix altered", func(t *testing.T) {
		e := tsuStamped(t)
		other := e.PKI.NewTSU(t, nil).Cert
		err := tsa.WriteMaterial(ndir(e), 1, &tsa.Material{Chain: []*x509.Certificate{other, e.PKI.Inter.Cert, e.PKI.Root.Cert}})
		if err == nil {
			t.Fatal("altered prefix accepted")
		}
		if d := run(t, e, verify.Options{}).Deposits[0]; d.Status != verify.StatusTimestamped {
			t.Fatalf("%s", testenv.JSON(d))
		}
	})
}
