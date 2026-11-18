// Package stamp fetches the missing timestamps of an archive (spec §4 step
// 5, §5): one provider, no fallback, evidence written append-only.
package stamp

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Options configures Run.
type Options struct {
	Trust *trust.Store // the archive's trusted lists (trust.Open); required
	Creds creds.Store  // required
	// Lock acquires the archive's writer lock (spec §4 steps 1 and 6) and
	// returns its release function. Required.
	Lock    func() (release func(), err error)
	Fetcher *tsa.Fetcher   // validation material; nil: defaults
	RootCAs *x509.CertPool // TLS roots for the provider; nil: system roots
	Timeout time.Duration  // per request; 0: tsa.Client default
	Now     func() time.Time
}

// ClockTolerance is how far genTime may differ from the PC clock before a
// warning is shown (spec §5 "Time sanity").
const ClockTolerance = 5 * time.Minute

// Outcome is the result for one Ablage that had no valid timestamp.
type Outcome struct {
	Sequence int           `json:"sequence"`
	Name     string        `json:"name"`
	Root     string        `json:"root,omitempty"`
	Stamped  bool          `json:"stamped"`
	Attempt  int           `json:"attempt,omitempty"`
	GenTime  *time.Time    `json:"gen_time,omitempty"`
	Reason   verify.Reason `json:"reason,omitempty"`  // set unless Stamped
	Message  string        `json:"message,omitempty"` // German detail; never contains secrets
	Warning  string        `json:"warning,omitempty"` // German, e.g. the PC clock is off
}

// Result of a run.
type Result struct {
	Provider string    `json:"provider,omitempty"`
	Outcomes []Outcome `json:"outcomes"`
	Warnings []string  `json:"warnings,omitempty"` // German
}

// Reasons maps the Prüfwert of each still pending Ablage to its reason, for
// verify.Options.Reasons.
func (r *Result) Reasons() map[string]verify.Reason {
	m := map[string]verify.Reason{}
	for _, o := range r.Outcomes {
		if !o.Stamped && o.Reason != "" && o.Root != "" {
			m[o.Root] = o.Reason
		}
	}
	return m
}

type pending struct {
	dep      archive.Deposit
	root     string
	dir      string // the Nachweis folder a new attempt goes to
	attempts []verify.Attempt
}

// Run stamps every Ablage of a without a complete, valid attempt, in order.
// Read-only archives never stamp.
func Run(ctx context.Context, a *archive.Archive, opts Options) (*Result, error) {
	if a.ReadOnly() {
		return nil, archive.ErrReadOnly
	}
	if opts.Trust == nil || opts.Creds == nil || opts.Lock == nil {
		return nil, errors.New("stamp: Options.Trust, Creds and Lock are required")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	res := &Result{Outcomes: []Outcome{}}
	var prov tsa.Provider
	if id := a.Config.TSA; id != nil {
		p, ok := tsa.ProviderByID(*id)
		if !ok {
			return nil, fmt.Errorf("Zeitstempel-Anbieter %q ist nicht bekannt", *id)
		}
		prov, res.Provider = p, p.ID
	}

	release, err := opts.Lock()
	if err != nil {
		return nil, err
	}
	defer release()

	if prov.ID != "" {
		if err := opts.Trust.Refresh(ctx, prov.TrustedList); err != nil {
			res.Warnings = append(res.Warnings, "Vertrauenslisten konnten nicht aktualisiert werden: "+err.Error())
		}
	}
	todo, err := findPending(ctx, a, opts, res)
	if err != nil {
		return nil, err
	}
	if len(todo) == 0 {
		sortOutcomes(res)
		return res, nil
	}
	if prov.ID == "" {
		for _, p := range todo {
			res.Outcomes = append(res.Outcomes, outcome(p, verify.ReasonNoProvider, "Kein Zeitstempel-Dienst eingerichtet"))
		}
		sortOutcomes(res)
		return res, nil
	}

	s := &stamper{ctx: ctx, opts: opts, prov: prov, now: now, fetcher: opts.Fetcher, dir: a.Dir}
	if s.fetcher == nil {
		s.fetcher = &tsa.Fetcher{}
	}
	stop := s.setup(a.Config.ArchiveID)
	stamped := false
	for _, p := range todo {
		if stop != nil {
			res.Outcomes = append(res.Outcomes, outcome(p, verify.Reason(stop.Reason), stop.Msg))
			continue
		}
		o, fatal := s.stamp(p)
		if o.Warning != "" {
			res.Warnings = append(res.Warnings, o.Warning)
		}
		res.Outcomes = append(res.Outcomes, o)
		stamped = stamped || o.Stamped
		if fatal {
			stop = &tsa.Error{Reason: tsa.Reason(o.Reason), Msg: "nicht versucht: " + o.Message}
		}
	}
	if stamped { // spec §7: refresh right after a successful stamp
		if err := opts.Trust.Refresh(ctx, prov.TrustedList); err != nil {
			res.Warnings = append(res.Warnings, "Vertrauenslisten konnten nach dem Zeitstempel nicht aktualisiert werden: "+err.Error())
		}
	}
	sortOutcomes(res)
	return res, nil
}

// sortOutcomes orders the outcomes by Ablage number: failed checks are
// found before the requests are sent.
func sortOutcomes(res *Result) {
	slices.SortStableFunc(res.Outcomes, func(a, b Outcome) int { return a.Sequence - b.Sequence })
}

// findPending returns the Ablagen whose bag is intact and that have no
// valid attempt. Changed or invalid bags are reported, never stamped.
// Attempts pair by imprint across all Nachweis folders (spec §1), and key
// compromise anywhere in the archive counts (spec §6 step 7).
func findPending(ctx context.Context, a *archive.Archive, opts Options, res *Result) ([]pending, error) {
	deps, err := a.Deposits()
	if err != nil {
		return nil, err
	}
	var roots []string
	hints := map[string]string{}
	for _, d := range deps {
		if d.Err == nil {
			roots = append(roots, d.Bag.Root)
			hints[d.Bag.Root] = filepath.Base(d.EvidenceDir)
		}
	}
	ev, err := verify.Evidence(ctx, a.Dir, roots, hints, verify.Options{Trust: opts.Trust, Fetcher: opts.Fetcher})
	if err != nil {
		return nil, err
	}
	var out []pending
	for _, d := range deps {
		if d.Err != nil {
			// Never stamped; a failed outcome like a changed Ablage, so the
			// run does not look complete (verify reports it as Ungültig).
			res.Outcomes = append(res.Outcomes, outcome(pending{dep: d}, "", fmt.Sprintf("Ablage ist nicht lesbar – kein Zeitstempel angefordert: %v", d.Err)))
			continue
		}
		atts := ev[d.Bag.Root]
		if hasValid(atts) {
			continue
		}
		p := pending{dep: d, root: d.Bag.Root, dir: d.EvidenceDir, attempts: atts}
		if len(atts) > 0 {
			p.dir = filepath.Join(a.Dir, atts[0].Nachweis) // keep adding to the folder in use
		}
		if !intact(p) {
			// Never stamp a changed or invalid Ablage; verify reports it.
			res.Outcomes = append(res.Outcomes, outcome(p, "", "Ablage ist verändert oder ungültig – kein Zeitstempel angefordert"))
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// intact checks the bag fully (spec §6 steps 1–4) against its Prüfwert.
func intact(p pending) bool {
	res, err := bag.Check(p.dep.Path)
	return err == nil && res.Status == bag.OK && res.Bag.Root == p.root
}

func hasValid(atts []verify.Attempt) bool {
	for _, a := range atts {
		if a.Outcome == verify.OutcomeValid {
			return true
		}
	}
	return false
}

func outcome(p pending, r verify.Reason, msg string) Outcome {
	return Outcome{Sequence: p.dep.Sequence, Name: p.dep.Name, Root: p.root, Reason: r, Message: msg}
}

type stamper struct {
	ctx     context.Context
	opts    Options
	prov    tsa.Provider
	now     func() time.Time
	fetcher *tsa.Fetcher
	client  *tsa.Client
	dir     string // the archive folder
}

// setup builds the client from the stored credentials. A non-nil result
// stops every request (no credentials, no trusted list).
func (s *stamper) setup(archiveID string) *tsa.Error {
	if len(s.opts.Trust.Anchors()) == 0 {
		return &tsa.Error{Reason: tsa.ReasonPruefdaten, Msg: "Keine geprüfte EU-Vertrauensliste vorhanden"}
	}
	c, err := s.opts.Creds.Get(archiveID, s.prov.ID)
	if errors.Is(err, creds.ErrNotFound) {
		return &tsa.Error{Reason: tsa.ReasonZugang, Msg: "Keine Zugangsdaten auf diesem PC"}
	}
	if err != nil {
		return &tsa.Error{Reason: tsa.ReasonZugang, Msg: "Zugangsdaten nicht lesbar: " + err.Error()}
	}
	cl := &tsa.Client{URL: s.prov.URL, Auth: s.prov.Auth, RootCAs: s.opts.RootCAs, Timeout: s.opts.Timeout}
	if s.prov.URLFromContract {
		cl.URL = c.URL
	}
	if cl.URL == "" {
		return &tsa.Error{Reason: tsa.ReasonZugang, Msg: "Adresse des Zeitstempel-Dienstes fehlt (laut Vertrag eintragen)"}
	}
	switch s.prov.Auth {
	case tsa.AuthBasic:
		cl.Creds = tsa.Credentials{User: c.User, Password: c.Password}
	case tsa.AuthClientCert:
		p12, err := os.ReadFile(c.P12Path)
		if err != nil {
			return &tsa.Error{Reason: tsa.ReasonZugang, Msg: "Zertifikatsdatei nicht lesbar", Err: err}
		}
		cert, err := tsa.LoadClientCert(p12, c.Password)
		if err != nil {
			var te *tsa.Error
			if errors.As(err, &te) {
				return te
			}
			return &tsa.Error{Reason: tsa.ReasonZugang, Msg: "Zertifikatsdatei ungültig", Err: err}
		}
		cl.Creds = tsa.Credentials{ClientCert: cert}
	}
	s.client = cl
	return nil
}

// stamp handles one Ablage. fatal means the reason affects every further
// request too (offline, access, quota, provider), so no further requests
// are sent in this run.
func (s *stamper) stamp(p pending) (o Outcome, fatal bool) {
	o = outcome(p, "", "")

	// Re-verify the bag right before the request (spec §4).
	if !intact(p) {
		o.Message = "Ablage hat sich vor der Anfrage verändert – kein Zeitstempel angefordert"
		return o, false
	}

	// A token whose validation material is not complete yet: within 24 h
	// of its genTime, complete that attempt instead of paying for a new
	// stamp (spec §5). Only after the window is a new stamp requested.
	for _, a := range p.attempts {
		if a.Outcome != verify.OutcomeDiscarded {
			continue
		}
		dir := filepath.Join(s.dir, a.Nachweis)
		if tok := a.TSAToken(); tok != nil && s.inWindow(tok) {
			o.Attempt = a.N
			return s.complete(o, dir, a.N, tok), false
		}
		if tok := a.IncompleteToken(); tok != nil && s.inWindow(tok) {
			o.Attempt = a.N
			return s.resume(o, dir, a.N), false
		}
	}

	pw, _ := rootBytes(p.root)
	reqPolicy, err := s.prov.RequestPolicyOID()
	if err == nil {
		if dir, n, req := s.unanswered(&p, pw, reqPolicy); req != nil {
			return s.send(o, dir, n, req)
		}
		var req *tsa.Request
		if req, err = tsa.NewRequest(pw, reqPolicy); err == nil {
			return s.send(o, p.dir, 0, req)
		}
	}
	o.Reason, o.Message = verify.ReasonProvider, err.Error()
	return o, true
}

// unanswered returns the newest attempt of p in the folder new attempts go
// to (p.dir) if it got no response and went to this provider with this
// request policy. Its stored request (same bytes, same nonce) is sent
// again, so offline, 401 or 402 runs do not leave a new attempt each time.
// The request is on disk before it is sent again (write-before-send). Only
// a request this program would build is resent (Request.Canonical).
func (s *stamper) unanswered(p *pending, pw [32]byte, policy asn1.ObjectIdentifier) (string, int, *tsa.Request) {
	var last *verify.Attempt
	for i := range p.attempts {
		// Numbers count per folder; NextAttempt allocates in p.dir.
		if filepath.Join(s.dir, p.attempts[i].Nachweis) != p.dir {
			continue
		}
		if last == nil || p.attempts[i].N > last.N {
			last = &p.attempts[i]
		}
	}
	if last == nil || last.Outcome != verify.OutcomeNoResponse {
		return "", 0, nil
	}
	dir := p.dir
	prov, err := os.ReadFile(filepath.Join(dir, verify.ProviderFile(last.N)))
	if err != nil || strings.TrimSpace(string(prov)) != s.prov.ID {
		return "", 0, nil
	}
	der, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("%d.tsq", last.N)))
	if err != nil {
		return "", 0, nil
	}
	req, err := tsa.ParseRequest(der)
	if err != nil || req.Imprint != pw || !req.Policy.Equal(policy) || !req.Canonical() {
		return "", 0, nil
	}
	return dir, last.N, req
}

// inWindow reports whether evidence for tok may still be obtained (spec §5:
// produced within 24 h after genTime).
func (s *stamper) inWindow(tok *tsa.Token) bool {
	return !s.now().After(tok.GenTime.Add(tsa.GracePeriod))
}

// send sends req as attempt n of dir; n == 0 writes a new attempt first.
// A resent attempt (n > 0) is already on disk with its provider marker,
// both checked by unanswered.
func (s *stamper) send(o Outcome, dir string, n int, req *tsa.Request) (Outcome, bool) {
	resent := n > 0
	var err error
	if !resent {
		n, err = tsa.NextAttempt(dir)
		if err == nil {
			err = tsa.WriteRequest(dir, n, req.DER) // n.tsq before sending
		}
		if err == nil {
			// The provider the request goes to, for verification later
			// (declared accuracy, trusted list). It is not sent anywhere.
			err = writeOnce(filepath.Join(dir, verify.ProviderFile(n)), []byte(s.prov.ID))
		}
	}
	if err != nil {
		o.Reason, o.Message = verify.ReasonProvider, "Anfrage konnte nicht gespeichert werden: "+err.Error()
		return o, true
	}
	o.Attempt = n
	tsr, err := s.client.Send(s.ctx, req)
	if err != nil {
		var te *tsa.Error
		if !errors.As(err, &te) {
			te = &tsa.Error{Reason: tsa.ReasonAnbieter, Msg: "Anfrage fehlgeschlagen", Err: err}
		}
		if tsr != nil {
			tsa.WriteResponse(dir, n, tsr) // the provider's refusal, kept as evidence
		}
		o.Reason, o.Message = verify.Reason(te.Reason), te.Msg
		return o, true
	}
	policies, err := s.prov.ExpectedPolicyOIDs()
	if err != nil {
		o.Reason, o.Message = verify.ReasonProvider, err.Error()
		return o, true
	}
	tok, err := s.fetcher.Accept(s.ctx, tsr, tsa.VerifyOptions{Request: req, Policies: policies, Anchors: s.opts.Trust.Anchors()})
	if err != nil {
		// Not accepted (spec §5): the response is not stored.
		o.Reason, o.Message = verify.ReasonProvider, "Antwort des Anbieters ist kein gültiger Zeitstempel: "+err.Error()
		return o, true
	}
	if err := tsa.WriteResponse(dir, n, tsr); err != nil {
		o.Reason, o.Message = verify.ReasonProvider, "Antwort konnte nicht gespeichert werden: "+err.Error()
		return o, true
	}
	// A resent request may be answered with the token of its earlier send,
	// so only a genTime in the future says something about this PC's clock.
	if skew := tok.GenTime.Sub(s.now()); skew > ClockTolerance || (!resent && skew < -ClockTolerance) {
		o.Warning = fmt.Sprintf("Ablage %04d: Die Uhr dieses PCs weicht um %s von der Zeit des Zeitstempels ab. Bitte die PC-Uhr prüfen.", o.Sequence, skew.Abs().Round(time.Second))
	}
	return s.complete(o, dir, n, tok), false
}

// resume accepts the stored response of attempt n again, fetching the
// issuer certificates that were not stored (AIA), then completes it.
func (s *stamper) resume(o Outcome, dir string, n int) Outcome {
	raw, err := storedAttempt(dir, n)
	if err != nil {
		return material(o, err)
	}
	prov := s.prov
	if b, err := os.ReadFile(filepath.Join(dir, verify.ProviderFile(n))); err == nil {
		if p, ok := tsa.ProviderByID(strings.TrimSpace(string(b))); ok {
			prov = p // the provider the request went to
		}
	}
	policies, err := prov.ExpectedPolicyOIDs()
	if err != nil {
		return material(o, err)
	}
	opts := tsa.VerifyOptions{Request: raw.Request, Policies: policies, Anchors: s.opts.Trust.Anchors()}
	if raw.Material != nil && raw.MaterialErr == nil {
		opts.Intermediates = raw.Material.Chain
	}
	tok, err := s.fetcher.Accept(s.ctx, raw.TSR, opts)
	if err != nil {
		return material(o, err)
	}
	return s.complete(o, dir, n, tok)
}

// complete stores what attempt n still lacks: first the chain
// (n.chain.pem, as soon as it holds every issuer the revocation evidence
// needs, so certificates fetched via AIA are never lost), then the
// revocation evidence of each certificate whose stored evidence is missing
// or not acceptable, one by one, so the pieces fetched in time are kept
// even if another one fails. Nothing stored is fetched again. A stored
// chain is only ever extended (spec §5): missing path certificates and the
// issuer of a signer that is itself the trusted-list identity are
// appended, the issuer taken from the token or fetched via AIA.
func (s *stamper) complete(o Outcome, dir string, n int, tok *tsa.Token) Outcome {
	raw, err := storedAttempt(dir, n)
	if err != nil {
		return material(o, err)
	}
	if raw.MaterialErr != nil {
		return material(o, raw.MaterialErr)
	}
	have := map[string]tsa.Evidence{}
	var stored []*x509.Certificate
	if m := raw.Material; m != nil {
		have, stored = m.Evidence, m.Chain
	}
	chain := slices.Clone(stored)
	for _, c := range chainOf(tok) {
		if !slices.ContainsFunc(chain, c.Equal) {
			chain = append(chain, c)
		}
	}
	if top := tok.Chain[0]; len(tok.Chain) == 1 && issuerOf(top, nil, chain) == nil {
		iss := s.fetcher.FetchIssuer(s.ctx, top)
		if iss == nil {
			return material(o, fmt.Errorf("Aussteller-Zertifikat von %s nicht abrufbar", top.Subject.CommonName))
		}
		chain = append(chain, iss)
	}
	if len(chain) > len(stored) {
		if err := tsa.WriteMaterial(dir, n, &tsa.Material{Chain: chain, Evidence: map[string]tsa.Evidence{}}); err != nil {
			return material(o, fmt.Errorf("Zertifikatskette konnte nicht gespeichert werden: %w", err))
		}
	}

	merged := &tsa.Material{Chain: chain, Evidence: map[string]tsa.Evidence{}}
	for k, ev := range have {
		merged.Evidence[k] = ev
	}
	checks, err := tsa.ValidateMaterial(tok, merged)
	if checks == nil && err != nil {
		return material(o, err)
	}
	fresh := map[string]tsa.Evidence{}
	var missing []string
	for _, c := range checks {
		if c.Err == nil {
			continue // stored and acceptable
		}
		key := tsa.CertKey(c.Cert)
		ev, err := s.fetchOne(tok, c.Cert, chain)
		if err == nil {
			if old, ok := have[key]; ok && old.Kind == ev.Kind && !bytes.Equal(old.Raw, ev.Raw) {
				err = fmt.Errorf("gespeicherte %s-Antwort ist nicht verwendbar und wird nicht ersetzt", ev.Kind)
			}
		}
		if err != nil {
			missing = append(missing, c.Cert.Subject.CommonName+": "+err.Error())
			continue
		}
		fresh[key], merged.Evidence[key] = ev, ev
	}
	if len(fresh) > 0 {
		if err := tsa.WriteMaterial(dir, n, &tsa.Material{Chain: chain, Evidence: fresh}); err != nil {
			return material(o, err)
		}
	}
	if len(missing) > 0 {
		return material(o, errors.New(strings.Join(missing, "; ")))
	}
	if _, err := tsa.ValidateMaterial(tok, merged); err != nil {
		return material(o, err)
	}
	g := tok.GenTime.UTC()
	o.Stamped, o.GenTime, o.Reason, o.Message = true, &g, "", ""
	return o
}

// fetchOne fetches acceptable revocation evidence for cert alone. It asks
// the Fetcher for a token whose path is just cert and its issuer.
func (s *stamper) fetchOne(tok *tsa.Token, cert *x509.Certificate, chain []*x509.Certificate) (tsa.Evidence, error) {
	pool := append(slices.Clone(chain), tok.Certs...)
	iss := issuerOf(cert, tok.Chain, pool)
	if iss == nil {
		return tsa.Evidence{}, errors.New("Aussteller-Zertifikat fehlt")
	}
	sub := &tsa.Token{GenTime: tok.GenTime, Signer: cert, Chain: []*x509.Certificate{cert, iss}, Certs: pool}
	m, checks, err := s.fetcher.Collect(s.ctx, sub)
	if err != nil {
		for _, c := range checks {
			if c.Err != nil {
				return tsa.Evidence{}, c.Err
			}
		}
		return tsa.Evidence{}, err
	}
	ev, ok := m.Evidence[tsa.CertKey(cert)]
	if !ok {
		return tsa.Evidence{}, errors.New("keine Sperrinformation")
	}
	return ev, nil
}

func material(o Outcome, err error) Outcome {
	o.Reason, o.Message = verify.ReasonMaterial, "Prüfdaten (Zertifikatskette, Sperrstatus) noch nicht vollständig: "+err.Error()
	return o
}

func storedAttempt(dir string, n int) (*tsa.Attempt, error) {
	atts, err := tsa.ReadAttempts(dir)
	if err != nil {
		return nil, err
	}
	for i := range atts {
		if atts[i].N == n && atts[i].TSR != nil && atts[i].Request != nil {
			return &atts[i], nil
		}
	}
	return nil, fmt.Errorf("Versuch %d nicht gefunden", n)
}

// chainOf is what the token itself contributes to n.chain.pem: the
// validated path, plus the issuer of the signer from the token's
// certificates when the signer itself is the trusted-list identity.
func chainOf(tok *tsa.Token) []*x509.Certificate {
	ch := slices.Clone(tok.Chain)
	if len(ch) == 1 {
		if iss := issuerOf(ch[0], nil, tok.Certs); iss != nil {
			ch = append(ch, iss)
		}
	}
	return ch
}

// issuerOf returns c's issuer: the next certificate of the path, else one
// from pool that signed c.
func issuerOf(c *x509.Certificate, path, pool []*x509.Certificate) *x509.Certificate {
	for i, p := range path {
		if p.Equal(c) && i+1 < len(path) {
			return path[i+1]
		}
	}
	for _, p := range pool {
		if !p.Equal(c) && bytes.Equal(p.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(p) == nil {
			return p
		}
	}
	return nil
}

func rootBytes(root string) ([32]byte, error) {
	var r [32]byte
	var b []byte
	if _, err := fmt.Sscanf(root, "sha256:%x", &b); err != nil || len(b) != 32 {
		return r, fmt.Errorf("bad Prüfwert %q", root)
	}
	copy(r[:], b)
	return r, nil
}
