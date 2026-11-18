package bag

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tag files whose hashes match (a careful forger fixed the tagmanifest) but
// whose values are not what the v1 writer can produce are Ungültig.
func TestCheckRejectsBadTagValues(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "a.zip")
	writeZip(t, demoIn, zipPath, testMeta(1))
	reMTime := regexp.MustCompile(`"mtime":"[^"]*"`)
	mtime := func(v string) func(string) string {
		return func(s string) string {
			done := false
			return reMTime.ReplaceAllStringFunc(s, func(m string) string {
				if done {
					return m
				}
				done = true
				return `"mtime":"` + v + `"`
			})
		}
	}
	set := func(v string) func(string) string { return func(string) string { return v } }
	replace := func(old, new string) func(string) string {
		return func(s string) string { return strings.Replace(s, old, new, 1) }
	}
	cases := []struct {
		name, file string
		edit       func(string) string
	}{
		{"mtime nonsense", fileFiles, mtime("nonsense")},
		{"mtime with offset", fileFiles, mtime("2026-10-01T08:00:00+01:00")},
		{"mtime with fraction", fileFiles, mtime("2026-10-01T08:00:00.5Z")},
		{"mtime month 13", fileFiles, mtime("2026-13-01T08:00:00Z")},
		{"Bagging-Date 2026-99-99", fileBagInfo, replace("Bagging-Date: 2026-11-18", "Bagging-Date: 2026-99-99")},
		{"Bagging-Date 30 February", fileBagInfo, replace("Bagging-Date: 2026-11-18", "Bagging-Date: 2026-02-30")},
		{"bag-info not UTF-8", fileBagInfo, replace("Alpenblick", "Alpen\xffblick")},
		{"VERIFY.txt CRLF", fileVerify, func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") }},
		{"VERIFY.txt edited", fileVerify, replace("DEUTSCH", "Deutsch")},
		{"skipped traversal", fileSkipped, set("system\t../../invoice.pdf\n")},
		{"skipped not a system file", fileSkipped, set("system\tRechnung.pdf\n")},
		{"skipped absolute", fileSkipped, set("system\t/Thumbs.db\n")},
		{"skipped dot segment", fileSkipped, set("system\t./Thumbs.db\n")},
		{"skipped backslash", fileSkipped, set("system\tsub\\Thumbs.db\n")},
		{"skipped duplicate", fileSkipped, set("system\tThumbs.db\nsystem\tThumbs.db\n")},
		{"skipped not UTF-8", fileSkipped, set("system\t~$\xff.docx\n")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bag := extract(t, zipPath, t.TempDir())
			rewriteTag(t, bag, c.file, c.edit)
			r := mustCheck(t, bag)
			if r.Status != Invalid || !hasProblem(r, 4, c.file) {
				t.Fatalf("status %v, problems %v", r.Status, r.Problems)
			}
		})
	}
	// Legitimate skipped entries still pass.
	bag := extract(t, zipPath, t.TempDir())
	rewriteTag(t, bag, fileSkipped, set("system\tsub/desktop.ini\nsystem\t~$Angebot.docx\n"))
	if r := mustCheck(t, bag); r.Status != OK {
		t.Fatalf("valid skipped.txt rejected: %v", r.Problems)
	}
}

// rebuildOrdered copies a ZIP with archive/zip, writing the entries in the
// order reorder returns (physical and central order agree).
func rebuildOrdered(t *testing.T, src, dst string, reorder func([]*zip.File) []*zip.File) {
	t.Helper()
	zr, err := zip.OpenReader(src)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for _, f := range reorder(append([]*zip.File(nil), zr.File...)) {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		fh := f.FileHeader
		w, err := zw.CreateRaw(&fh)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func move(fs []*zip.File, from, to int) []*zip.File {
	f := fs[from]
	fs = append(fs[:from], fs[from+1:]...)
	return append(fs[:to], append([]*zip.File{f}, fs[to:]...)...)
}

// swapCentral swaps the first two central-directory records of a small
// (non-zip64) ZIP; the local entries stay where they are.
func swapCentral(t *testing.T, data []byte) []byte {
	t.Helper()
	le := binary.LittleEndian
	e := data[len(data)-22:]
	cdOff := int(le.Uint32(e[16:]))
	cd := data[cdOff : len(data)-22]
	recLen := func(p []byte) int {
		return 46 + int(le.Uint16(p[28:])) + int(le.Uint16(p[30:])) + int(le.Uint16(p[32:]))
	}
	n1 := recLen(cd)
	n2 := recLen(cd[n1:])
	var out bytes.Buffer
	out.Write(data[:cdOff])
	out.Write(cd[n1 : n1+n2])
	out.Write(cd[:n1])
	out.Write(cd[n1+n2:])
	out.Write(e)
	return out.Bytes()
}

func TestZipOrderAndComment(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "good.zip")
	writeZip(t, demoIn, good, testMeta(1))
	data, _ := os.ReadFile(good)

	files := map[string][]byte{}
	// Archive comment in the end record.
	withComment := append(append([]byte(nil), data...), "hallo"...)
	binary.LittleEndian.PutUint16(withComment[len(data)-2:], 5)
	files["archive comment"] = withComment
	files["central order differs from physical order"] = swapCentral(t, data)
	// Data in front of the first entry, with consistent offsets.
	var pre bytes.Buffer
	pre.WriteString("VERSTECKT")
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	zw := zip.NewWriter(&pre)
	zw.SetOffset(9)
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		fh := f.FileHeader
		w, _ := zw.CreateRaw(&fh)
		w.Write(b)
	}
	zw.Close()
	files["data before the first entry"] = pre.Bytes()
	for name, b := range files {
		p := filepath.Join(tmp, strings.ReplaceAll(name, " ", "_")+".zip")
		os.WriteFile(p, b, 0o644)
	}

	orders := map[string]func([]*zip.File) []*zip.File{
		"reversed": func(fs []*zip.File) []*zip.File {
			for i, j := 0, len(fs)-1; i < j; i, j = i+1, j-1 {
				fs[i], fs[j] = fs[j], fs[i]
			}
			return fs
		},
		"bagit.txt not first":       func(fs []*zip.File) []*zip.File { return move(fs, 0, 1) },
		"tagmanifest not last":      func(fs []*zip.File) []*zip.File { return move(fs, len(fs)-1, len(fs)-2) },
		"payload after a tag file":  func(fs []*zip.File) []*zip.File { return move(fs, 1, len(fs)-2) },
		"tag file inside payload":   func(fs []*zip.File) []*zip.File { return move(fs, len(fs)-2, 2) },
		"faithful order (accepted)": func(fs []*zip.File) []*zip.File { return fs },
	}
	for name, reorder := range orders {
		rebuildOrdered(t, good, filepath.Join(tmp, strings.ReplaceAll(name, " ", "_")+".zip"), reorder)
		files[name] = nil
	}
	for name := range files {
		t.Run(name, func(t *testing.T) {
			r := mustCheck(t, filepath.Join(tmp, strings.ReplaceAll(name, " ", "_")+".zip"))
			if strings.HasSuffix(name, "(accepted)") {
				if r.Status != OK {
					t.Fatalf("rejected: %v", r.Problems)
				}
				return
			}
			if r.Status != Invalid || r.Problems[0].Step != 1 {
				t.Fatalf("status %v, problems %v", r.Status, r.Problems)
			}
			t.Log(r.Problems[0].Msg)
		})
	}
}

func TestTotalSizeBound(t *testing.T) {
	var total int64
	if !addBounded(&total, 40, 100) || !addBounded(&total, 60, 100) || total != 100 {
		t.Fatalf("within bound: %d", total)
	}
	if addBounded(&total, 1, 100) || addBounded(&total, math.MaxInt64, math.MaxInt64) || addBounded(&total, -1, 100) {
		t.Fatal("bound or overflow not detected")
	}

	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "a.zip")
	writeZip(t, demoIn, zipPath, testMeta(1))
	dir := extract(t, zipPath, filepath.Join(tmp, "x"))

	old := totalLimit
	totalLimit = 1000 // the demo payload is larger
	t.Cleanup(func() { totalLimit = old })
	for _, p := range []string{zipPath, dir} {
		if r := mustCheck(t, p); r.Status != Invalid || !strings.Contains(r.Problems[0].Msg, "zu groß") {
			t.Fatalf("%s: status %v, problems %v", p, r.Status, r.Problems)
		}
	}
	if _, err := Scan(demoIn); err == nil || !strings.Contains(err.Error(), "zu groß") {
		t.Fatalf("Scan: %v", err)
	}
	totalLimit = old

	// A ZIP file larger than any valid Ablage is refused before parsing.
	if _, err := parseZip(zeroReader{}, maxZipBytes()+1); err == nil || !strings.Contains(err.Error(), "zu groß") {
		t.Fatalf("huge ZIP: %v", err)
	}
}

type zeroReader struct{}

func (zeroReader) ReadAt(p []byte, off int64) (int, error) {
	clear(p)
	return len(p), nil
}
