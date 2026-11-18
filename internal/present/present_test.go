package present

import (
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func TestTimeZones(t *testing.T) {
	winter := time.Date(2026, 11, 5, 8, 14, 22, 0, time.UTC)
	summer := time.Date(2026, 7, 5, 8, 14, 22, 0, time.UTC)
	if got := TimeFull(winter); got != "05.11.2026, 09:14:22 Uhr MEZ (UTC+01:00)" {
		t.Error(got)
	}
	if got := TimeFull(summer); got != "05.07.2026, 10:14:22 Uhr MESZ (UTC+02:00)" {
		t.Error(got)
	}
}

func TestLabels(t *testing.T) {
	at := time.Date(2026, 11, 5, 8, 14, 23, 0, time.UTC)
	for _, tc := range []struct{ got, want string }{
		{Label(verify.Deposit{Status: verify.StatusTimestamped, Qualification: verify.Qualified}), "Zeitgestempelt · qualifiziert"},
		{Label(verify.Deposit{Status: verify.StatusPending, Reason: verify.ReasonOffline}), "Ausstehend · offline"},
		{Label(verify.Deposit{Status: verify.StatusChanged}), "Verändert"},
		{Proven(&verify.Proven{At: &at, Via: 12}), "spätestens 05.11.2026, 09:14:23 Uhr MEZ (über Ablage 0012)"},
		{Proven(&verify.Proven{GenTime: at}), "Zeitstempel vom 05.11.2026, 09:14:23 Uhr MEZ (Genauigkeit nicht angegeben)"},
		{Size(32925696), "31,4 MB"},
		{Finding(verify.Finding{Code: verify.FindingGap, Sequence: 2, To: 4}), "Lücke: Ablagen 0002 bis 0004 fehlen"},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}
