package export

import (
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Workflow simulation W7/W8/W10: after a failed first attempt only 1.tsq
// exists, so the manual procedure must not hard-code 1.tsr; openssl needs
// the chain; and "unbestätigt" resolves with an explicit online check.
func TestGuideManualProcedure(t *testing.T) {
	gen := time.Date(2026, 11, 5, 8, 14, 22, 0, time.UTC)
	rep := &verify.Report{Company: "Muster GmbH", ArchiveID: "a-1", CheckedAt: gen.Add(time.Hour), Deposits: []verify.Deposit{{
		Sequence: 1, Name: "2026-10 Eingangsrechnungen", Belegjahr: 2026, Status: verify.StatusTimestamped, Qualification: verify.Qualified,
		Attempts: []verify.Attempt{
			{N: 1, Nachweis: "0001_2026-10 Eingangsrechnungen.nachweis", Outcome: verify.OutcomeNoResponse},
			{N: 2, Nachweis: "0001_2026-10 Eingangsrechnungen.nachweis", Outcome: verify.OutcomeValid, Material: true},
		},
		Token: &verify.Token{Attempt: 2, GenTime: gen},
	}}}
	g := strings.ReplaceAll(string(guide(rep)), "\r\n", "\n")
	for _, want := range []string{
		`Zeitstempel: 0001_2026-10 Eingangsrechnungen.nachweis\2.tsr (Versuch 2)`,
		"den höchsten Versuch n, zu dem n.tsr und n.chain.pem vorliegen",
		"    openssl ts -reply -in n.tsr -text\n",
		"    openssl ts -verify -digest <Prüfwert-hex> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem -partial_chain\n",
		"nicht, ob der Dienst ein qualifizierter",
		`    .\rechnungsarchiv.exe verify '.' --online`,
		"Stempelzeit (Time stamp) zuzüglich der angegebenen Genauigkeit (Accuracy)",
	} {
		if !strings.Contains(g, want) {
			t.Errorf("PRUEFANLEITUNG.txt lacks %q", want)
		}
	}
	for _, banned := range []string{"1.tsr", "1.chain.pem", "mit Internetverbindung läuft"} {
		if strings.Contains(g, banned) {
			t.Errorf("PRUEFANLEITUNG.txt still contains %q", banned)
		}
	}
}
