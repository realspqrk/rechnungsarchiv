package verify

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/realspqrk/rechnungsarchiv/internal/bag"
)

// BridgeDir holds the Brückendaten of an export: BridgeDir/<NNNN>/ with
// chain.json and tagmanifest-sha256.txt, nothing else (spec §8).
const BridgeDir = "brueckendaten"

// Names of the two Brückendaten files.
const (
	ChainFile       = "chain.json"
	TagManifestFile = "tagmanifest-sha256.txt"
)

var reBridge = regexp.MustCompile(`^[0-9]{4,}$`)

// readBridges reads and checks all Brückendaten (spec §6 step 9).
func readBridges(dir string) ([]Bridge, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Bridge
	for _, de := range des {
		if !de.IsDir() || !reBridge.MatchString(de.Name()) {
			continue
		}
		seq, _ := strconv.Atoi(de.Name())
		b := Bridge{Sequence: seq}
		tm, err1 := os.ReadFile(filepath.Join(dir, de.Name(), TagManifestFile))
		cj, err2 := os.ReadFile(filepath.Join(dir, de.Name(), ChainFile))
		if err := errors.Join(err1, err2); err != nil {
			b.Detail = err.Error()
		} else {
			b.chain, err = checkBridge(seq, tm, cj)
			if err != nil {
				b.Detail = err.Error()
			} else {
				b.Valid, b.Root = true, bag.RootOf(tm)
			}
		}
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b Bridge) int { return cmp.Compare(a.Sequence, b.Sequence) })
	return out, nil
}

// checkBridge verifies that chain.json is the one the tagmanifest names and
// is canonical. The Prüfwert is sha256(tagmanifest) by definition.
func checkBridge(seq int, tm, cj []byte) (*bag.Chain, error) {
	sum := sha256.Sum256(cj)
	want := hex.EncodeToString(sum[:]) + "  " + ChainFile
	found := false
	for _, l := range strings.Split(string(tm), "\n") {
		if l == want {
			found = true
		}
	}
	if !found {
		return nil, errors.New("chain.json passt nicht zu tagmanifest-sha256.txt")
	}
	c, err := bag.ParseChain(cj)
	if err != nil {
		return nil, err
	}
	if c.Sequence != seq {
		return nil, fmt.Errorf("chain.json nennt Nummer %d", c.Sequence)
	}
	return c, nil
}

// node is one sequence entry of the verified set: an Ablage or a bridge.
type node struct {
	seq       int
	root      string
	archiveID string
	previous  *string
	intact    bool     // chain.json and tagmanifest are trustworthy
	dep       *Deposit // nil for a bridge
}

// chain applies spec §6 step 9 and the inheritance of step 8. It works on
// the sequence numbers present only: gaps are computed arithmetically, so
// a number like 10^12 costs nothing (sequences are ≥ 1, so differences
// cannot overflow).
func chain(ctx context.Context, rep *Report) error {
	if rep.Mode == ModeDeposit {
		return nil
	}
	var nodes []node
	for i := range rep.Deposits {
		d := &rep.Deposits[i]
		if d.fileSeq != 0 && d.ArchiveID != "" && d.fileSeq != d.Sequence {
			rep.Findings = append(rep.Findings, Finding{Code: FindingFileNameMismatch, Sequence: d.Sequence,
				Detail: fmt.Sprintf("%s: chain.json nennt Nummer %d", d.File, d.Sequence)})
		}
		if d.ArchiveID == "" || d.Sequence < 1 {
			continue // chain.json unreadable: reported as Ungültig/Verändert
		}
		nodes = append(nodes, node{seq: d.Sequence, root: d.Root, archiveID: d.ArchiveID, previous: d.Previous,
			intact: tagsIntact(d), dep: d})
	}
	for _, b := range rep.Bridged {
		if !b.Valid || b.chain == nil || b.Sequence < 1 {
			rep.Findings = append(rep.Findings, Finding{Code: FindingBridgeInvalid, Sequence: b.Sequence, Detail: b.Detail})
			continue
		}
		nodes = append(nodes, node{seq: b.Sequence, root: b.Root, archiveID: b.chain.ArchiveID, previous: b.chain.Previous, intact: true})
	}
	if len(nodes) == 0 {
		return ctx.Err()
	}

	// archive_id is the same everywhere.
	want := rep.ArchiveID
	if want == "" {
		want = majority(nodes)
		rep.ArchiveID = want
	}
	for _, n := range nodes {
		if n.archiveID != want {
			rep.Findings = append(rep.Findings, Finding{Code: FindingArchiveIDMismatch, Sequence: n.seq, Detail: n.archiveID})
		}
	}

	bySeq := map[int][]node{}
	lo, hi := 0, 0
	for _, n := range nodes {
		bySeq[n.seq] = append(bySeq[n.seq], n)
		if n.dep != nil || rep.Mode == ModeArchive {
			if lo == 0 || n.seq < lo {
				lo = n.seq
			}
			hi = max(hi, n.seq)
		}
	}
	if rep.Mode == ModeArchive {
		lo = 1 // exactly one genesis
	} else if lo > 1 {
		rep.Findings = append(rep.Findings, Finding{Code: FindingPredecessorNotInExport, Sequence: lo})
	}
	var seqs []int // the numbers present in lo..hi, ascending
	for _, s := range slices.Sorted(maps.Keys(bySeq)) {
		if len(bySeq[s]) > 1 {
			rep.Findings = append(rep.Findings, Finding{Code: FindingDuplicateNumber, Sequence: s})
		}
		if s >= lo && s <= hi {
			seqs = append(seqs, s)
		}
	}

	// Sequence numbers are contiguous; previous equals the predecessor's
	// Prüfwert. Only adjacent numbers present are compared.
	linked := map[int]bool{} // seq s commits to seq s-1
	prev := lo - 1           // lo ≥ 1 in archive mode; 0 or more otherwise
	for _, s := range seqs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s-prev > 1 {
			rep.Findings = append(rep.Findings, Finding{Code: FindingGap, Sequence: prev + 1, To: s - 1})
		} else if s-prev == 1 && s > lo {
			cur, before := bySeq[s], bySeq[prev]
			if len(cur) == 1 && len(before) == 1 && cur[0].intact && before[0].root != "" {
				if cur[0].previous == nil || *cur[0].previous != before[0].root {
					rep.Findings = append(rep.Findings, Finding{Code: FindingPreviousMismatch, Sequence: s})
				} else {
					linked[s] = before[0].intact
					if cur[0].dep != nil { // nil for Brückendaten nodes
						cur[0].dep.LinkedPrevious = before[0].intact
					}
				}
			}
		}
		prev = s
	}

	inherit(rep, bySeq, linked, hi)
	slices.SortStableFunc(rep.Findings, func(a, b Finding) int { return cmp.Compare(a.Sequence, b.Sequence) })
	return ctx.Err()
}

// inherit gives an Ablage without a valid own token the earliest
// belegt_spätestens of a valid successor it is linked to (spec §6 step 8).
// The walk follows linked numbers only, so it is bounded by their count.
func inherit(rep *Report, bySeq map[int][]node, linked map[int]bool, hi int) {
	for i := range rep.Deposits {
		d := &rep.Deposits[i]
		if d.Proven != nil || len(d.Problems) > 0 || d.Root == "" || len(bySeq[d.Sequence]) != 1 ||
			(d.Status != StatusPending && d.Status != StatusIndeterminate) {
			continue
		}
		var best *Proven
		for s := d.Sequence; s < hi && linked[s+1]; s++ {
			n := bySeq[s+1][0]
			if n.dep == nil || n.dep.Status != StatusTimestamped || n.dep.Proven == nil || n.dep.Proven.Via != 0 {
				continue
			}
			p := n.dep.Proven
			if best == nil || earlier(p, best) {
				c := *p
				c.Via = s + 1
				best = &c
			}
		}
		d.Proven = best
	}
}

func earlier(a, b *Proven) bool {
	switch {
	case a.At != nil && b.At != nil:
		return a.At.Before(*b.At)
	case a.At != nil:
		return true // a known latest time beats an unknown one
	case b.At != nil:
		return false
	}
	return a.GenTime.Before(b.GenTime)
}

// tagsIntact reports whether the Ablage's tag files match its tagmanifest,
// so chain.json can be trusted.
func tagsIntact(d *Deposit) bool {
	for _, p := range d.Problems {
		if p.Step <= 2 || (p.Step == 4 && p.Status == StatusInvalid) {
			return false
		}
	}
	return d.Root != ""
}

func majority(nodes []node) string {
	count := map[string]int{}
	best := ""
	for _, n := range nodes {
		count[n.archiveID]++
		if count[n.archiveID] > count[best] || (count[n.archiveID] == count[best] && n.archiveID < best) {
			best = n.archiveID
		}
	}
	return best
}
