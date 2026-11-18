//go:build testtsa

package export_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/testenv"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func junction(t *testing.T, link, target string) {
	t.Helper()
	if runtime.GOOS != "windows" {
		if err := os.Symlink(target, link); err != nil {
			t.Skip(err)
		}
		return
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
}

// Finding 3: a junction that leads into the source archive is refused,
// and so is a target behind a junction at all.
func TestExportRefusesJunctionIntoArchive(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{})
	before := snapshot(t, e.Dir)

	base := t.TempDir()
	link := filepath.Join(base, "sieht aus wie extern")
	junction(t, link, e.Dir)
	defer os.Remove(link)
	for _, dst := range []string{filepath.Join(link, "export"), link} {
		if _, err := export.Run(context.Background(), e.Dir, dst, options(e, "")); err == nil {
			t.Fatalf("export to %s accepted", dst)
		}
	}
	if after := snapshot(t, e.Dir); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("export wrote into the source archive through a junction")
	}

	// A junction to an empty folder elsewhere is refused as well.
	elsewhere := filepath.Join(base, "leer")
	os.Mkdir(elsewhere, 0o755)
	link2 := filepath.Join(base, "link2")
	junction(t, link2, elsewhere)
	defer os.Remove(link2)
	if _, err := export.Run(context.Background(), e.Dir, filepath.Join(link2, "export"), options(e, "")); err == nil {
		t.Fatal("target behind a junction accepted")
	}
}

// Finding 5: a renamed Nachweis folder is exported with its Ablage.
func TestExportIncludesRenamedNachweis(t *testing.T) {
	e := testenv.New(t)
	e.Add(testenv.Incoming, archive.Options{Belegjahr: 2026})
	if o := e.Stamp().Outcomes; len(o) != 1 || !o[0].Stamped {
		t.Fatalf("%+v", o)
	}
	if err := os.Rename(filepath.Join(e.Dir, "0001_"+testenv.Incoming+".nachweis"), filepath.Join(e.Dir, "umbenannt.nachweis")); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "x")
	res, err := export.Run(context.Background(), e.Dir, dst, options(e, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "umbenannt.nachweis", "1.tsr")); err != nil {
		t.Fatal(err)
	}
	if d := res.Report.Deposits[0]; d.Status != verify.StatusTimestamped {
		t.Fatalf("%s", testenv.JSON(d))
	}
}

// Finding 4: a huge sequence number does not make the export walk every
// integer up to it.
func TestExportExtremeSequence(t *testing.T) {
	e := testenv.New(t)
	d1 := e.Add(testenv.Incoming, archive.Options{Belegjahr: 2026})
	const seq = 1_000_000_000_000
	in, err := bag.Scan(testenv.Demo(testenv.Outgoing))
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%d_Weit weg.zip", seq)
	f, err := os.Create(filepath.Join(e.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	prev := d1.Bag.Root
	_, err = bag.Write(f, "Weit weg", in, bag.Meta{
		Company: "Muster Tischlerei GmbH", BaggingDate: "2026-11-18", ArchiveID: e.Archive.Config.ArchiveID,
		Sequence: seq, Previous: &prev, Belegjahr: 2026, RetentionYears: 7, ArchivedBy: "test@pc",
		ToolVersion: "test", SealNonce: strings.Repeat("ab", 16),
	})
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	var res *export.Result
	go func() {
		var err error
		res, err = export.Run(context.Background(), e.Dir, filepath.Join(t.TempDir(), "x"), options(e, ""))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("export does not finish")
	}
	var gap bool
	for _, f := range res.Report.Findings {
		gap = gap || (f.Code == verify.FindingGap && f.Sequence == 2 && f.To == seq-1)
	}
	if !gap || fmt.Sprint(res.Exported) != fmt.Sprint([]int{1, seq}) {
		t.Fatalf("%s", testenv.JSON(res.Report.Findings))
	}
}
