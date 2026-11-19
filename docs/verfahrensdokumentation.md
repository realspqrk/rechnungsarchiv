Keine Steuer- oder Rechtsberatung. Maßgeblich ist [docs/haftungsausschluss.md](haftungsausschluss.md).

# Verfahrensdokumentation — Vorlage

Unternehmen: [FIRMA], [RECHTSFORM], [ADRESSE], UID [UID]
Verantwortlich: [VERANTWORTLICH], Vertretung: [STELLVERTRETUNG]
Steuerberatung: [STEUERBERATUNG]
Gilt ab: [DATUM]. Nächste Durchsicht: [DATUM DER NÄCHSTEN DURCHSICHT]

Ersetzen Sie jede eckige Klammer. Hinweise in Kursivschrift streichen Sie, bevor das Dokument zu den Unterlagen kommt. Es beschreibt das Verfahren des Unternehmens. Das Programm führt dieses Verfahren nicht aus.

Grundlagen, wie das Projekt sie liest: § 11 UStG, § 1 E-Rechnung-UStV, UStR Rz 1564f, 1564g, 1565, 1566 und 1570, § 131 und § 132 BAO, § 18 UStG, § 212 UGB, Art. 5 Abs. 1 Buchst. e DSGVO. Zuordnung: [rechtsgrundlagen.md](rechtsgrundlagen.md).

## 1. Empfang und Kontrolle der Rechnungen

*Innerbetriebliches Kontrollverfahren mit verlässlichem Prüfpfad. Das Verfahren wird einmal beschrieben, nicht je Rechnung (UStR Rz 1564g). Die konkrete Wahl ist frei (Rz 1564f).*

1. Eingang: [WEG DES EINGANGS, z. B. E-Mail an rechnung@…, PEPPOL, Post].
2. Zuständig für die Erfassung: [FUNKTION].
3. Vor dem Archivieren wird geprüft: Lieferant, Rechnungsnummer, Datum, Leistung, Betrag, Umsatzsteuer, [WEITERE PUNKTE].
4. Abgleich mit der Lieferung oder Leistung: [WER, WORAN, z. B. Lieferschein, Auftrag, Leistungsnachweis].
5. Abgleich mit der Zahlung: [WER, WANN, z. B. bei Zahlfreigabe oder beim Kontoauszug].
6. Abweichungen: [WER ENTSCHEIDET, WO WIRD DAS FESTGEHALTEN].
7. Erst danach kommt die Datei in den Ordner, der archiviert wird: [ORDNER].

Das Programm prüft den Inhalt der Rechnung nicht und gleicht nichts mit Lieferung oder Zahlung ab.

## 2. E-Rechnungen: Format und Nachweise

*UStR Rz 1565 und 1570. Betrifft ebInterface, ZUGFeRD und XRechnung.*

1. Die Rechnung bleibt in dem Format, in dem sie eingegangen ist. Das Programm wandelt nicht um und erzeugt kein PDF/A.
2. Mit der Rechnung liegen im selben Ordner die Nachweise der Echtheit und der Unversehrtheit, soweit welche vorhanden sind: Signaturdatei, Protokoll der Signaturprüfung, EDI-Vereinbarung, PEPPOL-Nachweis, Dokumentation dieses Kontrollverfahrens. Sie gehören zur Rechnung und werden mit archiviert.
3. Geht dieselbe Rechnung in mehreren Formaten ein (zum Beispiel PDF und XML), kennzeichnet [VERANTWORTLICH] das Doppel vor dem Archivieren: [WIE, z. B. Dateiname oder Vermerk „Zweitschrift“]. Stellt das Unternehmen dieselbe Rechnung selbst in mehreren Formaten aus, kennzeichnet es die weitere Ausfertigung, sonst schuldet es die ausgewiesene Steuer (UStR Rz 1565). Beim Empfang begründet die fehlende Kennzeichnung diese Steuerschuld nicht.
4. Eine Umwandlung nimmt das Unternehmen nicht vor. *Falls doch: hier eintragen, woraus zweifelsfrei hervorgeht, dass der Inhalt gleich geblieben ist. Sonst diesen Satz streichen.*

## 3. Papier scannen

*UStR Rz 1566, § 132 Abs. 2 BAO.*

1. Scannen durch: [FUNKTION]. Gerät und Auflösung: [ANGABE]. Farbig, vollständig, jede Seite.
2. [VERANTWORTLICH] prüft den Scan gegen das Papier: alle Seiten, lesbar, Betrag und Rechnungsnummer stimmen.
3. Danach „Ordner archivieren“.
4. Das Papier bleibt liegen, bis die Ablage einen Zeitstempel hat. Status „Ausstehend“ genügt nicht.
5. Erst dann vernichtet [FUNKTION] das Papier. Vorher ist das mit [STEUERBERATUNG] geklärt.
6. Eine geschriebene Ablage wird nicht durch einen neuen Scan ersetzt. Korrekturen kommen in eine neue Ablage; der Anlass wird hier vermerkt: [WO].

Verträge und Urkunden in Papier, die zivilrechtlich im Original bleiben sollen, vernichtet das Unternehmen nicht. Liste: [LISTE ODER „keine“].

## 4. Rhythmus, Zuständigkeit, ein Schreib-PC

1. Rhythmus, frei gewählt: [WÖCHENTLICH / MONATLICH / QUARTALSWEISE / ANDERS]. Ordnername beginnt mit Jahr und Monat, zum Beispiel `2026-11 Eingangsrechnungen`, damit das Belegjahr vorausgefüllt wird.
2. Archivieren darf nur: [VERANTWORTLICH], am PC [SCHREIB-PC, NAME UND STANDORT].
3. Nur dieser PC schreibt in das Archiv unter [ARCHIVORT]. Ein synchronisierter Ordner ist kein Schreibziel. Das Archiv wird nicht kopiert, um darin weiterzuarbeiten.
4. Vertretung bei Abwesenheit: [STELLVERTRETUNG], am selben PC. Die Schreibsperre eines anderen PCs wird nur nach ausdrücklicher Zustimmung im Programm übernommen.
5. Belegjahr: das Jahr, auf das sich die Belege beziehen. Liegen in einem Ordner Dateien aus mehreren Jahren, bricht [VERANTWORTLICH] ab und trennt die Ordner. Das Programm warnt nur.

## 5. Zeitstempel-Anbieter

1. Anbieter: [ZEITSTEMPEL-ANBIETER aus der Liste des Programms]. Konto: [KONTONAME]. Vertrag vom [DATUM], abgelegt unter [ORDNER].
2. Das Programm sendet beim Stempeln den Prüfwert, die Anmeldung beim Dienst (Kontoname und Passwort oder Zugangszertifikat) und die IP-Adresse. Dateinamen, Rechnungsinhalte und die Prüfsummen der einzelnen Dateien sendet es nicht.
3. Die Preise stehen in der Tabelle der [README](../README.md), mit dem dort genannten Stand. Vor dem Kauf prüft [VERANTWORTLICH] den aktuellen Preis und ob der Anbieter an österreichische Unternehmen verkauft.
4. Ohne eingerichteten Dienst bleiben neue Ablagen ausstehend. Einen kostenlosen Dienst richtet das Unternehmen nicht ein.
5. Fällt der Anbieter aus, bleiben vorhandene Zeitstempel mit der gespeicherten Zertifikatskette und dem Sperrstatus prüfbar. Für neue Ablagen braucht es einen anderen qualifizierten EU-Dienst; angeboten werden nur im Echtbetrieb erprobte, weitere über die Projektseite anfragen. Das Programm wechselt nicht von selbst.

## 6. Sicherung und jährliche Wiederherstellungsprobe

1. Unmittelbar nach dem Zeitstempel kopiert [VERANTWORTLICH] das Archiv auf den zweiten Datenträger [ZWEITER DATENTRÄGER, ORT, ART]. Eine Kopie davor ist keine Sicherung des Stempels.
2. Einmal im Jahr, bis [MONAT], kopiert [VERANTWORTLICH] diese Sicherung in einen eigenen Ordner [ORDNER], der nicht das laufende Archiv ist.
3. Diese Kopie prüft [VERANTWORTLICH] mit `rechnungsarchiv verify --archiv "[ORDNER]"`; das schreibt nichts. Alternativ im Fenster: Einstellungen → „Archiv wechseln“ → „Bestehendes Archiv öffnen …“, dann „Archiv kontrollieren“ und über „Zuletzt geöffnet“ zurück zum laufenden Archiv; ausstehende Zeitstempel der Kopie werden dabei nicht geholt („Später“). Ergebnis und Datum trägt [VERANTWORTLICH] hier ein: [LISTE ODER PFAD ZUM PROTOKOLL].
4. Schlägt die Probe fehl, wird die Sicherung nicht weiterverwendet. Vorgehen: [WEN INFORMIEREN, WIE WIEDERHERSTELLEN].

## 7. Lesbarkeit

*§ 132 Abs. 3 BAO. Auf Kosten des Unternehmens, in angemessener Zeit.*

1. PDF-Rechnungen öffnet [PROGRAMM]. XML, ebInterface, ZUGFeRD und XRechnung öffnet [PROGRAMM].
2. Die Buchhaltungsdaten, aus denen die Eintragungen prüfbar sind, kommen aus [BUCHHALTUNGSSYSTEM], nicht aus diesem Archiv. Format für die Prüfung: [TEXT / XML / ANDERES]. Ein PDF der Buchhaltung ersetzt diese Daten nicht.
3. Für eine Außenprüfung stellt [VERANTWORTLICH] mit „Export für Betriebsprüfung“ einen eigenen Ordner bereit. Das Archiv selbst bleibt unverändert. Anleitung im Export: `PRUEFANLEITUNG.txt`. Kurzfassung: [fuer-pruefer.md](fuer-pruefer.md).

## 8. Aufbewahrung und Löschung

1. Gewählte Regelfrist im Programm: [7 / 10 / 12 / 22] Jahre. 7 ist die gesetzliche Frist nach § 132 Abs. 1 BAO. 12 und 22 nur, wo § 18 Abs. 10 UStG das verlangt (Wohnungsgemeinnützigkeit bzw. Grundstücke). 10 Jahre im Programm ist freiwillige Vorsorge; § 207 BAO allein begründet keine Aufbewahrungspflicht. Aufzeichnungen nach § 18 Abs. 12 UStG haben eine eigene Frist von 10 Jahren.
2. Fristbeginn: Ende des Kalenderjahres, auf das der Beleg sich bezieht. Wirtschaftsjahr abweichend vom Kalenderjahr: [JA/NEIN]. Wenn ja, Beginn mit dem Ende des Kalenderjahres, in dem das Wirtschaftsjahr endet.
3. Das Programm speichert `Aufbewahrung-Jahre` und `Aufbewahrung-Bis` (31.12. des Belegjahres plus die gewählte Zahl). Eine Sperre für schwebende Verfahren gibt es in dieser Version nicht. Solange ein Verfahren offen ist, löscht [VERANTWORTLICH] die betroffene Ablage nicht. Verfahren: [AKTENZEICHEN ODER „keines“].
4. Eine Löschfunktion ist in dieser Version nicht enthalten. Nach Fristende löscht [VERANTWORTLICH] die Ablage und ihren Nachweisordner von Hand, auf dem Schreib-PC und auf dem zweiten Datenträger. Vorher Vermerk mit Datum, Nummer der Ablage und Name: [ORDNER DES VERMERKS].
5. Das Löschen der jeweils letzten Ablage zeigt das Programm nicht an. Eine Lücke in der Nummerierung zeigt es an.

## 9. Nicht Gegenstand

In dieses Archiv kommen nicht:

- Kassenbelege und das Datenerfassungsprotokoll der Registrierkasse (RKSV). Der digitale Kassenbeleg nach § 132a BAO wird [WO ANDERS / NICHT VERWENDET] geführt.
- Bücher, Inventare, Konten, Bilanzen und Geschäftsbriefe (§ 212 UGB). Sie liegen in [BUCHHALTUNGSSYSTEM / ORT].

## 10. Technischer Teil

So arbeitet das Programm. Die Feststellung bleibt die des Unternehmens nur dort, wo oben ein Verfahren beschrieben ist.

Eine **Ablage** ist ein ZIP im lokalen Archiv. Sie enthält den gewählten Ordner unverändert, einmal geschrieben. Danach schreibt das Programm dieses ZIP nicht mehr um.

Der **Prüfwert** ist eine Prüfsumme von 32 Byte über das Inhaltsverzeichnis der Ablage (SHA-256 von `tagmanifest-sha256.txt`). Er steht in den Einzelheiten der Ablage.

Den **qualifizierten Zeitstempel** holt das Programm genau einmal je Ablage bei dem eingerichteten EU-Dienst (RFC 3161). Neben der Ablage liegen danach der Zeitstempel, die Zertifikatskette und der Sperrstatus. Die spätere Kontrolle braucht den Anbieter nicht, wenn diese Prüfdaten vorhanden sind.

Die **Kontrolle** („Archiv kontrollieren“ oder `verify`) schreibt nichts ins Archiv. Nach einer erfolgreichen Kontrolle lautet das Ergebnis: Die Dateien stimmen mit dem Stand überein, den der Zeitstempel spätestens für diesen Zeitpunkt belegt. Bei einem qualifizierten Zeitstempel gilt die Vermutung der Richtigkeit von Zeitpunkt und Unversehrtheit des Prüfwerts (Art. 41 Abs. 2 eIDAS). Über die Zeit davor sagt der Stempel nichts. Echtheit, inhaltliche Richtigkeit und Vollständigkeit belegt er nicht.

Jede Ablage nennt den Prüfwert der vorigen. Eine fehlende Nummer meldet das Programm als Lücke, eine doppelt vergebene Nummer als solche. Das ist keine Aussage, dass jede Rechnung des Unternehmens vorhanden ist.

Der **Export** („Export für Betriebsprüfung“) schreibt einen eigenen Ordner: die gewählten Ablagen, Zeitstempel-Unterlagen, Brückendaten ohne Dateinamen und ohne Rechnungen für Ablagen dazwischen, die gespeicherten EU-Vertrauenslisten, das Prüfprogramm, je Ablage ein Archivprotokoll, `DATEIVERZEICHNIS.csv` und `PRUEFANLEITUNG.txt`.

Das **Archivprotokoll** ist eine sachliche Feststellung zu einer Ablage: Prüfwert, Zeitstempel und das vollständige Dateiverzeichnis. Es enthält keinen Haftungsausschluss. Maßgeblich ist das Prüfprogramm. Das Protokoll selbst ist nicht der Nachweis.
