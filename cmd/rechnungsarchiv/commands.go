package main

import (
	"errors"
	"fmt"
	"github.com/realspqrk/rechnungsarchiv"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/export"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/report"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// command is one entry of the command table. help, schema and the parser
// read the same table.
type command struct {
	Name    string
	Args    string // e.g. "<ordner>"
	MinArgs int
	MaxArgs int
	Summary string // German, one line
	Help    string // German, details
	Flags   []flagSpec
	Writes  bool // changes the archive or writes files
	Consent bool // needs --yes without a terminal (exit 8)
	// Effects and Network say in German what the command writes and what
	// it sends ("nichts", "keines" when nothing). Help and schema print
	// them; every command sets both.
	Effects string
	Network string
	// Output is the zero value of what the command prints as JSON on
	// stdout (schema: commands[].output); nil: nothing (gui).
	Output any
	// NotGlobal are global flags this command refuses (gui: --archiv,
	// --json). Its help leaves them out; the schema names them.
	NotGlobal []string
	run       func(a *app, inv *invocation) error
}

var (
	flagYes = flagSpec{Name: "yes", Kind: boolFlag, Help: "Zustimmung des Benutzers liegt vor (auch -y); ohne Terminal Pflicht"}
	// stamp needs no consent of its own (schema: consent false); --yes only
	// confirms a --take-over.
	flagYesTakeOver = flagSpec{Name: "yes", Kind: boolFlag, Help: "Zustimmung des Benutzers liegt vor (auch -y); nur zusammen mit --take-over nötig"}
	flagTakeOver    = flagSpec{Name: "take-over", Kind: boolFlag, Help: "Sperre eines anderen PCs übernehmen, die nach einem Absturz hängt (nur mit --yes, nach Rückfrage)"}
	flagOnline      = flagSpec{Name: "online", Kind: boolFlag, Help: "Vertrauenslisten und Sperrstatus zusätzlich online abfragen"}
)

var commands []*command

func init() {
	commands = []*command{
		{Name: "status", Summary: "Zustand des Archivs: Anbieter, ausstehende Ablagen, Vertrauensliste, Sperre. Agenten rufen das zuerst auf.",
			Help:    "Liest nur. Ausstehend heißt hier: noch kein gültiger Zeitstempel gespeichert; die vollständige Kontrolle macht verify.",
			Effects: "nichts", Network: "keines", Output: statusOut{},
			run: (*app).status},
		{Name: "init", Args: "<ordner>", MinArgs: 1, MaxArgs: 1, Writes: true,
			Summary: "Neues Archiv anlegen und als Standardarchiv merken.",
			Flags:   []flagSpec{{Name: "firma", Kind: stringFlag, Arg: "<name>", Help: "Firmenname (Pflicht)"}},
			Effects: "den neuen Archivordner; merkt ihn in settings.json als Standardarchiv (ein bisheriges Archiv bleibt unverändert). Misslingt das Merken: Warnung und \"default\": false.",
			Network: "keines", Output: initOut{},
			run: (*app).init},
		{Name: "archive", Args: "<ordner>", MinArgs: 1, MaxArgs: 1, Writes: true, Consent: true,
			Summary: "Ordner archivieren: neue Ablage anlegen, endgültig. Mit eingerichtetem Dienst wird ihr Zeitstempel geholt (kostet einen); ohne Dienst bleibt sie ausstehend, ohne Netz.",
			Help: "Die Dateien werden unverändert in eine neue Ablage übernommen. Mit eingerichtetem Dienst wird danach der Zeitstempel dieser Ablage geholt (kostet einen); ohne Dienst bleibt sie ausstehend, und nichts geht ins Netz. Ältere ausstehende holt rechnungsarchiv stamp.\n" +
				"Steht \"committed\": true in der Ausgabe, ist die Ablage gespeichert, auch wenn der Exit-Code nicht 0 ist (etwa 3, 6 oder 7). Nicht erneut archivieren; nach der Behebung rechnungsarchiv stamp --archiv <archiv>. " +
				"Fehlt die Ausgabe (Abbruch, Zeitüberschreitung), vor einem neuen Versuch mit rechnungsarchiv verify --archiv <archiv> prüfen, ob die Ablage schon gespeichert ist.",
			Effects: "mit --dry-run nichts. Sonst eine neue Ablage (ZIP und Nachweis) und, mit Dienst, die Vertrauenslisten im Archiv. Vorhandene Ablagen bleiben unverändert.",
			Network: "mit --dry-run oder ohne Dienst keines. Sonst die Vertrauenslisten, dann ein Zeitstempel für diese Ablage (kostet), danach Aussteller-Zertifikat und Sperrstatus der Antwort.",
			Output:  oneOf{planOut{DryRun: true}, archiveOut{}},
			Flags: []flagSpec{
				{Name: "name", Kind: stringFlag, Arg: "<name>", Help: "Name der Ablage (Standard: Ordnername)"},
				{Name: "belegjahr", Kind: intFlag, Arg: "<jahr>", Help: "Belegjahr (Standard: Jahreszahl im Ordnernamen)"},
				{Name: "aufbewahrung", Kind: intFlag, Arg: "<jahre>", Help: "Aufbewahrungsfrist 7, 10, 12 oder 22 Jahre (Standard: Archiv-Einstellung)"},
				{Name: "dry-run", Kind: boolFlag, Help: "nur den Plan zeigen, nichts schreiben"},
				flagYes, flagTakeOver,
			},
			run: (*app).archive},
		{Name: "stamp", Writes: true, Summary: "Fehlende Zeitstempel holen.",
			Effects: "Nachweise ausstehender Ablagen; mit Dienst die Vertrauenslisten im Archiv, auch wenn nichts aussteht.",
			Network: "ohne Dienst keines. Sonst immer die Vertrauenslisten; je ausstehender Ablage eine Anfrage (kostet je einen Zeitstempel, stamp fragt nicht nach). Ausnahme: einer schon angenommenen Antwort fehlt nur Prüfmaterial; sie wird binnen 24 Stunden ohne neue Anfrage vervollständigt (Aussteller-Zertifikat, Sperrstatus).",
			Output:  stamp.Result{},
			Flags:   []flagSpec{flagYesTakeOver, flagTakeOver}, run: (*app).stamp},
		{Name: "verify", Args: "[<archiv>|<ablage.zip>|<export-ordner>]", MaxArgs: 1,
			Summary: "Archiv, Export oder einzelne Ablage kontrollieren. Schreibt nie ins Archiv.",
			Help:    "Ohne Pfad: das Archiv (--archiv, aktueller Ordner oder Standardarchiv). Exit 0 in Ordnung, 5 Verändert/Ungültig oder Befund der Kette, 6 Ausstehend/Unbestimmt.",
			Effects: "nichts im Archiv; mit --online neue Vertrauenslisten in den Cache %TEMP%\\rechnungsarchiv\\vertrauenslisten.",
			Network: "ohne --online keines. Mit --online Vertrauenslisten und Sperrstatus, ohne Prüfwert.",
			Output:  verify.Report{},
			Flags:   []flagSpec{flagOnline}, run: (*app).verify},
		{Name: "export", Args: "<ziel>", MinArgs: 1, MaxArgs: 1, Writes: true, Consent: true,
			Summary: "Export für Betriebsprüfung in einen neuen, leeren Ordner schreiben.",
			Flags: []flagSpec{
				{Name: "von", Kind: intFlag, Arg: "<jahr>", Help: "erstes Belegjahr (Pflicht)"},
				{Name: "bis", Kind: intFlag, Arg: "<jahr>", Help: "letztes Belegjahr (Pflicht)"},
				flagYes,
			},
			Help: "Exit 0, sobald der Export geschrieben ist. Ob er in Ordnung ist, sagen report.status und report.findings in der Ausgabe (predecessor_not_in_export ist neutral); der Export prüft dabei online, wenn die Listen aktualisiert werden konnten. Eine spätere Kontrolle: rechnungsarchiv verify --online \"<ziel>\" (ohne --online nur mit den gespeicherten Nachweisen). " +
				"Misslingt das Aktualisieren der Vertrauenslisten, nimmt der Export die gespeicherten (Hinweis auf der Fehlerausgabe).",
			Effects: "den neuen Zielordner; das Archiv wird nur gelesen. Neue Vertrauenslisten in den Cache und in den Export.",
			Network: "Vertrauenslisten; gelingt das, prüft der Export online (Sperrstatus). Kein Zeitstempel. Ohne --yes und ohne Terminal: Exit 8, nichts gesendet.",
			Output:  export.Result{},
			run:     (*app).export},
		{Name: "providers", Summary: "Angebotene qualifizierte Zeitstempel-Dienste (im Echtbetrieb erprobt; ohne Preise, siehe README).",
			Effects: "nichts", Network: "keines", Output: []providerInfo{}, run: (*app).providers},
		{Name: "provider set", Args: "<id>", MinArgs: 1, MaxArgs: 1, Writes: true,
			Summary: "Zeitstempel-Dienst für dieses Archiv einrichten. Passwort nur über die Standardeingabe.",
			Help: "Benutzer/Passwort (basic): --user; das Passwort kommt als erste Zeile über die Standardeingabe oder wird im Terminal verdeckt abgefragt.\n" +
				"Client-Zertifikat (clientcert): --p12; das Zertifikatspasswort ebenso. Gespeichert wird im Anmeldeinformationsspeicher dieses PCs, nie im Archiv.\n" +
				"Adresse laut Vertrag (url_from_contract, z. B. Disig): zusätzlich --url <https-Adresse>, etwa rechnungsarchiv provider set disig --p12 \"<datei.p12>\" --url \"https://…\".\n" +
				"Angeboten werden nur die Dienste aus rechnungsarchiv providers; ein nicht mehr angebotener bleibt für ein Archiv, das ihn schon verwendet, einstellbar.",
			Flags: []flagSpec{
				{Name: "user", Kind: stringFlag, Arg: "<name>", Help: "Benutzername (Zugang basic)"},
				{Name: "p12", Kind: stringFlag, Arg: "<datei>", Help: "PKCS#12-Datei (Zugang clientcert)"},
				{Name: "url", Kind: stringFlag, Arg: "<https-url>", Help: "Adresse laut Vertrag (nur Anbieter mit url_from_contract)"},
			},
			Effects: "den Dienst in rechnungsarchiv.json, die Zugangsdaten in die Windows-Anmeldeinformationsverwaltung (bei --p12 den Pfad, nicht die Datei).",
			Network: "keines; der Dienst wird nicht kontaktiert.",
			Output:  providerOut{},
			run:     (*app).providerSet},
		{Name: "provider show", Summary: "Eingerichteten Dienst zeigen und ob Zugangsdaten auf diesem PC liegen (nie das Passwort).",
			Effects: "nichts", Network: "keines", Output: providerOut{}, run: (*app).providerShow},
		{Name: "protocol", Args: "<ablage>", MinArgs: 1, MaxArgs: 1, Writes: true,
			Summary: "Archivprotokoll (PDF) für eine Ablage speichern.",
			Help: "<ablage> ist die Nummer im Archiv (z. B. 3 oder 0003) oder der Pfad einer Ablage-ZIP. Ohne --out: <NNNN>_<Name>.archivprotokoll.pdf im aktuellen Ordner; mit --out <ordner> (muss bestehen) darin unter diesem Namen. Nie in den Archivordner.\n" +
				"Exit 0, sobald die PDF gespeichert ist; das Prüfergebnis steht in status und reason.",
			Flags:   []flagSpec{{Name: "out", Kind: stringFlag, Arg: "<datei.pdf|ordner>", Help: "Zieldatei oder bestehender Zielordner"}, flagOnline},
			Effects: "eine neue PDF außerhalb des Archivs, nie überschrieben; mit --online den Cache wie verify.",
			Network: "ohne --online keines; mit --online wie verify.",
			Output:  protocolOut{},
			run:     (*app).protocol},
		{Name: "gui", Args: "[<ordner>]", MaxArgs: 1, Writes: true,
			Summary: "Fenster öffnen; mit <ordner> gleich den Dialog „Ordner archivieren“ für diesen Ordner.",
			Help: "Öffnet das zuletzt verwendete Archiv und endet, wenn das Fenster geschlossen wird. Archiviert wird nur nach Bestätigung im Fenster; fehlen Zeitstempel, fragt es beim Öffnen, ob sie geholt werden sollen.\n" +
				"Ohne Argumente aus dem Explorer oder Startmenü gestartet, öffnet rechnungsarchiv.exe ebenfalls das Fenster; im Terminal zeigt es die Hilfe. " +
				"Verknüpfungen zeigen auf rechnungsarchiv-gui.exe: es startet rechnungsarchiv.exe gui ohne Konsolenfenster.\n" +
				"gui gibt keine Daten aus (--json ist ein Aufruffehler). Agenten öffnen das Fenster nur, wenn der Benutzer es sehen will.",
			Effects:   "ein beschreibbares geöffnetes Archiv wird Standardarchiv (settings.json) und aufgeräumt (übrige .tmp-Dateien, Schreibschutz der Ablage-ZIPs); Vertrauenslisten im Archiv (Nur Ansicht: im Cache). Ablagen, Zeitstempel und Export nur nach Bestätigung im Fenster.",
			Network:   "beim Öffnen Vertrauenslisten, ohne Prüfwert. „Archiv kontrollieren“ wie verify --online, „Export für Betriebsprüfung“ wie export. Zeitstempel erst nach Bestätigung im Fenster, dann wie archive oder stamp.",
			NotGlobal: []string{"archiv", "json"},
			run:       (*app).gui},
		{Name: "schema", Summary: "Maschinenlesbare Beschreibung aller Befehle (JSON).",
			Effects: "nichts", Network: "keines", Output: selfDoc{}, run: (*app).schema},
	}
}

func find(name string) *command {
	for _, c := range commands {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func hasSub(word string) bool { return word == "provider" }

// ---------------------------------------------------------------- status

type archiveInfo struct {
	Path             string  `json:"path"`
	Source           string  `json:"source"` // "flag", "cwd" or "default"
	ArchiveID        string  `json:"archive_id"`
	Company          string  `json:"company"`
	RetentionDefault int     `json:"retention_default"`
	Provider         *string `json:"provider"`
}

type providerInfo struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Country         string `json:"country"`
	Auth            string `json:"auth"`
	URL             string `json:"url,omitempty"`
	URLFromContract bool   `json:"url_from_contract"`
	TrustedList     string `json:"trusted_list"`
}

func providerOf(p tsa.Provider) providerInfo {
	return providerInfo{ID: p.ID, Name: p.Name, Country: p.Country, Auth: p.Auth, URL: p.URL, URLFromContract: p.URLFromContract, TrustedList: p.TrustedList}
}

type statusDeposit struct {
	Sequence int           `json:"sequence"`
	Name     string        `json:"name"`
	File     string        `json:"file"`
	Root     string        `json:"root,omitempty"`
	Status   verify.Status `json:"status,omitempty"` // pending entries: "pending" or "indeterminate" (key compromise)
	Reason   verify.Reason `json:"reason,omitempty"`
	Error    string        `json:"error,omitempty"`
}

type statusOut struct {
	Version     string             `json:"version"`
	Archive     archiveInfo        `json:"archive"`
	Provider    *providerInfo      `json:"provider"`
	Credentials *bool              `json:"credentials"` // on this PC; null if unknown
	Lock        *archive.LockInfo  `json:"lock"`
	Counts      statusCounts       `json:"deposits"`
	Pending     []statusDeposit    `json:"pending" schema:"nonnull"`
	Unreadable  []statusDeposit    `json:"unreadable" schema:"nonnull"`
	Last        *statusDeposit     `json:"last"`
	TrustLists  []verify.TrustList `json:"trust_lists" schema:"nonnull"`
	TrustIssued *time.Time         `json:"trust_list_issued"`     // newest national list
	Next        []string           `json:"next" schema:"nonnull"` // German hints for the next step
}

type statusCounts struct {
	Total       int `json:"total"`
	Timestamped int `json:"timestamped"`
	Pending     int `json:"pending"`
	Unreadable  int `json:"unreadable"`
}

func (a *app) openRead() (*archive.Archive, archiveInfo, error) {
	dir, source, err := a.archiveDir()
	if err != nil {
		return nil, archiveInfo{}, err
	}
	ar, err := archive.OpenReadOnly(dir)
	if err != nil {
		return nil, archiveInfo{}, err
	}
	return ar, infoOf(ar, source), nil
}

func infoOf(ar *archive.Archive, source string) archiveInfo {
	return archiveInfo{Path: ar.Dir, Source: source, ArchiveID: ar.Config.ArchiveID, Company: ar.Config.Company,
		RetentionDefault: ar.Config.RetentionDefault, Provider: ar.Config.TSA}
}

func (a *app) status(inv *invocation) error {
	ar, info, err := a.openRead()
	if err != nil {
		return err
	}
	out := statusOut{Version: archive.Version, Archive: info, Pending: []statusDeposit{}, Unreadable: []statusDeposit{}, TrustLists: []verify.TrustList{}, Next: []string{}}
	provID := ""
	if id := ar.Config.TSA; id != nil {
		provID = *id
		if p, ok := tsa.ProviderByID(provID); ok {
			pi := providerOf(p)
			out.Provider = &pi
		} else {
			a.warn(fmt.Sprintf("Zeitstempel-Anbieter %q ist dieser Version nicht bekannt.", provID))
		}
		if has, err := sys.creds().Has(ar.Config.ArchiveID, provID); err == nil {
			out.Credentials = &has
		} else {
			a.warn("Anmeldeinformationsspeicher nicht lesbar: " + err.Error())
		}
	}
	if out.Lock, err = ar.LockHolder(); err != nil {
		a.warn("Sperrdatei nicht lesbar: " + err.Error())
	}
	ts, err := sys.trustRead(ar.Dir)
	if err != nil {
		return err
	}
	deps, err := ar.Deposits()
	if err != nil {
		return err
	}
	// One evaluation over the whole archive, as stamp and verify do: pairing
	// by imprint across all Nachweise and key compromise pooled (spec §6).
	var roots []string
	hints := map[string]string{}
	for _, d := range deps {
		if d.Err == nil {
			roots = append(roots, d.Bag.Root)
			hints[d.Bag.Root] = filepath.Base(d.EvidenceDir)
		}
	}
	ev, err := verify.Evidence(a.ctx, ar.Dir, roots, hints, verify.Options{Trust: ts, Provider: provID})
	if err != nil {
		return err
	}
	for _, d := range deps {
		sd := statusDeposit{Sequence: d.Sequence, Name: d.Name, File: filepath.Base(d.Path)}
		out.Counts.Total++
		if d.Err != nil {
			sd.Error = d.Err.Error()
			out.Unreadable = append(out.Unreadable, sd)
			out.Counts.Unreadable++
			continue
		}
		sd.Root = d.Bag.Root
		atts := ev[d.Bag.Root]
		last := sd
		out.Last = &last
		if slices.ContainsFunc(atts, func(x verify.Attempt) bool { return x.Outcome == verify.OutcomeValid }) {
			out.Counts.Timestamped++
			continue
		}
		sd.Status = verify.StatusPending
		switch {
		case slices.ContainsFunc(atts, func(x verify.Attempt) bool { return x.Outcome == verify.OutcomeCompromised }):
			sd.Status = verify.StatusIndeterminate // spec §6 step 7
		case provID == "":
			sd.Reason = verify.ReasonNoProvider
		}
		out.Pending = append(out.Pending, sd)
		out.Counts.Pending++
	}
	for _, l := range ts.Lists() {
		_, online := l.FetchedOnline()
		out.TrustLists = append(out.TrustLists, verify.TrustList{Territory: l.Territory, Sequence: l.Sequence, IssueDate: l.IssueDate.UTC(), FetchedOnline: online})
		if l.Territory != "EU" && (out.TrustIssued == nil || l.IssueDate.After(*out.TrustIssued)) {
			t := l.IssueDate.UTC()
			out.TrustIssued = &t
		}
	}
	switch {
	case out.Provider == nil:
		out.Next = append(out.Next, "Zeitstempel-Dienst einrichten: rechnungsarchiv providers, dann rechnungsarchiv provider set <id> …")
	case out.Credentials != nil && !*out.Credentials:
		out.Next = append(out.Next, "Zugangsdaten auf diesem PC fehlen: rechnungsarchiv provider set "+provID+" …")
	}
	if out.Counts.Pending > 0 && out.Provider != nil {
		out.Next = append(out.Next, "Ausstehende Zeitstempel holen: rechnungsarchiv stamp")
	}
	if out.Counts.Total == 0 {
		out.Next = append(out.Next, "Erste Ablage: rechnungsarchiv archive --dry-run \"<ordner>\"")
	}
	a.emit(out, func(w io.Writer) { renderStatus(w, &out) })
	return nil
}

// ---------------------------------------------------------------- init

type initOut struct {
	Archive archiveInfo `json:"archive"`
	Default bool        `json:"default"` // remembered as the default archive
}

func (a *app) init(inv *invocation) error {
	company := inv.str("firma")
	if company == "" {
		return usageErr("init: --firma <name> fehlt", `rechnungsarchiv init "<ordner>" --firma "<name>"`)
	}
	dir, err := filepath.Abs(inv.args[0])
	if err != nil {
		return err
	}
	ar, err := archive.Init(dir, company)
	if err != nil {
		return err
	}
	remembered := true
	if err := archive.RememberDefault(ar.Dir); err != nil {
		remembered = false
		a.warn("Archiv konnte nicht als Standardarchiv gemerkt werden: " + err.Error())
	}
	out := initOut{infoOf(ar, "flag"), remembered}
	a.emit(out, func(w io.Writer) {
		fmt.Fprintf(w, "Archiv angelegt: %s\nFirma:          %s\n", ar.Dir, ar.Config.Company)
		fmt.Fprintln(w, "Nächster Schritt: rechnungsarchiv providers, dann rechnungsarchiv provider set <id> …")
	})
	return nil
}

// ---------------------------------------------------------------- archive

type planFile struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

type planOut struct {
	DryRun         bool       `json:"dry_run"`
	Archive        string     `json:"archive"`
	Folder         string     `json:"folder"`
	Sequence       int        `json:"sequence"`
	Name           string     `json:"name"`
	File           string     `json:"file"`
	Belegjahr      int        `json:"belegjahr"`
	RetentionYears int        `json:"retention_years"`
	RetentionUntil string     `json:"retention_until"`
	Provider       *string    `json:"provider"`
	Count          int        `json:"count"`
	Bytes          int64      `json:"bytes"`
	Files          []planFile `json:"files" schema:"nonnull"`
	Skipped        []string   `json:"skipped" schema:"nonnull"`
	Known          []string   `json:"known" schema:"nonnull"` // content already in an earlier Ablage (no deduplication)
	Years          []int      `json:"years" schema:"nonnull"` // distinct modification years
	Notes          []string   `json:"notes" schema:"nonnull"` // German
}

type depositOut struct {
	Sequence       int    `json:"sequence"`
	Name           string `json:"name"`
	File           string `json:"file"`
	Root           string `json:"root"`
	Count          int    `json:"count"`
	Bytes          int64  `json:"bytes"`
	Belegjahr      int    `json:"belegjahr"`
	RetentionYears int    `json:"retention_years"`
	RetentionUntil string `json:"retention_until"`
}

type archiveOut struct {
	Committed bool          `json:"committed"`
	Archive   string        `json:"archive"`
	Deposit   depositOut    `json:"deposit"`
	Stamp     *stamp.Result `json:"stamp"`
	Warnings  []string      `json:"warnings,omitempty"`
}

func (a *app) archive(inv *invocation) error {
	folder, err := filepath.Abs(inv.args[0])
	if err != nil {
		return err
	}
	if fi, err := os.Stat(folder); err != nil {
		return err
	} else if !fi.IsDir() {
		return usageErr("archive: "+folder+" ist kein Ordner", "")
	}
	ro, info, err := a.openRead()
	if err != nil {
		return err
	}
	opts := archive.Options{Name: inv.str("name"), Belegjahr: inv.num("belegjahr"), RetentionYears: inv.num("aufbewahrung")}
	r, err := ro.Resolve(folder, opts)
	if err != nil {
		return err
	}
	if inv.flag("dry-run") {
		return a.plan(ro, folder, r)
	}
	if inv.flag("take-over") && !inv.flag("yes") {
		return usageErr("--take-over nur zusammen mit --yes, nachdem der Benutzer zugestimmt hat", "")
	}
	seq, _ := ro.NextSequence()
	q := fmt.Sprintf("Ordner %q als Ablage %s „%s“ (Belegjahr %d) archivieren und einen Zeitstempel holen?", folder, present.Number(seq), r.Name, r.Belegjahr)
	if err := a.consent("archive", q, map[string]any{"action": "archive", "folder": folder, "name": r.Name, "belegjahr": r.Belegjahr, "retention_years": r.RetentionYears, "sequence": seq}); err != nil {
		return err
	}
	w, err := archive.Open(info.Path)
	if err != nil {
		return err
	}
	dep, err := w.Archive(folder, r)
	var le *archive.LockedError
	if errors.As(err, &le) && inv.flag("take-over") {
		if err = w.TakeOver(le); err == nil {
			dep, err = w.Archive(folder, r)
		}
	}
	out := archiveOut{Committed: dep != nil, Archive: w.Dir}
	var ce *archive.CommittedError
	switch {
	case errors.As(err, &ce):
		out.Warnings = append(out.Warnings, ce.Error())
		a.warn(ce.Error())
	case err != nil:
		return err
	}
	info2 := dep.Bag.Info
	out.Deposit = depositOut{Sequence: dep.Sequence, Name: dep.Name, File: filepath.Base(dep.Path), Root: dep.Bag.Root,
		Count: len(dep.Bag.Manifest), Belegjahr: info2.Belegjahr, RetentionYears: info2.RetentionYears, RetentionUntil: info2.RetentionUntil}
	for _, f := range dep.Bag.Files {
		out.Deposit.Bytes += f.Size
	}
	// Only the new Ablage: older pending ones are fetched by `stamp`.
	res, serr := a.runStamp(w, inv.flag("take-over"), []string{dep.Bag.Root})
	out.Stamp = res
	a.emit(out, func(w io.Writer) { renderArchived(w, &out) })
	if serr != nil {
		ce := classify(serr)
		ce.Hint = strings.TrimSpace("Die Ablage ist gespeichert; nicht erneut archivieren. Später rechnungsarchiv stamp. " + ce.Hint)
		a.report(ce)
		return nil
	}
	a.stampExit(res, true)
	return nil
}

func (a *app) plan(ro *archive.Archive, folder string, r archive.Options) error {
	p, err := ro.Preview(folder)
	if err != nil {
		return err
	}
	seq, err := ro.NextSequence()
	if err != nil {
		return err
	}
	out := planOut{DryRun: true, Archive: ro.Dir, Folder: folder, Sequence: seq, Name: r.Name,
		File: bag.DirName(seq, r.Name) + ".zip", Belegjahr: r.Belegjahr, RetentionYears: r.RetentionYears,
		RetentionUntil: bag.RetentionUntil(r.Belegjahr, r.RetentionYears), Provider: ro.Config.TSA,
		Files: []planFile{}, Skipped: nonNil(p.Input.Skipped), Known: nonNil(p.Known), Years: p.Years, Notes: []string{}}
	if out.Years == nil {
		out.Years = []int{}
	}
	for _, f := range p.Input.Files {
		out.Files = append(out.Files, planFile{Path: f.Path, Size: f.Size, MTime: f.ModTime.UTC().Format(time.RFC3339)})
		out.Bytes += f.Size
	}
	out.Count = len(out.Files)
	if out.Count == 0 {
		out.Notes = append(out.Notes, "Der Ordner enthält keine Dateien, die archiviert würden.")
	}
	if len(p.Years) > 1 {
		ys := make([]string, len(p.Years))
		for i, y := range p.Years {
			ys[i] = fmt.Sprint(y)
		}
		out.Notes = append(out.Notes, fmt.Sprintf("Die Dateien wurden in mehreren Jahren geändert (%s). Belegjahr %d prüfen.", strings.Join(ys, ", "), r.Belegjahr))
	}
	if len(p.Known) > 0 {
		out.Notes = append(out.Notes, fmt.Sprintf("%d Datei(en) liegen inhaltsgleich bereits in früheren Ablagen. Sie werden trotzdem archiviert.", len(p.Known)))
	}
	if ro.Config.TSA == nil {
		out.Notes = append(out.Notes, "Kein Zeitstempel-Dienst eingerichtet: die Ablage bliebe ausstehend.")
	}
	a.emit(out, func(w io.Writer) { renderPlan(w, &out) })
	return nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ---------------------------------------------------------------- stamp

// runStamp fetches the missing timestamps (of roots only, if not nil),
// holding the archive's writer lock (taken over after confirmation if
// takeOver).
func (a *app) runStamp(w *archive.Archive, takeOver bool, roots []string) (*stamp.Result, error) {
	ts, err := sys.trust(w.Dir)
	if err != nil {
		return nil, err
	}
	lock := func() (func(), error) {
		release, err := w.Lock()
		var le *archive.LockedError
		if takeOver && errors.As(err, &le) {
			if err := w.TakeOver(le); err != nil {
				return nil, err
			}
			return w.Lock()
		}
		return release, err
	}
	return stamp.Run(a.ctx, w, stamp.Options{Trust: ts, Creds: sys.creds(), Lock: lock, RootCAs: sys.rootCAs, Roots: roots})
}

func (a *app) stamp(inv *invocation) error {
	if inv.flag("take-over") && !inv.flag("yes") {
		return usageErr("--take-over nur zusammen mit --yes, nachdem der Benutzer zugestimmt hat", "")
	}
	dir, _, err := a.archiveDir()
	if err != nil {
		return err
	}
	w, err := archive.Open(dir)
	if err != nil {
		return err
	}
	res, err := a.runStamp(w, inv.flag("take-over"), nil)
	if err != nil {
		return err
	}
	a.emit(res, func(w io.Writer) { renderStamp(w, res) })
	a.stampExit(res, false)
	return nil
}

// stampExit sets the exit code for a stamping run: 5 a changed Ablage was
// not stamped, 7 provider error, 3 no provider, 6 validation material
// still missing. Provider and configuration problems also get an error
// object on stderr.
func (a *app) stampExit(res *stamp.Result, committed bool) {
	for _, w := range res.Warnings {
		a.warn(w)
	}
	var changed, provider, noProvider, material []int
	msg := ""
	reason := verify.Reason("")
	for _, o := range res.Outcomes {
		if o.Stamped {
			continue
		}
		switch o.Reason {
		case "":
			changed = append(changed, o.Sequence)
		case verify.ReasonNoProvider:
			noProvider = append(noProvider, o.Sequence)
		case verify.ReasonMaterial:
			material = append(material, o.Sequence)
		default:
			if msg == "" {
				msg, reason = o.Message, o.Reason
			}
			provider = append(provider, o.Sequence)
		}
	}
	after := ""
	if committed {
		after = "Die Ablage ist gespeichert; nicht erneut archivieren. "
	}
	switch {
	case len(changed) > 0:
		a.warn(fmt.Sprintf("Ablage(n) %s verändert, ungültig oder nicht lesbar: kein Zeitstempel angefordert. rechnungsarchiv verify ausführen.", numbers(changed)))
		a.exit = exitFound
	case len(provider) > 0:
		a.report(&cliError{Code: "PROVIDER_ERROR", exit: exitProvider, Message: "Zeitstempel ausstehend (" + present.Reason(reason) + "): " + msg,
			Hint:    after + providerHint(reason),
			Details: map[string]any{"reason": reason, "sequences": provider}})
	case len(noProvider) > 0:
		a.report(&cliError{Code: "NO_PROVIDER", exit: exitConfig, Message: "Kein Zeitstempel-Dienst eingerichtet; Ablage(n) " + numbers(noProvider) + " bleiben ausstehend.",
			Hint:    after + "rechnungsarchiv providers, dann rechnungsarchiv provider set <id> …, dann rechnungsarchiv stamp",
			Details: map[string]any{"reason": verify.ReasonNoProvider, "sequences": noProvider}})
	case len(material) > 0:
		a.warn(fmt.Sprintf("Ablage(n) %s: Prüfdaten noch nicht vollständig; rechnungsarchiv stamp später wiederholen.", numbers(material)))
		a.exit = exitPending
	}
}

func providerHint(r verify.Reason) string {
	switch r {
	case verify.ReasonOffline:
		return "Internetverbindung prüfen, dann rechnungsarchiv stamp."
	case verify.ReasonAccess:
		return "Zugangsdaten prüfen: rechnungsarchiv provider show, rechnungsarchiv provider set <id> …"
	case verify.ReasonQuota:
		return "Guthaben beim Anbieter aufladen, dann rechnungsarchiv stamp."
	}
	return "Später rechnungsarchiv stamp wiederholen; bleibt der Fehler, den Anbieter fragen."
}

func numbers(seqs []int) string {
	s := make([]string, len(seqs))
	for i, n := range seqs {
		s[i] = present.Number(n)
	}
	return strings.Join(s, ", ")
}

// ---------------------------------------------------------------- verify

// trustBase is the folder whose vertrauenslisten/ belongs to path.
func trustBase(path string) string {
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return path
	}
	return filepath.Dir(path)
}

func (a *app) runVerify(path string, online bool) (*verify.Report, error) {
	ts, err := sys.trustRead(trustBase(path))
	if err != nil {
		return nil, err
	}
	return verify.Path(a.ctx, path, verify.Options{Trust: ts, Online: online})
}

func (a *app) verify(inv *invocation) error {
	var path string
	if len(inv.args) == 1 {
		p, err := filepath.Abs(inv.args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(p); err != nil {
			return err
		}
		path = p
	} else {
		dir, _, err := a.archiveDir()
		if err != nil {
			return err
		}
		path = dir
	}
	rep, err := a.runVerify(path, inv.flag("online"))
	if err != nil {
		return err
	}
	a.emit(rep, func(w io.Writer) { renderReport(w, rep) })
	a.exit = verifyExit(rep)
	return nil
}

func verifyExit(rep *verify.Report) int {
	switch {
	case rep.Status == verify.StatusInvalid || rep.Status == verify.StatusChanged || !rep.ChainOK():
		return exitFound
	case rep.Status == verify.StatusIndeterminate || rep.Status == verify.StatusPending:
		return exitPending
	}
	return exitOK
}

// ---------------------------------------------------------------- export

func (a *app) export(inv *invocation) error {
	from, to := inv.num("von"), inv.num("bis")
	if from == 0 || to == 0 {
		return usageErr("export: --von <jahr> und --bis <jahr> angeben", `rechnungsarchiv export --von 2026 --bis 2026 "<ziel>" --yes`)
	}
	if from > to {
		return usageErr("export: --von liegt nach --bis", "")
	}
	dst, err := filepath.Abs(inv.args[0])
	if err != nil {
		return err
	}
	_, info, err := a.openRead()
	if err != nil {
		return err
	}
	q := fmt.Sprintf("Export der Belegjahre %d bis %d nach %q schreiben?", from, to, dst)
	if err := a.consent("export", q, map[string]any{"action": "export", "from": from, "to": to, "target": dst}); err != nil {
		return err
	}
	res, err := export.Run(a.ctx, info.Path, dst, export.Options{From: from, To: to, SourceTrust: sys.trustRead, ExportTrust: sys.trustExport})
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		a.warn(w)
	}
	if res.Report.Status != verify.StatusTimestamped || !res.Report.ChainOK() {
		a.warn("Die Kontrolle des Exports ergab: " + present.Status(res.Report.Status) + ". Details: rechnungsarchiv verify " + present.PSQuote(dst))
	}
	a.emit(res, func(w io.Writer) { renderExport(w, res) })
	return nil
}

// ---------------------------------------------------------------- providers

func (a *app) providers(inv *invocation) error {
	out := []providerInfo{}
	for _, p := range tsa.Providers() {
		if p.Offered("") {
			out = append(out, providerOf(p))
		}
	}
	a.emit(out, func(w io.Writer) { renderProviders(w, out) })
	return nil
}

type credInfo struct {
	Stored   bool   `json:"stored"`
	User     string `json:"user,omitempty"`
	P12      string `json:"p12,omitempty"`
	URL      string `json:"url,omitempty"`
	Password bool   `json:"password"` // a password is stored (never shown)
}

type providerOut struct {
	Archive     string        `json:"archive"`
	Provider    *providerInfo `json:"provider"`
	Credentials *credInfo     `json:"credentials"` // null if unknown or no provider
}

func (a *app) providerSet(inv *invocation) error {
	id := inv.args[0]
	p, ok := tsa.ProviderByID(id)
	if !ok {
		return usageErr("unbekannter Zeitstempel-Dienst: "+safeWord(id), "rechnungsarchiv providers")
	}
	// Before anything provider-specific: a provider that is not offered is
	// only accepted for an archive already using it (new credentials).
	if p.Hidden {
		ro, _, err := a.openRead()
		if err != nil {
			return err
		}
		cur := ""
		if ro.Config.TSA != nil {
			cur = *ro.Config.TSA
		}
		if !p.Offered(cur) {
			return usageErr(safeWord(id)+" wird derzeit nicht angeboten (noch nicht im Echtbetrieb erprobt). Weitere Anbieter? Melden Sie sich: "+rechnungsarchiv.IssuesURL, "rechnungsarchiv providers")
		}
	}
	user, p12, url := inv.str("user"), inv.str("p12"), inv.str("url")
	switch p.Auth {
	case tsa.AuthBasic:
		if user == "" || p12 != "" {
			return usageErr(id+" verwendet Benutzer/Passwort: --user <name> angeben, kein --p12", "rechnungsarchiv provider set --help")
		}
	case tsa.AuthClientCert:
		if p12 == "" || user != "" {
			return usageErr(id+" verwendet ein Client-Zertifikat: --p12 <datei> angeben, kein --user", "rechnungsarchiv provider set --help")
		}
	}
	if p.URLFromContract {
		if !strings.HasPrefix(url, "https://") {
			return usageErr(id+": --url <https-Adresse laut Vertrag> angeben", "")
		}
	} else if url != "" {
		return usageErr(id+" hat eine feste Adresse; --url ist nicht vorgesehen", "")
	}
	var p12Data []byte
	if p12 != "" {
		abs, err := filepath.Abs(p12)
		if err != nil {
			return err
		}
		if p12Data, err = os.ReadFile(abs); err != nil {
			return err
		}
		p12 = abs
	}
	dir, _, err := a.archiveDir()
	if err != nil {
		return err
	}
	w, err := archive.Open(dir)
	if err != nil {
		return err
	}
	what := "Passwort für " + p.Name
	if p12 != "" {
		what = "Zertifikatspasswort"
	}
	pw, err := a.secret(what)
	if err != nil {
		return err
	}
	if p12Data != nil {
		if _, err := tsa.LoadClientCert(p12Data, pw); err != nil {
			return &cliError{Code: "CONFIG", exit: exitConfig, Message: "Zertifikatsdatei oder Passwort ungültig: " + err.Error(), Hint: "Datei und Zertifikatspasswort prüfen."}
		}
	}
	c := creds.Credential{User: user, Password: pw, P12Path: p12, URL: url}
	if err := sys.creds().Set(w.Config.ArchiveID, p.ID, c); err != nil {
		return err
	}
	if err := w.SetProvider(&p.ID); err != nil {
		return err
	}
	pi := providerOf(p)
	out := providerOut{Archive: w.Dir, Provider: &pi, Credentials: &credInfo{Stored: true, User: user, P12: p12, URL: url, Password: true}}
	a.emit(out, func(w io.Writer) { renderProvider(w, &out, "eingerichtet") })
	return nil
}

func (a *app) providerShow(inv *invocation) error {
	ar, info, err := a.openRead()
	if err != nil {
		return err
	}
	out := providerOut{Archive: info.Path}
	if ar.Config.TSA != nil {
		id := *ar.Config.TSA
		pi := providerInfo{ID: id}
		if p, ok := tsa.ProviderByID(id); ok {
			pi = providerOf(p)
		}
		out.Provider = &pi
		c, err := sys.creds().Get(ar.Config.ArchiveID, id)
		switch {
		case err == nil:
			out.Credentials = &credInfo{Stored: true, User: c.User, P12: c.P12Path, URL: c.URL, Password: c.Password != ""}
		case errors.Is(err, creds.ErrNotFound):
			out.Credentials = &credInfo{}
		default:
			a.warn("Anmeldeinformationsspeicher nicht lesbar: " + err.Error())
		}
	}
	a.emit(out, func(w io.Writer) { renderProvider(w, &out, "") })
	return nil
}

// ---------------------------------------------------------------- protocol

type protocolOut struct {
	File          string               `json:"file"`
	Sequence      int                  `json:"sequence"`
	Name          string               `json:"name"`
	Status        verify.Status        `json:"status"`
	Qualification verify.Qualification `json:"qualification,omitempty"`
	Reason        verify.Reason        `json:"reason,omitempty"`
}

func (a *app) protocol(inv *invocation) error {
	ref := inv.args[0]
	var rep *verify.Report
	var dep *verify.Deposit
	var archiveDir string
	if seq, ok := parseNumber(ref); ok {
		dir, _, err := a.archiveDir()
		if err != nil {
			return err
		}
		archiveDir = dir
		if rep, err = a.runVerify(dir, inv.flag("online")); err != nil {
			return err
		}
		for i := range rep.Deposits {
			if rep.Deposits[i].Sequence == seq {
				if dep != nil {
					return &cliError{Code: "ERROR", exit: exitError, Message: "Nummer doppelt vergeben: Ablage " + present.Number(seq) + "; den Pfad der ZIP angeben."}
				}
				dep = &rep.Deposits[i]
			}
		}
		if dep == nil {
			return &cliError{Code: "NOT_FOUND", exit: exitUsage, Message: "keine Ablage " + present.Number(seq) + " im Archiv " + dir, Hint: "rechnungsarchiv status --json"}
		}
	} else {
		p, err := filepath.Abs(ref)
		if err != nil {
			return err
		}
		fi, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !isAblagePath(p, fi) {
			return usageErr("protocol: <ablage> ist eine Nummer im Archiv oder der Pfad einer Ablage-ZIP (oder einer entpackten Ablage)", "rechnungsarchiv protocol --help")
		}
		if rep, err = a.runVerify(p, inv.flag("online")); err != nil {
			return err
		}
		if rep.Mode != verify.ModeDeposit || len(rep.Deposits) != 1 {
			return usageErr("protocol: <ablage> ist eine Nummer im Archiv oder der Pfad einer einzelnen Ablage", "rechnungsarchiv protocol --help")
		}
		dep = &rep.Deposits[0]
		if archiveDir, err = enclosingArchive(p); err != nil {
			return err
		}
	}
	out := inv.str("out")
	if out == "" {
		out = report.FileName(dep)
	} else if fi, err := os.Stat(out); err == nil && fi.IsDir() {
		out = filepath.Join(out, report.FileName(dep))
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	guard := func() error { return outsideArchive(out, archiveDir) }
	if err := guard(); err != nil {
		return err
	}
	pdf, err := report.PDF(rep, dep, report.Options{})
	if err != nil {
		return err
	}
	if err := writeNewFile(out, pdf, guard); err != nil {
		if errors.Is(err, os.ErrExist) {
			return &cliError{Code: "EXISTS", exit: exitError, Message: "Datei existiert bereits: " + out, Hint: "anderen Namen mit --out angeben; vorhandene Dateien werden nie überschrieben."}
		}
		return err
	}
	res := protocolOut{File: out, Sequence: dep.Sequence, Name: dep.Name, Status: dep.Status, Qualification: dep.Qualification, Reason: dep.Reason}
	a.emit(res, func(w io.Writer) {
		fmt.Fprintf(w, "Archivprotokoll gespeichert: %s\nAblage %s „%s“: %s\n", out, present.Number(dep.Sequence), dep.Name, present.Label(*dep))
	})
	return nil
}

// parseNumber accepts an Ablage number like "3" or "0003".
func parseNumber(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, n > 0
}

// isAblagePath reports whether p is one of protocol's documented inputs: an
// Ablage ZIP, or an extracted Ablage (a folder with bagit.txt). An archive
// or export folder is neither.
func isAblagePath(p string, fi os.FileInfo) bool {
	if fi.Mode().IsRegular() {
		return strings.EqualFold(filepath.Ext(p), ".zip")
	}
	if !fi.IsDir() || isArchive(p) {
		return false
	}
	b, err := os.Stat(filepath.Join(p, "bagit.txt"))
	return err == nil && b.Mode().IsRegular()
}

// enclosingArchive returns the archive folder that physically contains p
// (followed through links and junctions), or "" if there is none.
func enclosingArchive(p string) (string, error) {
	phys, err := export.Physical(p)
	if err != nil {
		return "", err
	}
	for dir := filepath.Dir(phys); ; dir = filepath.Dir(dir) {
		if isArchive(dir) {
			return dir, nil
		}
		if filepath.Dir(dir) == dir {
			return "", nil
		}
	}
}

// outsideArchive refuses a target inside archiveDir, compared both as
// written and where the paths physically lead, so a junction or link to
// the archive cannot smuggle the file in. archiveDir "" allows any target.
func outsideArchive(target, archiveDir string) error {
	refuse := usageErr("das Archivprotokoll wird nie in den Archivordner geschrieben", "--out <datei.pdf> außerhalb des Archivs angeben")
	// The destination is judged on its own, whatever the input was: no
	// physical ancestor of target may be an archive.
	if enc, err := enclosingArchive(target); err != nil {
		return err
	} else if enc != "" {
		return refuse
	}
	if archiveDir == "" {
		return nil
	}
	if within(target, archiveDir) {
		return refuse
	}
	pt, err1 := export.Physical(target)
	pa, err2 := export.Physical(archiveDir)
	if err := errors.Join(err1, err2); err != nil {
		return err
	}
	if within(pt, pa) {
		return refuse
	}
	return nil
}

// writeNewFile creates path (never replacing a file) and writes data.
// check runs again on the created file before anything is written, so a
// junction swapped in meanwhile is caught; the empty file is removed.
func writeNewFile(path string, data []byte, check func() error) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := check(); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
	}
	return err
}

// ---------------------------------------------------------------- schema

func (a *app) schema(inv *invocation) error {
	writeJSON(a.stdout, schemaDoc())
	return nil
}
