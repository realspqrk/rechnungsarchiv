//go:build testtsa

package stamp_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func evDir(e *testenv.Env) string { return filepath.Join(e.Dir, "0001_"+testenv.Incoming+".nachweis") }

func remove(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		os.Chmod(p, 0o644)
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
}

func hits(e *testenv.Env, path string) int {
	return e.PKI.Hits[path]
}

// Finding 6: an intermediate fetched via AIA is stored right after the
// token is accepted, so a failed revocation fetch leaves a resumable
// attempt (Ausstehend·prüfdaten), never an Ungültig one or a second stamp.
func TestAIAChainPersistedBeforeRevocation(t *testing.T) {
	e := testenv.New(t)
	e.TSA.Chain = nil // the token carries only the TSU certificate
	e.Add(testenv.Incoming, archive.Options{})
	e.PKI.CRLHook = func(string) []byte { return []byte("not a CRL") }

	if o := one(t, e.Stamp()); o.Stamped || o.Reason != verify.ReasonMaterial || o.Attempt != 1 {
		t.Fatalf("%+v", o)
	}
	b, err := os.ReadFile(filepath.Join(evDir(e), "1.chain.pem"))
	if err != nil {
		t.Fatal(err)
	}
	certs, _ := tsa.ParsePEM(b)
	if len(certs) < 2 || !certs[1].Equal(e.PKI.Inter.Cert) {
		t.Fatalf("chain.pem lacks the AIA intermediate: %d certs", len(certs))
	}
	d := verifyArchive(t, e, verify.Options{}).Deposits[0]
	if d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
		t.Fatalf("%s", testenv.JSON(d))
	}

	e.PKI.CRLHook = nil
	if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
		t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}

// Finding 6: an attempt whose path material is missing (written by an
// older version, or lost) is not Ungültig; stamping completes it.
func TestMissingPathMaterialResumed(t *testing.T) {
	e := testenv.New(t)
	e.TSA.Chain = nil
	e.Add(testenv.Incoming, archive.Options{})
	e.PKI.CRLHook = func(string) []byte { return []byte("not a CRL") }
	one(t, e.Stamp())
	remove(t, filepath.Join(evDir(e), "1.chain.pem"))
	os.RemoveAll(filepath.Join(evDir(e), "1.rev"))

	d := verifyArchive(t, e, verify.Options{}).Deposits[0]
	if d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
		t.Fatalf("missing intermediate must not be Ungültig: %s", testenv.JSON(d))
	}
	e.PKI.CRLHook = nil
	if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
		t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}

// Finding 7: within 24 h only the missing evidence is fetched and written
// for the same attempt; no second stamp is bought.
func TestPartialMaterialCompleted(t *testing.T) {
	t.Run("intermediate CRL missing", func(t *testing.T) {
		e := testenv.New(t)
		e.Add(testenv.Incoming, archive.Options{})
		one(t, e.Stamp())
		remove(t, filepath.Join(evDir(e), "1.rev", tsa.CertKey(e.PKI.Inter.Cert)+".crl"))
		if d := verifyArchive(t, e, verify.Options{}).Deposits[0]; d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial {
			t.Fatalf("%s", testenv.JSON(d))
		}
		ocspBefore, crlBefore := hits(e, "/ocsp/inter"), hits(e, "/crl/root.crl")
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
			t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
		}
		if hits(e, "/ocsp/inter") != ocspBefore || hits(e, "/crl/root.crl") != crlBefore+1 {
			t.Fatalf("fetched more than the missing piece: ocsp %d→%d crl %d→%d", ocspBefore, hits(e, "/ocsp/inter"), crlBefore, hits(e, "/crl/root.crl"))
		}
		if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
			t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
		}
	})
	t.Run("crash after creating the rev folder", func(t *testing.T) {
		e := testenv.New(t)
		e.Add(testenv.Incoming, archive.Options{})
		one(t, e.Stamp())
		rev := filepath.Join(evDir(e), "1.rev")
		des, _ := os.ReadDir(rev)
		for _, de := range des {
			remove(t, filepath.Join(rev, de.Name()))
		}
		remove(t, filepath.Join(evDir(e), "1.chain.pem"))
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
			t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
		}
		if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
			t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
		}
	})
	t.Run("progress is kept when one piece fails", func(t *testing.T) {
		e := testenv.New(t)
		e.Add(testenv.Incoming, archive.Options{})
		e.PKI.CRLHook = func(string) []byte { return []byte("not a CRL") }
		if o := one(t, e.Stamp()); o.Stamped || o.Reason != verify.ReasonMaterial {
			t.Fatalf("%+v", o)
		}
		if _, err := os.Stat(filepath.Join(evDir(e), "1.rev", tsa.CertKey(e.PKI.TSU.Cert)+".ocsp")); err != nil {
			t.Fatalf("the TSU's OCSP answer was not kept: %v", err)
		}
		e.PKI.CRLHook = nil
		ocspBefore := hits(e, "/ocsp/inter")
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 || hits(e, "/ocsp/inter") != ocspBefore {
			t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
		}
	})
}

// Finding 5: a renamed Nachweis folder still pairs by imprint, so no
// second stamp is bought.
func TestRenamedNachweisNotRestamped(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	one(t, e.Stamp())
	if err := os.Rename(evDir(e), filepath.Join(e.Dir, "umbenannt.nachweis")); err != nil {
		t.Fatal(err)
	}
	if res := e.Stamp(); len(res.Outcomes) != 0 || e.TSA.Requests() != 1 {
		t.Fatalf("%s", testenv.JSON(res))
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}

// Finding 8: the provider is recorded with the request.
func TestProviderRecordedWithRequest(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	one(t, e.Stamp())
	b, err := os.ReadFile(filepath.Join(evDir(e), "1.provider"))
	if err != nil || string(b) != e.Provider.ID {
		t.Fatalf("%q %v", b, err)
	}
}
