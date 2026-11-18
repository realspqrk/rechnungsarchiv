package report

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func sample(files int) (*verify.Report, *verify.Deposit) {
	gen := time.Date(2026, 11, 5, 8, 14, 22, 0, time.UTC)
	at := gen.Add(time.Second)
	ms := int64(1000)
	prev := "sha256:" + strings.Repeat("ab", 32)
	d := verify.Deposit{
		Sequence: 11, Name: "2026-10 Eingangsrechnungen", File: "0011_2026-10 Eingangsrechnungen.zip",
		Root: "sha256:" + strings.Repeat("0f", 32), Status: verify.StatusTimestamped, Qualification: verify.Qualified,
		ArchiveID: "6f1c0d7e-1111-4222-8333-444455556666", Previous: &prev, Company: "Muster Tischlerei GmbH",
		Belegjahr: 2026, BaggingDate: "2026-11-05", ArchivedBy: "anna@buero-pc", RetentionYears: 7, RetentionUntil: "2033-12-31",
		Token: &verify.Token{
			Attempt: 1, GenTime: gen, AccuracyMS: &ms, AccuracySource: "token", Serial: "4711", Policy: "0.4.0.2023.1.1",
			Signer: "CN=TSU 1,O=ASETEC", SignerSerial: "99", Issuer: "CN=CA,O=ASETEC", TSP: "Mensatek", Qualification: verify.Qualified,
			TrustList:  &verify.TrustList{Territory: "ES", Sequence: 190, IssueDate: gen.Add(48 * time.Hour)},
			Revocation: []verify.Revocation{{Subject: "CN=TSU 1", Kind: "ocsp", Status: "good", ThisUpdate: gen.Add(time.Minute), Source: "stored"}},
		},
		Proven: &verify.Proven{At: &at, GenTime: gen, AccuracyMS: &ms, Qualification: verify.Qualified},
	}
	for i := 0; i < files; i++ {
		d.Files = append(d.Files, verify.File{
			Path: fmt.Sprintf("data/Lieferant %03d/2026-10-%02d Rechnung mit einem recht langen Namen Nr. %d.pdf", i, i%28+1, i),
			Size: int64(1000 + i*1234), MTime: "2026-10-02T08:00:00Z", SHA256: fmt.Sprintf("%064x", i+1), State: verify.FileOK,
		})
		d.Counts.Bytes += int64(1000 + i*1234)
	}
	d.Counts.Files, d.Counts.Matched = files, files
	rep := &verify.Report{Mode: verify.ModeExport, CheckedAt: time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC), Company: d.Company,
		Status: d.Status, Deposits: []verify.Deposit{d}}
	return rep, &rep.Deposits[0]
}

// facts renders the Feststellung as "Label: Wert" lines.
func facts(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r.key + ": " + r.val + "\n")
	}
	return b.String()
}

func TestStatementQualified(t *testing.T) {
	rep, d := sample(64)
	d.Counts.Bytes = 32925696
	want := "Ergebnis: Zeitgestempelt · qualifiziert\n" +
		"Inhalt: 64 Dateien, 31,4 MB\n" +
		"Prüfwert: sha256:" + strings.Repeat("0f", 32) + "\n" +
		"Zeitstempel: Mensatek (Spanien), 05.11.2026, 09:14:22 Uhr MEZ (± 1 s)\n" +
		"Qualifikation: qualifizierter Zeitstempeldienst nach eIDAS zum Zeitpunkt des Zeitstempels (laut EU-Vertrauensliste)\n" +
		"Kontrolle: 18.11.2026: 64 von 64 Dateien stimmen mit dem Dateiverzeichnis überein\n" +
		"Belegt: Dateistand bestand spätestens am 05.11.2026, 09:14:23 Uhr MEZ (Zeitstempel)\n"
	if got := facts(Statement(rep, d)); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// Each status gets a statement that claims no more than verify established.
func TestStatementFollowsStatus(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(d *verify.Deposit)
		want    []string
		mustNot []string
	}{
		{"unconfirmed", func(d *verify.Deposit) {
			d.Token.Qualification, d.Qualification = verify.Unconfirmed, verify.Unconfirmed
		},
			[]string{"Qualifikation: nicht festgestellt", "spätestens am 05.11.2026, 09:14:23 Uhr MEZ"}, []string{"qualifizierter Zeitstempeldienst nach eIDAS zum"}},
		{"not qualified", func(d *verify.Deposit) {
			d.Token.Qualification, d.Qualification = verify.NotQualified, verify.NotQualified
		},
			[]string{"nicht als qualifizierter Zeitstempeldienst nach eIDAS eingetragen"}, nil},
		{"no accuracy", func(d *verify.Deposit) { d.Token.AccuracyMS, d.Proven.At, d.Proven.AccuracyMS = nil, nil, nil },
			[]string{"ohne Angabe der Genauigkeit kein spätester Zeitpunkt", "(nicht angegeben)"}, []string{"spätestens"}},
		{"pending", func(d *verify.Deposit) {
			d.Status, d.Reason, d.Token, d.Proven, d.Qualification = verify.StatusPending, verify.ReasonOffline, nil, nil, ""
		}, []string{"Zeitstempel: kein gültiger (Grund: offline)", "Belegt: kein Zeitpunkt belegt", "Ergebnis: Ausstehend · offline"}, []string{"spätestens", "Mensatek"}},
		{"changed", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Qualification = verify.StatusChanged, nil, ""
			d.Counts.Matched, d.Counts.Changed, d.Counts.Missing = 61, 2, 1
		}, []string{"Inhalt laut Dateiverzeichnis: 64 Dateien", "61 von 64 Dateien stimmen", "2 Dateien verändert, 1 Datei fehlt", "Belegt: kein Zeitpunkt belegt"}, []string{"64 von 64", "spätestens", "Inhalt: "}},
		{"invalid", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Token, d.Qualification = verify.StatusInvalid, nil, nil, ""
		}, []string{"Zeitstempel: besteht die kryptografische Prüfung nicht", "Belegt: kein Zeitpunkt belegt"}, []string{"Kontrolle", "spätestens"}},
		{"key compromise", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Qualification = verify.StatusIndeterminate, nil, ""
			d.Token.Compromised = true
			d.Token.Revocation[0].Status, d.Token.Revocation[0].Reason = "revoked", 1
		}, []string{"widerrufen (Grund: Kompromittierung des Schlüssels)", "nachträgliche Erstellung des Zeitstempels nicht ausgeschlossen", "Belegt: kein Zeitpunkt belegt"}, []string{"spätestens", "qualifizierter Zeitstempeldienst nach"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, d := sample(64)
			tc.edit(d)
			got := facts(Statement(rep, d))
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in\n%s", w, got)
				}
			}
			for _, w := range tc.mustNot {
				if strings.Contains(got, w) {
					t.Errorf("must not contain %q:\n%s", w, got)
				}
			}
		})
	}
}

func TestStatementInherited(t *testing.T) {
	rep, d := sample(3)
	succ := *d
	succ.Sequence = 12
	tok := *d.Token
	succ.Token = &tok
	rep.Deposits = append(rep.Deposits, succ)
	d = &rep.Deposits[0]
	p := *d.Proven
	p.Via = 12
	d.Status, d.Reason, d.Token, d.Qualification, d.Proven = verify.StatusPending, verify.ReasonMaterial, nil, "", &p
	got := facts(Statement(rep, d))
	for _, w := range []string{"Zeitstempel: kein gültiger (Grund: Prüfdaten fehlen)", "Verkettung: Ablage 0012 schließt diesen Prüfwert ein", "Zeitstempel der Ablage 0012: Mensatek (Spanien)", "Belegt: Dateistand bestand spätestens am 05.11.2026, 09:14:23 Uhr MEZ (Zeitstempel der Ablage 0012)"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in\n%s", w, got)
		}
	}
}

func TestPDFCompleteAndDeterministic(t *testing.T) {
	rep, d := sample(150)
	lines, err := Text(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, " ") // wrapped lines break at spaces
	for _, f := range d.Files {
		if !strings.Contains(all, f.SHA256) {
			t.Fatalf("SHA-256 of %s missing", f.Path)
		}
	}
	if !strings.Contains(all, "Lieferant 149/") || !strings.Contains(all, "Seite 1 von ") {
		t.Fatal("file list or page footer incomplete")
	}
	pages := strings.Count(all, "Archivprotokoll Ablage 0011 · erstellt am 18.11.2026, 11:00:00 Uhr MEZ mit Rechnungsarchiv 1.0.0")
	if pages < 3 || !strings.Contains(all, fmt.Sprintf("Seite %d von %d", pages, pages)) {
		t.Fatalf("%d pages, footer wrong", pages)
	}
	for _, banned := range []string{"Gewähr", "Haftung", "haftet", "Haftungsausschluss", "zertifiziert", "BMF-genehmigt", "Siegel"} {
		if strings.Contains(all, banned) {
			t.Fatalf("PDF contains %q", banned)
		}
	}
	for _, want := range []string{"Feststellung", "Angaben", "Dateiverzeichnis (150 Dateien)", "Nr. | Byte | Änderungsdatum | SHA-256 | Datei (unter data/)",
		"gewählte Frist 7 Jahre, gewöhnliches Ende 31.12.2033", "(UTC+01:00)", "nicht gesperrt", "| 1.000 |"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
	// A statement of facts only: no instructions, no explanations.
	for _, banned := range []string{"Unabhängige Kontrolle", "PRUEFANLEITUNG", "rechnungsarchiv.exe", "PowerShell", "sortiert nach"} {
		if strings.Contains(all, banned) {
			t.Errorf("PDF contains %q", banned)
		}
	}
	a, err := PDF(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := PDF(rep, d, Options{Version: "1.0.0"})
	if !bytes.HasPrefix(a, []byte("%PDF-")) || !bytes.Equal(a, b) {
		t.Fatal("PDF not deterministic")
	}
	if FileName(d) != "0011_2026-10 Eingangsrechnungen.archivprotokoll.pdf" {
		t.Fatal(FileName(d))
	}
}

// Finding 9: the details show the signed genTime with its fraction; only
// belegt_spätestens is rounded up to the second.
func TestGenTimeFractionInDetails(t *testing.T) {
	rep, d := sample(1)
	gen := time.Date(2026, 11, 5, 12, 0, 0, 900_000_000, time.UTC)
	at := time.Date(2026, 11, 5, 12, 0, 2, 0, time.UTC)
	d.Token.GenTime, d.Proven.GenTime, d.Proven.At = gen, gen, &at
	lines, err := Text(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, " ")
	for _, want := range []string{"05.11.2026, 13:00:00,9 Uhr MEZ (UTC+01:00)", "spätestens am 05.11.2026, 13:00:02 Uhr MEZ"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
	if s := facts(Statement(rep, d)); !strings.Contains(s, "05.11.2026, 13:00:00,9 Uhr MEZ (± 1 s)") {
		t.Errorf("statement: %s", s)
	}
}

// Workflow simulation W11: text extraction (pdftotext -layout) put values
// before their labels. In the tables, label and value share a baseline, so
// extraction reads each pair on one line, label first.
func TestDetailsAreLabelValueLines(t *testing.T) {
	rep, d := sample(2)
	lines, err := Text(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{
		"Ergebnis: Zeitgestempelt",
		"Archiv-ID: 6f1c0d7e-1111-4222-8333-444455556666",
		"Prüfwert: sha256:0f0f",
		"Aufbewahrung: gewählte Frist 7 Jahre",
		"Anbieter: Mensatek",
		"Seriennummer: 4711",
		"Qualifikation: qualifiziert",
		"Belegt: Dateistand bestand spätestens am 05.11.2026, 09:14:23 Uhr MEZ",
	}
	for _, w := range wantPrefix {
		found := false
		for _, l := range lines {
			found = found || strings.HasPrefix(l, w)
		}
		if !found {
			t.Errorf("no line starting with %q in\n%s", w, strings.Join(lines, "\n"))
		}
	}

	pdftotext, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed; extraction order not checked")
	}
	pdf, err := PDF(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "p.pdf")
	if err := os.WriteFile(file, pdf, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(pdftotext, "-layout", "-enc", "UTF-8", file, "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, w := range []string{
		`Archiv-ID +6f1c0d7e-1111-4222-8333-444455556666`,
		`Seriennummer +4711`,
		`Policy-OID +0\.4\.0\.2023\.1\.1`,
		`Aussteller +CN=CA,O=ASETEC`,
		`Qualifikation +qualifiziert\r?\n`,
		`2 +2\.234 +02\.10\.2026, 10:00:00 Uhr MESZ +0{63}2 +Lieferant 001/`,
	} {
		if !regexp.MustCompile(`(?m)^ *` + w).MatchString(text) {
			t.Errorf("pdftotext -layout lacks a line %q:\n%s", w, text)
		}
	}
}

// Code review 14: the header drew the subtitles on one line each, so a long
// (valid) Ablage or company name ran off the page and hid the Belegjahr.
// Visual review 5: a table continued on the next page without its heading.
func TestLongHeaderWrapsAndTablesContinue(t *testing.T) {
	rep, d := sample(150)
	d.Name = strings.Repeat("Wareneingang ", 18) + "Ende"
	d.Company = strings.Repeat("Muster Tischlerei und Holzhandel ", 6) + "GmbH"
	lines, err := Text(rep, d, Options{Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(lines, " ")
	if !strings.Contains(all, d.Name+"“ · Belegjahr 2026") || !strings.Contains(all, d.Company) {
		t.Fatal("header lost the name, the company or the Belegjahr")
	}
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}, Unit: gopdf.UnitPT})
	if err := pdf.AddTTFFontData("regular", goregular.TTF); err != nil {
		t.Fatal(err)
	}
	pdf.SetFont("regular", "", 11)
	for _, l := range lines[1:6] { // the wrapped header lines
		if w, _ := pdf.MeasureTextWidth(l); w > textW {
			t.Errorf("header line %.0f pt wide (max %.0f): %q", w, textW, l)
		}
	}
	if !strings.Contains(all, "Dateiverzeichnis (150 Dateien) – Fortsetzung Nr. | Byte") {
		t.Error("file table continues without its heading")
	}
}
