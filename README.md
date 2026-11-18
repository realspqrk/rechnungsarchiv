<div align="center">
  <h1>Rechnungsarchiv</h1>
  <p><b>Lokales Archiv für Rechnungen österreichischer Unternehmen, mit qualifiziertem Zeitstempel.</b><br>
  Ein Ordner mit Rechnungen wird zu einer Ablage. Die Rechnungen verlassen den Computer nie.</p>
  <p>
    <a href="https://github.com/realspqrk/rechnungsarchiv/releases"><img src="https://img.shields.io/github/v/release/realspqrk/rechnungsarchiv?include_prereleases" alt="Release"></a>
    <a href="https://github.com/realspqrk/rechnungsarchiv/blob/main/LICENSE"><img src="https://img.shields.io/github/license/realspqrk/rechnungsarchiv" alt="Apache-2.0"></a>
    <a href="https://github.com/realspqrk/rechnungsarchiv"><img src="https://img.shields.io/badge/Plattform-Windows-blue?logo=windows" alt="Plattform: Windows"></a>
    <a href="https://github.com/realspqrk/rechnungsarchiv"><img src="https://img.shields.io/badge/Status-in_Entwicklung-orange" alt="Status: in Entwicklung"></a>
    <a href="https://github.com/realspqrk/rechnungsarchiv/blob/main/go.mod"><img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go 1.27"></a>
  </p>
</div>

> [!IMPORTANT]
> Rechnungsarchiv ist freie Open-Source-Software. Für Sach- und Vermögensschäden aus leichter Fahrlässigkeit – insbesondere Datenverlust, einen abgelehnten Vorsteuerabzug, eine Schätzung oder Strafen – wird nicht gehaftet. Die Haftung für Vorsatz, grobe Fahrlässigkeit und Personenschäden bleibt bestehen. Ein weitergehender Ausschluss besteht nicht. Die Hinweise in diesem Projekt geben die eigene Einschätzung der Autorinnen und Autoren wieder; sie sind keine Steuer- oder Rechtsberatung und wurden weder von der Finanzverwaltung noch von Rechtsanwältinnen oder Rechtsanwälten geprüft. Klären Sie die Verwendung vor dem Einsatz mit Ihrer Steuerberaterin oder Ihrem Steuerberater. Vollständiger Text: [docs/haftungsausschluss.md](docs/haftungsausschluss.md).

Das Programm ist in Entwicklung. Eine Veröffentlichung gibt es noch nicht. Es läuft unter Windows.

| Für wen | Hier beginnen |
|---|---|
| Buchhaltung im Betrieb | [So funktioniert es](#so-funktioniert-es) und [Was es kostet](#zeitstempel-dienste) |
| Ein-Personen-Unternehmen, Skript oder KI-Agent | [Einrichten](#einrichten) und [Kommandozeile](#kommandozeile--ki-agenten) |
| Steuerberatung | [Was es belegt](#was-es-belegt--und-was-nicht), [Recht](#rechtlicher-rahmen), [Haftung](docs/haftungsausschluss.md) |
| Betriebsprüfung | [Export und eigene Prüfung](#für-die-betriebsprüfung) |
| Entwicklung | [Kommandozeile](#kommandozeile--ki-agenten) und [Für Entwickler](#für-entwickler) |

## Einrichten

Unter Windows:

1. Laden Sie das Programm herunter. Eine Veröffentlichung gibt es noch nicht.
2. Legen Sie ein Konto bei einem Zeitstempel-Dienst aus der [Tabelle](#zeitstempel-dienste) an. Empfehlung: Mensatek.
3. Starten Sie das Programm, geben Sie Firma und Archivordner ein und wählen Sie „Archiv anlegen“.
4. Tragen Sie die Zugangsdaten unter Einstellungen ein.
5. Ziehen Sie einen Ordner hinein.

## So funktioniert es

Unter Windows gibt es ein Fenster mit „Ordner archivieren“, „Archiv kontrollieren“ und „Export für Betriebsprüfung“. Dieselben Schritte gehen über die Kommandozeile.

1. **Ordner archivieren.** Sie wählen einen Ordner mit Rechnungen – für eine Woche, einen Monat, ein Quartal, ganz wie Sie möchten. Das Programm übernimmt die Dateien unverändert in eine Ablage. Eine Ablage ist ein ZIP im lokalen Archiv. Nach dem Speichern wird dieses ZIP nicht mehr umgeschrieben.
2. **Qualifizierter Zeitstempel auf den Prüfwert.** Zur Ablage gehört ein Prüfwert von 32 Byte. Das Programm holt dafür genau einen qualifizierten Zeitstempel nach RFC 3161 bei einem EU-Dienst Ihrer Wahl. Der Dienst erhält den Prüfwert, Ihren Kontonamen und Ihre IP-Adresse. Dateinamen, Rechnungsinhalte und die Prüfsummen der einzelnen Dateien bleiben auf dem Computer. Neben der Ablage liegen danach der Zeitstempel, die Zertifikatskette und der Sperrstatus.
3. **Später kontrollieren oder exportieren.** Mit „Archiv kontrollieren“ prüfen Sie die Ablagen. Die Prüfung braucht den Dienstanbieter nicht, wenn die Prüfdaten beim Stempeln gespeichert wurden. Mit „Export für Betriebsprüfung“ stellen Sie die Unterlagen für eine Prüfung zusammen.

Ohne eingerichteten Dienst bleiben neue Ablagen ausstehend. Einen kostenlosen Dienst bietet das Programm bewusst nicht an: Er hätte keine gesetzliche Vermutung, und bekannte kostenlose Dienste (z. B. DigiCert) stehen in den USA. Ausstehende Zeitstempel werden beim nächsten Programmstart, nach dem nächsten erfolgreichen Zeitstempel oder über „Jetzt nachholen“ geholt.

Für den Start brauchen Sie einen Windows-PC, einen Ordner für das Archiv und ein Konto bei einem Dienst aus der Tabelle unten. Zugangsdaten liegen im Windows-Anmeldeinformationsspeicher, nicht in der Kommandozeile.

## Was es belegt – und was nicht

Nach einer erfolgreichen Kontrolle lautet das Ergebnis:

> Die Dateien stimmen mit dem spätestens am … belegten Stand überein.

Der Zeitstempel belegt, dass dieser Stand spätestens zu dem genannten Zeitpunkt vorlag. Über die Zeit davor sagt er nichts.

Bei einem qualifizierten Zeitstempel gilt die Vermutung der Richtigkeit von Zeitpunkt und Unversehrtheit (Art. 41 Abs. 2 eIDAS). Die Vermutung betrifft den Prüfwert. War ein Dienst zum Zeitpunkt des Stempels nicht qualifiziert, gibt es keine Vermutung; die Behörde würdigt den Stempel dann frei (§ 167 Abs. 2 BAO). Art. 41 Abs. 1 eIDAS verbietet nur, ihm die Wirkung allein wegen der elektronischen Form abzusprechen.

Nicht belegt ist:

- die Echtheit der Rechnungen;
- die Richtigkeit ihres Inhalts;
- die Vollständigkeit des Archivs;
- ob die letzte Ablage gelöscht wurde.

Die Pflichten des Verfahrens bleiben beim Unternehmen. Dazu gehören insbesondere:

- Echtheit der Herkunft und Unversehrtheit des Inhalts nach § 11 UStG und der E-Rechnung-UStV, zum Beispiel durch ein innerbetriebliches Kontrollverfahren;
- die Aufbewahrung der Rechnung in der empfangenen Form und die Lesbarkeit über die gesamte Frist;
- die Entscheidung, welche Belege archiviert werden.

Sicherungen sind erforderlich. Die Sicherungskopie wird nach dem Zeitstempel auf einem zweiten Datenträger angelegt. Einmal im Jahr kopieren Sie die Sicherung in einen eigenen Ordner und prüfen diese wiederhergestellte Kopie mit „Archiv nur kontrollieren“.

Das Programm ersetzt keine Verfahrensdokumentation und keine Buchhaltung.

## Zeitstempel-Dienste

✓ in der Tabelle bedeutet: der Dienst ist in der EU-Vertrauensliste als qualifizierter Zeitstempeldienst mit Status „granted“ eingetragen (Abfrage 18.11.2026). Es genügt ein Anbieter. Das Programm prüft die EU-Vertrauensliste selbst und zeigt bei jeder Ablage an, ob der Dienst zum Zeitpunkt des Stempels qualifiziert war.

| Anbieter (Land) | eIDAS-qualifiziert (Stand EU-Vertrauensliste 18.11.2026) | Kleinstes Paket | ca. Kosten pro Ablage | Zugang | Hinweis |
|---|---|---|---|---|---|
| Mensatek / ASETEC (ES) | ✓ (granted seit 04.11.2026) | ab 10 € Guthaben, Guthaben verfällt nicht, ca. 0,042 € pro Zeitstempel | ≈ 0,04 € | Benutzer/Passwort (HTTP Basic) | Empfehlung: günstigste Option |
| Disig (SK) | ✓ | 100 Zeitstempel 11,67 € (mind. 365 Tage gültig) | ≈ 0,12 € | Client-Zertifikat | Alternative |
| Aruba (IT) | ✓ | 50 Zeitstempel 13,50 € | ≈ 0,27 € | Benutzer/Passwort | Endpunkt noch zu bestätigen |
| Namirial (IT) | ✓ | 100 Zeitstempel 25 €, verfallen nicht | ≈ 0,25 € | Benutzer/Passwort (über Vertrieb) | – |
| GLOBALTRUST (AT) | ✓ | 1.000 Zeitstempel 185 €/Jahr, verfallen zum Jahresende | ≈ 3,70 € bei 50 Ablagen/Jahr | Benutzer/Passwort | österreichischer Anbieter, für KMU-Mengen teuer |

Preise netto, ohne Gewähr, Stand 18.11.2026 laut Anbieter-Websites; vor dem Kauf prüfen, ob der Anbieter an österreichische Unternehmen verkauft (Reverse Charge). Die Preise stehen bewusst nur hier, nicht im Programm.

Ein Anbieter genügt. Fällt er weg, bleiben bestehende Zeitstempel gültig und prüfbar (Zertifikatskette und Sperrstatus werden beim Stempeln gespeichert); für neue Ablagen wählen Sie einen anderen Anbieter. Das Programm wechselt den Anbieter nicht von selbst und bietet keinen kostenlosen Dienst an. Die eingebaute Liste enthält nur qualifizierte EU-Dienste.

## Für die Betriebsprüfung

„Export für Betriebsprüfung“ schreibt einen eigenen Ordner. Das Archiv selbst bleibt unverändert. Im Export liegen:

- die gewählten Ablagen und ihre Zeitstempel-Unterlagen, unverändert;
- Brückendaten für Ablagen dazwischen, die Sie nicht mitgeben (nur die Kettendaten, keine Dateinamen und keine Rechnungen);
- die gespeicherten EU-Vertrauenslisten;
- `rechnungsarchiv.exe`, das Prüfprogramm;
- je Ablage ein **Archivprotokoll** (PDF): eine sachliche Feststellung, was die Ablage enthält, mit Prüfwert, Zeitstempel und dem vollständigen Verzeichnis aller Dateien samt Prüfsummen;
- `DATEIVERZEICHNIS.csv`: eine Zeile je Datei mit Ablage, Pfad, Größe, SHA-256, Belegjahr und belegtem Zeitpunkt;
- `PRUEFANLEITUNG.txt`: wie Sie mit dem Prüfprogramm und von Hand prüfen.

Das Archivprotokoll gibt das Ergebnis der Kontrolle wieder. Maßgeblich ist das Prüfprogramm. Die Prüfung schreibt nichts ins Archiv. Ein Archivprotokoll können Sie jederzeit auch für eine einzelne Ablage speichern.

So prüfen Sie selbst: `rechnungsarchiv.exe` auf das exportierte Archiv oder eine einzelne Ablage anwenden und `PRUEFANLEITUNG.txt` lesen. `PRUEFANLEITUNG.txt` enthält den genauen PowerShell-Befehl für den Exportordner, mit Pfaden in Anführungszeichen. Eine Prüfung ohne Schreibzugriff braucht keinen Zeitstempel-Dienst, wenn die Prüfdaten im Export liegen.

## Kommandozeile & KI-Agenten

Ein Programm: `rechnungsarchiv`. Die grafische Oberfläche und die Kommandozeile verwenden dieselbe Prüfung. Die Befehlsbeschreibung steht in [docs/cli.md](docs/cli.md).

Beispiel, ein neues Archiv und die erste Ablage. Das Passwort liest das Programm von der Standardeingabe, nie als Argument:

```text
rechnungsarchiv init "D:\Buchhaltung\Rechnungsarchiv" --firma "Musterfirma GmbH"
rechnungsarchiv provider set mensatek --user musterfirma
rechnungsarchiv archive --dry-run "D:\Scans\2026-11 Eingangsrechnungen"
rechnungsarchiv archive "D:\Scans\2026-11 Eingangsrechnungen" --yes
```

Alternative mit Client-Zertifikat (z. B. Disig): `rechnungsarchiv provider set disig --p12 "C:\Pfad\zertifikat.p12"` (Zertifikatspasswort über die Standardeingabe).

Zuerst `status --json`. Mit `--json`, und immer wenn die Ausgabe kein Terminal ist, kommt nur JSON auf die Standardausgabe. `archive --dry-run` zeigt den Plan und schreibt nichts. `archive` und `export` ohne Terminal brauchen `--yes`. Beides ist endgültig oder kostet einen Zeitstempel. Zugangsdaten gehören in den Anmeldeinformationsspeicher, nicht in den Aufruf.

```text
rechnungsarchiv status --json
rechnungsarchiv archive --dry-run <ordner>
rechnungsarchiv archive <ordner> --yes
rechnungsarchiv verify <archiv-oder-ablage>
rechnungsarchiv export --von <jahr> --bis <jahr> <ziel> --yes
```

| Code | Bedeutung |
|---|---|
| 0 | in Ordnung |
| 1 | allgemeiner Fehler |
| 2 | Aufruf fehlerhaft |
| 3 | Konfiguration oder kein Dienst eingerichtet |
| 4 | Archiv ist auf einem anderen PC gesperrt |
| 5 | Prüfung hat Verändert oder Ungültig ergeben |
| 6 | etwas ist ausstehend oder unbestimmt |
| 7 | Fehler beim Anbieter (Zugang, Guthaben, Netz) |
| 8 | Zustimmung fehlt (`--yes`) |

Weitere Befehle: `init`, `stamp`, `providers` (ohne Preise), `provider show` (nie das Passwort), `protocol <nummer>` (Archivprotokoll einer Ablage als PDF), `schema --json`. Nach `init` verwenden die Befehle dieses Archiv, solange weder `--archiv` angegeben ist noch der aktuelle Ordner ein Archiv ist.

## Rechtlicher Rahmen

Keine Rechtsberatung. Die tragenden Normen, soweit sie dieses Programm betreffen:

- **§ 131 BAO.** Auf Datenträgern gespeicherte Unterlagen müssen bis zum Ende der Frist inhaltsgleich, vollständig und geordnet wiedergegeben werden können (§ 131 Abs. 3 BAO). Der ursprüngliche Inhalt darf nicht unkenntlich gemacht werden (§ 131 Abs. 1 Z 6 lit. b BAO).
- **§ 132 BAO.** Bücher, Aufzeichnungen und Belege sind sieben Jahre aufzubewahren. Die Frist beginnt mit dem Ende des Kalenderjahres, auf das der Beleg sich bezieht. Weicht das Wirtschaftsjahr davon ab, beginnt sie mit dem Ende des Kalenderjahres, in dem das Wirtschaftsjahr endet. Schwebende Verfahren verlängern die Frist. Zehn Jahre sind keine gesetzliche Aufbewahrungsfrist.
- **§ 11 UStG und E-Rechnung-UStV.** Echtheit der Herkunft, Unversehrtheit des Inhalts und Lesbarkeit der Rechnung sind Pflichten des Unternehmens. Das Programm übernimmt sie nicht.
- **§ 18 Abs. 10 UStG.** Aufzeichnungen über Grundstücke sind 22 Jahre aufzubewahren, bestimmte Fälle nach dem Wohnungsgemeinnützigkeitsgesetz 12 Jahre.
- **Art. 41 eIDAS.** Nur der qualifizierte Zeitstempel trägt die Vermutung für Zeitpunkt und Unversehrtheit (Abs. 2). Die Vermutung gilt für den Prüfwert.
- **DSGVO.** Rechtsgrundlage der gesetzlich gebotenen Aufbewahrung ist Art. 6 Abs. 1 lit. c. Art. 5 Abs. 1 lit. e verlangt die Löschung nach Fristende. Eine Löschfunktion ist in dieser Version nicht enthalten. Beim Stempeln erhält der Anbieter den Prüfwert, den Kontonamen und die IP-Adresse. Das Programm verwendet nur Anbieter in der EU.
- Das Bundesministerium für Finanzen zertifiziert keine Archivierungssoftware.

Unterlagen dazu:

- [docs/rechtsgrundlagen.md](docs/rechtsgrundlagen.md) (in Arbeit)
- [docs/verfahrensdokumentation.md](docs/verfahrensdokumentation.md) (in Arbeit)
- [docs/fuer-pruefer.md](docs/fuer-pruefer.md) (in Arbeit)
- [docs/datenschutz.md](docs/datenschutz.md) (in Arbeit)

## Bekannte Grenzen

- Wird der Schlüssel des Anbieters später kompromittiert, lässt sich mit nur einem Zeitstempeldienst nicht ausschließen, dass ein Stempel rückdatiert wurde. Ein zweiter Stempel eines anderen Anbieters wäre die Gegenmaßnahme. Er ist in dieser Version nicht enthalten.
- Das Löschen der jeweils letzten Ablage erkennt das Programm nicht.
- Nur ein PC darf in das Archiv schreiben. Ein synchronisierter Ordner ist kein zulässiges Schreibziel.

## Häufige Fragen

**Was muss ich tun, und was kostet das?** Sobald es eine Veröffentlichung gibt: Programm installieren, ein Konto bei einem Dienst aus der Tabelle anlegen, den Archivordner wählen, dann „Ordner archivieren“. Die Software ist Apache-2.0. Die Zeitstempel zahlen Sie beim Anbieter. Die Beträge stehen in der Tabelle, netto, Stand 18.11.2026, ohne Gewähr.

**Brauche ich Internet?** Für den Zeitstempel ja, und für das Nachladen der EU-Vertrauenslisten. Die spätere Kontrolle gespeicherter Ablagen braucht den Anbieter nicht, wenn Zeitstempel, Zertifikatskette und Sperrstatus vorhanden sind. Fehlt eine Vertrauensliste, die nach dem Stempel ausgestellt wurde, bleibt offen, ob der Dienst zu diesem Zeitpunkt qualifiziert war, bis eine neuere Liste oder eine Online-Prüfung vorliegt.

**Was passiert, wenn der Anbieter pleitegeht?** Bestehende Zeitstempel bleiben mit den gespeicherten Prüfdaten prüfbar. Für neue Ablagen wählen Sie einen anderen qualifizierten EU-Anbieter.

**Kann ich danach die Papierrechnungen wegwerfen?** Erst nachdem Sie das mit Ihrer Steuerberaterin oder Ihrem Steuerberater geklärt haben. Der vorgesehene Ablauf steht in der [Verfahrensdokumentation](docs/verfahrensdokumentation.md): Scan prüfen, archivieren, den Zeitstempel abwarten, und erst dann das Papier vernichten.

**Ersetzt das meine Buchhaltung oder die Verfahrensdokumentation?** Nein. Buchhaltung, innerbetriebliches Kontrollverfahren und Verfahrensdokumentation bleiben bei Ihnen.

## Unterstützen & Danke

- ⭐ [Dieses Repo mit einem Stern markieren](https://github.com/realspqrk/rechnungsarchiv)
- 💖 [GitHub Sponsors](https://github.com/sponsors/realspqrk)
- ☕ [Ko-fi](https://ko-fi.com/spqrk)
- 💛 [PayPal](https://paypal.me/realSPQRK)

Danke an die offenen Standards und Projekte, auf denen das hier aufbaut: BagIt (Library of Congress, RFC 8493), RFC 3161, die EU-Vertrauenslisten der Europäischen Kommission und der Mitgliedstaaten, [digitorus/timestamp](https://github.com/digitorus/timestamp) (Go), [Wails](https://wails.io) und GoBDify für die Anregung.

## Für Entwickler

<details>
<summary><strong>Build, layout, and specification</strong></summary>

Go. Layout, rules, and UI wording: [AGENTS.md](AGENTS.md). Byte-level format: [docs/spec.md](docs/spec.md). Decisions: [docs/adr/](docs/adr/). Architecture: [docs/architecture.md](docs/architecture.md). CLI reference: [docs/cli.md](docs/cli.md).

The CLI and the GUI render one `verify.Report`. Business logic stays in `internal/`. The GUI is a thin Wails v3 shell. The toolchain in `go.mod` is go1.27.4 and is fetched automatically. Prices stay in this README only.

```sh
go build ./...                 # toolchain go1.27.4 is fetched automatically (go.mod)
go test ./...                  # unit + end-to-end (offline; network tests behind -tags online)
go test -tags online ./...     # also hits real timestamp services
go vet ./... && gofmt -l .
```

The verifier is security-critical. Missing validation material is `Unbestimmt`, never a positive result. An Ablage ZIP is never rewritten after commit.

</details>

## Lizenz & Haftung

Apache-2.0. Der Text steht in [LICENSE](LICENSE).

Für Sach- und Vermögensschäden aus leichter Fahrlässigkeit wird nicht gehaftet. Die Haftung für Vorsatz, grobe Fahrlässigkeit und Personenschäden bleibt bestehen. Keine Steuer- oder Rechtsberatung. Maßgeblich ist [docs/haftungsausschluss.md](docs/haftungsausschluss.md).
