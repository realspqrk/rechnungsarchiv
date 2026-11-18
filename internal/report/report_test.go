package report

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestStatementQualified(t *testing.T) {
	rep, d := sample(64)
	d.Counts.Bytes = 32925696
	want := "Ablage 0011 „2026-10 Eingangsrechnungen“ enthält 64 Dateien (31,4 MB). Ihr Prüfwert ist sha256:" + strings.Repeat("0f", 32) + ". " +
		"Für diesen Prüfwert besteht ein Zeitstempel von Mensatek (Spanien) vom 05.11.2026, 09:14:22 Uhr MEZ (± 1 s); " +
		"der Dienst war laut EU-Vertrauensliste zu diesem Zeitpunkt ein qualifizierter Zeitstempeldienst nach der eIDAS-Verordnung. " +
		"Bei der Kontrolle am 18.11.2026 stimmten alle 64 Dateien im Dateiverzeichnis mit dem Stand überein, den der Zeitstempel spätestens für den 05.11.2026, 09:14:23 Uhr MEZ belegt."
	if got := Statement(rep, d); got != want {
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
			[]string{"nicht festgestellt", "spätestens für den 05.11.2026, 09:14:23 Uhr MEZ"}, []string{"war laut EU-Vertrauensliste zu diesem Zeitpunkt ein"}},
		{"not qualified", func(d *verify.Deposit) {
			d.Token.Qualification, d.Qualification = verify.NotQualified, verify.NotQualified
		},
			[]string{"nicht als qualifizierter Zeitstempeldienst"}, nil},
		{"no accuracy", func(d *verify.Deposit) { d.Token.AccuracyMS, d.Proven.At, d.Proven.AccuracyMS = nil, nil, nil },
			[]string{"Genauigkeit ist nicht angegeben", "(nicht angegeben)"}, []string{"spätestens"}},
		{"pending", func(d *verify.Deposit) {
			d.Status, d.Reason, d.Token, d.Proven, d.Qualification = verify.StatusPending, verify.ReasonOffline, nil, nil, ""
		}, []string{"noch kein gültiger Zeitstempel vor (Grund: offline)", "Einen Zeitpunkt belegt diese Kontrolle nicht."}, []string{"spätestens", "Zeitstempel von"}},
		{"changed", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Qualification = verify.StatusChanged, nil, ""
			d.Counts.Matched, d.Counts.Changed, d.Counts.Missing = 61, 2, 1
		}, []string{"Das Dateiverzeichnis der Ablage 0011", "stimmten 61 von 64 Dateien", "2 Dateien verändert, 1 Datei fehlen", "kein Zeitpunkt belegt"}, []string{"stimmten alle", "spätestens", "enthält"}},
		{"invalid", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Token, d.Qualification = verify.StatusInvalid, nil, nil, ""
		}, []string{"besteht die kryptografische Prüfung nicht", "Ein Zeitpunkt ist nicht belegt."}, []string{"stimmten", "spätestens"}},
		{"key compromise", func(d *verify.Deposit) {
			d.Status, d.Proven, d.Qualification = verify.StatusIndeterminate, nil, ""
			d.Token.Compromised = true
			d.Token.Revocation[0].Status, d.Token.Revocation[0].Reason = "revoked", 1
		}, []string{"widerrufen (Grund: Kompromittierung des Schlüssels)", "nicht ausgeschlossen, dass der Zeitstempel nachträglich erstellt wurde"}, []string{"spätestens", "qualifizierter Zeitstempeldienst nach"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rep, d := sample(64)
			tc.edit(d)
			got := Statement(rep, d)
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
	got := Statement(rep, d)
	for _, w := range []string{"(Grund: Prüfdaten fehlen)", "schließt Ablage 0012 diesen Prüfwert ein", "Für Ablage 0012 besteht ein Zeitstempel", "den der Zeitstempel der Ablage 0012 spätestens für den 05.11.2026, 09:14:23 Uhr MEZ belegt"} {
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
	for _, want := range []string{"Feststellung", "Angaben", "Dateiverzeichnis", "Unabhängige Kontrolle",
		`.\rechnungsarchiv.exe verify '0011_2026-10 Eingangsrechnungen.zip'`, "Gewählte Frist: 7 Jahre, gewöhnliches Datum 31.12.2033",
		"PRUEFANLEITUNG.txt", "(UTC+01:00)", "nicht gesperrt"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
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

// Finding 2: the command names the file as a single-quoted PowerShell
// literal, so "$(…)" in a name is never evaluated.
func TestCommandQuotesFileName(t *testing.T) {
	for _, tc := range []struct{ file, want string }{
		{"0011_$(Start-Process calc).zip", `.\rechnungsarchiv.exe verify '0011_$(Start-Process calc).zip'`},
		{"0012_O'Brien.zip", `.\rechnungsarchiv.exe verify '0012_O''Brien.zip'`},
		{"0013_Müller’s `x`.zip", `.\rechnungsarchiv.exe verify '0013_Müller’’s ` + "`x`" + `.zip'`},
	} {
		rep, d := sample(1)
		d.File = tc.file
		lines, err := Text(rep, d, Options{Version: "1.0.0"})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, l := range lines {
			found = found || l == tc.want
		}
		if !found {
			t.Errorf("no line %q in %q", tc.want, lines)
		}
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
	for _, want := range []string{"05.11.2026, 13:00:00,9 Uhr MEZ (UTC+01:00)", "spätestens 05.11.2026, 13:00:02 Uhr MEZ"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q", want)
		}
	}
	if s := Statement(rep, d); !strings.Contains(s, "vom 05.11.2026, 13:00:00,9 Uhr MEZ (± 1 s)") {
		t.Errorf("statement: %s", s)
	}
}

// Workflow simulation W11: text extraction (pdftotext -layout) put the
// values of the two-column "Angaben" block before their labels. Every
// label/value pair is one "Label: Wert" line in reading order.
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
		"Aufbewahrung: Gewählte Frist: 7 Jahre",
		"Anbieter: Mensatek",
		"Seriennummer: 4711",
		"Qualifikation: qualifiziert",
		"Belegt: spätestens 05.11.2026, 09:14:23 Uhr MEZ",
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
		"Archiv-ID: 6f1c0d7e-1111-4222-8333-444455556666",
		"Seriennummer: 4711",
		"Policy-OID: 0.4.0.2023.1.1",
		"Aussteller: CN=CA,O=ASETEC",
		"Qualifikation: qualifiziert",
	} {
		if !strings.Contains(text, w) {
			t.Errorf("pdftotext -layout lacks %q:\n%s", w, text)
		}
	}
}
