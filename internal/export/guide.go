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
		if f := tokenFile(d); f != "" {
			p("      Zeitstempel: %s", f)
		}
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
	p("PowerShell in diesem Ordner öffnen. Pfade in einfachen Anführungszeichen ('…'), damit PowerShell nichts darin auswertet; ein Apostroph im Namen wird verdoppelt (O''Brien):")
	p("")
	p(`    .\%s verify '<Export-Ordner>'`, ExeName)
	p("    echo $LASTEXITCODE")
	p("")
	p(`<Export-Ordner> ist der Pfad dieses Ordners; im Ordner selbst genügt '.'. Eine einzelne Ablage: .\%s verify 'NNNN_Name.zip'`, ExeName)
	p("")
	p("  0  in Ordnung")
	p("  5  die Prüfung hat Verändert oder Ungültig ergeben")
	p("  6  etwas ist ausstehend oder unbestimmt; das ist kein positives Ergebnis")
	p("Andere Codes betreffen den Aufruf, die Konfiguration oder den Anbieter, nicht die Rechnungen.")
	p("Liegen Zeitstempel, Zertifikatskette und Sperrstatus im Export, braucht die Kontrolle den Anbieter nicht. Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, bleibt offen, ob der Dienst zu diesem Zeitpunkt qualifiziert war (unbestätigt), bis eine neuere Liste vorliegt. Ohne weitere Angabe läuft die Kontrolle offline; mit Internetverbindung lädt dieser Aufruf zusätzlich aktuelle Vertrauenslisten und Sperrstatus (in einen Zwischenspeicher außerhalb dieses Ordners):")
	p("")
	p(`    .\%s verify '.' --online`, ExeName)
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
	p("c) Im Nachweisordner NNNN_Name.nachweis neben der ZIP liegt je Versuch n eine Anfrage n.tsq. Nur ein beantworteter Versuch hat auch die Antwort n.tsr und die Zertifikatskette n.chain.pem. Welcher Versuch gilt, steht oben bei der Ablage (Zeitstempel: …); sonst den höchsten Versuch n, zu dem n.tsr und n.chain.pem vorliegen. n in den Befehlen durch diese Nummer ersetzen:")
	p("")
	p("    openssl ts -reply -in n.tsr -text")
	p("    openssl ts -verify -digest <Prüfwert-hex> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem")
	p("")
	p("   <Prüfwert-hex> sind die 64 Zeichen ohne das Präfix sha256:. Der angezeigte Abdruck (Message imprint) muss dazu passen. \"Verification: OK\" heißt: Abdruck und Signatur stimmen, und die Signatur führt über die Kette in n.chain.pem. Das prüft nicht, ob der Dienst ein qualifizierter EU-Zeitstempeldienst war; das zeigt nur die EU-Vertrauensliste (Schritt d). Fehlt der Vertrauensanker in n.chain.pem, ihn zusätzlich als -CAfile angeben.")
	p("   Der Zeitstempel belegt den Stand spätestens zur Stempelzeit (Time stamp) zuzüglich der angegebenen Genauigkeit (Accuracy).")
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

// tokenFile names the response the Ablage's own timestamp rests on, e.g.
// `0001_Name.nachweis\2.tsr (Versuch 2)`; "" without an own token.
func tokenFile(d verify.Deposit) string {
	if d.Token == nil {
		return ""
	}
	for _, a := range d.Attempts {
		if a.N == d.Token.Attempt && a.Nachweis != "" {
			return fmt.Sprintf(`%s\%d.tsr (Versuch %d)`, a.Nachweis, a.N, a.N)
		}
	}
	return fmt.Sprintf("%d.tsr (Versuch %d)", d.Token.Attempt, d.Token.Attempt)
}
