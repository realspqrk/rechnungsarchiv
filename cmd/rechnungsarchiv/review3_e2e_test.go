//go:build testtsa

package main

import (
	"crypto/x509"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

// storeAttempt stores attempt n for root in dir: a token of e's signer and
// complete evidence from the PKI's current revocation state.
func storeAttempt(t *testing.T, e *testenv.Env, dir string, n int, root string) {
	t.Helper()
	h, _ := hex.DecodeString(strings.TrimPrefix(root, "sha256:"))
	var pw [32]byte
	copy(pw[:], h)
	req, err := tsa.NewRequest(pw, nil)
	if err != nil {
		t.Fatal(err)
	}
	srv := &tsatest.TSA{Signer: e.PKI.TSU, Chain: []*x509.Certificate{e.PKI.Inter.Cert}, Policy: tsatest.DefaultPolicy}
	srv.SetEdit(testenv.WithAccuracy)
	tsr, err := srv.Respond(req.DER)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	signer := e.PKI.TSU.Cert
	m := &tsa.Material{Chain: []*x509.Certificate{signer, e.PKI.Inter.Cert, e.PKI.Root.Cert}, Evidence: map[string]tsa.Evidence{
		tsa.CertKey(signer):           {Kind: tsa.KindOCSP, Raw: e.PKI.OCSP(signer.SerialNumber, e.PKI.Inter, e.PKI.Responder, now.Add(-time.Hour), now.Add(24*time.Hour))},
		tsa.CertKey(e.PKI.Inter.Cert): {Kind: tsa.KindCRL, Raw: e.PKI.CRL(e.PKI.Root, now.Add(-time.Hour), now.Add(7*24*time.Hour), nil)},
	}}
	if err := errors.Join(tsa.WriteRequest(dir, n, req.DER), tsa.WriteResponse(dir, n, tsr), tsa.WriteMaterial(dir, n, m)); err != nil {
		t.Fatal(err)
	}
}

// Review 3 finding 2: status pools key-compromise evidence across Ablagen
// like verify and stamp do. Two Ablagen share a signer; the keyCompromise
// OCSP answer is stored only with the second.
func TestStatusPoolsCompromise(t *testing.T) {
	w, e := tsaWorld(t)
	dir := filepath.Join(t.TempDir(), "Rechnungsarchiv")
	w.run("", "init", dir, "--firma", "Musterfirma GmbH").want(t, 0)
	w.run(testenv.Password+"\n", "provider", "set", e.Provider.ID, "--user", testenv.User).want(t, 0)
	w.run("", "archive", demo(testenv.Incoming), "--yes").want(t, 0)
	w.run("", "archive", demo(testenv.Outgoing), "--yes").want(t, 0)
	if st := w.run("", "status").want(t, 0); get(st, "deposits", "timestamped") != 2.0 {
		t.Fatalf("status %v", st)
	}
	rep := w.run("", "verify").want(t, 0)
	root2 := get(rep["deposits"].([]any)[1], "root").(string)

	e.PKI.Revoke(e.PKI.TSU.Cert, tsa.ReasonKeyCompromise)
	storeAttempt(t, e, filepath.Join(dir, "0002_"+testenv.Outgoing+".nachweis"), 2, root2)

	rep = w.run("", "verify").want(t, 6)
	for _, d := range rep["deposits"].([]any) {
		if get(d, "status") != "indeterminate" {
			t.Fatalf("verify %v", rep["deposits"])
		}
	}
	st := w.run("", "status").want(t, 0)
	pend, _ := st["pending"].([]any)
	if get(st, "deposits", "timestamped") != 0.0 || len(pend) != 2 {
		t.Fatalf("status %v", st)
	}
	for _, d := range pend {
		if get(d, "status") != "indeterminate" {
			t.Fatalf("status pending %v", pend)
		}
	}
}
