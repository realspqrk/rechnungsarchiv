package bag

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const demoIn = "../../testdata/demo/2026-10 Eingangsrechnungen"

func testMeta(seq int) Meta {
	return Meta{
		Company: "Alpenblick Tischlerei GmbH", BaggingDate: "2026-11-18",
		ArchiveID: "6f1c1e9a-3b7d-4c1e-9a55-0d7c2b9e4a10", Sequence: seq,
		Belegjahr: 2026, RetentionYears: 7, ArchivedBy: "gerhard@buero-pc",
		ToolVersion: "test", SealNonce: "00112233445566778899aabbccddeeff",
		Previous: previous(seq),
	}
}

func previous(seq int) *string {
	if seq == 1 {
		return nil
	}
	p := "sha256:" + strings.Repeat("ab", 32)
	return &p
}

// copyDir copies a folder tree, keeping modification times.
func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if fi.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return err
		}
		return os.Chtimes(target, fi.ModTime(), fi.ModTime())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, folder, out string, m Meta) *Written {
	t.Helper()
	in, err := Scan(folder)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := Write(f, "2026-10 Eingangsrechnungen", in, m)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// extract unpacks a ZIP with the standard library, which also shows that
// our minimal writer produces ZIPs other readers accept.
func extract(t *testing.T, zipPath, dst string) string {
	t.Helper()
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Method != zip.Store || f.Flags != 0x800 || len(f.Extra) != 0 || f.ModifiedDate != 0x21 || f.ModifiedTime != 0 {
			t.Fatalf("%s: profile violated: method %d flags %x extra %d", f.Name, f.Method, f.Flags, len(f.Extra))
		}
		p := filepath.Join(dst, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc) // also verifies the CRC
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dst, strings.Split(zr.File[0].Name, "/")[0])
}

func mustCheck(t *testing.T, path string) *Result {
	t.Helper()
	r, err := Check(path)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestWriteAndCheckDemo(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "in")
	copyDir(t, demoIn, src)
	zipA := filepath.Join(tmp, "a.zip")
	w := writeZip(t, src, zipA, testMeta(1))

	r := mustCheck(t, zipA)
	if r.Status != OK || len(r.Problems) != 0 {
		t.Fatalf("status %v, problems %v", r.Status, r.Problems)
	}
	if len(r.Matched) != 9 || len(r.Bag.Files) != 9 {
		t.Fatalf("matched %d files, want 9 (8 PDFs + 1 XML)", len(r.Matched))
	}
	if r.Bag.Root != w.Root || !strings.HasPrefix(w.Root, "sha256:") || len(w.Root) != 71 {
		t.Fatalf("root %q vs written %q", r.Bag.Root, w.Root)
	}
	if !slices.Equal(r.Bag.Skipped, []string{"Thumbs.db"}) {
		t.Fatalf("skipped %v", r.Bag.Skipped)
	}
	if r.Bag.Dir != "0001_2026-10 Eingangsrechnungen" || r.Bag.Chain.Previous != nil || r.Bag.Info.RetentionUntil != "2033-12-31" {
		t.Fatalf("metadata: %+v %+v %+v", r.Bag.Dir, r.Bag.Chain, r.Bag.Info)
	}

	// Identical input and nonce give the identical Prüfwert and bytes.
	zipB := filepath.Join(tmp, "b.zip")
	if w2 := writeZip(t, src, zipB, testMeta(1)); w2.Root != w.Root {
		t.Fatalf("Prüfwert not stable: %s vs %s", w.Root, w2.Root)
	}
	a, _ := os.ReadFile(zipA)
	b, _ := os.ReadFile(zipB)
	if !bytes.Equal(a, b) {
		t.Fatal("ZIP bytes differ for identical input")
	}
	// A different nonce changes the Prüfwert.
	m := testMeta(1)
	m.SealNonce = "ffeeddccbbaa99887766554433221100"
	if w3 := writeZip(t, src, filepath.Join(tmp, "c.zip"), m); w3.Root == w.Root {
		t.Fatal("nonce does not affect the Prüfwert")
	}

	// The extracted bag checks the same, also when wrapped in a folder.
	wrap := filepath.Join(tmp, "x")
	bagDir := extract(t, zipA, wrap)
	for _, p := range []string{bagDir, wrap} {
		if r := mustCheck(t, p); r.Status != OK || r.Bag.Root != w.Root {
			t.Fatalf("%s: %v %v", p, r.Status, r.Problems)
		}
	}
}

func TestCheckDetectsChanges(t *testing.T) {
	tmp := t.TempDir()
	zipPath := filepath.Join(tmp, "a.zip")
	writeZip(t, demoIn, zipPath, testMeta(2))
	pdf := "data/2026-10-09 Druckerei Weißenböck 26-311.pdf"

	cases := []struct {
		name   string
		mutate func(bag string)
		want   Status
		check  func(r *Result) bool
	}{
		{"payload tampered", func(bag string) {
			p := filepath.Join(bag, filepath.FromSlash(pdf))
			data, _ := os.ReadFile(p)
			data[len(data)/2] ^= 1
			os.WriteFile(p, data, 0o644)
		}, Changed, func(r *Result) bool { return slices.Equal(r.Changed, []string{pdf}) && len(r.Matched) == 8 }},
		{"file added", func(bag string) {
			os.WriteFile(filepath.Join(bag, "data", "nachgereicht.pdf"), []byte("%PDF-1.4"), 0o644)
		}, Changed, func(r *Result) bool { return slices.Equal(r.Added, []string{"data/nachgereicht.pdf"}) }},
		{"file removed", func(bag string) {
			os.Remove(filepath.Join(bag, filepath.FromSlash(pdf)))
		}, Changed, func(r *Result) bool { return slices.Equal(r.Missing, []string{pdf}) }},
		{"system junk is ignored", func(bag string) {
			os.WriteFile(filepath.Join(bag, "data", "Thumbs.db"), []byte("x"), 0o644)
			os.WriteFile(filepath.Join(bag, "desktop.ini"), []byte("x"), 0o644)
		}, OK, func(r *Result) bool { return len(r.Ignored) == 2 }},
		{"tag file edited", func(bag string) {
			p := filepath.Join(bag, "bag-info.txt")
			data, _ := os.ReadFile(p)
			os.WriteFile(p, bytes.Replace(data, []byte("Bagging-Date: 2026-11-18"), []byte("Bagging-Date: 2026-11-17"), 1), 0o644)
		}, Changed, func(r *Result) bool { return hasProblem(r, 2, "bag-info.txt") }},
		{"extra tag file", func(bag string) {
			os.WriteFile(filepath.Join(bag, "notiz.txt"), []byte("x"), 0o644)
		}, Changed, func(r *Result) bool { return hasProblem(r, 2, "notiz.txt") }},
		{"Payload-Oxum inconsistent but tagmanifest updated", func(bag string) {
			rewriteTag(t, bag, "bag-info.txt", func(s string) string {
				return strings.Replace(s, "Payload-Oxum: ", "Payload-Oxum: 1", 1)
			})
		}, Invalid, func(r *Result) bool { return hasProblem(r, 4, "Payload-Oxum") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bag := extract(t, zipPath, t.TempDir())
			c.mutate(bag)
			r := mustCheck(t, bag)
			if r.Status != c.want || !c.check(r) {
				t.Fatalf("status %v (want %v); problems %v; changed %v added %v missing %v ignored %v",
					r.Status, c.want, r.Problems, r.Changed, r.Added, r.Missing, r.Ignored)
			}
		})
	}
}

// rewriteTag edits a tag file and fixes tagmanifest-sha256.txt, as a
// careful forger would.
func rewriteTag(t *testing.T, bag, name string, edit func(string) string) {
	t.Helper()
	tags := map[string][]byte{}
	for _, n := range tagManifestOrder {
		data, err := os.ReadFile(filepath.Join(bag, n))
		if err != nil {
			t.Fatal(err)
		}
		tags[n] = data
	}
	tags[name] = []byte(edit(string(tags[name])))
	os.WriteFile(filepath.Join(bag, name), tags[name], 0o644)
	os.WriteFile(filepath.Join(bag, fileTagManifest), renderTagManifest(tags), 0o644)
}

func hasProblem(r *Result, step int, substr string) bool {
	for _, p := range r.Problems {
		if p.Step == step && strings.Contains(p.Msg, substr) {
			return true
		}
	}
	return false
}

// rebuild copies a valid Ablage ZIP entry by entry with archive/zip,
// letting mutate change headers or add entries.
func rebuild(t *testing.T, src, dst string, mutate func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool) {
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
	for _, f := range zr.File {
		rc, _ := f.Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		fh := f.FileHeader
		if mutate != nil && !mutate(zw, &fh, data) {
			continue
		}
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

func TestMaliciousZips(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "good.zip")
	writeZip(t, demoIn, good, testMeta(1))
	pdfSuffix := "Werkzeug Großmann RE-90112.pdf"

	// A faithful rebuild by another writer is still accepted.
	same := filepath.Join(tmp, "same.zip")
	rebuild(t, good, same, nil)
	if r := mustCheck(t, same); r.Status != OK {
		t.Fatalf("faithful rebuild rejected: %v", r.Problems)
	}

	addCopy := func(name func(string) string) func(*zip.Writer, *zip.FileHeader, []byte) bool {
		return func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool {
			if strings.HasSuffix(fh.Name, pdfSuffix) {
				dup := *fh
				dup.Name = name(fh.Name)
				w, _ := zw.CreateRaw(&dup)
				w.Write(data)
			}
			return true
		}
	}
	cases := map[string]func(*zip.Writer, *zip.FileHeader, []byte) bool{
		"duplicate after case fold": addCopy(func(n string) string { return strings.Replace(n, "Werkzeug", "WERKZEUG", 1) }),
		"duplicate name":            addCopy(func(n string) string { return n }),
		"dot-dot":                   addCopy(func(n string) string { return strings.Replace(n, "/data/", "/data/../../", 1) }),
		"backslash":                 addCopy(func(n string) string { return strings.Replace(n, "/data/", "/data\\", 1) }),
		"absolute":                  addCopy(func(n string) string { return "/" + n }),
		"outside bag dir":           addCopy(func(n string) string { return "evil.pdf" }),
		"second top dir":            addCopy(func(n string) string { return "0002_x/" + n }),
		"extra field": func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool {
			if strings.HasSuffix(fh.Name, pdfSuffix) {
				fh.Extra = []byte{0x55, 0x54, 5, 0, 1, 0, 0, 0, 0} // extended timestamp
			}
			return true
		},
		"deflate": func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool {
			if strings.HasSuffix(fh.Name, pdfSuffix) {
				var buf bytes.Buffer
				fw, _ := flate.NewWriter(&buf, flate.BestCompression)
				fw.Write(data)
				fw.Close()
				fh.Method, fh.CompressedSize64 = zip.Deflate, uint64(buf.Len())
				w, _ := zw.CreateRaw(fh)
				w.Write(buf.Bytes())
				return false
			}
			return true
		},
		"encrypted flag": func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool {
			if strings.HasSuffix(fh.Name, pdfSuffix) {
				fh.Flags |= 1
			}
			return true
		},
		"real mtime": func(zw *zip.Writer, fh *zip.FileHeader, data []byte) bool {
			fh.Modified = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)
			w, _ := zw.CreateHeader(fh) // also adds a data descriptor
			w.Write(data)
			return false
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(tmp, strings.ReplaceAll(name, " ", "_")+".zip")
			rebuild(t, good, p, mutate)
			r := mustCheck(t, p)
			if r.Status != Invalid || r.Problems[0].Step != 1 {
				t.Fatalf("status %v, problems %v", r.Status, r.Problems)
			}
			t.Log(r.Problems[0].Msg)
		})
	}

	// Truncated and garbage files are Ungültig, not an I/O error.
	data, _ := os.ReadFile(good)
	for name, b := range map[string][]byte{"truncated": data[:len(data)-30], "garbage": []byte("not a zip at all, sorry......")} {
		p := filepath.Join(tmp, name+".zip")
		os.WriteFile(p, b, 0o644)
		if r := mustCheck(t, p); r.Status != Invalid {
			t.Fatalf("%s: %v", name, r.Status)
		}
	}
}

func TestScanRejectsBadInput(t *testing.T) {
	mk := func(t *testing.T, names ...string) string {
		dir := t.TempDir()
		for _, n := range names {
			p := filepath.Join(dir, filepath.FromSlash(n))
			os.MkdirAll(filepath.Dir(p), 0o755)
			if err := os.WriteFile(longPath(p), []byte(n), 0o644); err != nil {
				t.Skipf("cannot create %q here: %v", n, err)
			}
			t.Cleanup(func() { os.Remove(longPath(p)) })
		}
		return dir
	}
	t.Run("NFC collision", func(t *testing.T) {
		dir := mk(t, "Müller.pdf", "Mu\u0308ller.pdf")
		if _, err := Scan(dir); !errors.Is(err, ErrCollision) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("reserved name", func(t *testing.T) {
		dir := mk(t, "ok.pdf", "sub/AUX.pdf")
		if _, err := Scan(dir); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("trailing dot", func(t *testing.T) {
		dir := mk(t, "rechnung.")
		if _, err := Scan(dir); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("only junk", func(t *testing.T) {
		dir := mk(t, "Thumbs.db", "~$Rechnung.docx")
		os.Mkdir(filepath.Join(dir, "leer"), 0o755)
		if _, err := Scan(dir); !errors.Is(err, ErrEmpty) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		dir := mk(t, "a.pdf")
		if err := os.Symlink(filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.pdf")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := Scan(dir); !errors.Is(err, ErrLink) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("changed during write", func(t *testing.T) {
		dir := mk(t, "a.pdf")
		in, err := Scan(dir)
		if err != nil {
			t.Fatal(err)
		}
		os.Chtimes(filepath.Join(dir, "a.pdf"), time.Now(), time.Now().Add(time.Hour))
		f, _ := os.Create(filepath.Join(t.TempDir(), "x.zip"))
		defer f.Close()
		if _, err := Write(f, "x", in, testMeta(1)); !errors.Is(err, ErrChanged) {
			t.Fatalf("got %v", err)
		}
	})
	// Case-only collisions cannot be created on NTFS; check the rule directly.
	if err := newCollisions().add("data/a/Rechnung.pdf", false); err != nil {
		t.Fatal(err)
	}
	c := newCollisions()
	c.add("data/A/x.pdf", false)
	if err := c.add("data/a/RECHNUNG.pdf", false); !errors.Is(err, ErrCollision) {
		t.Fatalf("case collision via directory not detected: %v", err)
	}
	for _, n := range []string{"COM1.txt", "lpt9", "nul", "a:b", "a*b", "x ", "tab\tname", "CON .pdf"} {
		if ValidateComponent(n) == "" {
			t.Errorf("%q accepted", n)
		}
	}
	for _, n := range []string{"COM10.pdf", "Rechnung Nr. 1.pdf", "Größe ½.pdf", "~Rechnung.pdf"} {
		if why := ValidateComponent(n); why != "" {
			t.Errorf("%q rejected: %s", n, why)
		}
	}
}

// longPath lets tests create names Windows normally refuses.
func longPath(p string) string {
	if runtime.GOOS != "windows" {
		return p
	}
	// No filepath.Abs: GetFullPathName would strip trailing dots and spaces.
	return `\\?\` + p
}

func TestCanonicalJSON(t *testing.T) {
	got, err := CanonicalJSON(Obj{"z": nil, "b": []any{int64(1), "ä\n\"\\<>\u2028\x01"}, "a": Obj{}})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"a\":{},\"b\":[1,\"ä\\n\\\"\\\\<>\u2028\\u0001\"],\"z\":null}"
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// More than 65534 entries need the zip64 end records (and nothing else).
func TestZip64EndRecords(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "many.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := newZipWriter(f)
	const n = max16 + 2
	for i := 0; i < n; i++ {
		if err := zw.add("0001_x/data/"+strconv.Itoa(i), 1, strings.NewReader("x"), io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.close(); err != nil {
		t.Fatal(err)
	}
	fi, _ := f.Stat()
	entries, err := parseZip(f, fi.Size())
	if err != nil || len(entries) != n {
		t.Fatalf("parse: %v, %d entries", err, len(entries))
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil || len(zr.File) != n {
		t.Fatalf("archive/zip: %v", err)
	}
}
