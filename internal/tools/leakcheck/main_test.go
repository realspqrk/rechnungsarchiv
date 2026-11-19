package main

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The samples are assembled at run time, so this file itself stays clean.
func TestRulesCatchLeaksAndKeepPublicData(t *testing.T) {
	r := strings.Repeat
	leaks := []struct{ id, sample string }{
		{"private-key", "-----BEGIN " + "RSA PRIVATE KEY-----"},
		{"github-token", "ghp_" + r("a1B2", 9)},
		{"aws-key", "AKIA" + r("Q", 16)},
		{"aws-key", "aws_session_" + "token = " + r("FwoGZXIvYXdzE", 3)},
		{"azure-key", `"Account` + `Key": "` + r("k9Z+", 10) + `"`},
		{"api-key", "sk-" + "ant-" + r("x9", 20)},
		{"user-path", `C:` + `\Users\` + "mmuster" + `\Documents`},
		{"email", "max.muster" + "@" + "gmail.com"},
		{"phone", "+43 " + "664 123 4567"},
		{"password", `password = "` + "Sommer2026!" + `"`},
		{"password", `{"client_` + `secret": "` + "q7Rk2mXv9Lp4" + `"}`},
	}
	for _, l := range leaks {
		// As written, and as Windows PowerShell 5.1 `>` writes it (UTF-16LE, BOM).
		for _, data := range [][]byte{[]byte("x " + l.sample + " y"), append([]byte{0xFF, 0xFE}, utf16le("x "+l.sample+" y")...)} {
			got := scanBlob([]string{"docs/notes.md"}, "worktree", data, nil)
			if len(got) != 1 || got[0].rule != l.id {
				t.Errorf("%s: %q (%d bytes) gave %+v", l.id, l.sample, len(data), got)
				continue
			}
			if strings.Contains(got[0].excerpt, l.sample) && len(l.sample) > 12 {
				t.Errorf("%s: excerpt not redacted: %q", l.id, got[0].excerpt)
			}
		}
	}

	fine := []struct{ path, text string }{
		{"README.md", "office" + "@spqrk.net, " + `C:` + `\Users\Public`},
		{"testdata/trust/AT-75.xml", "info" + "@a-trust.at +43 1 " + "234 5678"},
		{"internal/tsa/client_test.go", `Password = "` + "geheim-und-lang" + `"`},
		{"docs/x.md", "Co-Authored-By: Claude <noreply" + "@anthropic.com>"},
		{"docs/media.md", "SM-05-focus" + "@3x.png, logo" + "@1.5x.webp"},
	}
	for _, f := range fine {
		if got := scanBlob([]string{f.path}, "worktree", []byte(f.text), nil); len(got) != 0 {
			t.Errorf("%s: false positive %+v", f.path, got)
		}
	}

	for _, name := range []string{"signing/metadata.json", ".env", "certs/zugang.p12", "keys/id_ed25519", ".claude/settings.local.json"} {
		if got := scanBlob([]string{name}, "worktree", []byte("{}"), nil); len(got) != 1 || got[0].rule != "file-name" {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got := scanBlob([]string{"internal/trust/pins/lotl-1.pem"}, "worktree", []byte("CERTIFICATE"), nil); len(got) != 0 {
		t.Errorf("public pin flagged: %+v", got)
	}

	// The private list matches anywhere, ignoring case.
	if got := scanBlob([]string{"docs/x.md"}, "history 0123456789", []byte("Konto MUSTER-TSA-7"), needleSet([]string{"muster-tsa-7"})); len(got) != 1 || got[0].rule != "private" {
		t.Errorf("private string: %+v", got)
	}
}

// One blob, several paths in history: a test exemption holds only if every
// path is a test, the name rule looks at each path.
func TestBlobWithSeveralPaths(t *testing.T) {
	secret := []byte(`Password = "` + "geheim-und-lang" + `"`)
	if got := scanBlob([]string{"internal/tsa/client_test.go", "internal/tsa/client.go"}, "history", secret, nil); len(got) != 1 || got[0].where != "internal/tsa/client.go (history)" {
		t.Errorf("renamed from a test: %+v", got)
	}
	if got := scanBlob([]string{"docs/a.txt", "certs/zugang.p12"}, "history", []byte("x"), nil); len(got) != 1 || got[0].rule != "file-name" {
		t.Errorf("key store under a second name: %+v", got)
	}

	var paths []string
	raw := ":000000 100644 " + strings.Repeat("0", 40) + " " + strings.Repeat("a", 40) + " A\x00a b.txt\x00" +
		"\x00\n:100644 100644 " + strings.Repeat("a", 40) + " " + strings.Repeat("b", 40) + " M\x00:odd\x00"
	rawPaths([]byte(raw), func(sha, path string) { paths = append(paths, sha[:1]+" "+path) })
	if want := []string{"a a b.txt", "a :odd", "b :odd"}; !slices.Equal(paths, want) {
		t.Errorf("rawPaths %q, want %q", paths, want)
	}
}

// The private list as Notepad or PowerShell saves it.
func TestPrivateListEncodings(t *testing.T) {
	text := "# Konten\r\nMuster-TSA\r\n\r\nc:" + `\users\jürgen` + "\r\n"
	want := []string{"Muster-TSA", "c:" + `\users\jürgen`}
	for _, data := range [][]byte{append([]byte{0xEF, 0xBB, 0xBF}, text...), append([]byte{0xFF, 0xFE}, utf16le(text)...)} {
		if got, err := parsePrivate(data); err != nil || !slices.Equal(got, want) {
			t.Errorf("% x…: %q, %v", data[:4], got, err)
		}
	}
	// Windows-1252 (Set-Content in PowerShell 5.1) or damaged UTF-16:
	// refused, not half-read.
	for _, bad := range [][]byte{[]byte("j\xfcrgen\r\n"), append([]byte{0xFF, 0xFE}, 'a', 0, 'b'), append([]byte{0xFF, 0xFE}, utf16le("a")[0], utf16le("a")[1], 0x00, 0xD8), append(utf16le("Muster-TSA\r\n"), 'x')} {
		if _, err := parsePrivate(bad); err == nil {
			t.Errorf("% x accepted", bad)
		}
	}
	// Output never shows a private string, whatever its case.
	if got := redactor(want)("docs/MUSTER-tsa-Konto.md (history)"); strings.Contains(strings.ToLower(got), "muster-tsa") {
		t.Errorf("not redacted: %s", got)
	}
	// A private string in a file name is published too.
	if got := scanBlob([]string{"docs/Muster-TSA-Konto.md"}, "history", []byte("ok"), needleSet(want)); len(got) != 1 || got[0].rule != "private-name" {
		t.Errorf("private file name: %+v", got)
	}
}

// Release files: a trace of this PC inside a ZIP inside the Setup, as UTF-16
// with a non-ASCII name in other case.
func TestArtifactsFindNestedUTF16(t *testing.T) {
	inner := zipOf(t, "lib/app/stub.exe", append([]byte("MZ\x00"), utf16le(`C:`+`\Users\MMüster\build`)...))
	setup := append([]byte("MZ setup stub\x00"), zipOf(t, "Rechnungsarchiv.nupkg", inner)...)
	got := scanArtifact("Setup.exe", setup, needleSet([]string{`c:` + `\users\mmüster`}))
	if !slices.ContainsFunc(got, func(f finding) bool { return strings.HasSuffix(f.where, "!lib/app/stub.exe") }) {
		t.Fatalf("nested hit %+v", got)
	}
	if got := scanArtifact("Setup.exe", setup, needleSet([]string{`c:` + `\users\runneradmin`})); len(got) != 0 {
		t.Fatalf("false hit %+v", got)
	}
	// A damaged package inside a valid Setup is never clean.
	damaged := zipOf(t, "Rechnungsarchiv.nupkg", []byte("PK no archive"))
	if got := scanArtifact("Setup.exe", damaged, nil); len(got) != 1 || got[0].rule != "unreadable" {
		t.Fatalf("damaged nested package %+v", got)
	}

	// A Setup or ZIP whose archive cannot be read is an error, not a pass.
	bad := filepath.Join(t.TempDir(), "X-win-Setup.exe")
	if err := os.WriteFile(bad, []byte("MZ no archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanArtifacts([]string{bad}, nil); err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("unreadable Setup: %v", err)
	}
}

func zipOf(t *testing.T, name string, data []byte) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, err := w.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(data)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
