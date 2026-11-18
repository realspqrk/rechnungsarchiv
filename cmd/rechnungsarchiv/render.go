package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Human output: German, aligned, short. Every fact comes from the result
// that the JSON mode prints; nothing is derived here.

func table(w io.Writer) *tabwriter.Writer { return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0) }

func sourceText(s string) string {
	switch s {
	case "cwd":
		return " (aktueller Ordner)"
	case "default":
		return " (Standardarchiv)"
	}
	return ""
}

func authText(auth string) string {
	if auth == tsa.AuthClientCert {
		return "Client-Zertifikat"
	}
	return "Benutzer/Passwort"
}

func yesNo(b bool) string {
	if b {
		return "ja"
	}
	return "nein"
}

func renderStatus(w io.Writer, s *statusOut) {
	t := table(w)
	fmt.Fprintf(t, "Archiv\t%s%s\n", s.Archive.Path, sourceText(s.Archive.Source))
	fmt.Fprintf(t, "Firma\t%s\n", s.Archive.Company)
	switch {
	case s.Provider != nil:
		cred := "unbekannt"
		if s.Credentials != nil {
			cred = yesNo(*s.Credentials)
		}
		fmt.Fprintf(t, "Zeitstempel-Dienst\t%s (%s), Zugangsdaten auf diesem PC: %s\n", s.Provider.ID, s.Provider.Name, cred)
	case s.Archive.Provider != nil:
		fmt.Fprintf(t, "Zeitstempel-Dienst\t%s (unbekannt)\n", *s.Archive.Provider)
	default:
		fmt.Fprintf(t, "Zeitstempel-Dienst\tkeiner eingerichtet\n")
	}
	fmt.Fprintf(t, "Ablagen\t%d (%d zeitgestempelt, %d ausstehend", s.Counts.Total, s.Counts.Timestamped, s.Counts.Pending)
	if s.Counts.Unreadable > 0 {
		fmt.Fprintf(t, ", %d nicht lesbar", s.Counts.Unreadable)
	}
	fmt.Fprintln(t, ")")
	for _, d := range s.Pending {
		label := "ausstehend"
		if d.Status == verify.StatusIndeterminate {
			label = "unbestimmt (Schlüssel kompromittiert)"
		}
		fmt.Fprintf(t, "  %s\t%s %s\n", label, present.Number(d.Sequence), d.Name)
	}
	for _, d := range s.Unreadable {
		fmt.Fprintf(t, "  nicht lesbar\t%s: %s\n", d.File, d.Error)
	}
	if s.TrustIssued != nil {
		fmt.Fprintf(t, "Vertrauensliste\tausgestellt %s\n", present.Date(*s.TrustIssued))
	} else {
		fmt.Fprintf(t, "Vertrauensliste\tkeine geprüfte Liste\n")
	}
	if s.Lock != nil {
		since := s.Lock.Since
		if ts, err := time.Parse(time.RFC3339, since); err == nil {
			since = present.Time(ts)
		}
		fmt.Fprintf(t, "Sperre\t%s (%s), seit %s\n", s.Lock.Host, s.Lock.User, since)
	}
	t.Flush()
	for _, n := range s.Next {
		fmt.Fprintln(w, "Nächster Schritt: "+n)
	}
}

func renderPlan(w io.Writer, p *planOut) {
	fmt.Fprintln(w, "Probelauf – es wird nichts geschrieben.")
	t := table(w)
	fmt.Fprintf(t, "Archiv\t%s\n", p.Archive)
	fmt.Fprintf(t, "Ordner\t%s\n", p.Folder)
	fmt.Fprintf(t, "Neue Ablage\t%s\n", p.File)
	fmt.Fprintf(t, "Belegjahr\t%d\n", p.Belegjahr)
	fmt.Fprintf(t, "Aufbewahrung\t%d Jahre, gewöhnlich bis %s\n", p.RetentionYears, present.ISODate(p.RetentionUntil))
	fmt.Fprintf(t, "Dateien\t%d (%s)\n", p.Count, present.Size(p.Bytes))
	if len(p.Skipped) > 0 {
		fmt.Fprintf(t, "Übersprungen\t%d Systemdateien\n", len(p.Skipped))
	}
	t.Flush()
	for _, f := range p.Files {
		fmt.Fprintf(w, "  %s  %s\n", f.Path, present.Size(f.Size))
	}
	for _, n := range p.Notes {
		fmt.Fprintln(w, "Hinweis: "+n)
	}
	fmt.Fprintln(w, "Archivieren mit: "+archiveCommand(p))
}

// archiveCommand is the command that archives exactly this plan: the
// resolved archive and every resolved option, quoted for PowerShell, so a
// different current folder or default archive cannot change it.
func archiveCommand(p *planOut) string {
	return fmt.Sprintf("rechnungsarchiv archive %s --archiv %s --name %s --belegjahr %d --aufbewahrung %d --yes",
		present.PSQuote(p.Folder), present.PSQuote(p.Archive), present.PSQuote(p.Name), p.Belegjahr, p.RetentionYears)
}

func renderArchived(w io.Writer, o *archiveOut) {
	d := o.Deposit
	fmt.Fprintf(w, "Ablage gespeichert: %s (%d Dateien, %s)\n", d.File, d.Count, present.Size(d.Bytes))
	if o.Stamp != nil {
		renderStamp(w, o.Stamp)
	}
}

func renderStamp(w io.Writer, r *stamp.Result) {
	if len(r.Outcomes) == 0 {
		fmt.Fprintln(w, "Keine fehlenden Zeitstempel.")
		return
	}
	t := table(w)
	for _, o := range r.Outcomes {
		switch {
		case o.Stamped && o.GenTime != nil:
			fmt.Fprintf(t, "%s %s\tZeitstempel vom %s\n", present.Number(o.Sequence), o.Name, present.Time(*o.GenTime))
		case o.Reason == "":
			fmt.Fprintf(t, "%s %s\t%s\n", present.Number(o.Sequence), o.Name, o.Message)
		default:
			fmt.Fprintf(t, "%s %s\tAusstehend · %s: %s\n", present.Number(o.Sequence), o.Name, present.Reason(o.Reason), o.Message)
		}
	}
	t.Flush()
}

func renderReport(w io.Writer, r *verify.Report) {
	head := r.Path
	if r.Company != "" {
		head += " (" + r.Company + ")"
	}
	fmt.Fprintln(w, "Kontrolle: "+head)
	t := table(w)
	for _, d := range r.Deposits {
		line := fmt.Sprintf("%s %s\t%s", present.Number(d.Sequence), d.Name, present.Label(d))
		if d.Proven != nil {
			line += "\t" + present.Proven(d.Proven)
		}
		fmt.Fprintln(t, line)
		c := d.Counts
		if c.Changed+c.Missing+c.Added > 0 {
			fmt.Fprintf(t, "\t%d verändert, %d fehlen, %d hinzugefügt (von %d Dateien)\n", c.Changed, c.Missing, c.Added, c.Files)
		}
		for _, p := range d.Problems {
			fmt.Fprintf(t, "\t%s\n", p.Message)
		}
	}
	for _, b := range r.Bridged {
		fmt.Fprintln(t, present.Bridged(b))
	}
	t.Flush()
	for _, f := range r.Findings {
		fmt.Fprintln(w, "Befund: "+present.Finding(f))
	}
	for _, p := range r.TrustProblems {
		fmt.Fprintln(w, "Vertrauensliste: "+p)
	}
	for _, d := range r.Deposits {
		if t := d.Token; t != nil && t.Qualification == verify.Unconfirmed {
			why := present.UnconfirmedReason(t)
			note := "Hinweis: Qualifikation unbestätigt. " + strings.ToUpper(why[:1]) + why[1:] + "."
			if !r.Online {
				note += " Mit --online lädt die Kontrolle die aktuelle Liste."
			}
			fmt.Fprintln(w, note)
			break
		}
	}
	n := len(r.Deposits)
	switch verifyExit(r) {
	case exitOK:
		fmt.Fprintf(w, "Ergebnis: in Ordnung (%d Ablagen zeitgestempelt).\n", n)
	case exitFound:
		fmt.Fprintf(w, "Ergebnis: %s – betroffene Ablagen aus der Sicherung wiederherstellen.\n", worstText(r))
	default:
		fmt.Fprintf(w, "Ergebnis: %s – kein positives Ergebnis.\n", present.Status(r.Status))
	}
}

func worstText(r *verify.Report) string {
	if r.Status == verify.StatusInvalid || r.Status == verify.StatusChanged {
		return present.Status(r.Status)
	}
	return "Befund der Kette"
}

func renderExport(w io.Writer, r *export.Result) {
	t := table(w)
	fmt.Fprintf(t, "Export geschrieben\t%s\n", r.Dir)
	fmt.Fprintf(t, "Ablagen\t%s\n", numbers(r.Exported))
	if len(r.Bridged) > 0 {
		fmt.Fprintf(t, "Brückendaten\t%s\n", numbers(r.Bridged))
	}
	if r.Report != nil {
		fmt.Fprintf(t, "Kontrolle des Exports\t%s\n", present.Status(r.Report.Status))
	}
	t.Flush()
	fmt.Fprintln(w, `Prüfen: im Exportordner .\rechnungsarchiv.exe verify `+present.PSQuote(r.Dir))
}

func renderProviders(w io.Writer, ps []providerInfo) {
	t := table(w)
	fmt.Fprintln(t, "ID\tAnbieter\tLand\tZugang")
	for _, p := range ps {
		auth := authText(p.Auth)
		if p.URLFromContract {
			auth += ", Adresse laut Vertrag"
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", p.ID, p.Name, p.Country, auth)
	}
	t.Flush()
	fmt.Fprintln(w, "Preise: siehe README (Zeitstempel-Dienste). Einrichten: rechnungsarchiv provider set <id> …")
}

func renderProvider(w io.Writer, o *providerOut, verb string) {
	t := table(w)
	fmt.Fprintf(t, "Archiv\t%s\n", o.Archive)
	if o.Provider == nil {
		fmt.Fprintf(t, "Zeitstempel-Dienst\tkeiner eingerichtet\n")
		t.Flush()
		return
	}
	name := o.Provider.ID
	if o.Provider.Name != "" {
		name += " (" + o.Provider.Name + ")"
	}
	if verb != "" {
		name += " – " + verb
	}
	fmt.Fprintf(t, "Zeitstempel-Dienst\t%s\n", name)
	if c := o.Credentials; c != nil {
		fmt.Fprintf(t, "Zugangsdaten auf diesem PC\t%s\n", yesNo(c.Stored))
		var parts []string
		if c.User != "" {
			parts = append(parts, "Benutzer "+c.User)
		}
		if c.P12 != "" {
			parts = append(parts, "Zertifikat "+c.P12)
		}
		if c.URL != "" {
			parts = append(parts, "Adresse "+c.URL)
		}
		if c.Password {
			parts = append(parts, "Passwort gespeichert")
		}
		if len(parts) > 0 {
			fmt.Fprintf(t, "\t%s\n", strings.Join(parts, ", "))
		}
	}
	t.Flush()
}
