//go:build testtsa

package stamp_test

import (
	"bytes"
	"context"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
	"golang.org/x/crypto/ocsp"
)

func verifyArchive(t *testing.T, e *testenv.Env, opts verify.Options) *verify.Report {
	t.Helper()
	if opts.Trust == nil {
		opts.Trust = e.Trust()
	}
	r, err := verify.Archive(context.Background(), e.Dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func one(t *testing.T, r *stamp.Result) stamp.Outcome {
	t.Helper()
	if len(r.Outcomes) != 1 {
		t.Fatalf("outcomes: %s", testenv.JSON(r))
	}
	return r.Outcomes[0]
}

func TestStampAndVerifyQualified(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	s := e.Trust()
	res, err := stamp.Run(context.Background(), e.Archive, e.StampOptions(s))
	if err != nil {
		t.Fatal(err)
	}
	o := one(t, res)
	if !o.Stamped || o.Attempt != 1 || len(res.Warnings) != 0 {
		t.Fatalf("stamp: %s", testenv.JSON(res))
	}
	for _, f := range []string{"1.tsq", "1.tsr", "1.chain.pem"} {
		if _, err := os.Stat(filepath.Join(e.Dir, "0001_"+testenv.Incoming+".nachweis", f)); err != nil {
			t.Fatal(err)
		}
	}
	// Same store: the list was fetched online right after the stamp.
	r := verifyArchive(t, e, verify.Options{Trust: s})
	d := r.Deposits[0]
	if r.Status != verify.StatusTimestamped || d.Qualification != verify.Qualified || d.Proven == nil || d.Proven.At == nil {
		t.Fatalf("report: %s", testenv.JSON(r))
	}
	if !r.ChainOK() || d.Token.TrustList == nil || !d.Token.TrustList.FetchedOnline || len(d.Token.Revocation) != 2 {
		t.Fatalf("details: %s", testenv.JSON(d.Token))
	}
	// Proven time: genTime + 1 s, rounded up to the second.
	if want := d.Token.GenTime.Add(time.Second).Truncate(time.Second); !d.Proven.At.Equal(want) && !d.Proven.At.Equal(want.Add(time.Second)) {
		t.Fatalf("proven %v for genTime %v", d.Proven.At, d.Token.GenTime)
	}

	// Offline from the cache only: the cached list predates genTime.
	off := verifyArchive(t, e, verify.Options{Trust: e.OfflineTrust(filepath.Join(e.Dir, "vertrauenslisten"))})
	if d := off.Deposits[0]; d.Status != verify.StatusTimestamped || d.Qualification != verify.Unconfirmed {
		t.Fatalf("offline: %s", testenv.JSON(d))
	}
	// A list issued after genTime decides offline, too.
	e.PublishLists(time.Now().Add(time.Minute))
	e.Trust().Refresh(context.Background(), testenv.Territory)
	off = verifyArchive(t, e, verify.Options{Trust: e.OfflineTrust(filepath.Join(e.Dir, "vertrauenslisten"))})
	if d := off.Deposits[0]; d.Qualification != verify.Qualified || d.Token.TrustList.FetchedOnline {
		t.Fatalf("newer list: %s", testenv.JSON(d))
	}

	// Nothing left to do.
	if res := e.Stamp(); len(res.Outcomes) != 0 || e.TSA.Requests() != 1 {
		t.Fatalf("second run: %s", testenv.JSON(res))
	}
}

func TestStampFailureReasons(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode testenv.Mode
		want verify.Reason
	}{
		{"offline", testenv.Offline, verify.ReasonOffline},
		{"401", testenv.Denied, verify.ReasonAccess},
		{"402", testenv.NoQuota, verify.ReasonQuota},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := testenv.New(t)
			e.Add(testenv.Incoming, archive.Options{})
			e.Add(testenv.Outgoing, archive.Options{})
			e.SetMode(tc.mode)
			res := e.Stamp()
			if len(res.Outcomes) != 2 {
				t.Fatalf("%s", testenv.JSON(res))
			}
			for _, o := range res.Outcomes {
				if o.Stamped || o.Reason != tc.want {
					t.Fatalf("%s", testenv.JSON(res))
				}
			}
			// The first failure stops the run: one request only, its n.tsq kept.
			if !strings.HasPrefix(res.Outcomes[1].Message, "nicht versucht") {
				t.Fatalf("second Ablage was tried: %s", testenv.JSON(res))
			}
			r := verifyArchive(t, e, verify.Options{Reasons: res.Reasons()})
			if r.Status != verify.StatusPending || r.Deposits[0].Reason != tc.want {
				t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
			}
			// Without the stamping result the reason is unknown, never invented.
			r = verifyArchive(t, e, verify.Options{})
			if d := r.Deposits[0]; d.Status != verify.StatusPending || d.Reason != "" || d.Attempts[0].Outcome != verify.OutcomeNoResponse {
				t.Fatalf("%s", testenv.JSON(d))
			}

			// A second refused run resends the same request: no new attempt.
			if res = e.Stamp(); res.Outcomes[0].Stamped || res.Outcomes[0].Attempt != 1 {
				t.Fatalf("second failure: %s", testenv.JSON(res))
			}
			e.SetMode(testenv.Normal)
			res = e.Stamp()
			if len(res.Outcomes) != 2 || !res.Outcomes[0].Stamped || res.Outcomes[0].Attempt != 1 || !res.Outcomes[1].Stamped {
				t.Fatalf("retry: %s", testenv.JSON(res))
			}
			// Owner: no clutter. The token answers the request stored first.
			if extra, _ := filepath.Glob(filepath.Join(e.Dir, "*.nachweis", "2.*")); len(extra) != 0 {
				t.Fatalf("new attempt written: %v", extra)
			}
			if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
				t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
			}
		})
	}
}

func TestNoProviderNoCredentials(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	e.SetProvider("")
	if o := one(t, e.Stamp()); o.Reason != verify.ReasonNoProvider {
		t.Fatalf("%+v", o)
	}
	r := verifyArchive(t, e, verify.Options{})
	if d := r.Deposits[0]; d.Status != verify.StatusPending || d.Reason != verify.ReasonNoProvider {
		t.Fatalf("%s", testenv.JSON(d))
	}

	e.SetProvider(e.Provider.ID)
	e.Creds.Delete(e.Archive.Config.ArchiveID, e.Provider.ID)
	if o := one(t, e.Stamp()); o.Reason != verify.ReasonAccess || e.TSA.Requests() != 0 {
		t.Fatalf("%+v", o)
	}
}

func TestReadOnlyNeverStamps(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	ro, err := archive.OpenReadOnly(e.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stamp.Run(context.Background(), ro, e.StampOptions(e.Trust())); err != archive.ErrReadOnly {
		t.Fatalf("got %v", err)
	}
}

func TestMissingMaterialIsRetried(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	e.PKI.OCSPHook = func(*ocsp.Request) []byte { return []byte("not evidence") }
	e.PKI.CRLHook = func(string) []byte { return []byte("not evidence") }

	o := one(t, e.Stamp())
	if o.Stamped || o.Reason != verify.ReasonMaterial || o.Attempt != 1 {
		t.Fatalf("%+v", o)
	}
	r := verifyArchive(t, e, verify.Options{})
	d := r.Deposits[0]
	if d.Status != verify.StatusPending || d.Reason != verify.ReasonMaterial || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
		t.Fatalf("discarded attempt must be Ausstehend·prüfdaten, not Ungültig: %s", testenv.JSON(d))
	}

	// Within 24 h: the same token gets its material, no new stamp is bought.
	e.PKI.OCSPHook, e.PKI.CRLHook = nil, nil
	if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 || e.TSA.Requests() != 1 {
		t.Fatalf("%+v requests=%d", o, e.TSA.Requests())
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}

func TestMaterialWindowExpiredNewAttempt(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	e.PKI.OCSPHook = func(*ocsp.Request) []byte { return []byte("x") }
	e.PKI.CRLHook = func(string) []byte { return []byte("x") }
	one(t, e.Stamp())
	e.PKI.OCSPHook, e.PKI.CRLHook = nil, nil

	// More than 24 h after genTime the attempt is discarded for good.
	opts := e.StampOptions(e.Trust())
	opts.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	res, err := stamp.Run(context.Background(), e.Archive, opts)
	if err != nil {
		t.Fatal(err)
	}
	o := one(t, res)
	if !o.Stamped || o.Attempt != 2 || e.TSA.Requests() != 2 {
		t.Fatalf("%s", testenv.JSON(res))
	}
	// The PC clock now looks 25 h off.
	if o.Warning == "" || len(res.Warnings) != 1 {
		t.Fatalf("no clock warning: %s", testenv.JSON(res))
	}
	r := verifyArchive(t, e, verify.Options{})
	if d := r.Deposits[0]; d.Status != verify.StatusTimestamped || d.Token.Attempt != 2 || d.Attempts[0].Outcome != verify.OutcomeDiscarded {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

func TestUnacceptableTokenNotStored(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	// A token under a policy the provider does not list (spec §5 item 5).
	e.TSA.SetEdit(func(p *tsatest.TokenParams) { p.Policy = asn1.ObjectIdentifier{1, 2, 3, 4} })
	o := one(t, e.Stamp())
	if o.Stamped || o.Reason != verify.ReasonProvider {
		t.Fatalf("%+v", o)
	}
	ev := filepath.Join(e.Dir, "0001_"+testenv.Incoming+".nachweis")
	if _, err := os.Stat(filepath.Join(ev, "1.tsr")); !os.IsNotExist(err) {
		t.Fatal("unaccepted response was stored")
	}
	if r := verifyArchive(t, e, verify.Options{}); r.Status != verify.StatusPending {
		t.Fatalf("%s", testenv.JSON(r.Deposits[0]))
	}
}

func TestChangedAblageIsNeverStamped(t *testing.T) {
	e := testenv.New(t)
	d := e.Add(testenv.Incoming, archive.Options{})
	os.Chmod(d.Path, 0o644)
	b, _ := os.ReadFile(d.Path)
	i := strings.Index(string(b), "%PDF")
	b[i+20] ^= 0xff
	os.WriteFile(d.Path, b, 0o444)
	o := one(t, e.Stamp())
	if o.Stamped || o.Reason != "" || e.TSA.Requests() != 0 {
		t.Fatalf("%+v", o)
	}
}

// Code review 19: a stored request that is not the program's own encoding
// is never resent (a fresh attempt goes out), and a provider marker that
// differs only in its line ending does not block the resend.
func TestResendSafeguards(t *testing.T) {
	nachweis := func(e *testenv.Env) string {
		m, _ := filepath.Glob(filepath.Join(e.Dir, "*.nachweis"))
		if len(m) != 1 {
			t.Fatalf("nachweis folders %v", m)
		}
		return m[0]
	}
	t.Run("altered request", func(t *testing.T) {
		e := testenv.New(t)
		e.Add(testenv.Incoming, archive.Options{})
		e.SetMode(testenv.Offline)
		e.Stamp()
		dir := nachweis(e)
		old, _ := os.ReadFile(filepath.Join(dir, "1.tsq"))
		req, err := tsa.ParseRequest(old)
		if err != nil {
			t.Fatal(err)
		}
		// The same imprint and nonce, plus an extension carrying a file name:
		// it parses and pairs, but must never be sent.
		type imprint struct {
			HashAlgorithm pkix.AlgorithmIdentifier
			HashedMessage []byte
		}
		type tsq struct {
			Version        int
			MessageImprint imprint
			Nonce          *big.Int
			CertReq        bool
			Extensions     []pkix.Extension `asn1:"optional,tag:0"`
		}
		bad, err := asn1.Marshal(tsq{Version: 1,
			MessageImprint: imprint{pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}, Parameters: asn1.NullRawValue}, req.Imprint[:]},
			Nonce:          req.Nonce, CertReq: true,
			Extensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3}, Value: []byte("Rechnung Muster GmbH.pdf")}}})
		if err != nil {
			t.Fatal(err)
		}
		if p, err := tsa.ParseRequest(bad); err != nil || p.Imprint != req.Imprint {
			t.Fatalf("altered request does not pair: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "1.tsq"), bad, 0o644); err != nil {
			t.Fatal(err)
		}
		e.SetMode(testenv.Normal)
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 2 {
			t.Fatalf("%+v", o)
		}
	})
	t.Run("marker with CRLF", func(t *testing.T) {
		e := testenv.New(t)
		e.Add(testenv.Incoming, archive.Options{})
		e.SetMode(testenv.Offline)
		e.Stamp()
		dir := nachweis(e)
		id, err := os.ReadFile(filepath.Join(dir, "1.provider"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "1.provider"), append(bytes.TrimSpace(id), '\r', '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "1.provider")); !bytes.HasSuffix(got, []byte("\r\n")) {
			t.Fatalf("marker not rewritten: %q", got)
		}
		e.SetMode(testenv.Normal)
		if o := one(t, e.Stamp()); !o.Stamped || o.Attempt != 1 {
			t.Fatalf("%+v", o)
		}
	})
}
