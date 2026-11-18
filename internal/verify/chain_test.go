package verify

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"
)

func extremeReport(seqs ...int) *Report {
	rep := &Report{Mode: ModeArchive, ArchiveID: "a"}
	for i, s := range seqs {
		root := "sha256:" + strings.Repeat(string(rune('a'+i)), 64)
		rep.Deposits = append(rep.Deposits, Deposit{Sequence: s, Root: root, ArchiveID: "a", Status: StatusTimestamped})
	}
	return rep
}

// Finding 4: gaps are computed from the numbers present, not by walking
// every integer between them.
func TestChainExtremeSequences(t *testing.T) {
	for _, tc := range []struct {
		seqs    []int
		gapFrom int
		gapTo   int
	}{
		{[]int{1, 1_000_000_000_000}, 2, 999_999_999_999},
		{[]int{1, math.MaxInt}, 2, math.MaxInt - 1},
		{[]int{math.MaxInt - 1, math.MaxInt}, 1, math.MaxInt - 2},
	} {
		rep := extremeReport(tc.seqs...)
		done := make(chan error, 1)
		go func() { done <- chain(context.Background(), rep) }()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: chain check does not finish", tc.seqs)
		}
		var gaps []Finding
		for _, f := range rep.Findings {
			if f.Code == FindingGap {
				gaps = append(gaps, f)
			}
		}
		if len(gaps) != 1 || gaps[0].Sequence != tc.gapFrom || gaps[0].To != tc.gapTo {
			t.Fatalf("%v: %+v", tc.seqs, rep.Findings)
		}
	}
}

func TestChainHonorsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := chain(ctx, extremeReport(1, 2, 3)); err == nil {
		t.Fatal("cancelled context ignored")
	}
}
