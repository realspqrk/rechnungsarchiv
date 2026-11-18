package verify

import (
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// Status is the verification status of an Ablage (spec §6). The JSON values
// are stable English codes; German labels live in internal/present.
type Status string

const (
	StatusTimestamped   Status = "timestamped"   // Zeitgestempelt
	StatusPending       Status = "pending"       // Ausstehend
	StatusIndeterminate Status = "indeterminate" // Unbestimmt
	StatusChanged       Status = "changed"       // Verändert
	StatusInvalid       Status = "invalid"       // Ungültig
)

// rank orders statuses by precedence: Ungültig > Verändert > Unbestimmt >
// Ausstehend > Zeitgestempelt.
func (s Status) rank() int {
	switch s {
	case StatusInvalid:
		return 4
	case StatusChanged:
		return 3
	case StatusIndeterminate:
		return 2
	case StatusPending:
		return 1
	}
	return 0
}

// Worse returns the status with the higher precedence.
func Worse(a, b Status) Status {
	if b.rank() > a.rank() {
		return b
	}
	return a
}

// Qualification qualifies a valid token (spec §6 step 6).
type Qualification string

const (
	Qualified    Qualification = "qualified"     // qualifiziert
	NotQualified Qualification = "not_qualified" // nicht qualifiziert
	Unconfirmed  Qualification = "unconfirmed"   // unbestätigt
)

// Reason is why an Ablage is Ausstehend; the values are the spec §5 codes.
type Reason string

const (
	ReasonNoProvider Reason = "kein-dienst"
	ReasonOffline    Reason = "offline"
	ReasonAccess     Reason = "zugang"
	ReasonQuota      Reason = "guthaben"
	ReasonMaterial   Reason = "prüfdaten"
	ReasonProvider   Reason = "anbieter"
)

// Mode says what was verified.
type Mode string

const (
	ModeArchive Mode = "archive" // a whole archive folder
	ModeExport  Mode = "export"  // an export subset (spec §8)
	ModeDeposit Mode = "deposit" // a single Ablage ZIP or extracted bag
)

// Report is the result of one verification run. CLI, GUI and the
// Archivprotokoll render it; none of them re-derives a result.
type Report struct {
	Mode      Mode      `json:"mode"`
	Path      string    `json:"path"`
	CheckedAt time.Time `json:"checked_at"`
	Online    bool      `json:"online"`
	ArchiveID string    `json:"archive_id,omitempty"`
	Company   string    `json:"company,omitempty"`
	// Provider is the provider configured for the archive ("" if unknown).
	// It only selects which trusted lists are refreshed; facts about the
	// issuer of a token come from the provider recorded with its attempt.
	Provider string    `json:"provider,omitempty"`
	Status   Status    `json:"status"` // worst Ablage status; Zeitgestempelt if there is none
	Deposits []Deposit `json:"deposits"`
	// Bridged are excluded Ablagen of an export, known only by Brückendaten.
	Bridged       []Bridge    `json:"bridged,omitempty"`
	Findings      []Finding   `json:"findings,omitempty"`
	TrustLists    []TrustList `json:"trust_lists"`
	TrustProblems []string    `json:"trust_problems,omitempty"`
}

// ChainOK reports whether the chain checks found nothing (spec §6 step 9).
// "predecessor_not_in_export" is neutral and does not count.
func (r *Report) ChainOK() bool {
	for _, f := range r.Findings {
		if f.Code != FindingPredecessorNotInExport {
			return false
		}
	}
	return true
}

// FindingCode names an archive-level finding.
type FindingCode string

const (
	FindingGap                    FindingCode = "gap"                       // Lücke: Sequence..To missing
	FindingDuplicateNumber        FindingCode = "duplicate_number"          // Nummer doppelt vergeben
	FindingArchiveIDMismatch      FindingCode = "archive_id_mismatch"       // Ablage belongs to another archive
	FindingPreviousMismatch       FindingCode = "previous_mismatch"         // previous ≠ predecessor's Prüfwert
	FindingPredecessorNotInExport FindingCode = "predecessor_not_in_export" // neutral
	FindingBridgeInvalid          FindingCode = "bridge_invalid"            // Brückendaten do not verify
	FindingFileNameMismatch       FindingCode = "file_name_mismatch"        // number in file name ≠ chain.json
)

// Finding is an archive-level result of the chain checks.
type Finding struct {
	Code     FindingCode `json:"code"`
	Sequence int         `json:"sequence,omitempty"`
	To       int         `json:"to,omitempty"` // last missing number of a gap
	Detail   string      `json:"detail,omitempty"`
}

// Deposit is the result for one Ablage.
type Deposit struct {
	Sequence      int           `json:"sequence"`
	Name          string        `json:"name"`
	File          string        `json:"file"` // ZIP or folder name
	Root          string        `json:"root,omitempty"`
	Status        Status        `json:"status"`
	Qualification Qualification `json:"qualification,omitempty"` // Zeitgestempelt only
	Reason        Reason        `json:"reason,omitempty"`        // Ausstehend only; "" if unknown

	ArchiveID string  `json:"archive_id,omitempty"`
	Previous  *string `json:"previous,omitempty"`
	// LinkedPrevious is true when the chain check verified that this Ablage
	// commits to the predecessor's Prüfwert and both tag sets are intact
	// (spec §6 step 9). It is the only source for "schließt an".
	LinkedPrevious bool   `json:"linked_previous,omitempty"`
	Company        string `json:"company,omitempty"`
	Belegjahr      int    `json:"belegjahr,omitempty"`
	BaggingDate    string `json:"bagging_date,omitempty"` // local assertion
	ArchivedBy     string `json:"archived_by,omitempty"`  // local assertion
	RetentionYears int    `json:"retention_years,omitempty"`
	RetentionUntil string `json:"retention_until,omitempty"`
	ToolVersion    string `json:"tool_version,omitempty"`

	Counts   Counts    `json:"counts"`
	Files    []File    `json:"files"`
	Added    []string  `json:"added,omitempty"`
	Ignored  []string  `json:"ignored,omitempty"`
	Problems []Problem `json:"problems,omitempty"`

	Token    *Token    `json:"token,omitempty"`
	Attempts []Attempt `json:"attempts,omitempty"`
	Proven   *Proven   `json:"proven,omitempty"`

	fileSeq   int  // number in the file name, 0 if none
	evaluated bool // the attempts were evaluated (the bag has a Prüfwert)
}

// Counts summarises the payload check (spec §6 step 3).
type Counts struct {
	Files   int   `json:"files"` // listed in the manifest
	Bytes   int64 `json:"bytes"` // per files.json
	Matched int   `json:"matched"`
	Changed int   `json:"changed"`
	Missing int   `json:"missing"`
	Added   int   `json:"added"`
	Ignored int   `json:"ignored"`
}

// FileState is the check result of one payload file.
type FileState string

const (
	FileOK      FileState = "ok"
	FileChanged FileState = "changed"
	FileMissing FileState = "missing"
)

// File is one payload file as recorded in files.json.
type File struct {
	Path   string    `json:"path"` // with "data/"
	Size   int64     `json:"size"`
	MTime  string    `json:"mtime"` // RFC 3339 UTC, from files.json
	SHA256 string    `json:"sha256"`
	State  FileState `json:"state"`
}

// Problem is a finding of the bag check (spec §6 steps 1–4).
type Problem struct {
	Step    int    `json:"step"`
	Status  Status `json:"status"`
	Message string `json:"message"` // German, from internal/bag
}

// Outcome classifies one stored attempt.
type Outcome string

const (
	OutcomeValid       Outcome = "valid"       // token and validation material valid
	OutcomeDiscarded   Outcome = "discarded"   // token valid, material missing or not timely
	OutcomeCompromised Outcome = "compromised" // token valid, key compromise revoked (spec §6 step 7)
	OutcomeInvalid     Outcome = "invalid"     // token fails a cryptographic check (spec §5 items 1–7)
	OutcomeRejected    Outcome = "rejected"    // provider answered without a token
	OutcomeNoResponse  Outcome = "no_response" // request stored, no response
	OutcomeNoTrust     Outcome = "no_trust"    // no verified trusted list to anchor the path
)

// Attempt is one stored attempt n of the Nachweis.
type Attempt struct {
	N int `json:"n"`
	// Nachweis is the folder the attempt is stored in. Attempts pair with an
	// Ablage by imprint, so it need not match the ZIP's name (spec §1).
	Nachweis string  `json:"nachweis"`
	Outcome  Outcome `json:"outcome"`
	Detail   string  `json:"detail,omitempty"`
	// Material reports that n.chain.pem was stored and readable.
	Material bool `json:"material"`

	token   *tokenInfo
	pending *tsa.Token // path incomplete: see IncompleteToken
}

// Token describes the token an Ablage's status rests on.
type Token struct {
	Attempt int `json:"attempt"`
	// Provider is the provider id recorded with the attempt (n.provider)
	// when it matches the trusted-list service of the token's path; "" if
	// unknown. Only then are its declared accuracy and its trusted list used.
	Provider       string        `json:"provider,omitempty"`
	GenTime        time.Time     `json:"gen_time"`
	AccuracyMS     *int64        `json:"accuracy_ms,omitempty"`
	AccuracySource string        `json:"accuracy_source,omitempty"` // "token", "provider" or ""
	Serial         string        `json:"serial"`                    // decimal
	Policy         string        `json:"policy"`
	Signer         string        `json:"signer"` // subject DN
	SignerSerial   string        `json:"signer_serial"`
	Issuer         string        `json:"issuer"`
	Chain          []string      `json:"chain"` // subject DNs, signer first, anchor last
	TSP            string        `json:"tsp,omitempty"`
	Service        string        `json:"service,omitempty"`
	Qualification  Qualification `json:"qualification"`
	TrustList      *TrustList    `json:"trust_list,omitempty"` // the list the qualification rests on
	Revocation     []Revocation  `json:"revocation"`
	Compromised    bool          `json:"compromised,omitempty"`
}

// Revocation summarises the revocation evidence for one certificate.
type Revocation struct {
	Subject    string    `json:"subject"`
	Kind       string    `json:"kind"`   // "ocsp" or "crl"
	Status     string    `json:"status"` // "good", "revoked" or "unknown"
	Reason     int       `json:"reason,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitzero"`
	ThisUpdate time.Time `json:"this_update"`
	NextUpdate time.Time `json:"next_update,omitzero"`
	Source     string    `json:"source"` // "stored" or "online"
}

// TrustList identifies a verified trusted list.
type TrustList struct {
	Territory     string    `json:"territory"`
	Sequence      int       `json:"sequence"`
	IssueDate     time.Time `json:"issue_date"`
	FetchedOnline bool      `json:"fetched_online"`
}

// Proven is belegt_spätestens (spec §6 step 8).
type Proven struct {
	// At is genTime + accuracy rounded up to the second; nil if the accuracy
	// is unknown ("Genauigkeit nicht angegeben").
	At         *time.Time `json:"at,omitempty"`
	GenTime    time.Time  `json:"gen_time"`
	AccuracyMS *int64     `json:"accuracy_ms,omitempty"`
	// Via is the successor whose token proves this Ablage (0: its own).
	Via           int           `json:"via,omitempty"`
	Qualification Qualification `json:"qualification"`
}

// Bridge is an Ablage excluded from an export and bridged by Brückendaten.
type Bridge struct {
	Sequence int    `json:"sequence"`
	Root     string `json:"root,omitempty"`
	Valid    bool   `json:"valid"`
	Detail   string `json:"detail,omitempty"`

	chain *bag.Chain
}
