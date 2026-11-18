Keine Steuer- oder Rechtsberatung. Maßgeblich ist [docs/haftungsausschluss.md](haftungsausschluss.md).

# Datenschutz — Hinweis für das Verarbeitungsverzeichnis

Baustein für Art. 30 DSGVO zum Archivieren von Ein- und Ausgangsrechnungen mit diesem Programm. [FIRMA] trägt ihn in das eigene Verzeichnis ein und ersetzt die Klammern. Er beschreibt nur diesen Vorgang.

## Verantwortlicher und Zweck

Verantwortlicher: [FIRMA], [KONTAKT]. Datenschutzkontakt: [KONTAKT ODER „keiner bestellt“].

Zweck: Erfüllung der Aufbewahrungspflichten für Rechnungen und Belege und Nachweis, dass die archivierten Dateien zu einem belegten Zeitpunkt in dieser Form vorlagen.

Rechtsgrundlage der Aufbewahrung ist Art. 6 Abs. 1 Buchst. c, wegen § 132 BAO, § 11 und § 18 UStG und § 212 UGB, soweit die Pflicht reicht. Währenddessen besteht kein Löschanspruch (Art. 17 Abs. 3 Buchst. b). Die Übermittlung des Prüfwerts stützt sich auf Art. 6 Abs. 1 Buchst. f: das Unternehmen will die Unversehrtheit der archivierten Dateien belegen.

## Daten und betroffene Personen

Betroffene Personen: Lieferanten, Kunden und die in den Rechnungen genannten Personen, dazu die Person, die archiviert (`Archiviert-Von`, Benutzer und Rechner).

Kategorien: der Inhalt der Rechnungsdateien, Dateinamen, Prüfsummen der Dateien, Belegjahr, gewählte Frist. Diese Daten bleiben auf den Rechnern und Datenträgern des Unternehmens. Rechnungen können Daten nach Art. 9 enthalten, etwa Gesundheitsdaten; Grundlage ist dann Art. 9 Abs. 2 Buchst. f.

An den Zeitstempel-Dienst gehen nur drei Angaben: der Prüfwert (32 Byte), der Kontoname beim Dienst und die IP-Adresse der Anfrage. Dateinamen, Rechnungsinhalte und die Prüfsummen der einzelnen Dateien sendet das Programm nicht. Welche Daten der Dienst danach speichert, richtet sich nach dessen eigenen Angaben.

## Empfänger und Drittland

Empfänger der Übermittlung an den Zeitstempel-Dienst ist nur der eingerichtete qualifizierte Vertrauensdiensteanbieter in der EU, [ZEITSTEMPEL-ANBIETER, LAND]. Weitere Kategorien: [INTERNE STELLEN], die Steuerberatung, soweit sie Unterlagen erhält, und die Abgabenbehörde, soweit eine Vorlagepflicht besteht. Auftragsverarbeiter: [KEINE / NAME].

Eine Übermittlung in ein Drittland findet nicht statt (Art. 44 DSGVO). Das Programm bietet keinen kostenlosen Dienst und keinen US-Dienst an. Ohne eingerichteten EU-Dienst bleiben neue Ablagen ausstehend.

Preise und die Liste der Dienste stehen in der Tabelle der [README](../README.md), mit dem dort genannten Stand.

## Fristen und Löschung

Die Frist wählt das Unternehmen (7, 12 oder 22 Jahre nach der jeweiligen Pflicht; 10 Jahre als freiwillige Vorsorge oder, wo § 18 Abs. 12 UStG gilt, als dessen Frist). Speicherbegrenzung: Art. 5 Abs. 1 Buchst. e. Nach Fristende sind die Daten zu löschen, soweit kein Verfahren und keine andere Pflicht entgegenstehen.

Eine Löschfunktion ist in dieser Version nicht enthalten. [FIRMA] löscht die Ablage und den Nachweisordner nach Fristende von Hand, auch auf der Sicherung, und behält einen Vermerk (Datum, Nummer der Ablage, Person). Das Löschen der jeweils letzten Ablage erkennt das Programm nicht.

## Prüfwert

Der Prüfwert ist eine einzige Prüfsumme über das Inhaltsverzeichnis der Ablage. In dieses Verzeichnis geht ein zufälliges `Seal-Nonce` ein (128 Bit, je Ablage neu). Prüfsummen einzelner Dateien verlassen den Rechner nicht.

Nach den Leitlinien 02/2025 des Europäischen Datenschutzausschusses, Randnummer 52 (Fassung 2.0 vom 07.07.2026), ist ein gesalzener Hash personenbezogen; nach Löschen des Salzes soll er nicht mehr zuordenbar sein. Das Nonce liegt nur in der Ablage. Mit dem Löschen der Ablage entfällt es beim Unternehmen. Solange Sicherungen, Exporte oder das Dateiverzeichnis bestehen, bleibt die Zuordnung über diese Kopien möglich. Für den Zeitstempel-Dienst, der nur den Prüfwert sieht, spricht die relative Betrachtung personenbezogener Daten (EuGH C-413/23 P) dafür, dass er daraus niemanden wiedererkennt. Das ist eine Einschätzung, keine Feststellung der Behörde.

Technische und organisatorische Maßnahmen: ein Schreib-PC, kein synchronisierter Schreibordner, Sicherung nach dem Zeitstempel, jährliche Wiederherstellungsprobe, Zugangsdaten im Windows-Anmeldeinformationsspeicher. Beschreibung: [verfahrensdokumentation.md](verfahrensdokumentation.md).

Leitlinien: <https://www.edpb.europa.eu/system/files/2026-07/edpb_guidelines_202502_blockchain_v2_en.pdf>
