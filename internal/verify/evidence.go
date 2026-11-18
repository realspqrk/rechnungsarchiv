package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// tokenInfo is what an attempt with a cryptographically valid token keeps.
type tokenInfo struct {
	tok      *tsa.Token
	checks   []tsa.CertCheck // stored evidence
	online   []tsa.CertCheck // evidence fetched in this run
	provider *tsa.Provider   // recorded with the attempt (n.provider); nil if none
	// compromise is the authenticated key-compromise evidence for a
	// certificate of the path, from anywhere in the verified set.
	compromise []hit
}

// hit is authenticated evidence that a certificate is revoked for key
// compromise (spec §6 step 7).
type hit struct {
	cert   *x509.Certificate
	rev    *tsa.Revocation
	source string // "stored" or "online"
}

// TSAToken returns the verified token of the attempt, or nil if its token
// did not pass the cryptographic checks (spec §5 items 1–7).
func (a Attempt) TSAToken() *tsa.Token {
	if a.token == nil {
		return nil
	}
	return a.token.tok
}

// IncompleteToken returns the token of a discarded attempt whose checks of
// spec §5 items 1–6 pass but whose certificate path cannot be built because
// an issuer certificate is not stored (path material unavailable, not a
// demonstrated failure). Its Chain ends at the last certificate found.
// Stamping completes such an attempt; it is never evidence by itself.
func (a Attempt) IncompleteToken() *tsa.Token { return a.pending }

// ProviderFile is the record of the provider an attempt was sent to,
// "<n>.provider" in the Nachweis: the provider id, written right after n.tsq
// and never replaced.
func ProviderFile(n int) string { return fmt.Sprintf("%d.provider", n) }

// env is the context of one verification run over a verified set.
type env struct {
	ctx     context.Context
	trust   *trust.Store
	anchors []*x509.Certificate
	online  bool
	fetcher *tsa.Fetcher
	now     time.Time

	dir   string                // the folder whose *.nachweis folders are searched
	index map[[32]byte][]string // Prüfwert → Nachweis folders (see Nachweise)

	// Key-compromise evidence across the verified set (spec §6 step 7).
	pool      map[string][]tsa.Evidence // CertKey → stored evidence of every attempt
	seen      map[[32]byte]bool         // stored evidence already pooled
	onlineHit map[string]*hit           // CertKey → online evidence of compromise
	scanned   map[string]*hit           // CertKey of cert and issuer → result of the pool scan
}

func (e *env) init(dir string) error {
	idx, err := indexNachweise(dir)
	if err != nil {
		return err
	}
	e.dir, e.index = dir, idx
	e.pool, e.seen = map[string][]tsa.Evidence{}, map[[32]byte]bool{}
	e.onlineHit, e.scanned = map[string]*hit{}, map[string]*hit{}
	return nil
}

var reTSQ = regexp.MustCompile(`^[1-9][0-9]{0,8}\.tsq$`)

// indexNachweise reads the requests in every *.nachweis folder of dir and
// pairs the folders with Prüfwerte by imprint. Only real directories count
// (no links or junctions).
func indexNachweise(dir string) (map[[32]byte][]string, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return map[[32]byte][]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	idx := map[[32]byte][]string{}
	for _, de := range des {
		if !de.IsDir() || !strings.HasSuffix(de.Name(), ".nachweis") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, de.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if !f.Type().IsRegular() || !reTSQ.MatchString(f.Name()) {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, de.Name(), f.Name()))
			if err != nil {
				return nil, err
			}
			req, err := tsa.ParseRequest(b)
			if err != nil {
				continue // an unreadable request pairs with nothing
			}
			if !slices.Contains(idx[req.Imprint], de.Name()) {
				idx[req.Imprint] = append(idx[req.Imprint], de.Name()) // ReadDir order: sorted
			}
		}
	}
	return idx, nil
}

// Nachweise pairs the *.nachweis folders of dir with Prüfwerte
// ("sha256:<hex>") by the imprint of their stored requests (spec §1: file
// names are only hints). The folders of each Prüfwert are sorted by name.
func Nachweise(dir string) (map[string][]string, error) {
	idx, err := indexNachweise(dir)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for r, fs := range idx {
		out["sha256:"+hex.EncodeToString(r[:])] = fs
	}
	return out, nil
}

// evaluate classifies every attempt in the scope whose request carries
// root (spec §6 step 5): the hint folder first, then the others by name,
// each in attempt order. Key compromise is applied later by settle.
func (e *env) evaluate(root [32]byte, hint string) ([]Attempt, error) {
	folders := slices.Clone(e.index[root])
	if i := slices.Index(folders, hint); i > 0 {
		folders = append(append([]string{hint}, folders[:i]...), folders[i+1:]...)
	}
	var out []Attempt
	for _, f := range folders {
		if err := e.ctx.Err(); err != nil {
			return nil, err
		}
		stored, err := tsa.ReadAttempts(filepath.Join(e.dir, f))
		if err != nil {
			return nil, err
		}
		for _, a := range tsa.MatchImprint(stored, root) {
			out = append(out, e.attempt(a, f))
		}
	}
	return out, nil
}

func (e *env) attempt(a tsa.Attempt, folder string) Attempt {
	at := Attempt{N: a.N, Nachweis: folder}
	var stored *tsa.Material
	if a.Material != nil && a.MaterialErr == nil {
		stored = a.Material
		at.Material = len(stored.Chain) > 0
	}
	e.pooled(a.Material)
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
	if stored != nil {
		opts.Intermediates = stored.Chain
	}
	// The expected policy is checked when the token is accepted (spec §5
	// item 5); here the issuing provider is not known for certain, so the
	// policy is reported, not checked.
	tok, err := tsa.VerifyResponse(a.TSR, opts)
	if err != nil {
		if top := missingIssuer(err); top != nil {
			// Items 1–6 hold if the path ends at top; only its issuer is not
			// stored. That is missing material, not a failed check.
			o := opts
			o.Anchors = append([]*x509.Certificate{top}, e.anchors...)
			if pt, perr := tsa.VerifyResponse(a.TSR, o); perr == nil {
				at.Outcome = OutcomeDiscarded
				at.Detail = "Zertifikatskette unvollständig: Aussteller von " + top.Subject.String() + " ist nicht gespeichert"
				at.pending = pt
				return at
			}
		}
		at.Outcome, at.Detail = OutcomeInvalid, err.Error()
		return at
	}
	ti := &tokenInfo{tok: tok, provider: e.recordedProvider(folder, a.N)}
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
		for _, c := range ti.online {
			if c.Revocation != nil && c.Revocation.KeyCompromise() {
				e.onlineHit[tsa.CertKey(c.Cert)] = &hit{cert: c.Cert, rev: c.Revocation, source: "online"}
			}
		}
	}
	if matErr != nil {
		at.Outcome, at.Detail = OutcomeDiscarded, matErr.Error()
		return at
	}
	at.Outcome = OutcomeValid
	return at
}

// missingIssuer returns the certificate at which path building stopped
// because no issuer for it is available, or nil if the path failed for
// another reason (a self-issued certificate no trusted list names, an
// expired certificate, a constraint …): that is a demonstrated failure.
func missingIssuer(err error) *x509.Certificate {
	var ua x509.UnknownAuthorityError
	if !errors.As(err, &ua) || ua.Cert == nil || bytes.Equal(ua.Cert.RawSubject, ua.Cert.RawIssuer) {
		return nil
	}
	return ua.Cert
}

// pooled adds stored evidence to the set-wide pool. Its file name is the
// CertKey of the certificate it speaks for; it is authenticated when used.
func (e *env) pooled(m *tsa.Material) {
	if m == nil {
		return
	}
	for k, ev := range m.Evidence {
		h := sha256.Sum256(append([]byte(k+"."+string(ev.Kind)+":"), ev.Raw...))
		if !e.seen[h] {
			e.seen[h] = true
			e.pool[k] = append(e.pool[k], ev)
		}
	}
}

// settle applies spec §6 step 7 across the verified set: authenticated
// evidence of any attempt (of any Ablage) or fetched online that revokes a
// certificate for key compromise makes every token whose path contains that
// certificate Unbestimmt. Call it after all attempts are evaluated.
func (e *env) settle(deps []Deposit) {
	for i := range deps {
		for j := range deps[i].Attempts {
			a := &deps[i].Attempts[j]
			if a.token == nil {
				continue
			}
			if hits := e.compromised(a.token.tok); len(hits) > 0 {
				a.token.compromise = hits
				a.Outcome = OutcomeCompromised
				a.Detail = "Zertifikat " + hits[0].cert.Subject.String() + " wegen Kompromittierung gesperrt"
			}
		}
	}
}

// compromised returns the key-compromise evidence for the certificates of
// tok's path: the signer and every CA in it, the anchor included when its
// issuer is known.
func (e *env) compromised(tok *tsa.Token) []hit {
	var out []hit
	for _, p := range pathPairs(tok) {
		key := tsa.CertKey(p[0])
		if h := e.onlineHit[key]; h != nil {
			out = append(out, *h)
			continue
		}
		sk := key + "/" + tsa.CertKey(p[1])
		h, ok := e.scanned[sk]
		if !ok {
			for _, ev := range e.pool[key] {
				// genTime only decides "covers"; compromise ignores the dates.
				if rev, err := tsa.CheckRevocation(ev.Kind, ev.Raw, p[0], p[1], tok.GenTime); err == nil && rev.KeyCompromise() {
					h = &hit{cert: p[0], rev: rev, source: "stored"}
					break
				}
			}
			e.scanned[sk] = h
		}
		if h != nil {
			out = append(out, *h)
		}
	}
	return out
}

// pathPairs returns (certificate, issuer) for each certificate of the
// validated path whose issuer is available.
func pathPairs(tok *tsa.Token) [][2]*x509.Certificate {
	ch := tok.Chain
	var out [][2]*x509.Certificate
	for i := 0; i+1 < len(ch); i++ {
		out = append(out, [2]*x509.Certificate{ch[i], ch[i+1]})
	}
	if len(ch) == 0 {
		return out
	}
	last := ch[len(ch)-1]
	if bytes.Equal(last.RawSubject, last.RawIssuer) {
		return out
	}
	for _, c := range tok.Certs {
		if bytes.Equal(c.RawSubject, last.RawIssuer) && last.CheckSignatureFrom(c) == nil {
			return append(out, [2]*x509.Certificate{last, c})
		}
	}
	return out
}

// recordedProvider reads n.provider; nil if it is missing or names no
// known provider.
func (e *env) recordedProvider(folder string, n int) *tsa.Provider {
	f, err := os.Open(filepath.Join(e.dir, folder, ProviderFile(n)))
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, 128)
	k, _ := f.Read(b)
	if p, ok := tsa.ProviderByID(strings.TrimSpace(string(b[:k]))); ok {
		return &p
	}
	return nil
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
		Compromised:  len(ti.compromise) > 0,
	}
	for _, c := range tok.Chain {
		t.Chain = append(t.Chain, c.Subject.String())
	}

	// Provider facts only from the provider recorded with this attempt, and
	// only if the token's path is not listed under another territory.
	prov := ti.provider
	d := e.trust.Qualify(tok.Chain, tok.GenTime, "")
	if prov != nil && d.Territory != "" && d.Territory != prov.TrustedList {
		prov = nil
	}
	if prov != nil && d.Result == trust.Unconfirmed {
		d = e.trust.Qualify(tok.Chain, tok.GenTime, prov.TrustedList)
	}
	if prov != nil {
		t.Provider = prov.ID
	}
	acc, src := accuracy(tok, prov)
	if src != "" {
		ms := (acc + time.Millisecond - 1).Milliseconds() // never shown smaller
		t.AccuracyMS, t.AccuracySource = &ms, src
	}
	t.Qualification = qualification(d.Result)
	t.TSP, t.Service = d.TSPName, d.Service
	if t.TSP == "" && len(tok.Signer.Subject.Organization) > 0 {
		t.TSP = tok.Signer.Subject.Organization[0]
	}
	if d.Territory != "" {
		t.TrustList = &TrustList{Territory: d.Territory, Sequence: d.Sequence, IssueDate: d.IssueDate.UTC(), FetchedOnline: d.FetchedOnline}
	}
	add := func(c *x509.Certificate, r *tsa.Revocation, source string) {
		rv := Revocation{
			Subject: c.Subject.String(), Kind: string(r.Kind), Reason: r.Reason,
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
	for _, set := range []struct {
		checks []tsa.CertCheck
		source string
	}{{ti.checks, "stored"}, {ti.online, "online"}} {
		for _, c := range set.checks {
			if c.Revocation != nil {
				add(c.Cert, c.Revocation, set.source)
			}
		}
	}
	// Compromise evidence found elsewhere in the verified set.
	for _, h := range ti.compromise {
		shown := slices.ContainsFunc(t.Revocation, func(r Revocation) bool {
			return r.Subject == h.cert.Subject.String() && r.Status == "revoked" && r.Reason == h.rev.Reason
		})
		if !shown {
			add(h.cert, h.rev, h.source)
		}
	}

	p := &Proven{GenTime: t.GenTime, AccuracyMS: t.AccuracyMS, Qualification: t.Qualification}
	if src != "" {
		at := ceilSecond(tok.GenTime.Add(acc)).UTC()
		p.At = &at
	}
	return t, p
}

// accuracy returns the token's accuracy, else the declared accuracy of the
// provider recorded with the attempt (spec §6 step 8). src is "" if neither
// is known.
func accuracy(tok *tsa.Token, prov *tsa.Provider) (time.Duration, string) {
	if tok.Accuracy != nil {
		return *tok.Accuracy, "token"
	}
	if prov != nil {
		if d, ok := prov.DeclaredAccuracy(); ok {
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
