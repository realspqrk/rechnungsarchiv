//go:build testtsa

package verify_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/x509"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/trust/trusttest"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func run(t *testing.T, e *testenv.Env, opts verify.Options) *verify.Report {
	t.Helper()
	if opts.Trust == nil {
		opts.Trust = e.Trust()
	}
	r, err := verify.Path(context.Background(), e.Dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// stamped returns an environment with n stamped Ablagen.
func stamped(t *testing.T, n int) *testenv.Env {
	t.Helper()
	e := testenv.New(t)
	for i := 0; i < n; i++ {
		e.Add(testenv.Incoming, archive.Options{Name: []string{"A", "B", "C", "D"}[i], Belegjahr: 2026})
	}
	for _, o := range e.Stamp().Outcomes {
		if !o.Stamped {
			t.Fatalf("not stamped: %+v", o)
		}
	}
	return e
}

func zipPath(e *testenv.Env, seq int, name string) string {
	return filepath.Join(e.Dir, "000"+string(rune('0'+seq))+"_"+name+".zip")
}

func writable(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTamperedPayloadIsChanged(t *testing.T) {
	e := stamped(t, 1)
	p := zipPath(e, 1, "A")
	writable(t, p)
	b, _ := os.ReadFile(p)
	i := bytes.Index(b, []byte("%PDF"))
	if i < 0 {
		t.Fatal("no PDF in the ZIP")
	}
	b[i+10] ^= 0xff
	os.WriteFile(p, b, 0o644)

	r := run(t, e, verify.Options{})
	d := r.Deposits[0]
	if r.Status != verify.StatusChanged || d.Status != verify.StatusChanged || d.Counts.Changed != 1 || d.Proven != nil || d.Qualification != "" {
		t.Fatalf("%s", testenv.JSON(d))
	}
	changed := 0
	for _, f := range d.Files {
		if f.State == verify.FileChanged {
			changed++
		}
	}
	if changed != 1 || len(d.Files) != d.Counts.Files {
		t.Fatalf("file states: %s", testenv.JSON(d.Files))
	}
}

func TestTamperedTokenIsInvalid(t *testing.T) {
	e := stamped(t, 1)
	tsr := filepath.Join(e.Dir, "0001_A.nachweis", "1.tsr")
	b, _ := os.ReadFile(tsr)
	b[len(b)-1] ^= 0x01 // inside the signature
	os.Chmod(tsr, 0o644)
	os.WriteFile(tsr, b, 0o644)

	r := run(t, e, verify.Options{})
	if d := r.Deposits[0]; d.Status != verify.StatusInvalid || d.Attempts[0].Outcome != verify.OutcomeInvalid || d.Proven != nil {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestKeyCompromiseIsIndeterminate(t *testing.T) {
	e := stamped(t, 1)
	e.PKI.Revoke(e.PKI.TSU.Cert, tsa.ReasonKeyCompromise)

	// The stored evidence predates the revocation.
	if r := run(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("offline: %s", testenv.JSON(r.Deposits[0]))
	}
	// Online, the current status counts, whatever the dates (spec §6 step 7).
	r := run(t, e, verify.Options{Online: true})
	d := r.Deposits[0]
	if d.Status != verify.StatusIndeterminate || d.Token == nil || !d.Token.Compromised || d.Proven != nil {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestInheritanceFromSuccessor(t *testing.T) {
	e := stamped(t, 2)
	if err := os.RemoveAll(filepath.Join(e.Dir, "0001_A.nachweis")); err != nil {
		t.Fatal(err)
	}
	r := run(t, e, verify.Options{})
	d1, d2 := r.Deposits[0], r.Deposits[1]
	if d1.Status != verify.StatusPending || d1.Proven == nil || d1.Proven.Via != 2 || d1.Token != nil {
		t.Fatalf("%s", testenv.JSON(d1))
	}
	if *d1.Proven.At != *d2.Proven.At || d1.Proven.Qualification != d2.Qualification || d2.Proven.Via != 0 {
		t.Fatalf("inherited %s from %s", testenv.JSON(d1.Proven), testenv.JSON(d2.Proven))
	}
	if r.Status != verify.StatusPending || !r.ChainOK() {
		t.Fatalf("%s", testenv.JSON(r.Findings))
	}
}

func TestGapAndDuplicate(t *testing.T) {
	e := stamped(t, 3)
	p := zipPath(e, 2, "B")

	// Copy Ablage 2 under another name: Nummer doppelt vergeben.
	b, _ := os.ReadFile(p)
	os.WriteFile(filepath.Join(e.Dir, "0002_Kopie.zip"), b, 0o444)
	r := run(t, e, verify.Options{})
	if !hasFinding(r, verify.FindingDuplicateNumber, 2) {
		t.Fatalf("%s", testenv.JSON(r.Findings))
	}

	// Remove both copies of Ablage 2: Lücke.
	for _, f := range []string{p, filepath.Join(e.Dir, "0002_Kopie.zip")} {
		writable(t, f)
		os.Remove(f)
	}
	r = run(t, e, verify.Options{})
	if !hasFinding(r, verify.FindingGap, 2) || len(r.Findings) != 1 || r.Findings[0].To != 2 || r.ChainOK() {
		t.Fatalf("%s", testenv.JSON(r.Findings))
	}
	// Ablage 1 can no longer inherit across the gap, but has its own token.
	if r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r))
	}

	// Remove Ablage 1 as well: the genesis is missing.
	writable(t, zipPath(e, 1, "A"))
	os.Remove(zipPath(e, 1, "A"))
	r = run(t, e, verify.Options{})
	if !hasFinding(r, verify.FindingGap, 1) || r.Findings[0].To != 2 {
		t.Fatalf("%s", testenv.JSON(r.Findings))
	}
}

func hasFinding(r *verify.Report, c verify.FindingCode, seq int) bool {
	for _, f := range r.Findings {
		if f.Code == c && f.Sequence == seq {
			return true
		}
	}
	return false
}

func TestSingleZipAndExtractedBag(t *testing.T) {
	e := stamped(t, 1)
	zp := zipPath(e, 1, "A")
	r, err := verify.Path(context.Background(), zp, verify.Options{Trust: e.Trust()})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mode != verify.ModeDeposit || r.Status != verify.StatusTimestamped || r.Deposits[0].Sequence != 1 {
		t.Fatalf("%s", testenv.JSON(r))
	}

	// Windows "Extract all": 0001_A/0001_A/… next to 0001_A.nachweis, plus junk.
	dst := filepath.Join(t.TempDir(), "0001_A")
	extract(t, zp, dst)
	os.WriteFile(filepath.Join(dst, "0001_A", "data", "Thumbs.db"), []byte("x"), 0o644)
	copyDir(t, filepath.Join(e.Dir, "0001_A.nachweis"), dst+".nachweis")
	r, err = verify.Path(context.Background(), dst, verify.Options{Trust: e.Trust()})
	if err != nil {
		t.Fatal(err)
	}
	if d := r.Deposits[0]; d.Status != verify.StatusTimestamped || d.Counts.Ignored != 1 {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestNoTrustListIsIndeterminate(t *testing.T) {
	e := stamped(t, 1)
	r := run(t, e, verify.Options{Trust: e.OfflineTrust(t.TempDir())})
	if d := r.Deposits[0]; d.Status != verify.StatusIndeterminate || d.Attempts[0].Outcome != verify.OutcomeNoTrust {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func extract(t *testing.T, zp, dst string) {
	t.Helper()
	zr, err := zip.OpenReader(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		p := filepath.Join(dst, filepath.FromSlash(f.Name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	filepath.WalkDir(src, func(p string, de os.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(src, p)
		if de.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, _ := os.ReadFile(p)
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
}

func TestForeignAndReplayedTokens(t *testing.T) {
	e := stamped(t, 2)
	evA := filepath.Join(e.Dir, "0001_A.nachweis")
	evB := filepath.Join(e.Dir, "0002_B.nachweis")
	reqA, _ := os.ReadFile(filepath.Join(evA, "1.tsq"))

	// Replay: Ablage B's response stored as an attempt for Ablage A.
	tsrB, _ := os.ReadFile(filepath.Join(evB, "1.tsr"))
	tsa.WriteRequest(evA, 2, reqA)
	tsa.WriteResponse(evA, 2, tsrB)
	// Injection: a token from a PKI no trusted list names, for A's request.
	other := tsatest.NewPKI(t, tsatest.PKIOptions{})
	foreign := &tsatest.TSA{Signer: other.TSU, Chain: []*x509.Certificate{other.Inter.Cert, other.Root.Cert}, Policy: tsatest.DefaultPolicy}
	tsrF, err := foreign.Respond(reqA)
	if err != nil {
		t.Fatal(err)
	}
	tsa.WriteRequest(evA, 3, reqA)
	tsa.WriteResponse(evA, 3, tsrF)

	r := run(t, e, verify.Options{})
	d := r.Deposits[0]
	if d.Status != verify.StatusTimestamped || d.Token.Attempt != 1 ||
		d.Attempts[1].Outcome != verify.OutcomeInvalid || d.Attempts[2].Outcome != verify.OutcomeInvalid {
		t.Fatalf("%s", testenv.JSON(d))
	}
	// Without the honest attempt the forged ones make the Ablage Ungültig.
	for _, f := range []string{"1.tsq", "1.tsr", "1.chain.pem"} {
		os.Chmod(filepath.Join(evA, f), 0o644)
		os.Remove(filepath.Join(evA, f))
	}
	os.RemoveAll(filepath.Join(evA, "1.rev"))
	if d := run(t, e, verify.Options{}).Deposits[0]; d.Status != verify.StatusInvalid || d.Proven != nil {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestDeclaredAccuracyNeedsKnownProvider(t *testing.T) {
	e := testenv.New(t)
	e.TSA.SetEdit(nil) // tokens without accuracy
	e.Add(testenv.Incoming, archive.Options{Name: "A", Belegjahr: 2026})
	e.Stamp()

	// Archive mode knows the provider: its declared accuracy (1 s) applies.
	d := run(t, e, verify.Options{}).Deposits[0]
	if d.Token.AccuracySource != "provider" || d.Proven.At == nil {
		t.Fatalf("%s", testenv.JSON(d.Token))
	}
	// A single ZIP names no provider: no exact latest time is asserted.
	r, err := verify.Path(context.Background(), zipPath(e, 1, "A"), verify.Options{Trust: e.Trust()})
	if err != nil {
		t.Fatal(err)
	}
	if d := r.Deposits[0]; d.Token.AccuracyMS != nil || d.Proven == nil || d.Proven.At != nil || !d.Proven.GenTime.Equal(d.Token.GenTime) {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestWithdrawnServiceNotQualified(t *testing.T) {
	e := stamped(t, 1)
	// A newer list: the service was withdrawn before the stamp.
	e.Lists.Publish(t, time.Now().Add(time.Minute), trusttest.Service{
		TSP: "Test Trust Services", Name: "Test QTST", Certs: []*x509.Certificate{e.PKI.Root.Cert},
		History: []trusttest.Status{
			{URI: trusttest.Granted, Start: time.Now().AddDate(-1, 0, 0)},
			{URI: trusttest.Withdrawn, Start: time.Now().Add(-time.Hour)},
		},
	})
	r := run(t, e, verify.Options{Online: true})
	d := r.Deposits[0]
	if d.Status != verify.StatusTimestamped || d.Qualification != verify.NotQualified || d.Proven.Qualification != verify.NotQualified {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestGarbageResponseIsInvalid(t *testing.T) {
	e := stamped(t, 1)
	ev := filepath.Join(e.Dir, "0001_A.nachweis")
	tsq, _ := os.ReadFile(filepath.Join(ev, "1.tsq"))
	for _, f := range []string{"1.tsq", "1.tsr", "1.chain.pem"} {
		os.Chmod(filepath.Join(ev, f), 0o644)
		os.Remove(filepath.Join(ev, f))
	}
	os.RemoveAll(filepath.Join(ev, "1.rev"))
	tsa.WriteRequest(ev, 1, tsq)
	tsa.WriteResponse(ev, 1, []byte{0x30, 0x82, 0xff, 0xff, 0x00})
	if d := run(t, e, verify.Options{}).Deposits[0]; d.Status != verify.StatusInvalid {
		t.Fatalf("%s", testenv.JSON(d))
	}
}
