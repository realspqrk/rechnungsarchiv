package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

// leafUnder returns a fake TSU certificate; Qualify only matches identities,
// path validation is the caller's job (internal/verify).
func leafUnder(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestQualify(t *testing.T) {
	at := fixtureNational(t, fixtureAT, "AT") // issued 2026-06-15T14:44:12Z
	sk := fixtureNational(t, fixtureSK, "SK") // issued 2026-09-18T12:00:00Z
	lists := []*List{at, sk}

	disigTSU := certsOf(t, sk, "(78) TSA Disig TSU 1")[0]           // TSU cert as SDI, granted since 2017-10-26
	disigNew := certsOf(t, sk, "(198) CA Disig Qualified TSA 1")[0] // granted since 2026-08-11
	viasec := certsOf(t, sk, "(166) PSCA TSA1 2023")[0]             // granted 2023-10-19, withdrawn 2025-06-25
	globalCA := certsOf(t, at, "GLOBALTRUST QUALIFIED TIMESTAMP 2")[0]
	if !globalCA.IsCA || disigTSU.IsCA {
		t.Fatal("fixture assumptions about SDI kinds broken")
	}
	leaf := leafUnder(t, "GLOBALTRUST TSU fake")

	tests := []struct {
		name      string
		path      []*x509.Certificate
		genTime   string
		want      Result
		territory string
	}{
		{"TSU cert as SDI, granted, list issued after", []*x509.Certificate{disigTSU}, "2026-01-01T00:00:00Z", Qualified, "SK"},
		{"CA as SDI, granted, list issued after", []*x509.Certificate{leaf, globalCA}, "2026-01-01T00:00:00Z", Qualified, "AT"},
		{"withdrawn after genTime", []*x509.Certificate{viasec}, "2024-06-01T00:00:00Z", Qualified, "SK"},
		{"withdrawn before genTime", []*x509.Certificate{viasec}, "2025-08-01T00:00:00Z", NotQualified, "SK"},
		{"token before the grant date", []*x509.Certificate{disigNew}, "2026-08-01T00:00:00Z", NotQualified, "SK"},
		{"list issued before genTime, not fetched", []*x509.Certificate{disigNew}, "2026-10-01T00:00:00Z", Unconfirmed, "SK"},
		{"list issued exactly at genTime", []*x509.Certificate{disigTSU}, "2026-09-18T12:00:00Z", Unconfirmed, "SK"},
		{"unknown signer", []*x509.Certificate{leaf}, "2026-01-01T00:00:00Z", Unconfirmed, ""},
		{"empty path", nil, "2026-01-01T00:00:00Z", Unconfirmed, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := qualify(lists, tc.path, mustTime(t, tc.genTime))
			if d.Result != tc.want || d.Territory != tc.territory {
				t.Fatalf("got %v from %q, want %v from %q", d.Result, d.Territory, tc.want, tc.territory)
			}
			if d.Territory == "SK" && (d.Sequence != 146 || !d.IssueDate.Equal(sk.IssueDate)) {
				t.Errorf("decision names list %d/%v", d.Sequence, d.IssueDate)
			}
			if tc.want != Unconfirmed && d.Service == "" {
				t.Error("decision names no service")
			}
		})
	}
}

func TestQualifyOnlineFetchedList(t *testing.T) {
	sk := fixtureNational(t, fixtureSK, "SK")
	disigNew := certsOf(t, sk, "(198) CA Disig Qualified TSA 1")[0]
	genTime := mustTime(t, "2026-11-10T08:00:00Z")

	// Fetched online by this run after genTime: the list's word counts.
	sk.fetchedOnline = mustTime(t, "2026-11-18T09:00:00Z")
	d := qualify([]*List{sk}, []*x509.Certificate{disigNew}, genTime)
	if d.Result != Qualified || !d.FetchedOnline {
		t.Fatalf("got %+v, want qualifiziert via online list", d)
	}

	// Fetched before genTime: no evidence about genTime.
	sk.fetchedOnline = mustTime(t, "2026-11-01T00:00:00Z")
	if d := qualify([]*List{sk}, []*x509.Certificate{disigNew}, genTime); d.Result != Unconfirmed {
		t.Fatalf("got %v, want unbestätigt", d.Result)
	}
}

func TestQualifyRejectsLookalikeCertificate(t *testing.T) {
	sk := fixtureNational(t, fixtureSK, "SK")
	real := certsOf(t, sk, "(78) TSA Disig TSU 1")[0]
	// Same subject name, different key: must not match the SDI.
	fake := leafUnder(t, real.Subject.CommonName)
	fake.RawSubject = real.RawSubject
	if d := qualify([]*List{sk}, []*x509.Certificate{fake}, mustTime(t, "2026-01-01T00:00:00Z")); d.Result != Unconfirmed {
		t.Fatalf("lookalike certificate got %v", d.Result)
	}
}

func TestResultString(t *testing.T) {
	for r, want := range map[Result]string{Qualified: "qualifiziert", NotQualified: "nicht qualifiziert", Unconfirmed: "unbestätigt"} {
		if r.String() != want {
			t.Errorf("%d: %q", r, r.String())
		}
	}
}
