Keine Steuer- oder Rechtsberatung. Maßgeblich ist [docs/haftungsausschluss.md](haftungsausschluss.md).

# Für Betriebsprüfung und Steuerberatung

Das Programm ist nicht durch das Bundesministerium für Finanzen freigegeben. Eine erfolgreiche Kontrolle ist keine Würdigung des Belegs durch die Behörde.

## Was ein Export enthält

„Export für Betriebsprüfung“ schreibt einen eigenen Ordner. Das Archiv des Unternehmens bleibt unverändert. Im Ordner liegen:

- die gewählten Ablagen (ZIP) und ihre Zeitstempel-Unterlagen, unverändert;
- Brückendaten für Ablagen dazwischen, die nicht mitgegeben wurden: nur `chain.json` und `tagmanifest-sha256.txt`, ohne Dateinamen und ohne Rechnungen;
- die gespeicherten EU-Vertrauenslisten;
- `rechnungsarchiv.exe`;
- je Ablage ein Archivprotokoll (PDF): sachliche Feststellung, Prüfwert, Zeitstempel, vollständiges Dateiverzeichnis mit Prüfsummen;
- `DATEIVERZEICHNIS.csv`: eine Zeile je Datei mit Ablage, Pfad, Größe, SHA-256, Belegjahr und belegtem Zeitpunkt;
- `PRUEFANLEITUNG.txt`.

Das Archivprotokoll gibt das Ergebnis der Kontrolle wieder. Es enthält keinen Haftungsausschluss. Es ist selbst nicht der Nachweis. Maßgeblich ist das Prüfprogramm. Die Prüfung schreibt nichts ins Archiv.

## Was das Ergebnis bedeutet

Nach einer erfolgreichen Kontrolle: Die Dateien stimmen mit dem Stand überein, den der Zeitstempel spätestens für diesen Zeitpunkt belegt.

Bei einem qualifizierten Zeitstempel gilt die Vermutung der Richtigkeit von Zeitpunkt und Unversehrtheit des Prüfwerts (Art. 41 Abs. 2 eIDAS). Die Vermutung betrifft den Prüfwert. War der Dienst zum Stempelzeitpunkt nicht qualifiziert, gibt es diese Vermutung nicht; die Behörde würdigt den Stempel frei (§ 167 Abs. 2 BAO).

Nicht belegt sind die Echtheit der Rechnungen, die Richtigkeit ihres Inhalts, die Vollständigkeit des Archivs und die Zeit vor dem Stempel. Echtheit der Herkunft und Unversehrtheit des Inhalts nach § 11 UStG bleiben beim Verfahren des Unternehmens.

## Kontrolle in zwei Minuten

In PowerShell, im Exportordner. Pfade in einfachen Anführungszeichen; so wertet PowerShell Zeichen wie `$` im Namen nicht aus:

```powershell
.\rechnungsarchiv.exe verify '<Export-Ordner>'
echo $LASTEXITCODE
```

| Code | Bedeutung |
|---|---|
| 0 | in Ordnung |
| 5 | die Prüfung hat Verändert oder Ungültig ergeben |
| 6 | etwas ist ausstehend oder unbestimmt; das ist kein positives Ergebnis |

Andere Codes betreffen den Aufruf, die Konfiguration oder den Anbieter. Sie sind keine Aussage über die Rechnungen. Dieselbe Prüfung führt „Archiv kontrollieren“ aus. Liegen Zeitstempel, Zertifikatskette und Sperrstatus im Export, braucht die Kontrolle den Anbieter nicht. Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, bleibt offen, ob der Dienst zu diesem Zeitpunkt qualifiziert war (Status unbestätigt), bis eine neuere Liste vorliegt. Die Kontrolle läuft ohne weitere Angabe offline. Mit Internetverbindung lädt dieser Aufruf zusätzlich aktuelle Vertrauenslisten und Sperrstatus, in einen Zwischenspeicher außerhalb des Exportordners:

```powershell
.\rechnungsarchiv.exe verify '.' --online
```

## Kontrolle ohne das Programm

Werkzeuge: `sha256sum` und `openssl` (zum Beispiel Git Bash oder WSL). PowerShell bringt sie nicht mit.

1. ZIP entpacken. Darin liegt genau ein BagIt-Ordner `NNNN_Name`. Die Originaldateien stehen unverändert unter `data/`.
2. In diesem Bag-Ordner:

```text
sha256sum -c manifest-sha256.txt
sha256sum -c tagmanifest-sha256.txt
sha256sum tagmanifest-sha256.txt
```

Die dritte Zeile liefert 64 Hexzeichen. Der Prüfwert ist `sha256:` plus dieser Wert. Er ist der SHA-256 über die genauen Bytes von `tagmanifest-sha256.txt`. Dateinamen mit `%`, Wagenrücklauf oder Zeilenumbruch stehen im Manifest prozentkodiert (BagIt, RFC 8493). Bei üblichen Rechnungsnamen entfällt das.

3. Im Nachweisordner `NNNN_Name.nachweis` neben der ZIP. Dort liegt je Versuch `n` eine Anfrage `n.tsq`. Nur ein beantworteter Versuch hat auch die Antwort `n.tsr` und die Zertifikatskette `n.chain.pem`; nach einem gescheiterten ersten Versuch gibt es etwa nur `1.tsq` und `2.tsr`. Welcher Versuch gilt, nennt das Prüfprogramm (in `PRUEFANLEITUNG.txt` bei jeder Ablage: „Zeitstempel: …\2.tsr“). Sonst nehmen Sie den höchsten Versuch `n`, zu dem `n.tsr` und `n.chain.pem` vorliegen, und setzen seine Nummer für `n` ein:

```text
openssl ts -reply -in n.tsr -text
openssl ts -verify -digest <Prüfwert-hex> -in n.tsr -CAfile n.chain.pem -untrusted n.chain.pem -partial_chain
```

`<Prüfwert-hex>` sind die 64 Zeichen ohne das Präfix `sha256:`. Die Anzeige des Abdrucks (`Message imprint`) muss dazu passen. `Verification: OK` heißt: Abdruck und Signatur stimmen, geprüft über die Kette in `n.chain.pem`. Ob der Dienst ein qualifizierter EU-Zeitstempeldienst war, prüft das nicht (siehe unten). Dieselbe Datei steht bei `-untrusted`, weil die Kette auch das Stempelzertifikat und Zwischenzertifikate enthält; `-CAfile` allein vertraut nur Ankerzertifikaten. Ohne `-CAfile` und `-untrusted` meldet openssl `unable to get local issuer certificate`. `-partial_chain` ist nötig, wenn die Kette bei einem Zwischenzertifikat endet. Das ist etwa der Fall, wenn die Vertrauensliste das Stempelzertifikat selbst als Dienst nennt (so bei Mensatek). Ohne die Option meldet openssl dann `unable to get issuer certificate`.

Der Zeitstempel belegt den Stand spätestens zur Stempelzeit (`Time stamp`) zuzüglich der angegebenen Genauigkeit (`Accuracy`). So rechnen auch das Prüfprogramm und das Archivprotokoll.

`openssl` prüft die Qualifikation nicht. Dafür die EU-Vertrauensliste: <https://eidas.ec.europa.eu/efda/trust-services/browse/eidas/tls>. Dienst des Anbieters, Typ qualifizierter Zeitstempel (QTST), Status „granted“ in einem Intervall, das die Stempelzeit enthält.

`chain.json` nennt unter `previous` den Prüfwert der vorigen Ablage. Die erste Ablage im Export kann einen Vorgänger außerhalb des Exports haben.

## Bekannte Grenzen

- Wird der Schlüssel des Anbieters später kompromittiert, lässt sich mit nur einem Zeitstempel-Dienst nicht ausschließen, dass ein Stempel rückdatiert wurde. Ein zweiter Stempel eines anderen Anbieters ist in dieser Version nicht enthalten. Ein Sperreintrag wegen Schlüsselkompromittierung führt zu „Unbestimmt“.
- Das Löschen der jeweils letzten Ablage erkennt das Programm nicht.
- Nur ein PC darf in das Archiv schreiben. Ein synchronisierter Ordner ist kein zulässiges Schreibziel.
