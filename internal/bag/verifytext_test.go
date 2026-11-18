package bag

import (
	"strings"
	"testing"
)

// Workflow simulation W7/W9: VERIFY.txt names the attempt n that has a
// response instead of 1.tsr, gives openssl the chain, says that openssl
// does not check the qualification, and bounds the time by genTime plus
// the stated accuracy (as verify and the Archivprotokoll do).
func TestVerifyTextManualProcedure(t *testing.T) {
	for _, want := range []string{
		"openssl ts -reply -in n.tsr -text\n",
		"openssl ts -verify -digest <Hashwert aus Schritt 3> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem\n",
		"openssl ts -verify -digest <hash from step 3> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem\n",
		"den höchsten Versuch n mit n.tsr\n   und n.chain.pem",
		"the highest n with n.tsr and n.chain.pem",
		"prüft openssl nicht",
		"openssl does not check whether the service\n   was a qualified",
		"spätestens zur Stempelzeit\n(Time stamp) zuzüglich der angegebenen Genauigkeit (Accuracy)",
		"no later than\nits time (Time stamp) plus the stated accuracy (Accuracy)",
	} {
		if !strings.Contains(verifyText, want) {
			t.Errorf("VERIFY.txt lacks %q", want)
		}
	}
	for _, banned := range []string{"1.tsr", "1.chain.pem", "zu seinem Zeitpunkt"} {
		if strings.Contains(verifyText, banned) {
			t.Errorf("VERIFY.txt still contains %q", banned)
		}
	}
}
