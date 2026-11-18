// Package report renders the Archivprotokoll (spec §10): one PDF per Ablage,
// derived only from verify.Report, in A4 landscape. It is a statement of
// facts in tables: no disclaimer, no explanations, no instructions, and no
// assessment beyond the verification result. Output is deterministic for a
// given Report (no creation date, fixed fonts).
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
// PDF stores glyph ids, so tests and reviews read the content here. A table
// row reads "Label: Wert".
func Text(rep *verify.Report, d *verify.Deposit, o Options) ([]string, error) {
	_, lines, err := render(rep, d, o)
	return lines, err
}

func render(rep *verify.Report, d *verify.Deposit, o Options) ([]byte, []string, error) {
	if o.Version == "" {
		o.Version = archive.Version
	}
	secs := build(rep, d)
	header := []string{
		"Archivprotokoll",
		nonEmpty(d.Company, rep.Company),
		fmt.Sprintf("Ablage %s „%s“ · Belegjahr %d", present.Number(d.Sequence), d.Name, d.Belegjahr),
	}
	footer := fmt.Sprintf("Archivprotokoll Ablage %s · erstellt am %s mit Rechnungsarchiv %s", present.Number(d.Sequence), present.Time(rep.CheckedAt), o.Version)
	// Two passes: the first counts the pages for "Seite x von y".
	l, err := layout(header, secs, footer, 0)
	if err != nil {
		return nil, nil, err
	}
	l, err = layout(header, secs, footer, l.page)
	if err != nil {
		return nil, nil, err
	}
	var buf bytes.Buffer
	if err := l.pdf.Write(&buf); err != nil {
		return nil, nil, err
	}
	return buf.Bytes(), l.lines, nil
}

// row is one table row: label and value.
type row struct{ key, val string }

// section is a heading with a label/value table (and an optional
// sub-table), or the file table.
type section struct {
	title string
	rows  []row
	sub   string // sub-heading of more
	more  []row
	files [][]string // Dateiverzeichnis: nr, path, bytes, mtime, sha256
}

func build(rep *verify.Report, d *verify.Deposit) []section {
	var rows []row
	kv := func(k, v string) {
		if v != "" {
			rows = append(rows, row{k, v})
		}
	}
	kv("Archiv-ID", d.ArchiveID)
	kv("Datei", d.File)
	switch {
	case d.Previous != nil:
		kv("Vorgänger", "Ablage "+present.Number(d.Sequence-1)+", Prüfwert "+*d.Previous)
	case d.ArchiveID != "":
		kv("Vorgänger", "keiner (erste Ablage des Archivs)")
	}
	if d.BaggingDate != "" || d.ArchivedBy != "" {
		v := present.ISODate(d.BaggingDate)
		if d.ArchivedBy != "" {
			v = strings.TrimSpace(v + " von " + d.ArchivedBy)
		}
		kv("Archiviert", v+" (lokale Angabe des archivierenden PCs)")
	}
	if d.RetentionYears > 0 {
		kv("Aufbewahrung", fmt.Sprintf("gewählte Frist %d Jahre, gewöhnliches Ende %s", d.RetentionYears, present.ISODate(d.RetentionUntil)))
	}
	kv("Kontrolle", present.TimeFull(rep.CheckedAt)+onlineText(rep.Online))
	if len(rep.TrustLists) > 0 && d.Token == nil {
		var ls []string
		for _, tl := range rep.TrustLists {
			ls = append(ls, trustListText(tl))
		}
		kv("Vertrauenslisten", strings.Join(ls, "; "))
	}
	details := section{title: "Angaben", rows: rows}

	if t := d.Token; t != nil {
		rows = nil
		kv("Anbieter", strings.TrimSpace(t.TSP+" "+paren(t.Service)))
		kv("Zeitpunkt", present.ExactFull(t.GenTime)) // as signed, with its fraction
		acc := present.Accuracy(t.AccuracyMS)
		switch t.AccuracySource {
		case "token":
			acc += " (laut Zeitstempel)"
		case "provider":
			acc += " (laut Angabe des Anbieters)"
		}
		kv("Genauigkeit", acc)
		kv("Seriennummer", t.Serial)
		kv("Policy-OID", t.Policy)
		kv("Signaturzertifikat", t.Signer)
		kv("Zertifikat-Seriennummer", t.SignerSerial)
		kv("Aussteller", t.Issuer)
		for _, r := range t.Revocation {
			kv("Sperrstatus", revocationText(r))
		}
		kv("Qualifikation", present.Qualification(t.Qualification))
		if tl := t.TrustList; tl != nil {
			kv("Vertrauensliste", trustListText(*tl))
		} else {
			kv("Vertrauensliste", "keine Liste nennt den Dienst")
		}
		details.sub, details.more = "Zeitstempel", rows
	}

	files := section{title: "Dateiverzeichnis (" + dateien(len(d.Files)) + ")"}
	for i, f := range d.Files {
		path := strings.TrimPrefix(f.Path, "data/")
		switch f.State {
		case verify.FileChanged:
			path += " [verändert]"
		case verify.FileMissing:
			path += " [fehlt]"
		}
		files.files = append(files.files, []string{fmt.Sprintf("%d", i+1), path, byteCount(f.Size), mtimeText(f.MTime), f.SHA256})
	}
	for _, a := range d.Added {
		files.rows = append(files.rows, row{"Zusätzlich vorhanden", strings.TrimPrefix(a, "data/") + " (nicht im Dateiverzeichnis)"})
	}
	return []section{{title: "Feststellung", rows: Statement(rep, d)}, details, files}
}

func paren(s string) string {
	if s == "" {
		return ""
	}
	return "(" + s + ")"
}

func onlineText(online bool) string {
	if online {
		return ", online (Vertrauenslisten und Sperrstatus abgerufen)"
	}
	return ", offline (gespeicherte Nachweise)"
}

func trustListText(tl verify.TrustList) string {
	s := fmt.Sprintf("%s Nr. %d, ausgestellt %s", tl.Territory, tl.Sequence, present.TimeFull(tl.IssueDate))
	if tl.FetchedOnline {
		s += ", bei dieser Kontrolle abgerufen"
	}
	return s
}

// revocationText is "gültig · OCSP-Auskunft vom … · CN=…", stated
// positively for a certificate in good standing (owner: "nicht gesperrt"
// read as suspicion).
func revocationText(r verify.Revocation) string {
	kind := "OCSP-Auskunft"
	if r.Kind == "crl" {
		kind = "Sperrliste (CRL)"
	}
	var s string
	switch r.Status {
	case "good":
		s = "gültig"
	case "revoked":
		s = "gesperrt seit " + present.TimeFull(r.RevokedAt)
	default:
		s = "Status unbekannt"
	}
	s += " · " + kind + " vom " + present.TimeFull(r.ThisUpdate)
	if r.Source == "online" {
		s += ", bei dieser Kontrolle abgerufen"
	}
	return s + " · " + r.Subject
}

// byteCount is the exact size with a thousands separator: "23.413".
func byteCount(n int64) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}

func mtimeText(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return present.Time(t)
}

// Page geometry (A4 landscape, points). Landscape gives every file one
// table row: path, size, date and the full SHA-256 side by side.
const (
	pageW, pageH = 841.89, 595.28
	marginX      = 40.0
	marginTop    = 40.0
	bodyBottom   = pageH - 50
	textW        = pageW - 2*marginX
	keyW         = 150.0 // label column of the tables
	keyGap       = 10.0  // labels end this far left of their value

	tableSize = 9.0
	tableLead = 12.0
	cellPad   = 4.0
)

// fileCols are the Dateiverzeichnis columns: x offset from marginX, width,
// and the field of a file row. The path comes last: with a wide gap before
// a column, text extraction (pdftotext -layout) moved the path column out
// of line with the hashes, so a copied row could pair a file with the
// SHA-256 of another one.
var fileCols = []struct {
	title string
	x, w  float64
	right bool
	field int // index into a file row: nr, path, bytes, mtime, sha256
}{
	{"Nr.", 0, 24, true, 0},
	{"Byte", 30, 50, true, 2},
	{"Änderungsdatum", 90, 130, false, 3},
	{"SHA-256", 226, 270, false, 4}, // 64 mono glyphs at 7 pt: 268.8 pt
	{"Datei (unter data/)", 504, textW - 504, false, 1},
}

const pathCol = 4

type rgb [3]uint8

var (
	black  = rgb{0, 0, 0}
	gray   = rgb{95, 99, 104}
	rule   = rgb{214, 217, 214}
	stripe = rgb{243, 245, 243}
)

type layouter struct {
	pdf    *gopdf.GoPdf
	y      float64
	page   int
	total  int
	footer string
	lines  []string
	cont   string // heading of the table in progress, repeated on a new page

	curFont string
	curSize float64
}

func layout(header []string, secs []section, footer string, total int) (*layouter, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pageW, H: pageH}, Unit: gopdf.UnitPT})
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
	if err := l.header(header); err != nil {
		return nil, err
	}
	for _, s := range secs {
		if err := l.section(s); err != nil {
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
	if l.cont != "" {
		name, size := l.curFont, l.curSize
		l.font("bold", 10)
		l.y += 10
		l.text(marginX, l.y, l.cont+" – Fortsetzung")
		l.y += 5
		l.font(name, size)
	}
}

// pageFooter: a rule, then the footer text left and "Seite x von y" right.
func (l *layouter) pageFooter() {
	name, size := l.curFont, l.curSize
	defer l.font(name, size) // the caller continues in its font on the next page
	y := pageH - 28.0
	l.line(marginX, y-11, marginX+textW, y-11, rule)
	l.font("regular", 7.5)
	l.color(gray)
	page := fmt.Sprintf("Seite %d von %d", l.page, max(l.total, l.page))
	l.draw(marginX, y, l.footer)
	if w, err := l.pdf.MeasureTextWidth(page); err == nil {
		l.draw(marginX+textW-w, y, page)
	}
	l.lines = append(l.lines, l.footer+" · "+page)
	l.color(black)
}

func (l *layouter) font(name string, size float64) {
	if name == "" {
		return
	}
	l.curFont, l.curSize = name, size
	l.pdf.SetFont(name, "", size)
}

func (l *layouter) color(c rgb) { l.pdf.SetTextColor(c[0], c[1], c[2]) }

func (l *layouter) line(x1, y1, x2, y2 float64, c rgb) {
	l.pdf.SetStrokeColor(c[0], c[1], c[2])
	l.pdf.SetLineWidth(0.5)
	l.pdf.Line(x1, y1, x2, y2)
}

func (l *layouter) fill(x, y, w, h float64, c rgb) {
	l.pdf.SetFillColor(c[0], c[1], c[2])
	l.pdf.RectFromUpperLeftWithStyle(x, y, w, h, "F")
}

// text draws s and records it as one line of Text.
func (l *layouter) text(x, y float64, s string) {
	l.draw(x, y, s)
	l.lines = append(l.lines, s)
}

// draw draws s without recording it (part of a line a caller records).
func (l *layouter) draw(x, y float64, s string) {
	l.pdf.SetXY(x, y)
	l.pdf.Text(s)
}

func (l *layouter) wrap(s string, w float64) ([]string, error) {
	if s == "" {
		return []string{""}, nil
	}
	return l.pdf.SplitTextWithWordWrap(s, w)
}

// ensure starts a new page unless h more points fit; it reports whether it
// did.
func (l *layouter) ensure(h float64) bool {
	if l.y+h > bodyBottom {
		l.newPage()
		return true
	}
	return false
}

// header: the title, then company and Ablage (wrapped: an Ablage name may
// be 240 characters, a company name has no limit), then a rule.
func (l *layouter) header(h []string) error {
	l.font("bold", 18)
	l.y += 18
	l.text(marginX, l.y, h[0])
	l.font("regular", 11)
	for _, s := range h[1:] {
		lines, err := l.wrap(s, textW)
		if err != nil {
			return err
		}
		for _, ln := range lines {
			l.ensure(15)
			l.y += 15
			l.text(marginX, l.y, ln)
		}
	}
	l.y += 10
	l.line(marginX, l.y, marginX+textW, l.y, rule)
	return nil
}

// heading starts a table; a table that runs onto a new page repeats it
// there as "… – Fortsetzung".
func (l *layouter) heading(s string, size float64) {
	l.cont = ""
	l.ensure(size + 18 + 3*tableLead)
	l.y += size + 12
	l.font("bold", size)
	l.text(marginX, l.y, s)
	l.y += 5
	l.cont = s
}

func (l *layouter) section(s section) error {
	l.heading(s.title, 12)
	if s.files != nil {
		if err := l.fileTable(s.files); err != nil {
			return err
		}
	}
	if err := l.table(s.rows); err != nil {
		return err
	}
	if s.sub != "" {
		l.heading(s.sub, 10)
		return l.table(s.more)
	}
	return nil
}

// table lays out label/value rows: the label right-aligned in the left
// column, the value wrapped in the right one, a rule under each row. Label
// and value share a baseline with a narrow gap, so text extraction
// (pdftotext -layout) reads them as one line, label first; with a wide gap
// it split the labels into a column of their own. Text records the row as
// "Label: Wert" and continuation lines on their own.
func (l *layouter) table(rows []row) error {
	for _, r := range rows {
		l.font("regular", tableSize)
		vals, err := l.wrap(r.val, textW-keyW)
		if err != nil {
			return err
		}
		keys, err := l.wrap(r.key, keyW-keyGap)
		if err != nil {
			return err
		}
		n := max(len(vals), len(keys))
		l.ensure(float64(min(n, 3))*tableLead + 2*cellPad)
		l.y += cellPad
		for i := 0; i < n; i++ {
			if i > 0 {
				l.ensure(tableLead)
			}
			base := l.y + tableLead - 3
			var k, v string
			if i < len(keys) {
				k = keys[i]
			}
			if i < len(vals) {
				v = vals[i]
			}
			l.font("regular", tableSize)
			l.color(gray)
			l.cell(0, keyW-keyGap, true, base, k)
			l.color(black)
			if r.key == "Ergebnis" {
				l.font("bold", tableSize)
			}
			l.draw(marginX+keyW, base, v)
			switch {
			case i == 0:
				l.lines = append(l.lines, k+": "+v)
			case k != "":
				l.lines = append(l.lines, k+" "+v)
			default:
				l.lines = append(l.lines, v)
			}
			l.y += tableLead
		}
		l.y += cellPad
		l.line(marginX, l.y, marginX+textW, l.y, rule)
	}
	return nil
}

// fileTable lays out the Dateiverzeichnis, one row per file (a long path
// wraps inside the last column), with the column header on every page.
func (l *layouter) fileTable(files [][]string) error {
	const size, lead = 8.0, 10.5
	head := func() {
		l.fill(marginX, l.y, textW, lead+2*cellPad, stripe)
		l.font("bold", size)
		base := l.y + cellPad + lead - 2.5
		var names []string
		for _, c := range fileCols {
			l.cell(c.x, c.w, c.right, base, c.title)
			names = append(names, c.title)
		}
		l.lines = append(l.lines, strings.Join(names, " | "))
		l.y += lead + 2*cellPad
		l.line(marginX, l.y, marginX+textW, l.y, rule)
	}
	l.ensure(3 * (lead + 2*cellPad))
	head()
	for _, f := range files {
		l.font("regular", size)
		paths, err := l.wrap(f[1], fileCols[pathCol].w)
		if err != nil {
			return err
		}
		h := float64(len(paths))*lead + 2*cellPad
		if l.ensure(h) {
			head()
		}
		base := l.y + cellPad + lead - 2.5
		var cells []string
		for _, c := range fileCols {
			v := f[c.field]
			if c.field == 1 {
				v = paths[0]
			}
			l.font("regular", size)
			if c.field == 4 {
				l.font("mono", 7)
			}
			l.cell(c.x, c.w, c.right, base, v)
			cells = append(cells, v)
		}
		l.lines = append(l.lines, strings.Join(cells, " | "))
		l.font("regular", size)
		for i, p := range paths[1:] {
			l.cell(fileCols[pathCol].x, fileCols[pathCol].w, false, base+float64(i+1)*lead, p)
			l.lines = append(l.lines, p)
		}
		l.y += h
		l.line(marginX, l.y, marginX+textW, l.y, rule)
	}
	return nil
}

// cell draws s in a column (x relative to marginX), right-aligned if asked.
func (l *layouter) cell(x, w float64, right bool, y float64, s string) {
	if right {
		if sw, err := l.pdf.MeasureTextWidth(s); err == nil {
			x += w - sw
		}
	}
	l.draw(marginX+x, y, s)
}
