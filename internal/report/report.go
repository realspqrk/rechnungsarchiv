// Package report renders the Archivprotokoll (spec §10): one PDF per Ablage,
// derived only from verify.Report. It contains no disclaimer and no
// assessment beyond the verification result. Output is deterministic for
// a given Report (no creation date, fixed fonts).
package report

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Options for rendering.
type Options struct {
	Version string // Rechnungsarchiv version; default archive.Version
}

// FileName is "<NNNN>_<Name>.archivprotokoll.pdf".
func FileName(d *verify.Deposit) string {
	return present.Number(d.Sequence) + "_" + d.Name + ".archivprotokoll.pdf"
}

// PDF renders the Archivprotokoll of d, an Ablage of rep.
func PDF(rep *verify.Report, d *verify.Deposit, o Options) ([]byte, error) {
	out, _, err := render(rep, d, o)
	return out, err
}

// Text returns the lines the PDF shows, page by page in reading order. The
// PDF stores glyph ids, so tests and reviews read the content here.
func Text(rep *verify.Report, d *verify.Deposit, o Options) ([]string, error) {
	_, lines, err := render(rep, d, o)
	return lines, err
}

func render(rep *verify.Report, d *verify.Deposit, o Options) ([]byte, []string, error) {
	if o.Version == "" {
		o.Version = archive.Version
	}
	blocks := build(rep, d)
	footer := fmt.Sprintf("Archivprotokoll Ablage %s · erstellt am %s mit Rechnungsarchiv %s", present.Number(d.Sequence), present.Time(rep.CheckedAt), o.Version)
	// Two passes: the first counts the pages for "Seite x von y".
	l, err := layout(blocks, footer, 0)
	if err != nil {
		return nil, nil, err
	}
	l, err = layout(blocks, footer, l.page)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := l.pdf.Write(&buf); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), l.lines, nil
}

type kind int

const (
	kTitle kind = iota
	kSubtitle
	kHeading
	kPara
	kKV   // key: value
	kFile // one Dateiverzeichnis entry
	kMono
)

type block struct {
	kind kind
	text string
	key  string
	file []string // nr, path, size, mtime, sha256, state
}

func build(rep *verify.Report, d *verify.Deposit) []block {
	bs := []block{
		{kind: kTitle, text: "Archivprotokoll"},
		{kind: kSubtitle, text: nonEmpty(d.Company, rep.Company)},
		{kind: kSubtitle, text: fmt.Sprintf("Ablage %s „%s“ · Belegjahr %d", present.Number(d.Sequence), d.Name, d.Belegjahr)},
		{kind: kHeading, text: "Feststellung"},
		{kind: kPara, text: Statement(rep, d)},
		{kind: kHeading, text: "Angaben"},
	}
	kv := func(k, v string) {
		if v != "" {
			bs = append(bs, block{kind: kKV, key: k, text: v})
		}
	}
	kv("Ergebnis", present.Label(*d))
	kv("Archiv-ID", d.ArchiveID)
	kv("Ablage", present.Number(d.Sequence)+" „"+d.Name+"“, Datei "+d.File)
	switch {
	case d.Previous != nil:
		kv("Vorgänger", "Ablage "+present.Number(d.Sequence-1)+", Prüfwert "+*d.Previous)
	case d.ArchiveID != "":
		kv("Vorgänger", "keiner (erste Ablage des Archivs)")
	}
	kv("Prüfwert", d.Root)
	if d.BaggingDate != "" || d.ArchivedBy != "" {
		kv("Archiviert am / von", present.ISODate(d.BaggingDate)+" / "+d.ArchivedBy+" (Angabe des archivierenden PCs)")
	}
	if d.RetentionYears > 0 {
		kv("Aufbewahrung", fmt.Sprintf("Gewählte Frist: %d Jahre, gewöhnliches Datum %s", d.RetentionYears, present.ISODate(d.RetentionUntil)))
	}
	if t := d.Token; t != nil {
		own := "Zeitstempel"
		kv(own+": Anbieter", strings.TrimSpace(t.TSP+" "+paren(t.Service)))
		kv(own+": Zeitpunkt", present.ExactFull(t.GenTime)) // as signed, with its fraction
		acc := present.Accuracy(t.AccuracyMS)
		switch t.AccuracySource {
		case "token":
			acc += " (laut Zeitstempel)"
		case "provider":
			acc += " (laut Angabe des Anbieters)"
		}
		kv(own+": Genauigkeit", acc)
		kv(own+": Seriennummer", t.Serial)
		kv(own+": Policy-OID", t.Policy)
		kv(own+": Signaturzertifikat", t.Signer+", Seriennummer "+t.SignerSerial)
		kv(own+": Aussteller", t.Issuer)
		for _, r := range t.Revocation {
			kv("Sperrstatus", revocationText(r))
		}
		kv("Qualifikation", present.Qualification(t.Qualification))
		if tl := t.TrustList; tl != nil {
			kv("Vertrauensliste", trustListText(*tl))
		} else {
			kv("Vertrauensliste", "keine Liste nennt den Dienst")
		}
	}
	if p := d.Proven; p != nil {
		kv("Belegt", present.Proven(p))
	}
	kv("Kontrolle", present.TimeFull(rep.CheckedAt)+onlineText(rep.Online))
	if len(rep.TrustLists) > 0 && d.Token == nil {
		var ls []string
		for _, tl := range rep.TrustLists {
			ls = append(ls, trustListText(tl))
		}
		kv("Vertrauenslisten", strings.Join(ls, "; "))
	}

	bs = append(bs, block{kind: kHeading, text: "Dateiverzeichnis"})
	bs = append(bs, block{kind: kPara, text: fmt.Sprintf("%s, sortiert nach Pfad (unter data/). Je Datei: Nummer, Pfad, Größe, ursprüngliches Änderungsdatum und SHA-256.", dateien(len(d.Files)))})
	for i, f := range d.Files {
		state := ""
		switch f.State {
		case verify.FileChanged:
			state = "verändert"
		case verify.FileMissing:
			state = "fehlt"
		}
		bs = append(bs, block{kind: kFile, file: []string{
			fmt.Sprintf("%d", i+1), strings.TrimPrefix(f.Path, "data/"), sizeText(f.Size), mtimeText(f.MTime), f.SHA256, state,
		}})
	}
	for _, a := range d.Added {
		bs = append(bs, block{kind: kPara, text: "Nicht im Dateiverzeichnis, aber vorhanden: " + strings.TrimPrefix(a, "data/")})
	}

	bs = append(bs,
		block{kind: kHeading, text: "Unabhängige Kontrolle"},
		block{kind: kPara, text: "Das Prüfprogramm liegt dem Export für die Betriebsprüfung bei. Im Exportordner in PowerShell:"},
		// A single-quoted literal: a name like "0011_$(…).zip" is never run.
		block{kind: kMono, text: `.\rechnungsarchiv.exe verify ` + present.PSQuote(d.File)},
		block{kind: kPara, text: "Die Kontrolle ohne dieses Programm (mit sha256sum und openssl) beschreiben PRUEFANLEITUNG.txt im Exportordner und VERIFY.txt in der Ablage vollständig. Maßgeblich ist das Ergebnis der Prüfung; dieses Protokoll gibt es wieder."},
	)
	return bs
}

func paren(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

func onlineText(online bool) string {
	if online {
		return ", mit Internetverbindung (Vertrauenslisten und Sperrstatus abgerufen)"
	}
	return ", ohne Internetverbindung (gespeicherte Nachweise)"
}

func trustListText(tl verify.TrustList) string {
	s := fmt.Sprintf("%s Nr. %d, ausgestellt %s", tl.Territory, tl.Sequence, present.TimeFull(tl.IssueDate))
	if tl.FetchedOnline {
		s += ", bei dieser Kontrolle abgerufen"
	}
	return s
}

func revocationText(r verify.Revocation) string {
	kind := "OCSP"
	if r.Kind == "crl" {
		kind = "Sperrliste (CRL)"
	}
	s := fmt.Sprintf("%s laut %s vom %s ", r.Subject, kind, present.TimeFull(r.ThisUpdate))
	switch r.Status {
	case "good":
		s += "nicht gesperrt"
	case "revoked":
		s += "gesperrt seit " + present.TimeFull(r.RevokedAt)
	default:
		s += "mit unbekanntem Status"
	}
	if r.Source == "online" {
		s += " (bei dieser Kontrolle abgerufen)"
	}
	return s
}

func sizeText(n int64) string {
	if n < 1024 {
		return present.Size(n)
	}
	return fmt.Sprintf("%s (%d Byte)", present.Size(n), n)
}

func mtimeText(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return present.Time(t)
}

// Page geometry (A4, points).
const (
	pageW, pageH = 595.28, 841.89
	marginX      = 50.0
	marginTop    = 50.0
	bodyBottom   = pageH - 60
	textW        = pageW - 2*marginX
)

type layouter struct {
	pdf    *gopdf.GoPdf
	y      float64
	page   int
	total  int
	footer string
	lines  []string

	curFont string
	curSize float64
}

func layout(blocks []block, footer string, total int) (*layouter, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4, Unit: gopdf.UnitPT})
	pdf.SetInfo(gopdf.PdfInfo{Title: "Archivprotokoll", Creator: "Rechnungsarchiv"}) // no CreationDate: deterministic
	for _, f := range []struct {
		name string
		data []byte
	}{{"regular", goregular.TTF}, {"bold", gobold.TTF}, {"mono", gomono.TTF}} { // fixed order: deterministic output
		if err := pdf.AddTTFFontData(f.name, f.data); err != nil {
			return nil, err
		}
	}
	l := &layouter{pdf: pdf, total: total, footer: footer}
	l.newPage()
	for _, b := range blocks {
		if err := l.block(b); err != nil {
			return nil, err
		}
	}
	l.pageFooter()
	return l, nil
}

func (l *layouter) newPage() {
	if l.page > 0 {
		l.pageFooter()
	}
	l.pdf.AddPage()
	l.page++
	l.y = marginTop
}

func (l *layouter) pageFooter() {
	name, size := l.curFont, l.curSize
	defer l.font(name, size) // the caller continues in its font on the next page
	l.font("regular", 7.5)
	l.pdf.SetTextColor(90, 90, 90)
	text := fmt.Sprintf("%s · Seite %d von %d", l.footer, l.page, max(l.total, l.page))
	lines, _ := l.wrap(text, textW)
	y := pageH - 40.0
	for _, s := range lines {
		l.text(marginX, y, s)
		y += 9
	}
	l.pdf.SetTextColor(0, 0, 0)
}

func (l *layouter) font(name string, size float64) {
	if name == "" {
		return
	}
	l.curFont, l.curSize = name, size
	l.pdf.SetFont(name, "", size)
}

func (l *layouter) text(x, y float64, s string) {
	l.pdf.SetXY(x, y)
	l.pdf.Text(s)
	l.lines = append(l.lines, s)
}

func (l *layouter) wrap(s string, w float64) ([]string, error) {
	if s == "" {
		return []string{""}, nil
	}
	return l.pdf.SplitTextWithWordWrap(s, w)
}

// ensure starts a new page unless h more points fit.
func (l *layouter) ensure(h float64) {
	if l.y+h > bodyBottom {
		l.newPage()
	}
}

func (l *layouter) para(font string, size, lead, x, w float64, s string) error {
	l.font(font, size)
	lines, err := l.wrap(s, w)
	if err != nil {
		return err
	}
	for _, ln := range lines {
		l.ensure(lead)
		l.y += lead
		l.text(x, l.y, ln)
	}
	return nil
}

func (l *layouter) block(b block) error {
	switch b.kind {
	case kTitle:
		l.y += 10
		return l.para("bold", 20, 24, marginX, textW, b.text)
	case kSubtitle:
		return l.para("regular", 12, 16, marginX, textW, b.text)
	case kHeading:
		l.ensure(50)
		l.y += 14
		return l.para("bold", 12.5, 16, marginX, textW, b.text)
	case kPara:
		l.y += 3
		return l.para("regular", 10, 13.5, marginX, textW, b.text)
	case kMono:
		l.y += 3
		return l.para("mono", 9, 12.5, marginX+10, textW-10, b.text)
	case kKV:
		const keyW = 150.0
		l.font("regular", 9.5)
		lines, err := l.wrap(b.text, textW-keyW)
		if err != nil {
			return err
		}
		l.ensure(13 * float64(min(len(lines), 3)))
		for i, ln := range lines {
			l.ensure(13)
			l.y += 13
			if i == 0 {
				l.font("bold", 9.5)
				l.text(marginX, l.y, b.key)
				l.font("regular", 9.5)
			}
			l.text(marginX+keyW, l.y, ln)
		}
		return nil
	case kFile:
		return l.file(b.file)
	}
	return nil
}

// file lays out one Dateiverzeichnis entry: number, path (wrapped) and
// size, then the modification date and the full SHA-256 on their own line.
func (l *layouter) file(f []string) error {
	const nrW, metaW = 30.0, 170.0
	pathW := textW - nrW - metaW
	metaX := marginX + nrW + pathW + 5
	l.font("regular", 8.5)
	path := f[1]
	if f[5] != "" {
		path += " [" + f[5] + "]"
	}
	lines, err := l.wrap(path, pathW)
	if err != nil {
		return err
	}
	l.ensure(11*float64(len(lines)) + 14)
	l.y += 3
	for i, ln := range lines {
		l.y += 11
		if i == 0 {
			l.text(marginX, l.y, f[0])
		}
		l.text(marginX+nrW, l.y, ln)
		if i == 0 {
			l.text(metaX, l.y, f[2])
		}
	}
	l.y += 10.5
	l.font("mono", 7.5)
	l.text(marginX+nrW, l.y, f[4])
	l.font("regular", 8.5)
	l.text(metaX, l.y, f[3])
	return nil
}
