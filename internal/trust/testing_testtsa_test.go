//go:build testtsa

package trust

import (
	"context"
	"crypto/x509"
	"path/filepath"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/trust/trusttest"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

// TestSyntheticListsVerify proves the trusttest fixture goes through the real
// signature verification, caching and qualification.
func TestSyntheticListsVerify(t *testing.T) {
	pins := lotlPins
	t.Cleanup(func() { lotlPins = pins }) // OpenTest replaces the pins
	pki := tsatest.NewPKI(t, tsatest.PKIOptions{})
	fx := trusttest.New(t, "ZZ")
	issue := time.Now().Add(-48 * time.Hour)
	fx.Publish(t, issue, trusttest.Service{
		TSP: "Test TSP", Name: "Test QTST", Certs: []*x509.Certificate{pki.Root.Cert},
		History: []trusttest.Status{{URI: trusttest.Granted, Start: issue.AddDate(-1, 0, 0)}},
	})
	dir := filepath.Join(t.TempDir(), DirName)
	s, err := OpenTest(TestConfig{WriteDir: dir, ReadDirs: []string{dir}, Pins: []*x509.Certificate{fx.LOTLSigner}, Client: fx.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(context.Background(), "ZZ"); err != nil {
		t.Fatal(err)
	}
	path := []*x509.Certificate{pki.TSU.Cert, pki.Inter.Cert, pki.Root.Cert}
	genTime := time.Now().Add(-time.Hour)
	if d := s.Qualify(path, genTime, "ZZ"); d.Result != Qualified || !d.FetchedOnline || d.Territory != "ZZ" {
		t.Fatalf("online: %+v", d)
	}
	if a := s.Anchors(); len(a) != 1 || !a[0].Equal(pki.Root.Cert) {
		t.Fatalf("anchors %v", a)
	}

	// From the cache only: the list predates genTime, so it cannot decide.
	cached, err := OpenTest(TestConfig{ReadDirs: []string{dir}, Pins: []*x509.Certificate{fx.LOTLSigner}})
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Problems()) > 0 {
		t.Fatal(cached.Problems())
	}
	if d := cached.Qualify(path, genTime, "ZZ"); d.Result != Unconfirmed {
		t.Fatalf("cached: %+v", d)
	}
	// An unknown signer with a decidable provider list is not qualified.
	other := tsatest.NewPKI(t, tsatest.PKIOptions{})
	if d := s.Qualify([]*x509.Certificate{other.TSU.Cert, other.Inter.Cert, other.Root.Cert}, genTime, "ZZ"); d.Result != NotQualified {
		t.Fatalf("unknown signer: %+v", d)
	}
}
