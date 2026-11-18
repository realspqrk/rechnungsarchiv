//go:build testtsa

package trust

import (
	"crypto/x509"
	"net/http"
	"time"
)

// TestConfig configures OpenTest.
type TestConfig struct {
	WriteDir string              // where Refresh stores lists
	ReadDirs []string            // caches to load (WriteDir is not added)
	Pins     []*x509.Certificate // LOTL signing certificates to trust
	Client   *http.Client        // serves LOTLURL and the national lists
	Now      func() time.Time
}

// OpenTest opens a store over synthetic, signed trusted lists (see
// trusttest). It exists only in builds with the testtsa tag: it replaces the
// pinned LOTL certificates for the whole test binary and loads no bootstrap
// snapshot. Verification itself is unchanged.
func OpenTest(c TestConfig) (*Store, error) {
	lotlPins = c.Pins
	return open(config{writeDir: c.WriteDir, readDirs: c.ReadDirs, client: c.Client, now: c.Now})
}
