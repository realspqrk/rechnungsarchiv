package main

// Mapping of core results (verify.Report, stamp.Result, archive.Preview,
// tsa.Provider, trust.Store) to the DTOs of service.go. Only renaming and
// counting happens here; every fact comes from the core.

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/bag"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// retentionOptions are the periods archive.Options accepts (ADR-0006).
func retentionOptions() []RetentionOption {
	return []RetentionOption{
		{7, "7 Jahre (gesetzlich, § 132 BAO)"},
		{10, "10 Jahre (freiwillig)"},
		{12, "12 Jahre (WGG)"},
		{22, "22 Jahre (Grundstücke)"},
	}
}

func totalFiles(ds []Deposit) int {
	n := 0
	for _, d := range ds {
		n += d.Files
	}
	return n
}

func countStates(ds []Deposit) Counts {
	c := Counts{Total: len(ds)}
	for _, d := range ds {
		switch d.State {
		case StateStamped:
			c.Stamped++
		case StatePending:
			c.Pending++
		case StateIndeterminate:
			c.Indeterminate++
		case StateModified:
			c.Modified++
		case StateInvalid:
			c.Invalid++
		}
	}
	return c
}

func stateOf(s verify.Status) string {
	switch s {
	case verify.StatusTimestamped:
		return StateStamped
	case verify.StatusPending:
		return StatePending
	case verify.StatusIndeterminate:
		return StateIndeterminate
	case verify.StatusChanged:
		return StateModified
	}
	return StateInvalid
}

func qualOf(q verify.Qualification) string {
	switch q {
	case verify.Qualified:
		return QualQualified
	case verify.NotQualified:
		return QualNotQualified
	case verify.Unconfirmed:
		return QualUnconfirmed
	}
	return ""
}

// reasonOf maps verify.Reason 1:1; only prüfdaten is spelled in ASCII.
func reasonOf(r verify.Reason) string {
	if r == verify.ReasonMaterial {
		return ReasonEvidence
	}
	return string(r)
}

func rfc3339(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// isoDate is the calendar date in Vienna, e.g. for a trusted list's issue.
func isoDate(t time.Time) string { return t.In(present.Vienna).Format("2006-01-02") }

// ownToken is the Ablage's own token (not a successor's proof).
func ownToken(d *verify.Deposit) *verify.Token {
	if d.Token == nil || d.Proven != nil && d.Proven.Via != 0 {
		return nil
	}
	return d.Token
}

func providerName(id string) string {
	if p, ok := tsa.ProviderByID(id); ok {
		return providerOf(p).Name
	}
	return id
}

func tokenProvider(t *verify.Token) (name, country string) {
	if p, ok := tsa.ProviderByID(t.Provider); t.Provider != "" && ok {
		return providerOf(p).Name, p.Country
	}
	name = t.TSP
	if name == "" {
		name = "unbekannter Anbieter"
	}
	if t.TrustList != nil {
		country = t.TrustList.Territory
	}
	return name, country
}

func depositOf(rep *verify.Report, d *verify.Deposit) Deposit {
	out := Deposit{Seq: d.Sequence, Name: d.Name, Belegjahr: d.Belegjahr, Files: d.Counts.Files, Bytes: d.Counts.Bytes,
		State: stateOf(d.Status), File: d.File}
	switch d.Status {
	case verify.StatusTimestamped:
		out.Qualification = qualOf(d.Qualification)
	case verify.StatusPending:
		out.Reason = reasonOf(d.Reason)
	}
	if t := ownToken(d); t != nil {
		out.GenTime = t.GenTime.Format(time.RFC3339Nano)
		out.Provider, _ = tokenProvider(t)
	}
	if p := d.Proven; p != nil {
		out.ProvenLatest = rfc3339(p.At)
		out.InheritedFrom = p.Via
		if p.Via != 0 {
			out.Qualification = qualOf(p.Qualification)
		}
	}
	return out
}

func findingsOf(rep *verify.Report) []Finding {
	var out []Finding
	for _, f := range rep.Findings {
		x := Finding{Seq: f.Sequence, Text: present.Finding(f)}
		switch f.Code {
		case verify.FindingGap:
			x.Kind = FindingGap
			x.Successor = max(f.To, f.Sequence) + 1
		case verify.FindingDuplicateNumber:
			x.Kind = FindingDuplicate
		case verify.FindingPredecessorNotInExport:
			continue // neutral, and never in a whole archive
		default:
			x.Kind = "befund"
		}
		out = append(out, x)
	}
	return out
}

func checkOf(rep *verify.Report) *CheckSummary {
	c := &CheckSummary{At: rep.CheckedAt.Format(time.RFC3339), Deposits: len(rep.Deposits), Findings: findingsOf(rep)}
	ds := make([]Deposit, len(rep.Deposits))
	for i := range rep.Deposits {
		ds[i] = depositOf(rep, &rep.Deposits[i])
		c.Files += ds[i].Files
		if i == 0 || ds[i].Seq < c.FirstSeq {
			c.FirstSeq = ds[i].Seq
		}
		c.LastSeq = max(c.LastSeq, ds[i].Seq)
	}
	c.Counts = countStates(ds)
	c.Deviations = c.Counts.Modified + c.Counts.Invalid
	return c
}

const maxChangedPaths = 20

func depositReportOf(rep *verify.Report, d *verify.Deposit, readOnly bool, dir string) DepositReport {
	out := DepositReport{
		Deposit: depositOf(rep, d), ReadOnly: readOnly, CheckedAt: rep.CheckedAt.Format(time.RFC3339),
		Path: archivePath(dir, d.File), Root: d.Root,
		Files:          FileCounts{Unchanged: d.Counts.Matched, Changed: d.Counts.Changed, Missing: d.Counts.Missing, Added: d.Counts.Added},
		RetentionYears: d.RetentionYears, RetentionUntil: d.RetentionUntil,
	}
	for _, f := range d.Files {
		if f.State != verify.FileOK {
			out.ChangedPaths = append(out.ChangedPaths, strings.TrimPrefix(f.Path, "data/"))
		}
	}
	for _, p := range d.Added {
		out.ChangedPaths = append(out.ChangedPaths, strings.TrimPrefix(p, "data/"))
	}
	if len(out.ChangedPaths) > maxChangedPaths {
		out.ChangedPaths = out.ChangedPaths[:maxChangedPaths]
	}
	if t := ownToken(d); t != nil {
		st := &StampInfo{GenTime: t.GenTime.Format(time.RFC3339Nano), Qualification: qualOf(t.Qualification),
			SignatureValid: true, KeyCompromise: t.Compromised}
		st.Provider, st.ProviderCountry = tokenProvider(t)
		if t.AccuracyMS != nil {
			st.AccuracyMs = int(*t.AccuracyMS)
		}
		if d.Proven != nil {
			st.ProvenLatest = rfc3339(d.Proven.At)
		}
		if l := t.TrustList; l != nil {
			st.TrustListCountry, st.TrustListDate = l.Territory, isoDate(l.IssueDate)
		}
		// Revocation evidence of the signer certificate.
		i := slices.IndexFunc(t.Revocation, func(r verify.Revocation) bool { return r.Subject == t.Signer })
		if i < 0 && len(t.Revocation) > 0 {
			i = 0
		}
		if i >= 0 {
			st.RevocationSource = strings.ToUpper(t.Revocation[i].Kind)
		}
		out.Stamp = st
	}
	if p := d.Proven; p != nil && p.Via != 0 {
		out.Inherited = &InheritedProof{FromSeq: p.Via, ProvenLatest: rfc3339(p.At), Qualification: qualOf(p.Qualification)}
	}
	if d.Previous != nil && d.Sequence > 1 {
		out.Predecessor = d.Sequence - 1
		out.PredecessorPresent = slices.ContainsFunc(rep.Deposits, func(x verify.Deposit) bool { return x.Sequence == d.Sequence-1 })
	}
	return out
}

func archivePath(dir, file string) string {
	if file == "" {
		return ""
	}
	return filepath.Join(dir, file)
}

func stampResultOf(res *stamp.Result) StampResult {
	var out StampResult
	for _, o := range res.Outcomes {
		switch {
		case o.Stamped:
			out.Stamped++
		case o.Reason != "": // "" = changed or invalid: shown as such, not pending
			out.Pending++
			if out.Reason == "" {
				out.Reason = reasonOf(o.Reason)
			}
		}
	}
	return out
}

// previewProblem maps a scan error (bag.Scan stops at the first violation)
// to the problem the dialog lists.
func previewProblem(err error) (PreviewProblem, bool) {
	kinds := []struct {
		err  error
		kind string
	}{{bag.ErrInvalidName, "name"}, {bag.ErrCollision, "collision"}, {bag.ErrPlaceholder, "placeholder"}, {bag.ErrLink, "link"}}
	for _, k := range kinds {
		if errors.Is(err, k.err) {
			path := err.Error()
			if _, rest, ok := strings.Cut(path, k.err.Error()+": "); ok {
				path = rest
			}
			return PreviewProblem{Path: path, Kind: k.kind}, true
		}
	}
	return PreviewProblem{}, false
}

func previewOf(out *Preview, p *archive.Preview) {
	years := map[int]int{}
	for _, f := range p.Input.Files {
		out.Files++
		out.Bytes += f.Size
		years[f.ModTime.Year()]++
	}
	for y, n := range years {
		out.Years = append(out.Years, YearCount{Year: y, Files: n})
	}
	slices.SortFunc(out.Years, func(a, b YearCount) int { return cmp.Or(b.Files-a.Files, b.Year-a.Year) })
	out.Skipped = p.Input.Skipped
	if len(p.Known) > 0 {
		out.Duplicates = []DuplicateHint{{Files: len(p.Known)}}
	}
	if p.Name != "" {
		out.SuggestedName = p.Name
	}
	switch {
	case p.Belegjahr != 0:
		out.SuggestedYear = p.Belegjahr
	case len(out.Years) > 0:
		out.SuggestedYear = out.Years[0].Year
	default:
		out.SuggestedYear = time.Now().Year()
	}
}

func exportPlanOf(rep *verify.Report, from, to int) ExportPlan {
	p := ExportPlan{From: from, To: to}
	for _, d := range rep.Deposits {
		if d.Belegjahr != 0 && !slices.Contains(p.Years, d.Belegjahr) {
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
	for _, d := range rep.Deposits {
		if d.Belegjahr >= p.From && d.Belegjahr <= p.To {
			p.Deposits++
			p.Files += d.Counts.Files
			p.Bytes += d.Counts.Bytes
		}
	}
	return p
}

func providerOf(p tsa.Provider) Provider {
	name := p.ShortName
	if name == "" {
		name = p.Name
	}
	return Provider{ID: p.ID, Name: name, Country: p.Country, Auth: p.Auth, Recommended: p.Recommended,
		URLFromContract: p.URLFromContract, Website: p.Website}
}

func providerRef(id string) *ProviderRef {
	if p, ok := tsa.ProviderByID(id); ok {
		return &ProviderRef{ID: p.ID, Name: providerOf(p).Name, Country: p.Country}
	}
	return &ProviderRef{ID: id, Name: fmt.Sprintf("%s (dieser Version unbekannt)", id)}
}

// trustInfo is the main screen's trusted-list line. Stale follows spec §7:
// the newest cached list is past its NextUpdate and the last refresh failed.
func trustInfo(ts *trust.Store, refreshErr error, now time.Time) TrustListInfo {
	var out TrustListInfo
	if ts == nil {
		return out
	}
	var newest time.Time
	past := false
	lists := ts.Lists()
	for _, l := range lists {
		if !l.NextUpdate.IsZero() && now.After(l.NextUpdate) {
			past = true
		}
		if l.Territory != "EU" && l.IssueDate.After(newest) {
			newest = l.IssueDate
		}
	}
	if !newest.IsZero() {
		out.Date = isoDate(newest)
	}
	out.Stale = refreshErr != nil && (past || len(lists) == 0)
	return out
}
