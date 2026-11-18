package report

import (
	"fmt"
	"strings"

	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// countries names trusted-list territories in German.
var countries = map[string]string{
	"AT": "Österreich", "BE": "Belgien", "BG": "Bulgarien", "CY": "Zypern", "CZ": "Tschechien",
	"DE": "Deutschland", "DK": "Dänemark", "EE": "Estland", "EL": "Griechenland", "ES": "Spanien",
	"FI": "Finnland", "FR": "Frankreich", "HR": "Kroatien", "HU": "Ungarn", "IE": "Irland",
	"IT": "Italien", "LI": "Liechtenstein", "LT": "Litauen", "LU": "Luxemburg", "LV": "Lettland",
	"MT": "Malta", "NL": "Niederlande", "NO": "Norwegen", "PL": "Polen", "PT": "Portugal",
	"RO": "Rumänien", "SE": "Schweden", "SI": "Slowenien", "SK": "Slowakei", "IS": "Island",
}

func country(t *verify.Token) string {
	if t.TrustList == nil {
		return ""
	}
	if c, ok := countries[t.TrustList.Territory]; ok {
		return c
	}
	return t.TrustList.Territory
}

func dateien(n int) string {
	if n == 1 {
		return "1 Datei"
	}
	return fmt.Sprintf("%d Dateien", n)
}

// tsp is "<TSP> (<Land>)".
func tsp(t *verify.Token) string {
	s := nonEmpty(t.TSP, "unbekannter Zeitstempeldienst")
	if c := country(t); c != "" {
		s += " (" + c + ")"
	}
	return s
}

func nonEmpty(s, alt string) string {
	if s == "" {
		return alt
	}
	return s
}

// tokenValue is "Mensatek (Spanien), 05.11.2026, 09:14:22 Uhr MEZ (± 1 s)".
func tokenValue(t *verify.Token) string {
	return fmt.Sprintf("%s, %s (%s)", tsp(t), present.Exact(t.GenTime), present.Accuracy(t.AccuracyMS))
}

// qualificationValue states the trusted-list result at genTime (spec §6
// step 6), in the past tense.
func qualificationValue(t *verify.Token) string {
	switch t.Qualification {
	case verify.Qualified:
		return "qualifizierter Zeitstempeldienst nach eIDAS zum Zeitpunkt des Zeitstempels (laut EU-Vertrauensliste)"
	case verify.NotQualified:
		return "zum Zeitpunkt des Zeitstempels nicht als qualifizierter Zeitstempeldienst nach eIDAS eingetragen (laut EU-Vertrauensliste)"
	}
	return "nicht festgestellt: keine nach dem Zeitstempel ausgestellte oder bei der Kontrolle abgerufene EU-Vertrauensliste"
}

// checkValue is the file check: "18.11.2026: 64 von 64 Dateien stimmen …".
func checkValue(rep *verify.Report, d *verify.Deposit) string {
	return fmt.Sprintf("%s: %d von %s stimmen mit dem Dateiverzeichnis überein", present.Date(rep.CheckedAt), d.Counts.Matched, dateien(d.Counts.Files))
}

// provenValue is what the timestamp proves for the checked files.
func provenValue(p *verify.Proven, via string) string {
	if p.At != nil {
		return fmt.Sprintf("Dateistand bestand spätestens am %s (%s)", present.Time(*p.At), via)
	}
	return fmt.Sprintf("Dateistand laut %s vom %s; ohne Angabe der Genauigkeit kein spätester Zeitpunkt", via, present.Exact(p.GenTime))
}

const noTime = "kein Zeitpunkt belegt"

// Statement is the Feststellung (spec §10): short facts that say exactly
// what the verification established and nothing more.
func Statement(rep *verify.Report, d *verify.Deposit) []row {
	rs := []row{{"Ergebnis", present.Label(*d)}}
	add := func(k, v string) { rs = append(rs, row{k, v}) }
	content := fmt.Sprintf("%s, %s", dateien(d.Counts.Files), present.Size(d.Counts.Bytes))
	switch d.Status {
	case verify.StatusChanged, verify.StatusInvalid:
		add("Inhalt laut Dateiverzeichnis", content)
	default:
		add("Inhalt", content)
	}
	if d.Root != "" {
		add("Prüfwert", d.Root)
	}
	// inherited states the successor's timestamp that covers this Ablage.
	inherited := func(p *verify.Proven) {
		via := "Zeitstempel der Ablage " + present.Number(p.Via)
		add("Verkettung", fmt.Sprintf("Ablage %s schließt diesen Prüfwert ein", present.Number(p.Via)))
		if succ := find(rep, p.Via); succ != nil && succ.Token != nil {
			add("Zeitstempel der Ablage "+present.Number(p.Via), tokenValue(succ.Token))
			add("Qualifikation", qualificationValue(succ.Token))
		}
		add("Kontrolle", checkValue(rep, d))
		add("Belegt", provenValue(p, via))
	}

	switch d.Status {
	case verify.StatusTimestamped:
		add("Zeitstempel", tokenValue(d.Token))
		add("Qualifikation", qualificationValue(d.Token))
		add("Kontrolle", checkValue(rep, d))
		add("Belegt", provenValue(d.Proven, "Zeitstempel"))

	case verify.StatusPending:
		v := "kein gültiger"
		if d.Reason != "" {
			v += " (Grund: " + present.Reason(d.Reason) + ")"
		}
		add("Zeitstempel", v)
		if p := d.Proven; p != nil && p.Via != 0 {
			inherited(p)
		} else {
			add("Kontrolle", checkValue(rep, d))
			add("Belegt", noTime)
		}

	case verify.StatusIndeterminate:
		if t := d.Token; t != nil && t.Compromised {
			add("Zeitstempel", fmt.Sprintf("%s, %s", tsp(t), present.Exact(t.GenTime)))
			add("Sperrstatus", fmt.Sprintf("Zertifikat des Dienstes oder einer übergeordneten Zertifizierungsstelle widerrufen (Grund: %s); nachträgliche Erstellung des Zeitstempels nicht ausgeschlossen", revocationReason(t)))
		} else {
			add("Zeitstempel", "vorhanden, nicht geprüft (keine geprüfte EU-Vertrauensliste)")
		}
		if p := d.Proven; p != nil && p.Via != 0 {
			inherited(p)
		} else {
			add("Kontrolle", checkValue(rep, d))
			add("Belegt", noTime)
		}

	case verify.StatusChanged:
		v := checkValue(rep, d)
		var parts []string
		if n := d.Counts.Changed; n > 0 {
			parts = append(parts, dateien(n)+" verändert")
		}
		if n := d.Counts.Missing; n > 0 {
			parts = append(parts, dateien(n)+plural(n, " fehlt", " fehlen"))
		}
		if n := d.Counts.Added; n > 0 {
			parts = append(parts, dateien(n)+" hinzugekommen")
		}
		if len(parts) > 0 {
			v += "; " + strings.Join(parts, ", ")
		}
		add("Kontrolle", v)
		for _, p := range d.Problems {
			if p.Step == 2 {
				add("Begleitdateien", "weichen vom Prüfwert-Verzeichnis ab")
				break
			}
		}
		add("Belegt", noTime+" (für den vorliegenden Stand)")

	case verify.StatusInvalid:
		msg := ""
		for _, p := range d.Problems {
			if p.Status == verify.StatusInvalid {
				msg = p.Message
				break
			}
		}
		if msg != "" {
			add("Befund", strings.TrimSuffix(msg, "."))
		} else {
			add("Zeitstempel", "besteht die kryptografische Prüfung nicht")
		}
		add("Belegt", noTime)
	}
	return rs
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func revocationReason(t *verify.Token) string {
	for _, r := range t.Revocation {
		if r.Status != "revoked" {
			continue
		}
		switch r.Reason {
		case 1:
			return "Kompromittierung des Schlüssels"
		case 2:
			return "Kompromittierung der Zertifizierungsstelle"
		case 0:
			return "nicht angegeben"
		}
	}
	return "nicht angegeben"
}

func find(rep *verify.Report, seq int) *verify.Deposit {
	for i := range rep.Deposits {
		if rep.Deposits[i].Sequence == seq {
			return &rep.Deposits[i]
		}
	}
	return nil
}
