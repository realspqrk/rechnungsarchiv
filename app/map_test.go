package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

func root(c string) string { return "sha256:" + strings.Repeat(c, 64) }

// Code review finding 4: the bag result and the token result are carried
// separately. A changed payload with a compromised token, or an invalid bag
// with an intact token, is never reported as one aggregate.
func TestDepositReportSeparatesContentAndToken(t *testing.T) {
	gen := time.Date(2026, 11, 5, 8, 14, 22, 0, time.UTC)
	tok := &verify.Token{Attempt: 1, GenTime: gen, Qualification: verify.Qualified}
	for _, tc := range []struct {
		name           string
		d              verify.Deposit
		content, token string
	}{
		{"changed + compromised", verify.Deposit{Status: verify.StatusChanged,
			Problems: []verify.Problem{{Step: 3, Status: verify.StatusChanged}}, Counts: verify.Counts{Changed: 1},
			Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeCompromised}}, Token: &verify.Token{Attempt: 1, GenTime: gen, Compromised: true}},
			ContentChanged, TokenCompromised},
		{"invalid bag + intact token", verify.Deposit{Status: verify.StatusInvalid,
			Problems: []verify.Problem{{Step: 1, Status: verify.StatusInvalid}},
			Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeValid}}, Token: tok},
			ContentInvalid, TokenValid},
		{"intact bag + invalid token", verify.Deposit{Status: verify.StatusInvalid,
			Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeInvalid}}},
			ContentUnchanged, TokenInvalid},
		{"valid wins over an earlier invalid attempt", verify.Deposit{Status: verify.StatusTimestamped,
			Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeInvalid}, {N: 2, Outcome: verify.OutcomeValid}}, Token: tok},
			ContentUnchanged, TokenValid},
		{"no trusted path", verify.Deposit{Status: verify.StatusIndeterminate,
			Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeNoTrust}}}, ContentUnchanged, TokenEvidence},
		{"pending", verify.Deposit{Status: verify.StatusPending, Attempts: []verify.Attempt{{N: 1, Outcome: verify.OutcomeNoResponse}}},
			ContentUnchanged, TokenMissing},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := tc.d
			d.Sequence, d.Root = 1, root("a")
			rep := &verify.Report{Deposits: []verify.Deposit{d}}
			r := depositReportOf(rep, &rep.Deposits[0], false, `D:\A`)
			if r.Content != tc.content || r.TokenState != tc.token {
				t.Fatalf("content %q token %q, want %q %q", r.Content, r.TokenState, tc.content, tc.token)
			}
			if tc.token != TokenValid && r.Stamp != nil && r.Stamp.SignatureValid {
				t.Fatalf("token %s reported with a valid signature", tc.token)
			}
		})
	}
}

// Code review finding 7: presence of the predecessor's number is not a
// verified connection.
func TestDepositReportPredecessorLink(t *testing.T) {
	prev := root("1")
	other := root("9")
	mk := func(previous *string, findings ...verify.Finding) DepositReport {
		rep := &verify.Report{Deposits: []verify.Deposit{
			{Sequence: 1, Root: prev, Status: verify.StatusTimestamped},
			{Sequence: 2, Root: root("2"), Previous: previous, Status: verify.StatusTimestamped},
		}, Findings: findings}
		return depositReportOf(rep, &rep.Deposits[1], false, `D:\A`)
	}
	if r := mk(&prev); r.Predecessor != 1 || !r.PredecessorPresent || !r.PredecessorLinked || len(r.ChainFindings) != 0 {
		t.Fatalf("linked %+v", r)
	}
	r := mk(&other, verify.Finding{Code: verify.FindingPreviousMismatch, Sequence: 2})
	if !r.PredecessorPresent || r.PredecessorLinked || len(r.ChainFindings) != 1 || !strings.Contains(r.ChainFindings[0], "verweist nicht") {
		t.Fatalf("mismatch %+v", r)
	}
	r = mk(&prev, verify.Finding{Code: verify.FindingArchiveIDMismatch, Sequence: 2})
	if r.PredecessorLinked || len(r.ChainFindings) != 1 || !strings.Contains(r.ChainFindings[0], "anderen Archiv") {
		t.Fatalf("archive id %+v", r)
	}
	// The predecessor itself is reported twice: no verified link either.
	r = mk(&prev, verify.Finding{Code: verify.FindingDuplicateNumber, Sequence: 1})
	if r.PredecessorLinked || len(r.ChainFindings) != 1 {
		t.Fatalf("duplicate predecessor %+v", r)
	}
	// Missing predecessor: a gap.
	rep := &verify.Report{Deposits: []verify.Deposit{{Sequence: 3, Root: root("3"), Previous: &prev}},
		Findings: []verify.Finding{{Code: verify.FindingGap, Sequence: 1, To: 2}}}
	r = depositReportOf(rep, &rep.Deposits[0], false, `D:\A`)
	if r.Predecessor != 2 || r.PredecessorPresent || r.PredecessorLinked || len(r.ChainFindings) != 1 {
		t.Fatalf("gap %+v", r)
	}
}

// Code review finding 9: an inherited proof without accuracy carries the
// successor's genTime, and no latest time.
func TestInheritedProofWithoutAccuracy(t *testing.T) {
	gen := time.Date(2026, 11, 5, 8, 14, 22, 0, time.UTC)
	rep := &verify.Report{Deposits: []verify.Deposit{{Sequence: 1, Root: root("1"), Status: verify.StatusPending,
		Proven: &verify.Proven{GenTime: gen, Via: 2, Qualification: verify.Qualified}}}}
	r := depositReportOf(rep, &rep.Deposits[0], false, `D:\A`)
	if r.Inherited == nil || r.Inherited.ProvenLatest != "" || r.Inherited.GenTime == "" || r.Deposit.ProvenLatest != "" {
		t.Fatalf("%+v %+v", r.Inherited, r.Deposit)
	}
}

// Code review finding 8: Ablagen that stamp refused (changed, invalid,
// unreadable) are reported, not dropped.
func TestStampResultReportsFailures(t *testing.T) {
	res := &stamp.Result{Outcomes: []stamp.Outcome{
		{Sequence: 1, Name: "A", Stamped: true},
		{Sequence: 2, Name: "B", Message: "Ablage verändert"},
		{Sequence: 3, Name: "C", Reason: verify.ReasonOffline},
	}, Warnings: []string{"Uhr des PCs geht falsch"}}
	r := stampResultOf(res)
	if r.Stamped != 1 || r.Pending != 1 || r.Failed != 1 || r.Reason != ReasonOffline {
		t.Fatalf("%+v", r)
	}
	if !slices.ContainsFunc(r.Warnings, func(w string) bool { return strings.Contains(w, "0002") && strings.Contains(w, "Ablage verändert") }) ||
		!slices.Contains(r.Warnings, "Uhr des PCs geht falsch") {
		t.Fatalf("warnings %q", r.Warnings)
	}
}
