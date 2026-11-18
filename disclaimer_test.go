package rechnungsarchiv

import (
	"strings"
	"testing"
)

func TestParseDisclaimer(t *testing.T) {
	d := ParseDisclaimer(DisclaimerMarkdown)
	if !strings.HasPrefix(d.Short, "Rechnungsarchiv legt Dateien als unveränderliche Ablagen") ||
		!strings.HasSuffix(d.Short, "Rechtsberatung.") {
		t.Fatalf("short disclaimer not found: %q", d.Short)
	}
	if strings.Contains(d.Short, "\n") {
		t.Fatalf("short disclaimer must be one line: %q", d.Short)
	}
	if d.Consent != "Mit dem Anlegen des Archivs bestätigen Sie, diesen Hinweis gelesen zu haben." {
		t.Fatalf("consent sentence: %q", d.Consent)
	}
	// Visual review 5: readers never see the maintainers' note.
	if strings.Contains(d.Markdown, "<!--") || strings.Contains(d.Markdown, "consent sentence") ||
		!strings.Contains(d.Markdown, "## Kurzfassung\n\n"+d.Short) {
		t.Fatalf("markdown for readers: %q", d.Markdown)
	}
	crlf := ParseDisclaimer(strings.ReplaceAll(DisclaimerMarkdown, "\n", "\r\n"))
	if crlf.Short != d.Short || crlf.Consent != d.Consent {
		t.Fatal("CRLF input parses differently")
	}
}
