// Package stamp fetches the missing timestamps of an archive (spec §4 step
// 5, §5): one provider, no fallback, evidence written append-only.
package stamp

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
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
	todo, err := findPending(ctx, a, opts, prov.ID, res)
	if err != nil {
		return nil, err
	}
	if len(todo) == 0 {
		return res, nil
	}
	if prov.ID == "" {
		for _, p := range todo {
			res.Outcomes = append(res.Outcomes, outcome(p, verify.ReasonNoProvider, "Kein Zeitstempel-Dienst eingerichtet"))
		}
		return res, nil
	}

	s := &stamper{ctx: ctx, opts: opts, prov: prov, now: now, fetcher: opts.Fetcher}
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
	return res, nil
}

// findPending returns the Ablagen whose bag is intact and that have no
// valid attempt. Changed or invalid bags are reported, never stamped.
func findPending(ctx context.Context, a *archive.Archive, opts Options, provider string, res *Result) ([]pending, error) {
	deps, err := a.Deposits()
	if err != nil {
		return nil, err
	}
	var out []pending
	for _, d := range deps {
		if d.Err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Ablage %04d %s ist nicht lesbar: %v", d.Sequence, d.Name, d.Err))
			continue
		}
		atts, err := verify.Attempts(ctx, d.EvidenceDir, d.Bag.Root, verify.Options{Trust: opts.Trust, Provider: provider, Fetcher: opts.Fetcher})
		if err != nil {
			return nil, err
		}
		if hasValid(atts) {
			continue
		}
		p := pending{dep: d, root: d.Bag.Root, attempts: atts}
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
	dir := p.dep.EvidenceDir

	// Re-verify the bag right before the request (spec §4).
	if !intact(p) {
		o.Message = "Ablage hat sich vor der Anfrage verändert – kein Zeitstempel angefordert"
		return o, false
	}

	// A valid token whose material could not be fetched yet: retry the
	// material within the 24 h window instead of paying for a new stamp.
	for _, a := range p.attempts {
		tok := a.TSAToken()
		if a.Outcome != verify.OutcomeDiscarded || a.Material || tok == nil || s.now().After(tok.GenTime.Add(tsa.GracePeriod)) {
			continue
		}
		o.Attempt = a.N
		return s.material(o, dir, a.N, tok), false
	}

	pw, _ := rootBytes(p.root)
	reqPolicy, err := s.prov.RequestPolicyOID()
	if err == nil {
		var req *tsa.Request
		if req, err = tsa.NewRequest(pw, reqPolicy); err == nil {
			return s.send(o, dir, req)
		}
	}
	o.Reason, o.Message = verify.ReasonProvider, err.Error()
	return o, true
}

func (s *stamper) send(o Outcome, dir string, req *tsa.Request) (Outcome, bool) {
	n, err := tsa.NextAttempt(dir)
	if err == nil {
		err = tsa.WriteRequest(dir, n, req.DER) // n.tsq before sending
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
	if skew := tok.GenTime.Sub(s.now()); skew > ClockTolerance || skew < -ClockTolerance {
		o.Warning = fmt.Sprintf("Ablage %04d: Die Uhr dieses PCs weicht um %s von der Zeit des Zeitstempels ab. Bitte die PC-Uhr prüfen.", o.Sequence, skew.Abs().Round(time.Second))
	}
	return s.material(o, dir, n, tok), false
}

// material fetches and stores the validation material of attempt n.
func (s *stamper) material(o Outcome, dir string, n int, tok *tsa.Token) Outcome {
	m, _, err := s.fetcher.Collect(s.ctx, tok)
	if err == nil {
		err = tsa.WriteMaterial(dir, n, m)
	}
	if err != nil {
		o.Reason, o.Message = verify.ReasonMaterial, "Prüfdaten (Zertifikatskette, Sperrstatus) noch nicht vollständig: "+err.Error()
		return o
	}
	g := tok.GenTime.UTC()
	o.Stamped, o.GenTime, o.Reason, o.Message = true, &g, "", ""
	return o
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
