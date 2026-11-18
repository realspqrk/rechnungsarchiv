//go:build testtsa

package export_test

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
)

// Workflow simulation W7/W8: the manual openssl commands of
// PRUEFANLEITUNG.txt and VERIFY.txt work as written after a failed first
// attempt (only 1.tsq) and a successful second one (2.tsr, 2.chain.pem).
func TestManualOpenSSLCommandsWork(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{Belegjahr: 2026})
	e.SetMode(testenv.Offline)
	if o := e.Stamp().Outcomes; len(o) != 1 || o[0].Stamped {
		t.Fatalf("offline attempt: %+v", o)
	}
	e.SetMode(testenv.Normal)
	if o := e.Stamp().Outcomes; len(o) != 1 || !o[0].Stamped || o[0].Attempt != 2 {
		t.Fatalf("second attempt: %+v", o)
	}
	dst := filepath.Join(t.TempDir(), "Export")
	res, err := export.Run(context.Background(), e.Dir, dst, options(e, ""))
	if err != nil {
		t.Fatal(err)
	}
	d := res.Report.Deposits[0]
	nachweis := filepath.Join(dst, "0001_"+testenv.Incoming+".nachweis")
	if _, err := os.Stat(filepath.Join(nachweis, "1.tsr")); !os.IsNotExist(err) {
		t.Fatalf("attempt 1 has a response: %v", err)
	}

	guideRaw, _ := os.ReadFile(filepath.Join(dst, export.GuideName))
	guide := string(guideRaw)
	if !strings.Contains(guide, `Zeitstempel: 0001_`+testenv.Incoming+`.nachweis\2.tsr (Versuch 2)`) {
		t.Fatalf("PRUEFANLEITUNG.txt does not name attempt 2:\n%s", guide)
	}
	zr, err := zip.OpenReader(filepath.Join(dst, "0001_"+testenv.Incoming+".zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var verifyTxt string
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/VERIFY.txt") {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			verifyTxt = string(b)
		}
	}

	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not installed; commands not run")
	}
	hex := strings.TrimPrefix(d.Root, "sha256:")
	for _, src := range []struct{ name, text, placeholder string }{
		{export.GuideName, guide, "<Prüfwert-hex>"},
		{"VERIFY.txt", verifyTxt, "<Hashwert aus Schritt 3>"},
		{"VERIFY.txt (EN)", verifyTxt, "<hash from step 3>"},
	} {
		var cmds []string
		for _, l := range strings.Split(strings.ReplaceAll(src.text, "\r\n", "\n"), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "openssl ts -") && (!strings.Contains(l, "-digest") || strings.Contains(l, src.placeholder)) {
				cmds = append(cmds, l)
			}
		}
		if len(cmds) < 2 {
			t.Fatalf("%s: commands not found:\n%s", src.name, src.text)
		}
		for _, c := range cmds {
			c = strings.ReplaceAll(c, src.placeholder, hex)
			c = strings.ReplaceAll(c, "n.tsr", "2.tsr")
			c = strings.ReplaceAll(c, "n.chain.pem", "2.chain.pem")
			args := strings.Fields(c)[1:]
			cmd := exec.Command(openssl, args...)
			cmd.Dir = nachweis
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %s: %v\n%s", src.name, c, err, out)
			}
			if strings.Contains(c, "-verify") && !strings.Contains(string(out), "Verification: OK") {
				t.Fatalf("%s: %s:\n%s", src.name, c, out)
			}
		}
	}
}
