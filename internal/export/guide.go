package export

import (
	"fmt"
	"strings"

	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// guide is PRUEFANLEITUNG.txt: verification with the bundled program and
// without it, consistent with docs/fuer-pruefer.md. CRLF for Notepad.
func guide(rep *verify.Report) []byte {
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	p("Rechnungsarchiv – Prüfanleitung für diesen Export")
	p("==================================================")
	p("")
	p("Firma: %s", rep.Company)
	p("Archiv-ID: %s", rep.ArchiveID)
	p("Erstellt: %s", present.TimeFull(rep.CheckedAt))
	p("")
	p("Ablagen in diesem Export:")
	for _, d := range rep.Deposits {
		p("  %s_%s (Belegjahr %d): %s", present.Number(d.Sequence), d.Name, d.Belegjahr, present.Label(d))
	}
	if len(rep.Bridged) > 0 {
		p("Nicht im Export, nur mit Brückendaten (ohne Dateinamen und ohne Rechnungen):")
		for _, br := range rep.Bridged {
			p("  Ablage %s", present.Number(br.Sequence))
		}
	}
	p("")
	p("Inhalt des Ordners")
	p("------------------")
	p("- NNNN_Name.zip: die Ablagen, unverändert. Jede ZIP enthält genau einen BagIt-Ordner; die Originaldateien liegen unverändert unter data/.")
	p("- NNNN_Name.nachweis\\: der Zeitstempel (n.tsq Anfrage, n.tsr Antwort), die Zertifikatskette (n.chain.pem) und die Sperrinformationen (n.rev\\).")
	p("- %s\\NNNN\\: Brückendaten nicht mitgegebener Ablagen zwischen den exportierten: nur chain.json und tagmanifest-sha256.txt.", verify.BridgeDir)
	p("- vertrauenslisten\\: die gespeicherten, signaturgeprüften EU-Vertrauenslisten.")
	p("- %s: das Prüfprogramm.", ExeName)
	p("- %s\\: je Ablage ein Archivprotokoll (PDF). Es gibt das Ergebnis der Kontrolle wieder und ist selbst nicht der Nachweis.", ProtocolsDir)
	p("- %s: eine Zeile je Datei mit Ablage, Pfad, Größe, SHA-256, Belegjahr und belegtem Zeitpunkt.", FileListName)
	p("")
	p("1. Kontrolle mit dem Prüfprogramm (Windows, PowerShell)")
	p("--------------------------------------------------------")
	p("PowerShell in diesem Ordner öffnen. Pfade in Anführungszeichen:")
	p("")
	p(`    .\%s verify "<Export-Ordner>"`, ExeName)
	p("    echo $LASTEXITCODE")
	p("")
	p(`<Export-Ordner> ist der Pfad dieses Ordners; im Ordner selbst genügt ".". Eine einzelne Ablage: .\%s verify "NNNN_Name.zip"`, ExeName)
	p("")
	p("  0  in Ordnung")
	p("  5  die Prüfung hat Verändert oder Ungültig ergeben")
	p("  6  etwas ist ausstehend oder unbestimmt; das ist kein positives Ergebnis")
	p("Andere Codes betreffen den Aufruf, die Konfiguration oder den Anbieter, nicht die Rechnungen.")
	p("Liegen Zeitstempel, Zertifikatskette und Sperrstatus im Export, braucht die Kontrolle den Anbieter nicht. Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, bleibt offen, ob der Dienst zu diesem Zeitpunkt qualifiziert war (unbestätigt), bis eine neuere Liste vorliegt oder die Kontrolle mit Internetverbindung läuft.")
	p("")
	p("2. Kontrolle ohne das Programm")
	p("------------------------------")
	p("Werkzeuge: sha256sum und openssl (zum Beispiel Git Bash oder WSL). PowerShell bringt sie nicht mit.")
	p("")
	p("a) ZIP entpacken. Darin liegt genau ein BagIt-Ordner NNNN_Name.")
	p("b) In diesem Bag-Ordner:")
	p("")
	p("    sha256sum -c manifest-sha256.txt")
	p("    sha256sum -c tagmanifest-sha256.txt")
	p("    sha256sum tagmanifest-sha256.txt")
	p("")
	p("   Die dritte Zeile liefert 64 Hexzeichen. Der Prüfwert ist sha256: plus dieser Wert, also der SHA-256 über die genauen Bytes von tagmanifest-sha256.txt. Dateinamen mit %%, Wagenrücklauf oder Zeilenumbruch stehen im Manifest prozentkodiert (BagIt, RFC 8493).")
	p("c) Im Nachweisordner NNNN_Name.nachweis neben der ZIP (1.tsr ist die erste Antwort, 1.chain.pem die Zertifikatskette):")
	p("")
	p("    openssl ts -reply -in 1.tsr -text")
	p("    openssl ts -verify -digest <Prüfwert-hex> -in 1.tsr -CAfile 1.chain.pem -untrusted 1.chain.pem")
	p("")
	p("   <Prüfwert-hex> sind die 64 Zeichen ohne das Präfix sha256:. Der angezeigte Abdruck (Message imprint) muss dazu passen. \"Verification: OK\" heißt: Abdruck und Signaturkette stimmen. Fehlt der Vertrauensanker in 1.chain.pem, ihn zusätzlich als -CAfile angeben.")
	p("d) openssl prüft die Qualifikation nicht. Dafür die EU-Vertrauensliste: https://eidas.ec.europa.eu/efda/trust-services/browse/eidas/tls – Dienst des Anbieters, Typ qualifizierter Zeitstempel (QTST), Status \"granted\" in einem Intervall, das die Stempelzeit enthält.")
	p("e) Verkettung: chain.json nennt unter previous den Prüfwert der vorigen Ablage. Die erste Ablage im Export kann einen Vorgänger außerhalb des Exports haben.")
	p("f) Brückendaten einer nicht mitgegebenen Ablage NNNN (im Ordner %s\\NNNN):", verify.BridgeDir)
	p("")
	p("    sha256sum tagmanifest-sha256.txt")
	p("    sha256sum chain.json")
	p("")
	p("   Die erste Zeile ergibt den Prüfwert dieser Ablage; ihn nennt die folgende Ablage unter previous. Der zweite Wert muss in tagmanifest-sha256.txt in der Zeile chain.json stehen.")
	p("")
	p("Was das Ergebnis bedeutet")
	p("-------------------------")
	p("Nach einer erfolgreichen Kontrolle stimmen die Dateien mit dem Stand überein, den der Zeitstempel spätestens für den genannten Zeitpunkt belegt. Nicht belegt sind die Echtheit der Rechnungen, die Richtigkeit ihres Inhalts, die Vollständigkeit des Archivs und die Zeit vor dem Stempel.")
	return []byte(strings.ReplaceAll(b.String(), "\n", "\r\n"))
}
