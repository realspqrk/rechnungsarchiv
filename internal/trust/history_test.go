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

	"github.com/realspqrk/rechnungsarchiv/internal/trust/trusttest"
)

// caUnder returns a self-signed CA certificate. Go fills in its
// subjectKeyIdentifier, so history instances (X509SKI) match it by the
// extension.
func caUnder(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:              time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.SubjectKeyId) == 0 {
		t.Fatal("CA certificate without subjectKeyIdentifier")
	}
	return c
}

// synthNational publishes services in a synthetic list and verifies it like
// a downloaded one.
func synthNational(t *testing.T, issue time.Time, services ...trusttest.Service) *List {
	t.Helper()
	fx := trusttest.New(t, "ZZ")
	fx.Publish(t, issue, services...)
	lotl, err := verifyLOTL(fx.Web.Get(trusttest.LOTLURL), []*x509.Certificate{fx.LOTLSigner})
	if err != nil {
		t.Fatal(err)
	}
	l, err := verifyNational(fx.Web.Get(fx.NationalURL), lotl, "ZZ")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestQualifyUsesHistoricTypeAndIdentity: type, identity and status of a
// ServiceHistoryInstance belong together. The current QTST type and
// certificates must not be combined with an earlier status.
func TestQualifyUsesHistoricTypeAndIdentity(t *testing.T) {
	day := func(y int) time.Time { return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC) }
	upgraded := caUnder(t, "Upgraded TSA CA") // ordinary TSA until 2023, then QTST
	oldKey := caUnder(t, "Rekeyed TSA CA old")
	newKey := caUnder(t, "Rekeyed TSA CA new") // SDI changed in 2023
	steady := caUnder(t, "Steady TSA CA")      // QTST throughout, withdrawn 2025
	leaf := leafUnder(t, "Steady TSU")         // TSU cert as SDI, no subjectKeyIdentifier

	l := synthNational(t, day(2026),
		trusttest.Service{TSP: "TSP A", Name: "Upgraded", Certs: []*x509.Certificate{upgraded}, History: []trusttest.Status{
			{URI: trusttest.Granted, Start: day(2020), Type: trusttest.TSA},
			{URI: trusttest.Granted, Start: day(2023)},
		}},
		trusttest.Service{TSP: "TSP B", Name: "Rekeyed", Certs: []*x509.Certificate{newKey}, History: []trusttest.Status{
			{URI: trusttest.Granted, Start: day(2020), Certs: []*x509.Certificate{oldKey}},
			{URI: trusttest.Granted, Start: day(2023)},
		}},
		trusttest.Service{TSP: "TSP C", Name: "Steady", Certs: []*x509.Certificate{steady, leaf}, History: []trusttest.Status{
			{URI: trusttest.Granted, Start: day(2020)},
			{URI: trusttest.Withdrawn, Start: day(2025)},
		}},
	)
	lookalike := leafUnder(t, "Steady TSU")
	lookalike.RawSubject = leaf.RawSubject

	tests := []struct {
		name    string
		path    []*x509.Certificate
		genTime time.Time
		want    Result
	}{
		{"ordinary TSA before the upgrade", []*x509.Certificate{upgraded}, day(2021), NotQualified},
		{"QTST after the upgrade", []*x509.Certificate{upgraded}, day(2024), Qualified},
		{"new key before it was the SDI", []*x509.Certificate{newKey}, day(2021), NotQualified},
		{"old key while it was the SDI", []*x509.Certificate{oldKey}, day(2021), Qualified},
		{"old key after the change", []*x509.Certificate{oldKey}, day(2024), NotQualified},
		{"new key after the change", []*x509.Certificate{newKey}, day(2024), Qualified},
		{"history identity by subjectKeyIdentifier", []*x509.Certificate{steady}, day(2021), Qualified},
		{"history identity by key hash", []*x509.Certificate{leaf}, day(2021), Qualified},
		{"withdrawn", []*x509.Certificate{steady}, day(2025), NotQualified},
		{"lookalike of a history identity", []*x509.Certificate{lookalike}, day(2021), Unconfirmed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if d := qualify([]*List{l}, tc.path, tc.genTime, ""); d.Result != tc.want {
				t.Fatalf("got %+v, want %v", d, tc.want)
			}
		})
	}

	// Anchors are the current certificates of every service that was QTST.
	got := anchorCerts([]*List{l})
	if len(got) != 4 {
		t.Fatalf("anchors: %d certificates", len(got))
	}
}

// TestQualifyHistoryWithoutIdentity: a history entry with an empty
// ServiceDigitalIdentity (FR list, Universign 2019–2022) says nothing about
// the certificates. The list cannot decide then: unbestätigt, also with the
// provider hint, never qualifiziert or nicht qualifiziert.
func TestQualifyHistoryWithoutIdentity(t *testing.T) {
	day := func(y int) time.Time { return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC) }
	ca := caUnder(t, "Gap TSA CA")
	l := synthNational(t, day(2026), trusttest.Service{TSP: "TSP", Name: "Gap", Certs: []*x509.Certificate{ca}, History: []trusttest.Status{
		{URI: trusttest.Granted, Start: day(2018)},
		{URI: trusttest.Granted, Start: day(2020), Certs: []*x509.Certificate{}}, // <ServiceDigitalIdentity/>
		{URI: trusttest.Withdrawn, Start: day(2022), Certs: []*x509.Certificate{}},
		{URI: trusttest.Granted, Start: day(2024)},
	}})
	for _, tc := range []struct {
		year int
		want Result
	}{{2019, Qualified}, {2021, Unconfirmed}, {2023, NotQualified}, {2025, Qualified}} {
		for _, hint := range []string{"", "ZZ"} {
			if d := qualify([]*List{l}, []*x509.Certificate{ca}, day(tc.year), hint); d.Result != tc.want {
				t.Errorf("%d (hint %q): got %+v, want %v", tc.year, hint, d, tc.want)
			}
		}
	}
}
