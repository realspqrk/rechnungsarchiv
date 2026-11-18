// Package present turns verify results into German text for the CLI, the
// GUI and the Archivprotokoll. It holds wording only; every fact comes from
// verify.Report.
package present

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Europe/Vienna on PCs without a Go installation

	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Vienna is the time zone all times are shown in.
var Vienna = mustLoad("Europe/Vienna")

func mustLoad(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

// Zone returns the German zone name (MEZ/MESZ) and the UTC offset.
func Zone(t time.Time) (name, offset string) {
	lt := t.In(Vienna)
	abbr, sec := lt.Zone()
	switch abbr {
	case "CET":
		abbr = "MEZ"
	case "CEST":
		abbr = "MESZ"
	}
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	return abbr, fmt.Sprintf("UTC%s%02d:%02d", sign, sec/3600, sec%3600/60)
}

// Time is "05.11.2026, 09:14:22 Uhr MEZ".
func Time(t time.Time) string {
	z, _ := Zone(t)
	return t.In(Vienna).Format("02.01.2006, 15:04:05") + " Uhr " + z
}

// TimeFull is Time with the UTC offset: "05.11.2026, 09:14:22 Uhr MEZ (UTC+01:00)".
func TimeFull(t time.Time) string {
	_, off := Zone(t)
	return Time(t) + " (" + off + ")"
}

// Date is "05.11.2026" in Vienna.
func Date(t time.Time) string { return t.In(Vienna).Format("02.01.2006") }

// ISODate turns "2026-11-05" into "05.11.2026"; other input is returned as is.
func ISODate(s string) string {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return s
	}
	return t.Format("02.01.2006")
}

// Number is the four-digit Ablage number.
func Number(seq int) string { return fmt.Sprintf("%04d", seq) }

// Size is "31,4 MB" (binary units, as Windows Explorer shows them).
func Size(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d Byte", n)
	case n < 1024*1024:
		return decimal(float64(n)/1024) + " KB"
	case n < 1024*1024*1024:
		return decimal(float64(n)/(1024*1024)) + " MB"
	}
	return decimal(float64(n)/(1024*1024*1024)) + " GB"
}

func decimal(f float64) string { return strings.Replace(fmt.Sprintf("%.1f", f), ".", ",", 1) }

// Status is the German status word.
func Status(s verify.Status) string {
	switch s {
	case verify.StatusTimestamped:
		return "Zeitgestempelt"
	case verify.StatusPending:
		return "Ausstehend"
	case verify.StatusIndeterminate:
		return "Unbestimmt"
	case verify.StatusChanged:
		return "Verändert"
	case verify.StatusInvalid:
		return "Ungültig"
	}
	return string(s)
}

// Qualification is "qualifiziert", "nicht qualifiziert" or "unbestätigt".
func Qualification(q verify.Qualification) string {
	switch q {
	case verify.Qualified:
		return "qualifiziert"
	case verify.NotQualified:
		return "nicht qualifiziert"
	}
	return "unbestätigt"
}

// Reason is the Ausstehend reason as shown in the UI.
func Reason(r verify.Reason) string {
	switch r {
	case verify.ReasonNoProvider:
		return "kein Dienst eingerichtet"
	case verify.ReasonOffline:
		return "offline"
	case verify.ReasonAccess:
		return "Zugangsdaten fehlen oder falsch"
	case verify.ReasonQuota:
		return "Guthaben aufgebraucht"
	case verify.ReasonMaterial:
		return "Prüfdaten fehlen"
	case verify.ReasonProvider:
		return "Anbieterfehler"
	}
	return "noch kein Zeitstempel"
}

// Label is the status with its qualifier: "Zeitgestempelt · qualifiziert",
// "Ausstehend · offline".
func Label(d verify.Deposit) string {
	switch d.Status {
	case verify.StatusTimestamped:
		return Status(d.Status) + " · " + Qualification(d.Qualification)
	case verify.StatusPending:
		return Status(d.Status) + " · " + Reason(d.Reason)
	}
	return Status(d.Status)
}

// Proven is "spätestens 05.11.2026, 09:14:23 Uhr MEZ (über Ablage 0012)".
func Proven(p *verify.Proven) string {
	if p == nil {
		return ""
	}
	var s string
	if p.At != nil {
		s = "spätestens " + Time(*p.At)
	} else {
		s = "Zeitstempel vom " + Time(p.GenTime) + " (Genauigkeit nicht angegeben)"
	}
	if p.Via != 0 {
		s += " (über Ablage " + Number(p.Via) + ")"
	}
	return s
}

// Accuracy is "± 1 s" or "nicht angegeben".
func Accuracy(ms *int64) string {
	if ms == nil {
		return "nicht angegeben"
	}
	if *ms%1000 == 0 {
		return fmt.Sprintf("± %d s", *ms/1000)
	}
	return fmt.Sprintf("± %d ms", *ms)
}

// Finding describes an archive-level finding.
func Finding(f verify.Finding) string {
	n := Number(f.Sequence)
	switch f.Code {
	case verify.FindingGap:
		if f.To > f.Sequence {
			return fmt.Sprintf("Lücke: Ablagen %s bis %s fehlen", n, Number(f.To))
		}
		return "Lücke: Ablage " + n + " fehlt"
	case verify.FindingDuplicateNumber:
		return "Nummer doppelt vergeben: Ablage " + n
	case verify.FindingArchiveIDMismatch:
		return "Ablage " + n + " gehört zu einem anderen Archiv"
	case verify.FindingPreviousMismatch:
		return fmt.Sprintf("Ablage %s verweist nicht auf den Prüfwert von Ablage %s", n, Number(f.Sequence-1))
	case verify.FindingPredecessorNotInExport:
		return "Vorgänger von Ablage " + n + " nicht im Export"
	case verify.FindingBridgeInvalid:
		return "Brückendaten für Ablage " + n + " stimmen nicht: " + f.Detail
	case verify.FindingFileNameMismatch:
		return "Dateiname passt nicht zur Nummer: " + f.Detail
	}
	return string(f.Code)
}

// Bridged is "Ablage 0003: nicht im Export".
func Bridged(b verify.Bridge) string { return "Ablage " + Number(b.Sequence) + ": nicht im Export" }
