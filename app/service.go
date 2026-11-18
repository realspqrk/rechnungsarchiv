package main

import "context"

// Service is the only way the GUI talks to the core. The shell (shell.go)
// adds native dialogs, Explorer and browser on top and never decides
// anything itself. real.go wires internal/*; fake.go serves sample data
// (RA_FAKE) for screenshots and tests.
//
// Rules for implementations:
//   - Errors carry a German, user-facing message (it is shown as is). A
//     lock held by another writer is a *UIError with Code "gesperrt".
//   - All state comes from verify.Report; the GUI only renders it.
//   - Read-only mode never writes, locks or stamps (spec §9).
type Service interface {
	// Status describes the open archive, or HasArchive=false (start screen).
	Status(ctx context.Context) (Status, error)
	// CreateArchive creates a new archive in an empty folder and opens it.
	CreateArchive(ctx context.Context, req CreateArchiveRequest) (Status, error)
	// OpenArchive opens an existing archive; readOnly = "Archiv nur kontrollieren".
	// In normal mode the implementation retries pending timestamps (spec: on every start).
	OpenArchive(ctx context.Context, dir string, readOnly bool) (Status, error)
	// CloseArchive returns to the start screen (no archive open).
	CloseArchive(ctx context.Context) error
	// Preview scans a folder without writing anything ("--dry-run").
	Preview(ctx context.Context, folder string) (Preview, error)
	// RetentionUntil returns the ordinary end of retention (spec bag-info
	// Aufbewahrung-Bis) as an ISO date, e.g. 2026+7 -> "2033-12-31".
	RetentionUntil(ctx context.Context, belegjahr, years int) (string, error)
	// Archive writes a new Ablage and tries to stamp it.
	Archive(ctx context.Context, opts ArchiveOptions) (ArchiveResult, error)
	// StampPending fetches missing timestamps ("Jetzt nachholen").
	StampPending(ctx context.Context) (StampResult, error)
	// Verify checks the whole archive ("Archiv kontrollieren").
	Verify(ctx context.Context) (CheckSummary, error)
	// VerifyDeposit checks one Ablage (detail screen).
	VerifyDeposit(ctx context.Context, seq int) (DepositReport, error)
	// SaveProtocol writes the Archivprotokoll PDF of one Ablage to path.
	SaveProtocol(ctx context.Context, seq int, path string) error
	// ExportPlan summarises what an export of the Belegjahre would contain.
	ExportPlan(ctx context.Context, from, to int) (ExportPlan, error)
	// Export writes the auditor export (spec §8) into opts.Target.
	Export(ctx context.Context, opts ExportOptions) (ExportResult, error)
	// Settings returns archive and provider settings (never secrets).
	Settings(ctx context.Context) (Settings, error)
	// SetSettings changes the fields that are non-nil and returns the result.
	SetSettings(ctx context.Context, u SettingsUpdate) (Settings, error)
	// Providers lists the built-in qualified providers (no prices).
	Providers(ctx context.Context) ([]Provider, error)
	// SetCredentials stores credentials for the configured provider in the
	// Windows Credential Manager. Empty secret fields keep the stored value.
	SetCredentials(ctx context.Context, c Credentials) (Settings, error)
	// Disclaimer returns the embedded docs/haftungsausschluss.md.
	Disclaimer(ctx context.Context) (DisclaimerText, error)
	// TakeOver removes the lock of another writer that the user saw
	// (Status.Lock or a "gesperrt" error) after the user confirmed it
	// (spec §1). A writer that is still running is never taken over.
	TakeOver(ctx context.Context) error
}

// UIError is an error the frontend handles specially; it reaches
// JavaScript as the "cause" of the rejected call.
type UIError struct {
	Code    string    `json:"code"` // "gesperrt"
	Message string    `json:"message"`
	Lock    *LockInfo `json:"lock,omitempty"`
}

func (e *UIError) Error() string { return e.Message }

// CodeLocked: another writer holds the archive (spec §1).
const CodeLocked = "gesperrt"

// LockInfo is the content of the archive's .lock (spec §1).
type LockInfo struct {
	Host  string `json:"host"`
	User  string `json:"user"`
	Since string `json:"since"` // RFC 3339
}

// Deposit states (spec §6, precedence Ungültig > Verändert > Unbestimmt > Ausstehend > Zeitgestempelt).
const (
	StateStamped       = "zeitgestempelt"
	StatePending       = "ausstehend"
	StateIndeterminate = "unbestimmt"
	StateModified      = "veraendert"
	StateInvalid       = "ungueltig"
)

// Qualifiers of a Zeitgestempelt token (spec §6 step 6).
const (
	QualQualified    = "qualifiziert"
	QualNotQualified = "nicht-qualifiziert"
	QualUnconfirmed  = "unbestaetigt"
)

// Pending reasons (spec §5). They map 1:1 to verify.Reason; only
// "pruefdaten" is spelled in ASCII for the spec's "prüfdaten". "" means the
// reason is not known yet (no stamping run in this session).
const (
	ReasonNoService = "kein-dienst"
	ReasonOffline   = "offline"
	ReasonAccess    = "zugang"
	ReasonCredit    = "guthaben"
	ReasonEvidence  = "pruefdaten"
	ReasonProvider  = "anbieter"
)

// DepositReport.Content: the bag checks alone (spec §6 steps 1–4).
const (
	ContentUnchanged = "unveraendert"
	ContentChanged   = "veraendert"
	ContentInvalid   = "ungueltig"
)

// DepositReport.TokenState: the stored attempts alone (spec §5–7).
const (
	TokenValid       = "gueltig"
	TokenInvalid     = "ungueltig"
	TokenCompromised = "kompromittiert"
	TokenEvidence    = "pruefdaten"
	TokenMissing     = "fehlt"
)

// Archive-level findings (spec §6 step 9).
const (
	FindingGap       = "luecke"
	FindingDuplicate = "nummer-doppelt"
)

// Status is everything the main screen shows.
type Status struct {
	HasArchive bool `json:"hasArchive"`
	// SuggestedDir is the default Archivordner on the start screen.
	SuggestedDir string       `json:"suggestedDir"`
	Archive      ArchiveInfo  `json:"archive"`
	Provider     *ProviderRef `json:"provider"` // nil: no provider set up
	// Deposits, newest first.
	Deposits  []Deposit     `json:"deposits"`
	Counts    Counts        `json:"counts"`
	Findings  []Finding     `json:"findings"`
	LastCheck *CheckSummary `json:"lastCheck"`
	TrustList TrustListInfo `json:"trustList"`
	// Busy names the background work in progress: "" | "stempeln"
	// (pending timestamps are fetched) | "pruefen" (trusted lists are
	// refreshed and the archive re-checked). The UI polls while it is set.
	Busy string `json:"busy"`
	// Lock is set when another writer holds the archive (seen by the last
	// writing operation); the UI offers to take it over after confirmation.
	Lock *LockInfo `json:"lock"`
}

type ArchiveInfo struct {
	Dir              string `json:"dir"`
	Company          string `json:"company"`
	ArchiveID        string `json:"archiveId"`
	ReadOnly         bool   `json:"readOnly"`
	RetentionDefault int    `json:"retentionDefault"`
}

type ProviderRef struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"` // ISO 3166-1 alpha-2
}

// Deposit is one Ablage as listed on the main screen.
type Deposit struct {
	Seq       int    `json:"seq"`
	Name      string `json:"name"`
	Belegjahr int    `json:"belegjahr"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
	State     string `json:"state"`
	// Qualification is set when the own or inherited proof is a timestamp.
	Qualification string `json:"qualification"`
	// Reason is set when State is ausstehend.
	Reason string `json:"reason"`
	// GenTime of the own token (RFC 3339), "" if none.
	GenTime string `json:"genTime"`
	// ProvenLatest is belegt_spätestens, already rounded up to the second (RFC 3339).
	ProvenLatest string `json:"provenLatest"`
	// InheritedFrom is the successor whose token proves this Ablage (0 = none).
	InheritedFrom int    `json:"inheritedFrom"`
	Provider      string `json:"provider"`
	// File is the ZIP's file name inside the archive folder.
	File string `json:"file"`
}

type Counts struct {
	Total         int `json:"total"`
	Stamped       int `json:"stamped"`
	Pending       int `json:"pending"`
	Indeterminate int `json:"indeterminate"`
	Modified      int `json:"modified"`
	Invalid       int `json:"invalid"`
}

// Finding is an archive-level result such as a gap.
type Finding struct {
	// Kind: luecke | nummer-doppelt | befund (any other chain finding;
	// Text describes it).
	Kind string `json:"kind"`
	// Seq is the missing (luecke) or duplicated (nummer-doppelt) number.
	Seq int `json:"seq"`
	// Successor is the Ablage that points to the missing one (luecke).
	Successor int `json:"successor"`
	// Text is the German description (internal/present).
	Text string `json:"text"`
}

// CheckSummary is the result of "Archiv kontrollieren".
type CheckSummary struct {
	At         string    `json:"at"` // RFC 3339
	Deposits   int       `json:"deposits"`
	Files      int       `json:"files"`
	Deviations int       `json:"deviations"`
	Counts     Counts    `json:"counts"`
	Findings   []Finding `json:"findings"`
	FirstSeq   int       `json:"firstSeq"`
	LastSeq    int       `json:"lastSeq"`
}

type TrustListInfo struct {
	Date  string `json:"date"`  // ISO date of the newest cached list
	Stale bool   `json:"stale"` // past NextUpdate and refresh failed
}

type CreateArchiveRequest struct {
	Company string `json:"company"`
	Dir     string `json:"dir"`
}

// Preview is the dry run of "Ordner archivieren".
type Preview struct {
	Folder string `json:"folder"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	// Duplicates: files identical to files in earlier Ablagen (hint only).
	Duplicates []DuplicateHint `json:"duplicates"`
	// Skipped system files (Thumbs.db, desktop.ini, …), by name.
	Skipped []string `json:"skipped"`
	// Years of the files' modification dates, most files first.
	Years []YearCount `json:"years"`
	// Problems block archiving (invalid names, cloud placeholders, …).
	Problems         []PreviewProblem  `json:"problems"`
	SuggestedName    string            `json:"suggestedName"`
	SuggestedYear    int               `json:"suggestedYear"`
	RetentionDefault int               `json:"retentionDefault"`
	RetentionOptions []RetentionOption `json:"retentionOptions"`
	NextSeq          int               `json:"nextSeq"`
}

type DuplicateHint struct {
	Files int `json:"files"`
	// Seq is the earlier Ablage; 0 means "in earlier Ablagen" (the core
	// does not say which one).
	Seq int `json:"seq"`
}

type YearCount struct {
	Year  int `json:"year"`
	Files int `json:"files"`
}

type PreviewProblem struct {
	Path string `json:"path"`
	// Kind: name (invalid on Windows), collision (same name after case fold),
	// placeholder (cloud file not on disk), link (symlink/reparse point).
	Kind string `json:"kind"`
}

type RetentionOption struct {
	Years int    `json:"years"`
	Label string `json:"label"`
}

type ArchiveOptions struct {
	Folder         string `json:"folder"`
	Name           string `json:"name"`
	Belegjahr      int    `json:"belegjahr"`
	RetentionYears int    `json:"retentionYears"`
}

type ArchiveResult struct {
	// Committed: the Ablage is stored. Once true the UI never offers to
	// archive the same folder again, whatever happened afterwards.
	Committed bool    `json:"committed"`
	Deposit   Deposit `json:"deposit"`
	// Warning: the Ablage is committed, but a later step failed (e.g.
	// stamping could not start). Never archive again; stamp later.
	Warning string `json:"warning"`
}

type StampResult struct {
	Stamped int `json:"stamped"`
	Pending int `json:"pending"`
	// Reason of the first Ablage still pending.
	Reason string `json:"reason"`
	// Failed counts Ablagen that were not stamped because they are changed,
	// invalid or unreadable (the CLI exits 5 for them).
	Failed int `json:"failed"`
	// Warnings (German) name those Ablagen and other problems of the run.
	Warnings []string `json:"warnings"`
}

// DepositReport is verify.Report for one Ablage, flattened for the UI.
type DepositReport struct {
	Deposit   Deposit    `json:"deposit"`
	ReadOnly  bool       `json:"readOnly"`
	CheckedAt string     `json:"checkedAt"`
	Path      string     `json:"path"` // the Ablage ZIP
	Root      string     `json:"root"` // Prüfwert, sha256:<hex>
	Files     FileCounts `json:"files"`
	// ChangedPaths lists changed, missing and added payload paths (first few).
	ChangedPaths []string `json:"changedPaths"`
	// Stamp is the own token, nil if there is none.
	Stamp *StampInfo `json:"stamp"`
	// Inherited proof via a successor (spec §6 step 8), nil if none.
	Inherited   *InheritedProof `json:"inherited"`
	Predecessor int             `json:"predecessor"` // 0 = first Ablage
	// PredecessorPresent: an Ablage with the predecessor's number is in the
	// archive (false: Lücke). It says nothing about the connection.
	PredecessorPresent bool `json:"predecessorPresent"`
	// PredecessorLinked: chain.json names exactly the Prüfwert of that
	// Ablage and the chain checks found nothing about either of them.
	PredecessorLinked bool `json:"predecessorLinked"`
	// ChainFindings (German) are the chain findings about this Ablage or
	// its predecessor (spec §6 step 9).
	ChainFindings []string `json:"chainFindings"`
	// Content is the result of the bag checks alone (spec §6 steps 1–4):
	// unveraendert | veraendert | ungueltig.
	Content string `json:"content"`
	// TokenState is the result of the stored attempts alone (spec §5–7),
	// independent of Content: gueltig | ungueltig | kompromittiert |
	// pruefdaten (no trusted path, Unbestimmt) | fehlt.
	TokenState     string `json:"tokenState"`
	RetentionYears int    `json:"retentionYears"`
	RetentionUntil string `json:"retentionUntil"` // ISO date
}

type FileCounts struct {
	Unchanged int `json:"unchanged"`
	Changed   int `json:"changed"`
	Missing   int `json:"missing"`
	Added     int `json:"added"`
}

type StampInfo struct {
	Provider        string `json:"provider"`
	ProviderCountry string `json:"providerCountry"`
	GenTime         string `json:"genTime"`
	// AccuracyMs is 0 when neither token nor provider list states one.
	AccuracyMs    int    `json:"accuracyMs"`
	ProvenLatest  string `json:"provenLatest"`
	Qualification string `json:"qualification"`
	// TrustList that decided the qualification: country and issue date.
	TrustListCountry string `json:"trustListCountry"`
	TrustListDate    string `json:"trustListDate"`
	SignatureValid   bool   `json:"signatureValid"`
	// Revocation evidence for the signer: "OCSP" or "CRL".
	RevocationSource string `json:"revocationSource"`
	// Revoked with keyCompromise/cACompromise/unspecified -> Unbestimmt.
	KeyCompromise bool `json:"keyCompromise"`
}

type InheritedProof struct {
	FromSeq int `json:"fromSeq"`
	// ProvenLatest is "" when the successor's accuracy is unknown; GenTime
	// is then the only time to show ("Genauigkeit nicht angegeben").
	ProvenLatest  string `json:"provenLatest"`
	GenTime       string `json:"genTime"`
	Qualification string `json:"qualification"`
}

type ExportPlan struct {
	From            int    `json:"from"`
	To              int    `json:"to"`
	Years           []int  `json:"years"` // Belegjahre present in the archive
	Deposits        int    `json:"deposits"`
	Files           int    `json:"files"`
	Bytes           int64  `json:"bytes"`
	SuggestedTarget string `json:"suggestedTarget"`
}

type ExportOptions struct {
	From   int    `json:"from"`
	To     int    `json:"to"`
	Target string `json:"target"`
}

type ExportResult struct {
	Target   string   `json:"target"`
	Deposits int      `json:"deposits"`
	Warnings []string `json:"warnings"` // German
}

type Settings struct {
	Company          string            `json:"company"`
	ArchiveDir       string            `json:"archiveDir"`
	RetentionDefault int               `json:"retentionDefault"`
	RetentionOptions []RetentionOption `json:"retentionOptions"`
	ProviderID       string            `json:"providerId"` // "" = none
	Credentials      CredentialState   `json:"credentials"`
	TrustListDate    string            `json:"trustListDate"`
}

// CredentialState says what is stored on this PC; never the secret itself.
type CredentialState struct {
	User        string `json:"user"`
	HasPassword bool   `json:"hasPassword"`
	P12Path     string `json:"p12Path"`
	URL         string `json:"url"`
}

type SettingsUpdate struct {
	Company          *string `json:"company"`
	RetentionDefault *int    `json:"retentionDefault"`
	ProviderID       *string `json:"providerId"`
}

// Provider is an entry of internal/tsa/providers.json. No prices, ever.
type Provider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`    // short display name, e.g. "Mensatek"
	Country     string `json:"country"` // ISO 3166-1 alpha-2
	Auth        string `json:"auth"`    // basic | clientcert
	Recommended bool   `json:"recommended"`
	// URLFromContract: the TSA URL is not public and comes with the contract.
	URLFromContract bool   `json:"urlFromContract"`
	Website         string `json:"website"`
}

type Credentials struct {
	User     string `json:"user"`
	Password string `json:"password"`
	P12Path  string `json:"p12Path"`
	// P12Password is the PKCS#12 password (clientcert).
	P12Password string `json:"p12Password"`
	URL         string `json:"url"`
}

// DisclaimerText mirrors rechnungsarchiv.Disclaimer for the bindings.
type DisclaimerText struct {
	Markdown string `json:"markdown"`
	Short    string `json:"short"`
	Consent  string `json:"consent"`
}
