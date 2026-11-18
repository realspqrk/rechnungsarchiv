package present

import (
	"encoding/base64"
	"encoding/binary"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Finding 2: PSQuote yields a literal that PowerShell never expands. The
// names are run through a real PowerShell where one is available.
func TestPSQuoteIsLiteral(t *testing.T) {
	names := []string{
		"0011_2026-10 Eingangsrechnungen.zip",
		"0011_$(Write-Output EINGESCHLEUST).zip",
		"0012_$env:USERNAME ${x} @(1) `n.zip",
		"0013_O'Brien.zip",
		"0014_Müller’s ‘a’ ‚b‛ \"c\".zip",
		"0015_'; Write-Output EINGESCHLEUST; '.zip",
	}
	for _, n := range names {
		q := PSQuote(n)
		if !strings.HasPrefix(q, "'") || !strings.HasSuffix(q, "'") {
			t.Fatalf("%q -> %q", n, q)
		}
	}
	if got := PSQuote("O'Brien’s"); got != "'O''Brien’’s'" {
		t.Fatal(got)
	}
	if runtime.GOOS != "windows" {
		return
	}
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no PowerShell")
	}
	var script strings.Builder
	script.WriteString("[Console]::OutputEncoding = [Text.Encoding]::UTF8\n")
	for _, n := range names {
		script.WriteString("Write-Output " + PSQuote(n) + "\n")
	}
	// -EncodedCommand (UTF-16LE) keeps the typographic quotes intact; a
	// script on stdin is read in the OEM code page.
	u := utf16.Encode([]rune(script.String()))
	raw := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(raw[2*i:], c)
	}
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(raw))
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("PowerShell: %v", err)
	}
	got := strings.Split(strings.TrimRight(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n"), "\n")
	if len(got) != len(names) {
		t.Fatalf("output %q", out)
	}
	for i, n := range names {
		if got[i] != n {
			t.Errorf("PowerShell turned %q into %q", n, got[i])
		}
	}
}

// Finding 9: Exact keeps the fraction of a second as signed.
func TestExactTime(t *testing.T) {
	for _, tc := range []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 11, 5, 12, 0, 0, 900_000_000, time.UTC), "05.11.2026, 13:00:00,9 Uhr MEZ (UTC+01:00)"},
		{time.Date(2026, 11, 5, 12, 0, 0, 123_456_000, time.UTC), "05.11.2026, 13:00:00,123456 Uhr MEZ (UTC+01:00)"},
		{time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), "05.11.2026, 13:00:00 Uhr MEZ (UTC+01:00)"},
	} {
		if got := ExactFull(tc.t); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	gen := time.Date(2026, 11, 5, 12, 0, 0, 900_000_000, time.UTC)
	if got := Proven(nil); got != "" {
		t.Fatal(got)
	}
	if got := Proven(&verify.Proven{GenTime: gen}); !strings.Contains(got, "13:00:00,9 Uhr MEZ") {
		t.Fatal(got)
	}
}
