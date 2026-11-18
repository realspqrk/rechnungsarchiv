//go:build testtsa

package export_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/report"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// snapshot records every file of dir with its size, mode and hash.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			t.Fatal(err)
		}
		fi, _ := de.Info()
		s := fmt.Sprintf("dir %v", fi.Mode())
		if !de.IsDir() {
			b, _ := os.ReadFile(p)
			s = fmt.Sprintf("%v %d %x %v", fi.Mode(), fi.Size(), sha256.Sum256(b), fi.ModTime().UnixNano())
		}
		out[p] = s
		return nil
	})
	return out
}

func options(e *testenv.Env, exe string) export.Options {
	return export.Options{
		From: 2026, To: 2026,
		// The source is read like trust.OpenReadOnly does: refreshed lists go
		// to a cache outside the archive.
		SourceTrust: func(src string) (*trust.Store, error) {
			return e.TrustAt([]string{filepath.Join(src, trust.DirName)}, filepath.Join(e.T.(*testing.T).TempDir(), "cache")), nil
		},
		ExportTrust: func(dst string) (*trust.Store, error) {
			return e.OfflineTrust(filepath.Join(dst, trust.DirName)), nil
		},
		Executable: func() (string, error) { return exe, nil },
		Version:    "1.0.0-test",
	}
}

func TestExportSubsetWithBridge(t *testing.T) {
	e := testenv.New(t)
	// Belegjahre 2025, 2026, 2025, 2026: exporting 2026 bridges Ablage 3.
	for i, y := range []int{2025, 2026, 2025, 2026} {
		e.Add(testenv.Incoming, archive.Options{Name: fmt.Sprintf("Ablage %d", i+1), Belegjahr: y})
	}
	for _, o := range e.Stamp().Outcomes {
		if !o.Stamped {
			t.Fatalf("%+v", o)
		}
	}
	exe := filepath.Join(t.TempDir(), "verifier.exe")
	os.WriteFile(exe, []byte("MZ test verifier"), 0o755)

	before := snapshot(t, e.Dir)
	dst := filepath.Join(t.TempDir(), "Export 2026")
	res, err := export.Run(context.Background(), e.Dir, dst, options(e, exe))
	if err != nil {
		t.Fatal(err)
	}
	if after := snapshot(t, e.Dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatalf("export wrote into the source archive:\nbefore %v\nafter  %v", before, after)
	}
	if fmt.Sprint(res.Exported) != "[2 4]" || fmt.Sprint(res.Bridged) != "[3]" || len(res.Warnings) != 0 {
		t.Fatalf("%s", testenv.JSON(res))
	}

	// Brückendaten: exactly chain.json and tagmanifest-sha256.txt.
	bridge, _ := os.ReadDir(filepath.Join(dst, verify.BridgeDir, "0003"))
	if len(bridge) != 2 {
		t.Fatalf("bridge files: %v", bridge)
	}
	for _, want := range []string{"0002_Ablage 2.zip", "0004_Ablage 4.zip", "0002_Ablage 2.nachweis/1.tsr", export.ExeName,
		export.FileListName, export.GuideName, "Archivprotokolle/0002_Ablage 2.archivprotokoll.pdf", "Archivprotokolle/0004_Ablage 4.archivprotokoll.pdf"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Error(err)
		}
	}
	for _, unwanted := range []string{"0001_Ablage 1.zip", "0003_Ablage 3.zip", "0003_Ablage 3.nachweis", "rechnungsarchiv.json"} {
		if _, err := os.Stat(filepath.Join(dst, unwanted)); err == nil {
			t.Errorf("%s exported", unwanted)
		}
	}
	lists, _ := os.ReadDir(filepath.Join(dst, trust.DirName))
	if len(lists) < 2 {
		t.Fatalf("trusted lists: %v", lists)
	}

	// The export verifies on its own, offline, in subset mode.
	rep := res.Report
	if rep.Mode != verify.ModeExport || rep.Status != verify.StatusTimestamped || !rep.ChainOK() {
		t.Fatalf("%s", testenv.JSON(rep))
	}
	if len(rep.Bridged) != 1 || !rep.Bridged[0].Valid || len(rep.Findings) != 1 || rep.Findings[0].Code != verify.FindingPredecessorNotInExport || rep.Findings[0].Sequence != 2 {
		t.Fatalf("%s %s", testenv.JSON(rep.Bridged), testenv.JSON(rep.Findings))
	}
	again, err := verify.Path(context.Background(), dst, verify.Options{Trust: e.OfflineTrust(filepath.Join(dst, trust.DirName))})
	if err != nil || again.Mode != verify.ModeExport || again.Status != verify.StatusTimestamped || len(again.Bridged) != 1 {
		t.Fatalf("%v %s", err, testenv.JSON(again))
	}

	// A broken bridge is a Lücke, not a silent pass.
	tm := filepath.Join(dst, verify.BridgeDir, "0003", verify.TagManifestFile)
	os.Chmod(tm, 0o644)
	b, _ := os.ReadFile(tm)
	os.WriteFile(tm, bytes.Replace(b, []byte("  chain.json"), []byte("  chain.jsoX"), 1), 0o644)
	broken, _ := verify.Path(context.Background(), dst, verify.Options{Trust: e.OfflineTrust(filepath.Join(dst, trust.DirName))})
	if broken.ChainOK() {
		t.Fatalf("broken bridge accepted: %s", testenv.JSON(broken.Findings))
	}

	// DATEIVERZEICHNIS.csv: BOM, ";" and one row per file.
	raw, _ := os.ReadFile(filepath.Join(dst, export.FileListName))
	if !bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		t.Fatal("CSV without BOM")
	}
	r := csv.NewReader(bytes.NewReader(raw[3:]))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if want := 1 + len(rep.Deposits[0].Files) + len(rep.Deposits[1].Files); len(rows) != want || rows[0][5] != "belegt spätestens" {
		t.Fatalf("%d rows, want %d: %v", len(rows), want, rows[0])
	}
	if !strings.HasPrefix(rows[1][5], "spätestens ") || rows[1][0] != "0002_Ablage 2" || len(rows[1][3]) != 64 {
		t.Fatalf("row %v", rows[1])
	}

	guide, _ := os.ReadFile(filepath.Join(dst, export.GuideName))
	for _, want := range []string{`.\rechnungsarchiv.exe verify '<Export-Ordner>'`, "sha256sum -c manifest-sha256.txt",
		"openssl ts -verify -digest <Prüfwert-hex> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem", "Ablage 0003"} {
		if !strings.Contains(string(guide), want) {
			t.Errorf("PRUEFANLEITUNG.txt lacks %q", want)
		}
	}

	// The Archivprotokoll carries the complete file list.
	d := &rep.Deposits[0]
	lines, err := report.Text(rep, d, report.Options{})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(lines, " ")
	for _, f := range d.Files {
		if !strings.Contains(text, f.SHA256) {
			t.Fatalf("protocol lacks %s", f.Path)
		}
	}
	for _, banned := range []string{"Gewähr", "Haftung"} {
		if strings.Contains(text, banned) {
			t.Fatalf("protocol contains %q", banned)
		}
	}
}

func TestExportRefusesTargetInsideArchive(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	before := snapshot(t, e.Dir)
	if _, err := export.Run(context.Background(), e.Dir, filepath.Join(e.Dir, "export"), options(e, "")); err == nil {
		t.Fatal("export into the archive accepted")
	}
	if after := snapshot(t, e.Dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("source changed")
	}
	dst := t.TempDir()
	os.WriteFile(filepath.Join(dst, "x"), nil, 0o644)
	if _, err := export.Run(context.Background(), e.Dir, dst, options(e, "")); err == nil {
		t.Fatal("non-empty target accepted")
	}
}

func TestExportWithoutExecutableWarns(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	o := options(e, "")
	o.Executable = func() (string, error) { return "", fmt.Errorf("unbekannt") }
	res, err := export.Run(context.Background(), e.Dir, filepath.Join(t.TempDir(), "x"), o)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "Prüfprogramm") {
		t.Fatalf("%v", res.Warnings)
	}
	// Not stamped: the protocol states Ausstehend.
	if d := res.Report.Deposits[0]; d.Status != verify.StatusPending {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

// Without Options.Executable the export bundles the running program, which
// is rechnungsarchiv.exe itself: CLI and GUI are one binary (ADR-0008).
func TestExportBundlesRunningExecutable(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	o := options(e, "")
	o.Executable = nil
	dst := filepath.Join(t.TempDir(), "x")
	res, err := export.Run(context.Background(), e.Dir, dst, o)
	if err != nil || len(res.Warnings) != 0 {
		t.Fatalf("%v %v", err, res.Warnings)
	}
	self, _ := os.Executable()
	want, _ := os.ReadFile(self)
	got, err := os.ReadFile(filepath.Join(dst, export.ExeName))
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("bundled %d bytes, running program %d bytes (%v)", len(got), len(want), err)
	}
}
