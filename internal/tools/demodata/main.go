// Command demodata writes the deterministic demo invoices in testdata/demo/.
// All companies, people, numbers and bank details are fictional.
//
//	go run ./internal/tools/demodata
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/signintech/gopdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

type party struct {
	name, street, city, uid string
}

type item struct {
	qty   int
	unit  string
	desc  string
	cents int64 // net unit amount in cents
}

type invoice struct {
	file    string
	from    party
	to      party
	number  string
	date    time.Time
	period  string
	items   []item
	payDays int
	iban    string
}

var self = party{"Alpenblick Tischlerei GmbH", "Lärchenweg 12", "4020 Linz", "ATU00000017"}

func d(day int) time.Time { return time.Date(2026, 10, day, 9, 0, 0, 0, time.UTC) }

var incoming = []invoice{
	{"2026-10-02 Bürobedarf Grünberger RE-26-0815.pdf",
		party{"Bürobedarf Grünberger GmbH", "Hauptplatz 3", "4600 Wels", "ATU00000025"}, self,
		"RE-26-0815", d(2), "Oktober 2026",
		[]item{{5, "Pkg.", "Kopierpapier A4, 80 g/m², 500 Blatt", 549}, {2, "Stk.", "Ordner breit, grün", 389}, {1, "Stk.", "Tischkalender 2027", 1290}},
		14, "AT00 1100 0000 0000 0815"},
	{"2026-10-05 Holzhandel Ebner RE-4471.pdf",
		party{"Holzhandel Ebner & Söhne KG", "Sägewerkstraße 8", "4170 Haslach", "ATU00000033"}, self,
		"4471", d(5), "05.10.2026",
		[]item{{12, "m³", "Fichte Schnittholz, getrocknet", 41500}, {30, "m²", "Eichenfurnier, Güteklasse A", 2850}},
		30, "AT00 3400 0000 0000 4471"},
	{"2026-10-07 Stromversorgung Mühltal RE-2026-10.pdf",
		party{"Stromversorgung Mühltal AG", "Kraftwerkstraße 1", "4190 Bad Leonfelden", "ATU00000041"}, self,
		"SM-2026-10-55102", d(7), "September 2026",
		[]item{{1, "Pausch.", "Netznutzung und Energie, Zählpunkt AT00…0042", 38712}},
		14, "AT00 2011 0000 0000 5510"},
	{"2026-10-09 Druckerei Weißenböck 26-311.pdf",
		party{"Druckerei Weißenböck e.U.", "Gutenberggasse 5", "4040 Linz", "ATU00000058"}, self,
		"26-311", d(9), "09.10.2026",
		[]item{{500, "Stk.", "Visitenkarten, beidseitig, 350 g", 9}, {200, "Stk.", "Flyer A5 „Möbel nach Maß“", 21}},
		14, "AT00 1500 0000 0000 0311"},
	{"2026-10-14 Werkzeug Großmann RE-90112.pdf",
		party{"Werkzeug Großmann GmbH", "Industriezeile 44", "4030 Linz", "ATU00000066"}, self,
		"90112", d(14), "14.10.2026",
		[]item{{4, "Stk.", "Hobelmesser HSS 410 mm", 4690}, {1, "Stk.", "Schärfservice Kreissägeblätter", 6800}},
		30, "AT00 3200 0000 0000 9011"},
	{"2026-10-20 Höller IT-Service 2026-0412.pdf",
		party{"Höller IT-Service e.U.", "Bahnhofstraße 17", "4810 Gmunden", "ATU00000074"}, self,
		"2026-0412", d(20), "Oktober 2026",
		[]item{{1, "Monat", "Wartungsvertrag Büro-PCs und Datensicherung", 18900}, {2, "Std.", "Einrichtung neuer Drucker", 9500}},
		14, "AT00 1200 0000 0000 0412"},
	{"2026-10-23 Spedition Fröhlich RE-7781.pdf",
		party{"Spedition Fröhlich GmbH", "Lagerhausstraße 2", "4061 Pasching", "ATU00000082"}, self,
		"7781", d(23), "21.10.2026",
		[]item{{1, "Fahrt", "Lieferung Einbauküche Linz – Steyr", 24000}, {3, "Std.", "Montagehilfe vor Ort", 5200}},
		21, "AT00 5400 0000 0000 7781"},
	{"2026-10-30 Kanzlei Bäuerle Buchhaltung 10-2026.pdf",
		party{"Kanzlei Bäuerle Steuerberatung GmbH", "Promenade 9", "4020 Linz", "ATU00000090"}, self,
		"B-2026-1030", d(30), "Oktober 2026",
		[]item{{1, "Monat", "Laufende Buchhaltung und Lohnverrechnung (3 Dienstnehmer)", 42000}},
		14, "AT00 1400 0000 0000 1030"},
}

var outgoing = []invoice{
	{"AR-2026-101 Familie Köhler.pdf", self,
		party{"Familie Köhler", "Föhrenstraße 21", "4400 Steyr", ""},
		"AR-2026-101", d(6), "September 2026",
		[]item{{1, "Stk.", "Einbauküche Eiche massiv, inkl. Montage", 1245000}},
		14, "AT00 2032 0000 0000 0101"},
	{"AR-2026-102 Gasthof Zur Lärche.pdf", self,
		party{"Gasthof Zur Lärche Hübner OG", "Dorfplatz 1", "4184 Helfenberg", "ATU00000108"},
		"AR-2026-102", d(13), "Oktober 2026",
		[]item{{8, "Stk.", "Wirtshaustisch Lärche 80×120 cm", 69000}, {32, "Stk.", "Sessel Lärche, geölt", 18500}},
		30, "AT00 2032 0000 0000 0101"},
	{"AR-2026-103 Ordination Dr. Weiß.pdf", self,
		party{"Ordination Dr. Maria Weiß", "Ärztezentrum, Stiege 2", "4020 Linz", ""},
		"AR-2026-103", d(21), "Oktober 2026",
		[]item{{1, "Stk.", "Empfangstheke nach Maß", 486000}, {6, "Std.", "Montage", 6800}},
		14, "AT00 2032 0000 0000 0101"},
	{"AR-2026-104 Hausverwaltung Öhlinger.pdf", self,
		party{"Hausverwaltung Öhlinger GmbH", "Schillerstraße 30", "4020 Linz", "ATU00000116"},
		"AR-2026-104", d(28), "Oktober 2026",
		[]item{{14, "Stk.", "Wohnungseingangstür lackiert, Einbruchschutz RC2", 128000}},
		30, "AT00 2032 0000 0000 0101"},
}

const ebInterface = `<?xml version="1.0" encoding="UTF-8"?>
<Invoice xmlns="http://www.ebinterface.at/schema/6p1/" GeneratingSystem="Demo" DocumentType="Invoice" InvoiceCurrency="EUR" Language="ger">
  <InvoiceNumber>RG-2026-1017</InvoiceNumber>
  <InvoiceDate>2026-10-17</InvoiceDate>
  <Delivery>
    <Period><FromDate>2026-10-01</FromDate><ToDate>2026-10-31</ToDate></Period>
  </Delivery>
  <Biller>
    <VATIdentificationNumber>ATU00000124</VATIdentificationNumber>
    <Address>
      <Name>Reinigungsdienst Glänzend GmbH</Name>
      <Street>Sauberweg 4</Street>
      <Town>Linz</Town>
      <ZIP>4030</ZIP>
      <Country CountryCode="AT">Österreich</Country>
    </Address>
  </Biller>
  <InvoiceRecipient>
    <VATIdentificationNumber>ATU00000017</VATIdentificationNumber>
    <Address>
      <Name>Alpenblick Tischlerei GmbH</Name>
      <Street>Lärchenweg 12</Street>
      <Town>Linz</Town>
      <ZIP>4020</ZIP>
      <Country CountryCode="AT">Österreich</Country>
    </Address>
  </InvoiceRecipient>
  <Details>
    <ItemList>
      <ListLineItem>
        <Description>Unterhaltsreinigung Werkstatt und Büro, Oktober 2026</Description>
        <Quantity Unit="C62">1</Quantity>
        <UnitPrice>320.00</UnitPrice>
        <TaxItem><TaxableAmount>320.00</TaxableAmount><TaxPercent TaxCategoryCode="S">20</TaxPercent><TaxAmount>64.00</TaxAmount></TaxItem>
        <LineItemAmount>320.00</LineItemAmount>
      </ListLineItem>
    </ItemList>
  </Details>
  <Tax>
    <TaxItem><TaxableAmount>320.00</TaxableAmount><TaxPercent TaxCategoryCode="S">20</TaxPercent><TaxAmount>64.00</TaxAmount></TaxItem>
  </Tax>
  <TotalGrossAmount>384.00</TotalGrossAmount>
  <PayableAmount>384.00</PayableAmount>
  <PaymentMethod>
    <UniversalBankTransaction>
      <BeneficiaryAccount><IBAN>AT000000000000001017</IBAN></BeneficiaryAccount>
    </UniversalBankTransaction>
  </PaymentMethod>
  <PaymentConditions><DueDate>2026-10-31</DueDate></PaymentConditions>
</Invoice>
`

func main() {
	root := filepath.Join("testdata", "demo")
	in := filepath.Join(root, "2026-10 Eingangsrechnungen")
	out := filepath.Join(root, "2026-10 Ausgangsrechnungen")
	for _, dir := range []string{in, out} {
		if err := os.RemoveAll(dir); err != nil {
			log.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	for _, inv := range incoming {
		write(filepath.Join(in, inv.file), mustPDF(inv), inv.date)
	}
	write(filepath.Join(in, "2026-10-17 Reinigungsdienst Glänzend RG-2026-1017.xml"), []byte(ebInterface), d(17))
	// System junk that the archiver must skip (listed in skipped.txt).
	write(filepath.Join(in, "Thumbs.db"), []byte("demo thumbnail cache, not an invoice\n"), d(31))
	for _, inv := range outgoing {
		write(filepath.Join(out, inv.file), mustPDF(inv), inv.date)
	}
}

func write(path string, data []byte, mtime time.Time) {
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		log.Fatal(err)
	}
}

func mustPDF(inv invoice) []byte {
	b, err := renderPDF(inv)
	if err != nil {
		log.Fatalf("%s: %v", inv.file, err)
	}
	return b
}

func renderPDF(inv invoice) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: *gopdf.PageSizeA4})
	pdf.SetCompressLevel(9)
	pdf.SetInfo(gopdf.PdfInfo{Title: "Rechnung " + inv.number, Author: inv.from.name, Creator: "Rechnungsarchiv-Demodaten"})
	if err := pdf.AddTTFFontData("r", goregular.TTF); err != nil {
		return nil, err
	}
	if err := pdf.AddTTFFontData("b", gobold.TTF); err != nil {
		return nil, err
	}
	pdf.AddPage()
	y := 60.0
	text := func(font string, size float64, x float64, s string) {
		_ = pdf.SetFont(font, "", size)
		pdf.SetXY(x, y)
		_ = pdf.Cell(nil, s)
	}
	right := func(font string, size float64, xRight float64, s string) {
		_ = pdf.SetFont(font, "", size)
		w, _ := pdf.MeasureTextWidth(s)
		pdf.SetXY(xRight-w, y)
		_ = pdf.Cell(nil, s)
	}
	line := func(dy float64) { y += dy }

	text("b", 14, 50, inv.from.name)
	line(18)
	text("r", 9, 50, inv.from.street+" · "+inv.from.city+uidSuffix(inv.from.uid))
	line(50)
	for _, s := range []string{inv.to.name, inv.to.street, inv.to.city} {
		text("r", 11, 50, s)
		line(15)
	}
	if inv.to.uid != "" {
		text("r", 9, 50, "UID: "+inv.to.uid)
		line(15)
	}
	line(30)
	text("b", 18, 50, "Rechnung")
	line(28)
	for _, kv := range [][2]string{
		{"Rechnungsnummer", inv.number},
		{"Rechnungsdatum", inv.date.Format("02.01.2006")},
		{"Leistungszeitraum", inv.period},
	} {
		text("r", 10, 50, kv[0]+":")
		text("r", 10, 170, kv[1])
		line(14)
	}
	line(20)
	text("b", 10, 50, "Menge")
	text("b", 10, 110, "Bezeichnung")
	right("b", 10, 545, "Betrag")
	line(16)
	var net int64
	for _, it := range inv.items {
		sum := int64(it.qty) * it.cents
		net += sum
		text("r", 10, 50, fmt.Sprintf("%d %s", it.qty, it.unit))
		text("r", 10, 110, it.desc)
		right("r", 10, 545, eur(sum))
		line(15)
	}
	line(10)
	vat := (net*20 + 50) / 100
	for _, kv := range [][2]string{
		{"Summe netto", eur(net)},
		{"zzgl. 20 % USt", eur(vat)},
		{"Rechnungsbetrag", eur(net + vat)},
	} {
		font := "r"
		if kv[0] == "Rechnungsbetrag" {
			font = "b"
		}
		text(font, 10, 330, kv[0])
		right(font, 10, 545, kv[1])
		line(15)
	}
	line(30)
	due := inv.date.AddDate(0, 0, inv.payDays)
	text("r", 10, 50, fmt.Sprintf("Zahlbar ohne Abzug bis %s auf %s.", due.Format("02.01.2006"), inv.iban))
	line(15)
	text("r", 10, 50, "Vielen Dank für Ihren Auftrag. Es gelten unsere Allgemeinen Geschäftsbedingungen.")
	y = 790
	text("r", 7, 50, "Demodaten für Rechnungsarchiv – alle Firmen, Personen und Kontodaten sind frei erfunden.")
	return pdf.GetBytesPdfReturnErr()
}

func uidSuffix(uid string) string {
	if uid == "" {
		return ""
	}
	return " · UID: " + uid
}

// eur formats cents as "1.234,56 EUR".
func eur(c int64) string {
	s := fmt.Sprintf("%d", c/100)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return fmt.Sprintf("%s,%02d EUR", b.String(), c%100)
}
