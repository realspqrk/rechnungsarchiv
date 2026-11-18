// Package verify is the single verification engine (spec §6). It checks a
// whole archive, an export subset or a single Ablage and returns a Report
// that the CLI, the GUI and the Archivprotokoll render. It never writes.
package verify

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

// Options configures a run.
type Options struct {
	// Trust supplies the verified trusted lists. Required. The caller opens
	// it (read-only mode: trust.OpenReadOnly).
	Trust *trust.Store
	// Online refreshes the trusted lists and fetches current revocation
	// status (spec §6 step 7, §7).
	Online  bool
	Fetcher *tsa.Fetcher
	// Provider is a provider id used as a hint for the declared accuracy and
	// the trusted-list territory. In archive mode it defaults to the
	// provider in rechnungsarchiv.json.
	Provider string
	// Reasons are Ausstehend reasons known from the last stamping run, keyed
	// by Prüfwert. They take precedence over what the evidence shows.
	Reasons map[string]Reason
	Now     func() time.Time
}

// ErrNoTrust is returned when Options.Trust is missing.
var ErrNoTrust = errors.New("verify: Options.Trust is required")

// Path verifies whatever path is: an archive folder (with
// rechnungsarchiv.json), an export folder (Ablage ZIPs without it), or a
// single Ablage ZIP or extracted bag.
func Path(ctx context.Context, path string, opts Options) (*Report, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.IsDir() {
		if _, err := os.Stat(filepath.Join(path, "rechnungsarchiv.json")); err == nil {
			return Archive(ctx, path, opts)
		}
		if zips, _ := listZips(path); len(zips) > 0 {
			return Export(ctx, path, opts)
		}
	}
	return Single(ctx, path, opts)
}

// Archive verifies a whole archive (read-only, spec §9).
func Archive(ctx context.Context, dir string, opts Options) (*Report, error) {
	a, err := archive.OpenReadOnly(dir)
	if err != nil {
		return nil, err
	}
	if opts.Provider == "" && a.Config.TSA != nil {
		opts.Provider = *a.Config.TSA
	}
	e, rep, err := start(ctx, ModeArchive, dir, opts)
	if err != nil {
		return nil, err
	}
	rep.ArchiveID, rep.Company = a.Config.ArchiveID, a.Config.Company
	deps, err := a.Deposits()
	if err != nil {
		return nil, err
	}
	for _, d := range deps {
		dep, err := e.deposit(d.Path, d.EvidenceDir, d.Sequence, d.Name)
		if err != nil {
			return nil, err
		}
		if dep.Status == StatusPending && dep.Reason == "" && a.Config.TSA == nil {
			dep.Reason = ReasonNoProvider
		}
		rep.Deposits = append(rep.Deposits, *dep)
	}
	finish(e, rep, opts, nil)
	return rep, nil
}

// Export verifies an export folder (spec §8): only the evidence inside it
// counts, excluded Ablagen are bridged by Brückendaten.
func Export(ctx context.Context, dir string, opts Options) (*Report, error) {
	e, rep, err := start(ctx, ModeExport, dir, opts)
	if err != nil {
		return nil, err
	}
	zips, err := listZips(dir)
	if err != nil {
		return nil, err
	}
	for _, z := range zips {
		dep, err := e.deposit(filepath.Join(dir, z.file), filepath.Join(dir, strings.TrimSuffix(z.file, ".zip")+".nachweis"), z.seq, z.name)
		if err != nil {
			return nil, err
		}
		rep.Deposits = append(rep.Deposits, *dep)
	}
	bridges, err := readBridges(filepath.Join(dir, BridgeDir))
	if err != nil {
		return nil, err
	}
	finish(e, rep, opts, bridges)
	return rep, nil
}

// Single verifies a single Ablage ZIP or extracted bag directory. Its
// Nachweis is looked up next to it.
func Single(ctx context.Context, path string, opts Options) (*Report, error) {
	e, rep, err := start(ctx, ModeDeposit, path, opts)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(filepath.Clean(path))
	stem := strings.TrimSuffix(base, ".zip")
	seq, name := 0, stem
	if m := reZip.FindStringSubmatch(stem + ".zip"); m != nil {
		seq, _ = strconv.Atoi(m[1])
		name = m[2]
	}
	evDir := filepath.Join(filepath.Dir(path), stem+".nachweis")
	dep, err := e.deposit(path, evDir, seq, name)
	if err != nil {
		return nil, err
	}
	rep.Deposits = append(rep.Deposits, *dep)
	finish(e, rep, opts, nil)
	return rep, nil
}

// Attempts evaluates the stored attempts for one Prüfwert, as verification
// does (spec §6 step 5). Stamping uses it to find Ablagen without a valid
// attempt.
func Attempts(ctx context.Context, evidenceDir, root string, opts Options) ([]Attempt, error) {
	e, _, err := start(ctx, ModeDeposit, evidenceDir, opts)
	if err != nil {
		return nil, err
	}
	r, err := parseRoot(root)
	if err != nil {
		return nil, err
	}
	return e.evaluate(evidenceDir, r)
}

func start(ctx context.Context, mode Mode, path string, opts Options) (*env, *Report, error) {
	if opts.Trust == nil {
		return nil, nil, ErrNoTrust
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}
	rep := &Report{Mode: mode, Path: path, CheckedAt: now().UTC(), Online: opts.Online, Provider: opts.Provider}
	if opts.Online {
		if err := opts.Trust.Refresh(ctx, Territories(opts.Trust, opts.Provider)...); err != nil {
			rep.TrustProblems = append(rep.TrustProblems, err.Error())
		}
	}
	e := &env{ctx: ctx, trust: opts.Trust, anchors: opts.Trust.Anchors(), online: opts.Online, fetcher: opts.Fetcher, now: now()}
	if opts.Provider != "" {
		if p, ok := tsa.ProviderByID(opts.Provider); ok {
			e.provider = &p
		}
	}
	return e, rep, nil
}

// Territories are the national lists to refresh (spec §7): the configured
// provider's and those already cached (the lists in use).
func Territories(s *trust.Store, provider string) []string {
	var out []string
	if p, ok := tsa.ProviderByID(provider); ok {
		out = append(out, p.TrustedList)
	}
	for _, l := range s.Lists() {
		if l.Territory != "EU" {
			out = append(out, l.Territory)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

func finish(e *env, rep *Report, opts Options, bridges []Bridge) {
	for _, l := range opts.Trust.Lists() {
		_, online := l.FetchedOnline()
		rep.TrustLists = append(rep.TrustLists, TrustList{Territory: l.Territory, Sequence: l.Sequence, IssueDate: l.IssueDate.UTC(), FetchedOnline: online})
	}
	for _, p := range opts.Trust.Problems() {
		rep.TrustProblems = append(rep.TrustProblems, p.Error())
	}
	for i := range rep.Deposits {
		d := &rep.Deposits[i]
		if d.Status == StatusPending {
			if r, ok := opts.Reasons[d.Root]; ok {
				d.Reason = r
			}
		}
	}
	rep.Bridged = bridges
	for _, d := range rep.Deposits {
		if rep.Company == "" {
			rep.Company = d.Company // Source-Organization, without rechnungsarchiv.json
		}
	}
	chain(rep)
	rep.Status = StatusTimestamped
	for _, d := range rep.Deposits {
		rep.Status = Worse(rep.Status, d.Status)
	}
	if rep.Deposits == nil {
		rep.Deposits = []Deposit{}
	}
	if rep.TrustLists == nil {
		rep.TrustLists = []TrustList{}
	}
}

// deposit runs spec §6 steps 1–8 (without inheritance) for one Ablage.
func (e *env) deposit(path, evDir string, seq int, name string) (*Deposit, error) {
	res, err := bag.Check(path)
	if err != nil {
		return nil, err
	}
	d := &Deposit{Sequence: seq, Name: name, File: filepath.Base(filepath.Clean(path)), Files: []File{}}
	switch res.Status {
	case bag.Changed:
		d.Status = StatusChanged
	case bag.Invalid:
		d.Status = StatusInvalid
	default:
		d.Status = StatusTimestamped // lowered below by the evidence
	}
	for _, p := range res.Problems {
		st := StatusChanged
		if p.Status == bag.Invalid {
			st = StatusInvalid
		}
		d.Problems = append(d.Problems, Problem{Step: p.Step, Status: st, Message: p.Msg})
	}
	b := res.Bag
	if b != nil {
		d.Root = b.Root
		if b.Chain != nil {
			d.ArchiveID, d.Previous = b.Chain.ArchiveID, b.Chain.Previous
			d.fileSeq = seq // file names are only a hint; chain.json decides
			d.Sequence = b.Chain.Sequence
		}
		if i := b.Info; i != nil {
			d.Company, d.Belegjahr, d.BaggingDate, d.ArchivedBy = i.SourceOrganization, i.Belegjahr, i.BaggingDate, i.ArchivedBy
			d.RetentionYears, d.RetentionUntil, d.ToolVersion = i.RetentionYears, i.RetentionUntil, i.ToolVersion
		}
		if name == "" || seq == 0 {
			if _, n, ok := strings.Cut(b.Dir, "_"); ok {
				d.Name = n
			}
		}
		d.files(res)
	}
	if d.Root == "" {
		if d.Status != StatusInvalid {
			d.Status = StatusChanged
		}
		return d, nil
	}
	root, err := parseRoot(d.Root)
	if err != nil {
		return nil, err
	}
	if d.Attempts, err = e.evaluate(evDir, root); err != nil {
		return nil, err
	}
	d.evidence(e)
	return d, nil
}

func (d *Deposit) files(res *bag.Result) {
	state := map[string]FileState{}
	for _, p := range res.Matched {
		state[p] = FileOK
	}
	for _, p := range res.Changed {
		state[p] = FileChanged
	}
	for _, p := range res.Missing {
		state[p] = FileMissing
	}
	b := res.Bag
	if b.Files != nil {
		for _, f := range b.Files {
			d.Files = append(d.Files, File{Path: f.Path, Size: f.Size, MTime: f.MTime, SHA256: f.SHA256, State: state[f.Path]})
			d.Counts.Bytes += f.Size
		}
	} else {
		for _, m := range b.Manifest {
			d.Files = append(d.Files, File{Path: m.Path, SHA256: m.SHA256, State: state[m.Path]})
		}
	}
	for i := range d.Files {
		if d.Files[i].State == "" {
			d.Files[i].State = FileChanged // listed in files.json, not in the manifest
		}
	}
	d.Added, d.Ignored = res.Added, res.Ignored
	d.Counts.Files = len(b.Manifest)
	d.Counts.Matched, d.Counts.Changed, d.Counts.Missing = len(res.Matched), len(res.Changed), len(res.Missing)
	d.Counts.Added, d.Counts.Ignored = len(res.Added), len(res.Ignored)
}

// evidence applies spec §6 steps 5–8 to the evaluated attempts.
func (d *Deposit) evidence(e *env) {
	var valid, compromisedAt *Attempt
	invalid, noTrust, discarded, rejected := false, false, false, false
	for i := range d.Attempts {
		a := &d.Attempts[i]
		switch a.Outcome {
		case OutcomeValid:
			if valid == nil {
				valid = a
			}
		case OutcomeCompromised:
			if compromisedAt == nil {
				compromisedAt = a
			}
		case OutcomeInvalid:
			invalid = true
		case OutcomeNoTrust:
			noTrust = true
		case OutcomeDiscarded:
			discarded = true
		case OutcomeRejected:
			rejected = true
		}
	}
	var st Status
	switch {
	case valid != nil:
		st = StatusTimestamped
		var p *Proven
		d.Token, p = e.token(valid.N, valid.token)
		d.Qualification = d.Token.Qualification
		if d.Status == StatusTimestamped {
			d.Proven = p
		}
	case invalid:
		st = StatusInvalid
	case compromisedAt != nil || noTrust:
		st = StatusIndeterminate
		if compromisedAt != nil {
			d.Token, _ = e.token(compromisedAt.N, compromisedAt.token)
		}
	default:
		st = StatusPending
		switch {
		case discarded:
			d.Reason = ReasonMaterial
		case rejected:
			d.Reason = ReasonProvider // a stored refusal of the provider
		}
	}
	if d.Status == StatusTimestamped {
		d.Status = st
	} else {
		d.Status = Worse(d.Status, st)
	}
	if d.Status != StatusTimestamped {
		d.Qualification = ""
	}
	if d.Status != StatusPending {
		d.Reason = ""
	}
}

func parseRoot(root string) ([32]byte, error) {
	var r [32]byte
	h, ok := strings.CutPrefix(root, "sha256:")
	b, err := hex.DecodeString(h)
	if !ok || err != nil || len(b) != 32 {
		return r, fmt.Errorf("verify: bad Prüfwert %q", root)
	}
	copy(r[:], b)
	return r, nil
}

var reZip = regexp.MustCompile(`^(\d{4,})_(.+)\.zip$`)

type zipName struct {
	file, name string
	seq        int
}

func listZips(dir string) ([]zipName, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []zipName
	for _, de := range des {
		if m := reZip.FindStringSubmatch(de.Name()); m != nil && de.Type().IsRegular() {
			seq, _ := strconv.Atoi(m[1])
			out = append(out, zipName{file: de.Name(), name: m[2], seq: seq})
		}
	}
	return out, nil
}
