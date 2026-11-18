//go:build testtsa

package stamp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
)

// Review 3 finding 4: an unreadable Ablage is a failed outcome (never
// stamped), not just a warning; readable Ablagen are still stamped.
func TestUnreadableDepositIsFailedOutcome(t *testing.T) {
	e := testenv.New(t)
	os.WriteFile(filepath.Join(e.Dir, "0001_broken.zip"), []byte("kein zip"), 0o644)
	r := e.Stamp()
	o := one(t, r)
	if o.Stamped || o.Sequence != 1 || o.Name != "broken" || o.Reason != "" || !strings.Contains(o.Message, "nicht lesbar") {
		t.Fatalf("%s", testenv.JSON(r))
	}
	if _, err := os.Stat(filepath.Join(e.Dir, "0001_broken.nachweis")); !os.IsNotExist(err) {
		t.Fatal("Nachweis folder created for an unreadable Ablage")
	}

	// Next to a readable Ablage: that one is stamped, the broken one fails.
	e2 := testenv.New(t)
	e2.Add(testenv.Incoming, archive.Options{})
	os.WriteFile(filepath.Join(e2.Dir, "0002_broken.zip"), []byte("kein zip"), 0o644)
	r = e2.Stamp()
	if len(r.Outcomes) != 2 || !r.Outcomes[0].Stamped || r.Outcomes[1].Stamped || r.Outcomes[1].Sequence != 2 {
		t.Fatalf("%s", testenv.JSON(r))
	}
}
