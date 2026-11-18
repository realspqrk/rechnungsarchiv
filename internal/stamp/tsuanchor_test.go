//go:build testtsa

package stamp_test

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
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// tsuAnchored is the Mensatek world: the trusted list names the TSU
// certificate itself as the QTST service identity, and the token carries
// only the TSU certificate. Its issuer, needed for the TSU's revocation
// evidence, is only available via AIA caIssuers.
func tsuAnchored(t *testing.T) *testenv.Env {
	t.Helper()
	e := testenv.New(t)
	e.PublishListsNaming(time.Now().Add(-47*time.Hour), e.PKI.TSU.Cert)
	e.TSA.Chain = nil
	e.Add(testenv.Incoming, archive.Options{})
	return e
}

func storedChain(t *testing.T, e *testenv.Env) []*x509.Certificate {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(evDir(e), "1.chain.pem"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	certs, err := tsa.ParsePEM(b)
	if err != nil {
		t.Fatal(err)
	}
	return certs
}

func wantQualified(t *testing.T, e *testenv.Env) {
	t.Helper()
	e.PublishListsNaming(time.Now().Add(time.Minute), e.PKI.TSU.Cert) // issued after genTime
	if err := e.Trust().Refresh(context.Background(), testenv.Territory); err != nil {
		t.Fatal(err)
	}
	r := verifyArchive(t, e, verify.Options{})
	if d := r.Deposits[0]; r.Status != verify.StatusTimestamped || d.Qualification != verify.Qualified || d.Token == nil || len(d.Token.Revocation) != 1 {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestTSUAnchoredTokenWithoutIssuer(t *testing.T) {
	t.Run("issuer via AIA in the first run", func(t *testing.T) {
		e := tsuAnchored(t)
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 {
			t.Fatalf("%+v", o)
		}
		if ch := storedChain(t, e); len(ch) != 2 || !ch[0].Equal(e.PKI.TSU.Cert) || !ch[1].Equal(e.PKI.Inter.Cert) {
			t.Fatalf("chain.pem: %d certs", len(ch))
		}
		wantQualified(t, e)
	})

	// The first material fetch fails (AIA/OCSP server down). The attempt is
	// discarded; the next run within 24 h completes it without buying a
	// new timestamp.
	t.Run("first fetch fails, completed later", func(t *testing.T) {
		e := tsuAnchored(t)
		e.PKI.SetDown(true)
		if o := one(t, e.Stamp()); o.Stamped || o.Reason != verify.ReasonMaterial || o.Attempt != 1 {
			t.Fatalf("%+v", o)
		}
		if ch := storedChain(t, e); len(ch) > 1 {
			t.Fatalf("chain.pem with %d certs although the issuer was not obtained", len(ch))
		}
		if d := verifyArchive(t, e, verify.Options{}).Deposits[0]; d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
			t.Fatalf("%s", testenv.JSON(d))
		}
		e.PKI.SetDown(false)
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
			t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
		}
		wantQualified(t, e)
	})

	// The state an older version left (Mensatek attempt 13 of the real
	// test archive): n.chain.pem holds only the TSU certificate. The chain
	// is extended append-only and the attempt completes.
	t.Run("old chain.pem with only the TSU is extended", func(t *testing.T) {
		e := tsuAnchored(t)
		e.PKI.SetDown(true)
		one(t, e.Stamp())
		if err := tsa.WriteMaterial(evDir(e), 1, &tsa.Material{Chain: []*x509.Certificate{e.PKI.TSU.Cert}}); err != nil {
			t.Fatal(err)
		}
		e.PKI.SetDown(false)
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
			t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
		}
		if ch := storedChain(t, e); len(ch) != 2 || !ch[0].Equal(e.PKI.TSU.Cert) || !ch[1].Equal(e.PKI.Inter.Cert) {
			t.Fatalf("chain.pem: %d certs", len(ch))
		}
		wantQualified(t, e)
	})

	// The token itself carries the issuer: no AIA fetch is needed.
	t.Run("issuer in the token", func(t *testing.T) {
		e := tsuAnchored(t)
		e.TSA.Chain = []*x509.Certificate{e.PKI.Inter.Cert}
		if o := one(t, e.Stamp()); !o.Stamped || hits(e, "/inter.cer") != 0 {
			t.Fatalf("%+v hits=%v", o, e.PKI.Hits)
		}
		wantQualified(t, e)
	})
}

// A stored chain that lacks an intermediate fetched via AIA (root anchor)
// is extended too; otherwise verification could never build the path.
func TestStoredChainMissingIntermediateExtended(t *testing.T) {
	e := testenv.New(t)
	e.TSA.Chain = nil
	e.Add(testenv.Incoming, archive.Options{})
	e.PKI.CRLHook = func(string) []byte { return []byte("not a CRL") }
	one(t, e.Stamp())
	remove(t, filepath.Join(evDir(e), "1.chain.pem"))
	if err := tsa.WriteMaterial(evDir(e), 1, &tsa.Material{Chain: []*x509.Certificate{e.PKI.TSU.Cert}}); err != nil {
		t.Fatal(err)
	}
	e.PKI.CRLHook = nil
	if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
		t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
	}
	if ch := storedChain(t, e); len(ch) < 2 || !ch[0].Equal(e.PKI.TSU.Cert) || !ch[1].Equal(e.PKI.Inter.Cert) {
		t.Fatalf("chain.pem: %d certs", len(ch))
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}
