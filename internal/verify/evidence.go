package verify

import (
	"context"
	"crypto/x509"
	"errors"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// tokenInfo is what an attempt with a cryptographically valid token keeps.
type tokenInfo struct {
	tok    *tsa.Token
	checks []tsa.CertCheck // stored evidence
	online []tsa.CertCheck // evidence fetched in this run
}

// TSAToken returns the verified token of the attempt, or nil if its token
// did not pass the cryptographic checks (spec §5 items 1–7).
func (a Attempt) TSAToken() *tsa.Token {
	if a.token == nil {
		return nil
	}
	return a.token.tok
}

// env is the context of one verification run.
type env struct {
	ctx      context.Context
	trust    *trust.Store
	anchors  []*x509.Certificate
	online   bool
	fetcher  *tsa.Fetcher
	provider *tsa.Provider // hint; nil if unknown
	now      time.Time
}

// evaluate classifies every attempt in evDir whose request carries root,
// in attempt order (spec §6 step 5).
func (e *env) evaluate(evDir string, root [32]byte) ([]Attempt, error) {
	stored, err := tsa.ReadAttempts(evDir)
	if err != nil {
		return nil, err
	}
	var out []Attempt
	for _, a := range tsa.MatchImprint(stored, root) {
		out = append(out, e.attempt(a))
	}
	return out, nil
}

func (e *env) attempt(a tsa.Attempt) Attempt {
	at := Attempt{N: a.N, Material: a.Material != nil && a.MaterialErr == nil}
	if a.TSR == nil {
		at.Outcome = OutcomeNoResponse
		return at
	}
	if _, err := tsa.ParseResponse(a.TSR); err != nil {
		var se *tsa.StatusError
		if errors.As(err, &se) {
			at.Outcome, at.Detail = OutcomeRejected, err.Error()
		} else {
			at.Outcome, at.Detail = OutcomeInvalid, err.Error()
		}
		return at
	}
	if len(e.anchors) == 0 {
		at.Outcome, at.Detail = OutcomeNoTrust, "keine geprüfte EU-Vertrauensliste vorhanden"
		return at
	}
	opts := tsa.VerifyOptions{Request: a.Request, Anchors: e.anchors}
	if at.Material {
		opts.Intermediates = a.Material.Chain
	}
	// The expected policy is checked when the token is accepted (spec §5
	// item 5); here the issuing provider is not known for certain, so the
	// policy is reported, not checked.
	tok, err := tsa.VerifyResponse(a.TSR, opts)
	if err != nil {
		at.Outcome, at.Detail = OutcomeInvalid, err.Error()
		return at
	}
	ti := &tokenInfo{tok: tok}
	at.token = ti

	var matErr error
	switch {
	case a.MaterialErr != nil:
		matErr = a.MaterialErr
	case a.Material == nil:
		matErr = errors.New("Zertifikatskette und Sperrinformationen fehlen")
	default:
		ti.checks, matErr = tsa.ValidateMaterial(tok, a.Material)
	}
	if e.online {
		_, ti.online, _ = e.fetcher.Collect(e.ctx, tok)
	}
	if compromised(ti.checks) || compromised(ti.online) {
		at.Outcome, at.Detail = OutcomeCompromised, "Zertifikat wegen Schlüsselkompromittierung gesperrt"
		return at
	}
	if matErr != nil {
		at.Outcome, at.Detail = OutcomeDiscarded, matErr.Error()
		return at
	}
	at.Outcome = OutcomeValid
	return at
}

// compromised applies spec §6 step 7 to authenticated evidence, whatever
// the dates.
func compromised(checks []tsa.CertCheck) bool {
	for _, c := range checks {
		if c.Revocation != nil && c.Revocation.KeyCompromise() {
			return true
		}
	}
	return false
}

// token builds the report view of a verified token and its proven time.
func (e *env) token(n int, ti *tokenInfo) (*Token, *Proven) {
	tok := ti.tok
	t := &Token{
		Attempt:      n,
		GenTime:      tok.GenTime.UTC(),
		Serial:       tok.Serial.String(),
		Policy:       tok.Policy.String(),
		Signer:       tok.Signer.Subject.String(),
		SignerSerial: tok.Signer.SerialNumber.String(),
		Issuer:       tok.Signer.Issuer.String(),
		Revocation:   []Revocation{},
		Compromised:  compromised(ti.checks) || compromised(ti.online),
	}
	for _, c := range tok.Chain {
		t.Chain = append(t.Chain, c.Subject.String())
	}
	acc, src := e.accuracy(tok)
	if src != "" {
		ms := (acc + time.Millisecond - 1).Milliseconds() // never shown smaller
		t.AccuracyMS, t.AccuracySource = &ms, src
	}
	territory := ""
	if e.provider != nil {
		territory = e.provider.TrustedList
	}
	d := e.trust.Qualify(tok.Chain, tok.GenTime, territory)
	t.Qualification = qualification(d.Result)
	t.TSP, t.Service = d.TSPName, d.Service
	if t.TSP == "" && len(tok.Signer.Subject.Organization) > 0 {
		t.TSP = tok.Signer.Subject.Organization[0]
	}
	if d.Territory != "" {
		t.TrustList = &TrustList{Territory: d.Territory, Sequence: d.Sequence, IssueDate: d.IssueDate.UTC(), FetchedOnline: d.FetchedOnline}
	}
	add := func(checks []tsa.CertCheck, source string) {
		for _, c := range checks {
			if c.Revocation == nil {
				continue
			}
			r := c.Revocation
			rv := Revocation{
				Subject: c.Cert.Subject.String(), Kind: string(r.Kind), Reason: r.Reason,
				ThisUpdate: r.ThisUpdate.UTC(), NextUpdate: r.NextUpdate.UTC(), Source: source,
			}
			switch r.Status {
			case tsa.StatusGood:
				rv.Status = "good"
			case tsa.StatusRevoked:
				rv.Status, rv.RevokedAt = "revoked", r.RevokedAt.UTC()
			default:
				rv.Status = "unknown"
			}
			if r.NextUpdate.IsZero() {
				rv.NextUpdate = time.Time{}
			}
			t.Revocation = append(t.Revocation, rv)
		}
	}
	add(ti.checks, "stored")
	add(ti.online, "online")

	p := &Proven{GenTime: t.GenTime, AccuracyMS: t.AccuracyMS, Qualification: t.Qualification}
	if src != "" {
		at := ceilSecond(tok.GenTime.Add(acc)).UTC()
		p.At = &at
	}
	return t, p
}

// accuracy returns the token's accuracy, else the provider's declared one
// (spec §6 step 8). src is "" if neither is known.
func (e *env) accuracy(tok *tsa.Token) (time.Duration, string) {
	if tok.Accuracy != nil {
		return *tok.Accuracy, "token"
	}
	if e.provider != nil {
		if d, ok := e.provider.DeclaredAccuracy(); ok {
			return d, "provider"
		}
	}
	return 0, ""
}

func qualification(r trust.Result) Qualification {
	switch r {
	case trust.Qualified:
		return Qualified
	case trust.NotQualified:
		return NotQualified
	}
	return Unconfirmed
}

func ceilSecond(t time.Time) time.Time {
	if tr := t.Truncate(time.Second); !tr.Equal(t) {
		return tr.Add(time.Second)
	}
	return t
}
