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

// issuer is "von <TSP> (<Land>)".
func issuer(t *verify.Token) string {
	s := "von " + nonEmpty(t.TSP, "einem Zeitstempeldienst")
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

// tokenSentence states the token and its qualification (spec §6 step 6).
func tokenSentence(t *verify.Token, subject string) string {
	s := fmt.Sprintf("Für %s besteht ein Zeitstempel %s vom %s (%s); ", subject, issuer(t), present.Exact(t.GenTime), present.Accuracy(t.AccuracyMS))
	switch t.Qualification {
	case verify.Qualified:
		s += "der Dienst war laut EU-Vertrauensliste zu diesem Zeitpunkt ein qualifizierter Zeitstempeldienst nach der eIDAS-Verordnung."
	case verify.NotQualified:
		s += "der Dienst war laut EU-Vertrauensliste zu diesem Zeitpunkt nicht als qualifizierter Zeitstempeldienst nach der eIDAS-Verordnung eingetragen."
	default:
		s += "ob der Dienst zu diesem Zeitpunkt ein qualifizierter Zeitstempeldienst nach der eIDAS-Verordnung war, ist mit den vorliegenden EU-Vertrauenslisten nicht festgestellt, weil keine nach dem Zeitstempel ausgestellte oder bei der Kontrolle abgerufene Liste vorliegt."
	}
	return s
}

// matchedSentence is the result of the file check against a proven time.
func matchedSentence(rep *verify.Report, d *verify.Deposit, p *verify.Proven, which string) string {
	s := fmt.Sprintf("Bei der Kontrolle am %s stimmten alle %s im Dateiverzeichnis mit dem Stand überein, ", present.Date(rep.CheckedAt), dateien(d.Counts.Files))
	if p.At != nil {
		return s + fmt.Sprintf("den %s spätestens für den %s belegt.", which, present.Time(*p.At))
	}
	return s + fmt.Sprintf("für den %s den %s angibt; eine Genauigkeit ist nicht angegeben, ein spätester Zeitpunkt wird daher nicht genannt.", which, present.Exact(p.GenTime))
}

// Statement is the Feststellung (spec §10): a factual paragraph that says
// exactly what the verification established and nothing more.
func Statement(rep *verify.Report, d *verify.Deposit) string {
	var b []string
	intact := d.Status != verify.StatusChanged && d.Status != verify.StatusInvalid
	head := fmt.Sprintf("Ablage %s „%s“", present.Number(d.Sequence), d.Name)
	if intact {
		b = append(b, fmt.Sprintf("%s enthält %s (%s).", head, dateien(d.Counts.Files), present.Size(d.Counts.Bytes)))
	} else {
		b = append(b, fmt.Sprintf("Das Dateiverzeichnis der %s nennt %s (%s).", head, dateien(d.Counts.Files), present.Size(d.Counts.Bytes)))
	}
	if d.Root != "" {
		b = append(b, "Ihr Prüfwert ist "+d.Root+".")
	}
	checked := fmt.Sprintf("Bei der Kontrolle am %s stimmten alle %s im Dateiverzeichnis mit dem Inhalt der Ablage überein.", present.Date(rep.CheckedAt), dateien(d.Counts.Files))

	switch d.Status {
	case verify.StatusTimestamped:
		b = append(b, tokenSentence(d.Token, "diesen Prüfwert"))
		b = append(b, matchedSentence(rep, d, d.Proven, "der Zeitstempel"))

	case verify.StatusPending:
		s := "Für diesen Prüfwert liegt noch kein gültiger Zeitstempel vor"
		if d.Reason != "" {
			s += " (Grund: " + present.Reason(d.Reason) + ")"
		}
		b = append(b, s+".")
		if p := d.Proven; p != nil && p.Via != 0 {
			succ := find(rep, p.Via)
			b = append(b, fmt.Sprintf("Über die Verkettung der Ablagen schließt Ablage %s diesen Prüfwert ein.", present.Number(p.Via)))
			if succ != nil && succ.Token != nil {
				b = append(b, tokenSentence(succ.Token, "Ablage "+present.Number(p.Via)))
			}
			b = append(b, matchedSentence(rep, d, p, "der Zeitstempel der Ablage "+present.Number(p.Via)))
		} else {
			b = append(b, checked, "Einen Zeitpunkt belegt diese Kontrolle nicht.")
		}

	case verify.StatusIndeterminate:
		if t := d.Token; t != nil && t.Compromised {
			b = append(b, fmt.Sprintf("Für diesen Prüfwert besteht ein Zeitstempel %s vom %s.", issuer(t), present.Exact(t.GenTime)))
			b = append(b, fmt.Sprintf("Laut Sperrinformation ist das Zertifikat des Dienstes oder einer übergeordneten Zertifizierungsstelle widerrufen (Grund: %s). Damit ist nicht ausgeschlossen, dass der Zeitstempel nachträglich erstellt wurde; sein Zeitpunkt ist nicht belegt.", revocationReason(t)))
		} else {
			b = append(b, "Für diesen Prüfwert liegt ein Zeitstempel vor, der mangels geprüfter EU-Vertrauensliste nicht geprüft werden konnte; sein Zeitpunkt ist nicht belegt.")
		}
		if p := d.Proven; p != nil && p.Via != 0 {
			b = append(b, fmt.Sprintf("Über die Verkettung der Ablagen schließt Ablage %s diesen Prüfwert ein.", present.Number(p.Via)))
			b = append(b, matchedSentence(rep, d, p, "der Zeitstempel der Ablage "+present.Number(p.Via)))
		} else {
			b = append(b, checked)
		}

	case verify.StatusChanged:
		var parts []string
		if n := d.Counts.Changed; n > 0 {
			parts = append(parts, fmt.Sprintf("%s verändert", dateien(n)))
		}
		if n := d.Counts.Missing; n > 0 {
			parts = append(parts, fmt.Sprintf("%s fehlen", dateien(n)))
		}
		if n := d.Counts.Added; n > 0 {
			parts = append(parts, fmt.Sprintf("%s hinzugekommen", dateien(n)))
		}
		s := fmt.Sprintf("Bei der Kontrolle am %s stimmten %d von %s im Dateiverzeichnis mit dem Inhalt der Ablage überein", present.Date(rep.CheckedAt), d.Counts.Matched, dateien(d.Counts.Files))
		if len(parts) > 0 {
			s += "; " + strings.Join(parts, ", ")
		}
		b = append(b, s+".")
		for _, p := range d.Problems {
			if p.Step == 2 {
				b = append(b, "Die Steuerdateien der Ablage weichen von ihrem Prüfwert-Verzeichnis ab.")
				break
			}
		}
		b = append(b, "Für den vorliegenden Stand der Ablage ist kein Zeitpunkt belegt.")

	case verify.StatusInvalid:
		msg := ""
		for _, p := range d.Problems {
			if p.Status == verify.StatusInvalid {
				msg = p.Message
				break
			}
		}
		if msg != "" {
			b = append(b, "Die Ablage ist ungültig: "+strings.TrimSuffix(msg, ".")+".")
		} else {
			b = append(b, "Der gespeicherte Zeitstempel zu diesem Prüfwert besteht die kryptografische Prüfung nicht.")
		}
		b = append(b, "Ein Zeitpunkt ist nicht belegt.")
	}
	return strings.Join(b, " ")
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
