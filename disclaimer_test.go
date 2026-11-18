package rechnungsarchiv

import (
	"strings"
	"testing"
)

func TestParseDisclaimer(t *testing.T) {
	d := ParseDisclaimer(DisclaimerMarkdown)
	if !strings.HasPrefix(d.Short, "Rechnungsarchiv ist freie Open-Source-Software.") ||
		!strings.HasSuffix(d.Short, "Ihrem Steuerberater.") {
		t.Fatalf("short disclaimer not found: %q", d.Short)
	}
	if strings.Contains(d.Short, "\n") {
		t.Fatalf("short disclaimer must be one line: %q", d.Short)
	}
	if d.Consent != "Mit dem Anlegen des Archivs bestätigen Sie, diesen Hinweis gelesen zu haben." {
		t.Fatalf("consent sentence: %q", d.Consent)
	}
	crlf := ParseDisclaimer(strings.ReplaceAll(DisclaimerMarkdown, "\n", "\r\n"))
	if crlf.Short != d.Short || crlf.Consent != d.Consent {
		t.Fatal("CRLF input parses differently")
	}
}
