package bag

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Status is the bag-level outcome of spec §6 steps 1–4. Higher values win
// (precedence Ungültig > Verändert > ok).
type Status int

const (
	OK      Status = iota // steps 1–4 passed
	Changed               // Verändert
	Invalid               // Ungültig
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Changed:
		return "Verändert"
	default:
		return "Ungültig"
	}
}

// Problem is one finding, with the spec §6 step that produced it.
type Problem struct {
	Step   int
	Status Status
	Msg    string // German, for the UI and the report
}

// Bag is the parsed metadata of a bag. Fields are nil/empty when the
// corresponding tag file is missing or unparsable.
type Bag struct {
	Dir         string // bag directory name, "<NNNN>_<Name>"
	Root        string // Prüfwert, "sha256:<hex>"; "" without tagmanifest
	TagManifest []byte // raw tagmanifest-sha256.txt (Brückendaten)
	ChainJSON   []byte // raw chain.json (Brückendaten)
	Info        *BagInfo
	Chain       *Chain
	Files       []FileEntry
	Manifest    []ManifestEntry
	Skipped     []string
}

// Result is the outcome of Check. Paths include the "data/" prefix.
type Result struct {
	Status   Status
	Problems []Problem
	Matched  []string
	Changed  []string
	Missing  []string
	Added    []string
	Ignored  []string // system junk next to an extracted bag, not counted as added
	Bag      *Bag
}

func (r *Result) add(step int, st Status, format string, a ...any) {
	r.Problems = append(r.Problems, Problem{Step: step, Status: st, Msg: fmt.Sprintf(format, a...)})
	if st > r.Status {
		r.Status = st
	}
}

const maxTagBytes = 64 << 20

var tagFiles = append(append([]string(nil), tagManifestOrder...), fileTagManifest)

// Check runs spec §6 steps 1–4 on a ZIP file or an extracted bag
// directory. The returned error is only for I/O failures; every finding
// about the bag itself is in the Result.
func Check(path string) (*Result, error) {
	res := &Result{}
	src, err := openSource(path)
	if err != nil {
		return checkFailed(res, err)
	}
	defer src.Close()
	if err := readTags(src, res); err != nil {
		return checkFailed(res, err)
	}
	if err := checkPayload(src, res); err != nil {
		return checkFailed(res, err)
	}
	checkConsistency(src, res)
	return res, nil
}

// Inspect reads a bag's metadata without hashing the payload. It fails if
// the bag is Ungültig or a tag file needed for the metadata is missing.
func Inspect(path string) (*Bag, error) {
	res := &Result{}
	src, err := openSource(path)
	if err != nil {
		return nil, err
	}
	defer src.Close()
	if err := readTags(src, res); err != nil {
		return nil, err
	}
	for _, p := range res.Problems {
		if p.Status == Invalid {
			return nil, errors.New(p.Msg)
		}
	}
	b := res.Bag
	if b.Root == "" || b.Chain == nil || b.Info == nil || b.Manifest == nil {
		return nil, fmt.Errorf("%s: Begleitdateien fehlen", path)
	}
	return b, nil
}

func checkFailed(res *Result, err error) (*Result, error) {
	var fe *formatError
	if errors.As(err, &fe) {
		res.add(1, Invalid, "%s", fe.msg)
		return res, nil
	}
	return nil, err
}

// readTags performs step 2 and parses the tag files.
func readTags(src *source, res *Result) error {
	b := &Bag{Dir: src.dir}
	res.Bag = b
	for _, rel := range src.sorted() {
		if strings.HasPrefix(rel, "data/") {
			continue
		}
		known := false
		for _, t := range tagFiles {
			known = known || rel == t
		}
		if !known && !(src.extracted && isJunk(rel)) {
			res.add(2, Changed, "unerwartete Datei im Ablage-Ordner: %s", rel)
		} else if !known {
			res.Ignored = append(res.Ignored, rel)
		}
	}
	tags := map[string][]byte{}
	for _, t := range tagFiles {
		if src.files[t] == nil {
			res.add(2, Changed, "Begleitdatei fehlt: %s", t)
			continue
		}
		data, crcOK, err := src.read(t, maxTagBytes)
		if err != nil {
			return err
		}
		if !crcOK {
			res.add(2, Changed, "Begleitdatei beschädigt (CRC): %s", t)
		}
		tags[t] = data
	}
	if tm, ok := tags[fileTagManifest]; ok {
		b.TagManifest, b.Root = tm, RootOf(tm)
		checkTagManifest(res, tm, tags)
	}
	parse := func(name string, fn func([]byte) error) {
		if data, ok := tags[name]; ok {
			if err := fn(data); err != nil {
				res.add(4, Invalid, "%s: %v", name, err)
			}
		}
	}
	parse(fileBagIt, func(d []byte) error {
		if string(d) != bagItContent {
			return errors.New("unerwarteter Inhalt")
		}
		return nil
	})
	parse(fileBagInfo, func(d []byte) (err error) { b.Info, err = parseBagInfo(d); return })
	parse(fileChain, func(d []byte) (err error) {
		b.ChainJSON = d
		b.Chain, err = parseChain(d)
		return
	})
	parse(fileFiles, func(d []byte) (err error) { b.Files, err = parseFiles(d); return })
	parse(fileManifest, func(d []byte) (err error) {
		b.Manifest, err = parseManifest(d)
		return
	})
	parse(fileSkipped, func(d []byte) (err error) { b.Skipped, err = parseSkipped(d); return })
	parse(fileVerify, func(d []byte) error {
		if string(d) != verifyText {
			return errors.New("weicht vom festen Text ab")
		}
		return nil
	})
	return nil
}

func checkTagManifest(res *Result, tm []byte, tags map[string][]byte) {
	lines, err := splitLines(tm)
	if err != nil || len(lines) != len(tagManifestOrder) {
		res.add(2, Changed, "tagmanifest-sha256.txt hat nicht das erwartete Format")
		return
	}
	for i, l := range lines {
		want := tagManifestOrder[i]
		h, name, ok := strings.Cut(l, "  ")
		if !ok || name != want || !reHex.MatchString(h) {
			res.add(2, Changed, "tagmanifest-sha256.txt: Zeile %d ist nicht %s", i+1, want)
			continue
		}
		data, present := tags[want]
		if !present {
			continue // already reported as missing
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != h {
			res.add(2, Changed, "Begleitdatei verändert: %s", want)
		}
	}
}

// checkPayload performs step 3: payload against manifest-sha256.txt.
func checkPayload(src *source, res *Result) error {
	man := res.Bag.Manifest
	if man == nil {
		return nil // missing or unparsable manifest is already reported
	}
	listed := make(map[string]bool, len(man))
	for _, e := range man {
		listed[e.Path] = true
		if src.files[e.Path] == nil {
			res.Missing = append(res.Missing, e.Path)
			continue
		}
		sum, crcOK, err := src.hash(e.Path)
		if err != nil {
			return err
		}
		if sum == e.SHA256 && crcOK {
			res.Matched = append(res.Matched, e.Path)
		} else {
			res.Changed = append(res.Changed, e.Path)
		}
	}
	for _, rel := range src.sorted() {
		if !strings.HasPrefix(rel, "data/") || listed[rel] {
			continue
		}
		if src.extracted && isJunk(rel) {
			res.Ignored = append(res.Ignored, rel)
		} else {
			res.Added = append(res.Added, rel)
		}
	}
	for _, p := range res.Changed {
		res.add(3, Changed, "Datei verändert: %s", p)
	}
	for _, p := range res.Missing {
		res.add(3, Changed, "Datei fehlt: %s", p)
	}
	for _, p := range res.Added {
		res.add(3, Changed, "Datei hinzugefügt: %s", p)
	}
	return nil
}

// checkConsistency performs step 4: Payload-Oxum, files.json and the
// manifest must agree; chain.json and bag-info.txt must agree.
func checkConsistency(src *source, res *Result) {
	b := res.Bag
	if b.Info != nil && b.Chain != nil {
		if b.Info.ExternalIdentifier != b.Chain.ArchiveID+"/"+strconv.Itoa(b.Chain.Sequence) {
			res.add(4, Invalid, "External-Identifier passt nicht zu chain.json")
		}
	}
	if b.Files != nil {
		var octets int64
		for _, f := range b.Files {
			octets += f.Size
		}
		if b.Info != nil && b.Info.PayloadOxum != fmt.Sprintf("%d.%d", octets, len(b.Files)) {
			res.add(4, Invalid, "Payload-Oxum passt nicht zu files.json")
		}
		if b.Manifest != nil {
			if len(b.Manifest) != len(b.Files) {
				res.add(4, Invalid, "files.json und manifest-sha256.txt nennen unterschiedlich viele Dateien")
			} else {
				for i, m := range b.Manifest {
					if f := b.Files[i]; f.Path != m.Path || f.SHA256 != m.SHA256 {
						res.add(4, Invalid, "files.json und manifest-sha256.txt widersprechen sich bei %s", m.Path)
						break
					}
				}
			}
		}
		matched := map[string]bool{}
		for _, p := range res.Matched {
			matched[p] = true
		}
		// A matched file has the manifest's bytes, so its size is known exactly.
		for _, f := range b.Files {
			if matched[f.Path] && f.Size != src.files[f.Path].size {
				res.add(4, Invalid, "files.json nennt eine falsche Größe für %s", f.Path)
			}
		}
	}
}
