//go:build online

package stamp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// TestRecoveryDryRun completes the discarded attempts of a real archive in
// a temporary copy, against the real AIA/OCSP/CRL servers. It never sends a
// timestamp request: it calls complete directly, with no TSA client. Set
// RECHNUNGSARCHIV_RECOVERY_DIR to the archive folder; the folder itself is
// only read.
func TestRecoveryDryRun(t *testing.T) {
	src := os.Getenv("RECHNUNGSARCHIV_RECOVERY_DIR")
	if src == "" {
		t.Skip("RECHNUNGSARCHIV_RECOVERY_DIR not set")
	}
	t.Setenv("APPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv(archive.ConfigDirEnv, t.TempDir())
	dir := filepath.Join(t.TempDir(), "Archiv")
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, err := archive.OpenReadOnly(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := trust.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := a.Deposits()
	if err != nil {
		t.Fatal(err)
	}
	var roots []string
	hints := map[string]string{}
	for _, d := range deps {
		if d.Err == nil {
			roots = append(roots, d.Bag.Root)
			hints[d.Bag.Root] = filepath.Base(d.EvidenceDir)
		}
	}
	ev, err := verify.Evidence(ctx, dir, roots, hints, verify.Options{Trust: ts})
	if err != nil {
		t.Fatal(err)
	}
	s := &stamper{ctx: ctx, now: time.Now, fetcher: &tsa.Fetcher{}, dir: dir} // client nil: no request possible
	for root, atts := range ev {
		for _, at := range atts {
			tok := at.TSAToken()
			t.Logf("%s attempt %d: %s %s", at.Nachweis, at.N, at.Outcome, at.Detail)
			if at.Outcome != verify.OutcomeDiscarded || tok == nil {
				continue
			}
			t.Logf("  genTime %s, in window: %v, signer %s", tok.GenTime.Local(), s.inWindow(tok), tok.Signer.Subject.CommonName)
			o := s.complete(Outcome{Root: root}, filepath.Join(dir, at.Nachweis), at.N, tok)
			t.Logf("  complete: stamped=%v reason=%q %s", o.Stamped, o.Reason, o.Message)
			b, _ := os.ReadFile(filepath.Join(dir, at.Nachweis, verify.ProviderFile(at.N)))
			t.Logf("  provider %s", b)
			if raw, err := storedAttempt(filepath.Join(dir, at.Nachweis), at.N); err == nil && raw.Material != nil {
				for _, c := range raw.Material.Chain {
					t.Logf("  chain.pem: %s (issuer %s)", c.Subject.CommonName, c.Issuer.CommonName)
				}
				for k, e := range raw.Material.Evidence {
					t.Logf("  rev: %s.%s (%d bytes)", k[:12], e.Kind, len(e.Raw))
				}
			}
		}
	}
	r, err := verify.Archive(ctx, dir, verify.Options{Trust: ts, Online: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Deposits {
		t.Logf("verify --online: %04d %s: %s %s %s", d.Sequence, d.Name, d.Status, d.Qualification, d.Reason)
		if d.Proven != nil && d.Proven.At != nil {
			t.Logf("  belegt spätestens %s", d.Proven.At.Local())
		}
	}
}
