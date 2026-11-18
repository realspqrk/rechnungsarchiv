//go:build online

package trust

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Release use (see the trusted-list playbook):
//
//	go test -tags online ./internal/trust -run TestOnline -update-bootstrap
var updateBootstrap = flag.Bool("update-bootstrap", false, "rewrite bootstrap/ from the lists fetched online")

// bootstrapTerritories are the countries of the providers in providers.json.
var bootstrapTerritories = []string{"AT", "ES", "SK", "IT", "FR"}

func TestOnlineRefresh(t *testing.T) {
	dir := t.TempDir()
	s, err := open(config{writeDir: dir, readDirs: []string{dir}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	start := time.Now()
	if err := s.Refresh(ctx, bootstrapTerritories...); err != nil {
		t.Fatal(err)
	}
	t.Logf("refresh took %s", time.Since(start).Round(time.Millisecond))

	for _, l := range s.Lists() {
		if _, online := l.FetchedOnline(); !online {
			t.Errorf("%s not marked as fetched online", l.Territory)
		}
		t.Logf("%s-%d issued %s, next update %s, %d QTST services, %d bytes",
			l.Territory, l.Sequence, l.IssueDate.Format(time.RFC3339), l.NextUpdate.Format(time.RFC3339), len(l.Services), len(l.Raw))
	}
	if got := len(s.Lists()); got != 1+len(bootstrapTerritories) {
		t.Fatalf("%d lists, want %d", got, 1+len(bootstrapTerritories))
	}

	now := time.Now().Add(-time.Hour)
	mensatek := onlineService(t, s.list("ES"), "MENSATEK QUALIFIED TSU01 - 2026")
	if cur := mensatek.History[len(mensatek.History)-1]; cur.Status != StatusGranted || cur.Start.Format("2006-01-02") != "2026-11-04" {
		t.Errorf("Mensatek current status %s since %s, want granted since 2026-11-04", cur.Status, cur.Start)
	}
	if d := s.Qualify(mensatek.Certs, now); d.Result != Qualified || d.Territory != "ES" || !d.FetchedOnline {
		t.Errorf("Mensatek: %+v", d)
	}
	disig := onlineService(t, s.list("SK"), "(198) CA Disig Qualified TSA 1")
	if d := s.Qualify(disig.Certs, now); d.Result != Qualified || d.Territory != "SK" {
		t.Errorf("Disig: %+v", d)
	}

	checkPins(t, s.Lists()[0])

	if *updateBootstrap {
		writeBootstrap(t, s.Lists())
	}
}

func onlineService(t *testing.T, l *List, nameContains string) Service {
	t.Helper()
	for _, s := range l.Services {
		if strings.Contains(s.Name, nameContains) && len(s.Certs) > 0 {
			return s
		}
	}
	t.Fatalf("%s: no service %q", l.Territory, nameContains)
	return Service{}
}

// checkPins compares the live LOTL's self-pointer (the certificates the
// Commission announces for signing the LOTL) with the pinned set. A mismatch
// means the release pins need updating before the Commission switches keys.
func checkPins(t *testing.T, lotl *List) {
	t.Helper()
	self, ok := lotl.pointer(territoryLOTL)
	if !ok {
		t.Fatal("LOTL has no self-pointer")
	}
	pinned := func(c *x509.Certificate) bool {
		for _, p := range lotlPins {
			if p.Equal(c) {
				return true
			}
		}
		return false
	}
	for _, c := range self.Certs {
		if !pinned(c) {
			t.Errorf("LOTL announces an unpinned signer: %s (sha256 %x); update internal/trust/pins after checking the OJ",
				c.Subject, sha256Of(c))
		}
	}
	if len(self.Certs) != len(lotlPins) {
		t.Errorf("LOTL announces %d signers, %d pinned", len(self.Certs), len(lotlPins))
	}
}

func sha256Of(c *x509.Certificate) [32]byte { return sha256.Sum256(c.Raw) }

func writeBootstrap(t *testing.T, lists []*List) {
	t.Helper()
	old, _ := filepath.Glob(filepath.Join("bootstrap", "*.xml.gz"))
	for _, p := range old {
		os.Remove(p)
	}
	total := 0
	for _, l := range lists {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		zw.Write(l.Raw)
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		name := filepath.Join("bootstrap", fmt.Sprintf("%s-%d.xml.gz", l.Territory, l.Sequence))
		if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		total += buf.Len()
		t.Logf("wrote %s (%d bytes)", name, buf.Len())
	}
	t.Logf("bootstrap total %d bytes", total)
}
