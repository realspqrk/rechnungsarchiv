//go:build testtsa

package verify_test

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// addAttempt stores attempt n for root in dir: a token signed by signer
// and complete evidence built from the PKI's current revocation state.
func addAttempt(t *testing.T, e *testenv.Env, dir string, n int, root string, signer *tsatest.Cert) {
	t.Helper()
	h, _ := hex.DecodeString(strings.TrimPrefix(root, "sha256:"))
	var pw [32]byte
	copy(pw[:], h)
	req, err := tsa.NewRequest(pw, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := &tsatest.TSA{Signer: signer, Chain: []*x509.Certificate{e.PKI.Inter.Cert}, Policy: tsatest.DefaultPolicy}
	srv.SetEdit(testenv.WithAccuracy)
	tsr, err := srv.Respond(req.DER)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	m := &tsa.Material{Chain: []*x509.Certificate{signer.Cert, e.PKI.Inter.Cert, e.PKI.Root.Cert}, Evidence: map[string]tsa.Evidence{
		tsa.CertKey(signer.Cert):      {Kind: tsa.KindOCSP, Raw: e.PKI.OCSP(signer.Cert.SerialNumber, e.PKI.Inter, e.PKI.Responder, now.Add(-time.Hour), now.Add(24*time.Hour))},
		tsa.CertKey(e.PKI.Inter.Cert): {Kind: tsa.KindCRL, Raw: e.PKI.CRL(e.PKI.Root, now.Add(-time.Hour), now.Add(7*24*time.Hour), nil)},
	}}
	if err := errors.Join(tsa.WriteRequest(dir, n, req.DER), tsa.WriteResponse(dir, n, tsr), tsa.WriteMaterial(dir, n, m)); err != nil {
		t.Fatal(err)
	}
}

// Finding 1: authenticated key-compromise evidence stored with another
// attempt of the same Ablage makes the valid attempt of the same key
// Unbestimmt; a token of an unaffected key is still selected.
func TestCompromiseAcrossAttempts(t *testing.T) {
	e := stamped(t, 1)
	ev := filepath.Join(e.Dir, "0001_A.nachweis")
	root := run(t, e, verify.Options{}).Deposits[0].Root

	e.PKI.Revoke(e.PKI.TSU.Cert, tsa.ReasonKeyCompromise)
	addAttempt(t, e, ev, 2, root, e.PKI.TSU) // stores the revoked OCSP answer
	d := run(t, e, verify.Options{}).Deposits[0]
	if d.Status != verify.StatusIndeterminate || d.Attempts[0].Outcome != verify.OutcomeCompromised ||
		d.Attempts[1].Outcome != verify.OutcomeCompromised || d.Token == nil || !d.Token.Compromised || d.Proven != nil {
		t.Fatalf("%s", testenv.JSON(d))
	}

	// A token of another, unaffected key: selected, Zeitgestempelt.
	tsu2 := e.PKI.NewTSU(t, nil)
	addAttempt(t, e, ev, 3, root, tsu2)
	d = run(t, e, verify.Options{}).Deposits[0]
	if d.Status != verify.StatusTimestamped || d.Token.Attempt != 3 || d.Attempts[0].Outcome != verify.OutcomeCompromised {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

// Finding 1: compromise evidence stored with another Ablage counts too.
func TestCompromiseAcrossAblagen(t *testing.T) {
	e := stamped(t, 2)
	r := run(t, e, verify.Options{})
	e.PKI.Revoke(e.PKI.TSU.Cert, tsa.ReasonKeyCompromise)
	addAttempt(t, e, filepath.Join(e.Dir, "0002_B.nachweis"), 2, r.Deposits[1].Root, e.PKI.TSU)

	r = run(t, e, verify.Options{})
	for _, d := range r.Deposits {
		if d.Status != verify.StatusIndeterminate || d.Token == nil || !d.Token.Compromised || d.Proven != nil {
			t.Fatalf("Ablage %d: %s", d.Sequence, testenv.JSON(d))
		}
	}
	// The statement names the reason from the evidence of the other Ablage.
	var revoked bool
	for _, rv := range r.Deposits[0].Token.Revocation {
		revoked = revoked || (rv.Status == "revoked" && rv.Reason == tsa.ReasonKeyCompromise)
	}
	if !revoked {
		t.Fatalf("compromise evidence not shown: %s", testenv.JSON(r.Deposits[0].Token))
	}
}

// Finding 5: a renamed Nachweis folder is found by its imprint.
func TestRenamedNachweisFoundByImprint(t *testing.T) {
	e := stamped(t, 2)
	if err := os.Rename(filepath.Join(e.Dir, "0001_A.nachweis"), filepath.Join(e.Dir, "Kopie vom Server.nachweis")); err != nil {
		t.Fatal(err)
	}
	r := run(t, e, verify.Options{})
	d := r.Deposits[0]
	if d.Status != verify.StatusTimestamped || d.Proven == nil || d.Proven.Via != 0 || d.Attempts[0].Nachweis != "Kopie vom Server.nachweis" {
		t.Fatalf("%s", testenv.JSON(d))
	}
	// A single ZIP finds it next to itself as well.
	single, err := verify.Path(context.Background(), zipPath(e, 1, "A"), verify.Options{Trust: e.Trust()})
	if err != nil || single.Deposits[0].Status != verify.StatusTimestamped {
		t.Fatalf("%v %s", err, testenv.JSON(single))
	}
}

// Finding 8: provider facts come from the provider recorded with the
// attempt, not from the archive's current provider.
func TestProviderFactsBoundToAttempt(t *testing.T) {
	e := testenv.New(t)
	e.TSA.SetEdit(nil) // tokens without accuracy: the declared one applies
	e.Add(testenv.Incoming, archive.Options{Name: "A", Belegjahr: 2026})
	e.Stamp()

	// The archive switches to another provider with another accuracy.
	other := e.Provider
	other.ID, other.Accuracy = e.Provider.ID+"-neu", "5s"
	tsa.RegisterTestProvider(other)
	e.SetProvider(other.ID)

	d := run(t, e, verify.Options{}).Deposits[0]
	if d.Token.Provider != e.Provider.ID || d.Token.AccuracySource != "provider" || d.Token.AccuracyMS == nil || *d.Token.AccuracyMS != 1000 {
		t.Fatalf("%s", testenv.JSON(d.Token))
	}

	// Without the recorded provider nothing is assumed.
	ev := filepath.Join(e.Dir, "0001_A.nachweis", "1.provider")
	os.Chmod(ev, 0o644)
	if err := os.Remove(ev); err != nil {
		t.Fatal(err)
	}
	d = run(t, e, verify.Options{}).Deposits[0]
	if d.Token.Provider != "" || d.Token.AccuracyMS != nil || d.Proven == nil || d.Proven.At != nil {
		t.Fatalf("%s", testenv.JSON(d))
	}
}
