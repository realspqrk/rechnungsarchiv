package app

// Fake service with sample data for every UI state (P6a). Selected with
// RA_FAKE=<scenario>; see fakeScenarios. It never touches the disk.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/realspqrk/rechnungsarchiv"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// fakeScenarios maps RA_FAKE values to a short description (shown by --help).
var fakeScenarios = map[string]string{
	"start":             "kein Archiv geöffnet (Startbildschirm)",
	"leer":              "neues Archiv ohne Ablage",
	"normal":            "13 Ablagen, 1 ausstehend (offline), Anbieter Mensatek",
	"kein-dienst":       "kein Zeitstempel-Dienst eingerichtet",
	"guthaben":          "Guthaben aufgebraucht: Archivieren endet ausstehend",
	"nur-kontrollieren": "Sicherung im Modus „nur kontrollieren“",
	"luecke":            "Ablage 0007 fehlt (Lücke)",
	"zustaende":         "jede Ablage in einem anderen Zustand, Vertrauensliste veraltet",
	"disig":             "Anbieter mit Client-Zertifikat und Adresse laut Vertrag",
	"abgelehnt":         "Ordner mit Dateien, die nicht archiviert werden können",
	"gesperrt":          "Archiv auf einem anderen PC in Verwendung (Sperre)",
	"nachholen":         "beim Öffnen werden ausstehende Zeitstempel geholt",
}

type fakeService struct {
	mu        sync.Mutex
	scenario  string
	delay     time.Duration
	open      bool
	archive   ArchiveInfo
	provider  string // provider id, "" = none
	creds     CredentialState
	deposits  []Deposit // newest first
	findings  []Finding
	lastCheck *CheckSummary
	trust     TrustListInfo
	variant   map[int]string // seq -> detail variant (zustaende)
	lock      *LockInfo      // gesperrt
	busyUntil time.Time      // nachholen
}

func newFakeService(scenario string) (*fakeService, error) {
	if scenario == "" || scenario == "1" {
		scenario = "normal"
	}
	if _, ok := fakeScenarios[scenario]; !ok {
		return nil, fmt.Errorf("unknown RA_FAKE scenario %q (known: %s)", scenario, strings.Join(fakeScenarioNames(), ", "))
	}
	f := &fakeService{scenario: scenario, delay: 300 * time.Millisecond, variant: map[int]string{}}
	if d := os.Getenv("RA_FAKE_DELAY_MS"); d != "" {
		if ms, err := strconv.Atoi(d); err == nil {
			f.delay = time.Duration(ms) * time.Millisecond
		}
	}
	f.trust = TrustListInfo{Date: "2026-11-18"}
	f.archive = ArchiveInfo{Dir: `D:\Buchhaltung\Archiv`, Company: "Musterfirma GmbH", ArchiveID: "6f1c2a9e-3b4d-4e8f-9a17-2c5d8e0b4f61", RetentionDefault: 7}
	f.provider = "mensatek"
	f.creds = CredentialState{User: "musterfirma", HasPassword: true}
	f.open = true
	f.deposits = sampleDeposits()
	f.lastCheck = f.todayCheck()

	switch scenario {
	case "start":
		f.open = false
		f.lastCheck = nil
	case "leer":
		f.deposits = nil
		f.lastCheck = nil
	case "kein-dienst":
		f.provider = ""
		f.creds = CredentialState{}
		f.deposits = f.deposits[len(f.deposits)-3:]
		for i := range f.deposits {
			f.deposits[i] = pendingDeposit(f.deposits[i], ReasonNoService)
		}
		f.lastCheck = nil
	case "guthaben":
		f.deposits[0] = pendingDeposit(f.deposits[0], ReasonCredit)
	case "nur-kontrollieren":
		f.archive.Dir = `E:\Sicherung\Archiv`
		f.archive.ReadOnly = true
		f.lastCheck = nil
	case "luecke":
		f.deposits = slices.DeleteFunc(f.deposits, func(d Deposit) bool { return d.Seq == 7 })
		f.findings = []Finding{{Kind: FindingGap, Seq: 7, Successor: 8, Text: "Lücke: Ablage 0007 fehlt"}}
		f.lastCheck.Findings = f.findings
		f.lastCheck.Deposits = len(f.deposits)
		f.lastCheck.Files = totalFiles(f.deposits)
	case "zustaende":
		f.applyStateMix()
		f.trust.Stale = true
		f.trust.Date = "2026-09-30"
		f.lastCheck = nil
	case "gesperrt":
		f.lock = &LockInfo{Host: "BUERO-PC2", User: "maria", Since: time.Now().Add(-26 * time.Hour).Truncate(time.Minute).Format(time.RFC3339)}
	case "nachholen":
		f.busyUntil = time.Now().Add(4 * time.Second)
	case "disig":
		f.provider = "disig"
		f.creds = CredentialState{P12Path: `C:\Zertifikate\disig-zugang.p12`, HasPassword: true, URL: "https://tsa.beispiel.invalid/musterfirma"}
	}
	f.recount()
	return f, nil
}

func fakeScenarioNames() []string {
	names := make([]string, 0, len(fakeScenarios))
	for n := range fakeScenarios {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// sampleDeposits returns 13 Ablagen with 1.842 files; 0013 is pending (offline).
func sampleDeposits() []Deposit {
	type row struct {
		name  string
		year  int
		files int
		mb    float64
		stamp string // local time Europe/Vienna, "" = pending
	}
	rows := []row{
		{"2024 Eingangsrechnungen", 2024, 412, 196.2, "2025-01-20T14:02:11+01:00"},
		{"2024 Ausgangsrechnungen", 2024, 268, 88.4, "2025-01-20T14:05:37+01:00"},
		{"2025 Eingangsrechnungen", 2025, 389, 171.9, "2026-01-19T09:41:03+01:00"},
		{"2025 Ausgangsrechnungen", 2025, 241, 79.3, "2026-01-19T09:44:48+01:00"},
		{"2026-Q1 Eingangsrechnungen", 2026, 118, 52.6, "2026-04-08T16:12:25+02:00"},
		{"2026-Q1 Ausgangsrechnungen", 2026, 66, 21.0, "2026-04-08T16:15:09+02:00"},
		{"2026-Q2 Eingangsrechnungen", 2026, 71, 33.8, "2026-07-06T11:20:44+02:00"},
		{"2026-Q2 Ausgangsrechnungen", 2026, 45, 14.7, "2026-07-06T11:23:02+02:00"},
		{"2026-09 Eingangsrechnungen", 2026, 57, 27.5, "2026-10-02T16:38:12+02:00"},
		{"2026-09 Ausgangsrechnungen", 2026, 28, 9.1, "2026-10-02T16:40:57+02:00"},
		{"2026-10 Eingangsrechnungen", 2026, 64, 31.4, "2026-11-05T09:14:22+01:00"},
		{"2026-10 Ausgangsrechnungen", 2026, 31, 10.2, "2026-11-05T09:16:40+01:00"},
		{"2026-11 Eingangsrechnungen", 2026, 52, 24.9, ""},
	}
	out := make([]Deposit, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		d := Deposit{Seq: i + 1, Name: r.name, Belegjahr: r.year, Files: r.files, Bytes: int64(r.mb * 1024 * 1024), File: fmt.Sprintf("%04d_%s.zip", i+1, r.name)}
		if r.stamp == "" {
			d = pendingDeposit(d, ReasonOffline)
		} else {
			d = stampedDeposit(d, mustTime(r.stamp), QualQualified)
		}
		out = append(out, d)
	}
	return out
}

func stampedDeposit(d Deposit, gen time.Time, qual string) Deposit {
	d.State = StateStamped
	d.Qualification = qual
	d.Reason = ""
	d.GenTime = gen.Format(time.RFC3339)
	d.ProvenLatest = gen.Add(time.Second).Format(time.RFC3339) // ± 1 s, already whole seconds
	d.InheritedFrom = 0
	d.Provider = "Mensatek"
	return d
}

func pendingDeposit(d Deposit, reason string) Deposit {
	d.State = StatePending
	d.Qualification = ""
	d.Reason = reason
	d.GenTime = ""
	d.ProvenLatest = ""
	d.InheritedFrom = 0
	d.Provider = ""
	return d
}

// applyStateMix gives 0006–0013 one state each (scenario zustaende).
func (f *fakeService) applyStateMix() {
	for i := range f.deposits {
		d := &f.deposits[i]
		switch d.Seq {
		case 13:
			*d = stampedDeposit(*d, mustTime("2026-11-17T15:03:51+01:00"), QualQualified)
		case 12:
			*d = pendingDeposit(*d, ReasonOffline)
			d.InheritedFrom = 13
			d.ProvenLatest = "2026-11-17T15:03:52+01:00"
			d.Qualification = QualQualified
		case 11:
			f.variant[11] = "unveraendert"
		case 10:
			d.Qualification = QualNotQualified
		case 9:
			d.Qualification = QualUnconfirmed
		case 8:
			d.State = StateModified
			f.variant[8] = "veraendert"
		case 7:
			d.State = StateIndeterminate
			d.Qualification = ""
		case 6:
			d.State = StateInvalid
			d.Qualification = ""
		}
	}
}

func (f *fakeService) todayCheck() *CheckSummary {
	now := time.Now()
	at := time.Date(now.Year(), now.Month(), now.Day(), 10, 32, 0, 0, time.Local)
	c := &CheckSummary{At: at.Format(time.RFC3339), Deposits: len(f.deposits), Files: totalFiles(f.deposits)}
	if len(f.deposits) > 0 {
		c.FirstSeq = f.deposits[len(f.deposits)-1].Seq
		c.LastSeq = f.deposits[0].Seq
	}
	c.Counts = countStates(f.deposits)
	c.Inherited = inheritedPending(f.deposits)
	return c
}

func (f *fakeService) recount() {
	if f.lastCheck != nil {
		f.lastCheck.Counts = countStates(f.deposits)
		f.lastCheck.Deviations = f.lastCheck.Counts.Modified + f.lastCheck.Counts.Invalid
		f.lastCheck.Inherited = inheritedPending(f.deposits)
	}
}

func (f *fakeService) sleep(ctx context.Context) {
	select {
	case <-time.After(f.delay):
	case <-ctx.Done():
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func (f *fakeService) Status(ctx context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.statusLocked(), nil
}

func (f *fakeService) statusLocked() Status {
	s := Status{HasArchive: f.open, SuggestedDir: `D:\Buchhaltung\Rechnungsarchiv`, TrustList: f.trust}
	if !f.open {
		return s
	}
	s.Archive = f.archive
	s.Lock = f.lock
	if !f.busyUntil.IsZero() {
		if time.Now().Before(f.busyUntil) {
			s.Busy = "stempeln"
		} else {
			f.busyUntil = time.Time{}
			f.stampAllLocked()
		}
	}
	if p, ok := fakeProvider(f.provider); ok {
		s.Provider = &ProviderRef{ID: p.ID, Name: p.Name, Country: p.Country}
	}
	s.Deposits = slices.Clone(f.deposits)
	s.Counts = countStates(f.deposits)
	s.Findings = slices.Clone(f.findings)
	if f.lastCheck != nil {
		c := *f.lastCheck
		s.LastCheck = &c
	}
	return s
}

func (f *fakeService) CreateArchive(ctx context.Context, req CreateArchiveRequest) (Status, error) {
	if strings.TrimSpace(req.Company) == "" {
		return Status{}, errors.New("Bitte geben Sie den Namen der Firma ein.")
	}
	if strings.TrimSpace(req.Dir) == "" {
		return Status{}, errors.New("Bitte wählen Sie einen Archivordner.")
	}
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = true
	f.archive = ArchiveInfo{Dir: req.Dir, Company: strings.TrimSpace(req.Company), ArchiveID: "0b7e5d21-8c3f-4a96-b2e4-71f9a3c6d058", RetentionDefault: 7}
	f.provider = ""
	f.creds = CredentialState{}
	f.deposits = nil
	f.findings = nil
	f.lastCheck = nil
	return f.statusLocked(), nil
}

func (f *fakeService) OpenArchive(ctx context.Context, dir string, readOnly bool) (Status, error) {
	if dir == "" {
		return Status{}, errors.New("Bitte wählen Sie einen Archivordner.")
	}
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = true
	f.archive.Dir = dir
	f.archive.ReadOnly = readOnly
	if len(f.deposits) == 0 {
		f.deposits = sampleDeposits()
		f.provider = "mensatek"
	}
	f.lastCheck = nil
	return f.statusLocked(), nil
}

func (f *fakeService) CloseArchive(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.open = false
	return nil
}

func (f *fakeService) Preview(ctx context.Context, folder string) (Preview, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open {
		return Preview{}, errNoArchive
	}
	if f.archive.ReadOnly {
		return Preview{}, errReadOnly
	}
	name := filepath.Base(folder)
	if name == "." || name == `\` || name == "" {
		name = "Neue Ablage"
	}
	p := Preview{
		Folder:           folder,
		Files:            53,
		Bytes:            32925696,
		Skipped:          []string{"Thumbs.db", "desktop.ini"},
		Years:            []YearCount{{2026, 49}, {2025, 4}},
		SuggestedName:    name,
		SuggestedYear:    2026,
		RetentionDefault: f.archive.RetentionDefault,
		RetentionOptions: retentionOptions(),
		NextSeq:          f.nextSeqLocked(),
	}
	if len(f.deposits) > 0 {
		p.Duplicates = []DuplicateHint{{Files: 1, Seq: 11}}
	}
	if f.scenario == "abgelehnt" {
		p.Problems = []PreviewProblem{
			{Path: "Lieferant AUX.pdf", Kind: "name"},
			{Path: "2026-11-03_Mayr_R-118.pdf", Kind: "placeholder"},
		}
	}
	return p, nil
}

func (f *fakeService) nextSeqLocked() int {
	if len(f.deposits) == 0 {
		return 1
	}
	return f.deposits[0].Seq + 1
}

func (f *fakeService) RetentionUntil(ctx context.Context, belegjahr, years int) (string, error) {
	if belegjahr < 1900 || years <= 0 {
		return "", errors.New("Ungültiges Belegjahr oder ungültige Frist.")
	}
	return fmt.Sprintf("%04d-12-31", belegjahr+years), nil
}

func (f *fakeService) Archive(ctx context.Context, opts ArchiveOptions) (ArchiveResult, error) {
	if strings.TrimSpace(opts.Name) == "" {
		return ArchiveResult{}, errors.New("Bitte geben Sie der Ablage einen Namen.")
	}
	f.sleep(ctx)
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.archive.ReadOnly {
		return ArchiveResult{}, errReadOnly
	}
	if f.scenario == "abgelehnt" {
		return ArchiveResult{}, errors.New("2 Dateien können nicht archiviert werden. Bitte beheben Sie das zuerst.")
	}
	if f.lock != nil {
		return ArchiveResult{}, f.lockedErr()
	}
	d := Deposit{Seq: f.nextSeqLocked(), Name: strings.TrimSpace(opts.Name), Belegjahr: opts.Belegjahr, Files: 53, Bytes: 32925696}
	d.File = fmt.Sprintf("%04d_%s.zip", d.Seq, d.Name)
	switch {
	case f.provider == "":
		d = pendingDeposit(d, ReasonNoService)
	case f.scenario == "guthaben":
		d = pendingDeposit(d, ReasonCredit)
	default:
		d = stampedDeposit(d, time.Now().Truncate(time.Second), QualQualified)
		d.Provider = f.providerNameLocked()
	}
	f.deposits = append([]Deposit{d}, f.deposits...)
	f.recount()
	return ArchiveResult{Committed: true, Deposit: d}, nil
}

func (f *fakeService) providerNameLocked() string {
	if p, ok := fakeProvider(f.provider); ok {
		return p.Name
	}
	return ""
}

func (f *fakeService) StampPending(ctx context.Context) (StampResult, error) {
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.archive.ReadOnly {
		return StampResult{}, errReadOnly
	}
	if f.lock != nil {
		return StampResult{}, f.lockedErr()
	}
	var r StampResult
	for i := range f.deposits {
		d := &f.deposits[i]
		if d.State != StatePending {
			continue
		}
		switch {
		case f.provider == "":
			r.Pending++
			r.Reason = ReasonNoService
		case f.scenario == "guthaben":
			r.Pending++
			r.Reason = ReasonCredit
		default:
			*d = stampedDeposit(*d, time.Now().Truncate(time.Second), QualQualified)
			d.Provider = f.providerNameLocked()
			r.Stamped++
		}
	}
	f.recount()
	return r, nil
}

func (f *fakeService) Verify(ctx context.Context) (CheckSummary, error) {
	f.sleep(ctx)
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.open {
		return CheckSummary{}, errNoArchive
	}
	c := f.todayCheck()
	c.At = time.Now().Format(time.RFC3339)
	c.Findings = slices.Clone(f.findings)
	c.Deviations = c.Counts.Modified + c.Counts.Invalid
	f.lastCheck = c
	return *c, nil
}

func (f *fakeService) VerifyDeposit(ctx context.Context, seq int) (DepositReport, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.deposits, func(d Deposit) bool { return d.Seq == seq })
	if i < 0 {
		return DepositReport{}, fmt.Errorf("Ablage %04d ist nicht im Archiv.", seq)
	}
	d := f.deposits[i]
	r := DepositReport{
		Deposit:        d,
		ReadOnly:       f.archive.ReadOnly,
		CheckedAt:      time.Now().Format(time.RFC3339),
		Path:           filepath.Join(f.archive.Dir, d.File),
		Root:           fakeRoot(d),
		Files:          FileCounts{Unchanged: d.Files},
		RetentionYears: 7,
		RetentionUntil: fmt.Sprintf("%04d-12-31", d.Belegjahr+7),
		Content:        ContentUnchanged,
		TokenState:     TokenValid,
	}
	if d.Seq > 1 {
		r.Predecessor = d.Seq - 1
		r.PredecessorPresent = slices.ContainsFunc(f.deposits, func(x Deposit) bool { return x.Seq == d.Seq-1 })
		r.PredecessorLinked = r.PredecessorPresent
		if !r.PredecessorPresent {
			r.ChainFindings = []string{fmt.Sprintf("Lücke: Ablage %04d fehlt", d.Seq-1)}
		}
	}
	stamp := func(qual string) *StampInfo {
		return &StampInfo{
			Provider: "Mensatek", ProviderCountry: "ES",
			GenTime: d.GenTime, AccuracyMs: 1000, ProvenLatest: d.ProvenLatest,
			Qualification: qual, TrustListCountry: "ES", TrustListDate: "2026-11-12",
			SignatureValid: true, RevocationSource: "OCSP",
		}
	}
	switch d.State {
	case StateStamped:
		r.Stamp = stamp(d.Qualification)
		if d.Qualification == QualUnconfirmed {
			r.Stamp.TrustListDate = ""
		}
	case StateModified:
		r.Stamp = stamp(QualQualified)
		r.Content = ContentChanged
		r.Files = FileCounts{Unchanged: d.Files - 1, Changed: 1}
		r.ChangedPaths = []string{"2026-10-14_Huber_R-4471.pdf"}
	case StateIndeterminate:
		r.Stamp = stamp("")
		r.Stamp.KeyCompromise = true
		r.Stamp.SignatureValid = false
		r.TokenState = TokenCompromised
	case StateInvalid:
		r.Stamp = nil
		r.TokenState = TokenInvalid
	case StatePending:
		r.TokenState = TokenMissing
		if d.InheritedFrom > 0 {
			r.Inherited = &InheritedProof{FromSeq: d.InheritedFrom, ProvenLatest: d.ProvenLatest, GenTime: d.ProvenLatest, Qualification: d.Qualification}
		}
	}
	// The normal scenario mirrors the mockup: 0013 pending without a successor.
	return r, nil
}

func fakeRoot(d Deposit) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("fake:%04d:%s", d.Seq, d.Name)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (f *fakeService) SaveProtocol(ctx context.Context, seq int, path string) error {
	// The fake writes nothing: a placeholder PDF could be mistaken for a real protocol.
	f.sleep(ctx)
	return nil
}

func (f *fakeService) ExportPlan(ctx context.Context, from, to int) (ExportPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := ExportPlan{From: from, To: to}
	for _, d := range f.deposits {
		if !slices.Contains(p.Years, d.Belegjahr) {
			p.Years = append(p.Years, d.Belegjahr)
		}
	}
	slices.Sort(p.Years)
	if len(p.Years) > 0 {
		if p.From == 0 {
			p.From = p.Years[0]
		}
		if p.To == 0 {
			p.To = p.Years[len(p.Years)-1]
		}
	}
	if p.From > p.To {
		p.From, p.To = p.To, p.From
	}
	for _, d := range f.deposits {
		if d.Belegjahr >= p.From && d.Belegjahr <= p.To {
			p.Deposits++
			p.Files += d.Files
			p.Bytes += d.Bytes
		}
	}
	p.SuggestedTarget = fmt.Sprintf(`E:\Betriebspruefung-%d-%d`, p.From, p.To)
	if p.From == p.To {
		p.SuggestedTarget = fmt.Sprintf(`E:\Betriebspruefung-%d`, p.From)
	}
	return p, nil
}

func (f *fakeService) Export(ctx context.Context, opts ExportOptions) (ExportResult, error) {
	if opts.Target == "" {
		return ExportResult{}, errors.New("Bitte wählen Sie ein Ziel für den Export.")
	}
	plan, err := f.ExportPlan(ctx, opts.From, opts.To)
	if err != nil {
		return ExportResult{}, err
	}
	if plan.Deposits == 0 {
		return ExportResult{}, errors.New("In diesen Belegjahren gibt es keine Ablage.")
	}
	f.sleep(ctx)
	f.sleep(ctx)
	return ExportResult{Target: opts.Target, Deposits: plan.Deposits}, nil
}

func (f *fakeService) Settings(ctx context.Context) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.settingsLocked()
}

func (f *fakeService) settingsLocked() (Settings, error) {
	if !f.open {
		return Settings{}, errNoArchive
	}
	return Settings{
		Company:          f.archive.Company,
		ArchiveDir:       f.archive.Dir,
		RetentionDefault: f.archive.RetentionDefault,
		RetentionOptions: retentionOptions(),
		ProviderID:       f.provider,
		Credentials:      f.creds,
		TrustListDate:    f.trust.Date,
	}, nil
}

func (f *fakeService) SetSettings(ctx context.Context, u SettingsUpdate) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.archive.ReadOnly {
		return Settings{}, errReadOnly
	}
	if u.Company != nil {
		c := strings.TrimSpace(*u.Company)
		if c == "" {
			return Settings{}, errors.New("Bitte geben Sie den Namen der Firma ein.")
		}
		f.archive.Company = c
	}
	if u.RetentionDefault != nil {
		if !slices.ContainsFunc(retentionOptions(), func(o RetentionOption) bool { return o.Years == *u.RetentionDefault }) {
			return Settings{}, errors.New("Diese Frist wird nicht angeboten.")
		}
		f.archive.RetentionDefault = *u.RetentionDefault
	}
	if u.ProviderID != nil && *u.ProviderID != f.provider {
		if *u.ProviderID != "" {
			if _, ok := fakeProvider(*u.ProviderID); !ok {
				return Settings{}, errors.New("Unbekannter Anbieter.")
			}
		}
		f.provider = *u.ProviderID
		f.creds = CredentialState{} // credentials belong to one provider
	}
	return f.settingsLocked()
}

func (f *fakeService) Providers(ctx context.Context) ([]Provider, error) {
	return slices.Clone(fakeProviders), nil
}

func (f *fakeService) SetCredentials(ctx context.Context, c Credentials) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.archive.ReadOnly {
		return Settings{}, errReadOnly
	}
	if f.provider == "" {
		return Settings{}, errors.New("Bitte wählen Sie zuerst einen Anbieter.")
	}
	f.creds.User = strings.TrimSpace(c.User)
	f.creds.P12Path = c.P12Path
	f.creds.URL = strings.TrimSpace(c.URL)
	if c.Password != "" || c.P12Password != "" {
		f.creds.HasPassword = true
	}
	return f.settingsLocked()
}

// stampAllLocked ends the simulated background run of "nachholen".
func (f *fakeService) stampAllLocked() {
	for i := range f.deposits {
		if f.deposits[i].State == StatePending {
			f.deposits[i] = stampedDeposit(f.deposits[i], time.Now().Truncate(time.Second), QualQualified)
		}
	}
	f.recount()
}

func (f *fakeService) lockedErr() error {
	return &UIError{Code: CodeLocked, Lock: f.lock,
		Message: "Archiv wird gerade auf " + f.lock.Host + " von " + f.lock.User + " verwendet (seit " + mustTime(f.lock.Since).Local().Format("02.01.2006 15:04") + "). Läuft das Programm dort nicht mehr (z. B. nach einem Absturz), können Sie die Sperre übernehmen."}
}

func (f *fakeService) TakeOver(ctx context.Context) error {
	f.sleep(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lock == nil {
		return errors.New("Es gibt keine Sperre, die übernommen werden könnte.")
	}
	f.lock = nil
	return nil
}

func (f *fakeService) Disclaimer(ctx context.Context) (DisclaimerText, error) {
	d := rechnungsarchiv.ParseDisclaimer(rechnungsarchiv.DisclaimerMarkdown)
	return DisclaimerText{Markdown: d.Markdown, Short: d.Short, Consent: d.Consent}, nil
}

// fakeProviders is internal/tsa/providers.json as the real service maps it
// (no prices).
var fakeProviders = func() []Provider {
	var out []Provider
	for _, p := range tsa.Providers() {
		out = append(out, providerOf(p))
	}
	return out
}()

func fakeProvider(id string) (Provider, bool) {
	i := slices.IndexFunc(fakeProviders, func(p Provider) bool { return p.ID == id })
	if i < 0 {
		return Provider{}, false
	}
	return fakeProviders[i], true
}
