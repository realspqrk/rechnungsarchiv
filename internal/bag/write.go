package bag

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

// Meta is everything the writer needs besides the payload.
type Meta struct {
	Company        string  // Source-Organization
	BaggingDate    string  // local date YYYY-MM-DD
	ArchiveID      string  // uuid v4
	Sequence       int     // 1-based
	Previous       *string // Prüfwert of the predecessor; nil for sequence 1
	Belegjahr      int
	RetentionYears int
	ArchivedBy     string // user@host
	ToolVersion    string
	SealNonce      string // 32 lowercase hex
}

// Written describes a bag that Write produced.
type Written struct {
	Dir         string // top-level directory, "<NNNN>_<Name>"
	Root        string // Prüfwert
	Files       []FileEntry
	TagManifest []byte
}

// DirName is the bag directory and ZIP base name: "<NNNN>_<Name>".
func DirName(seq int, name string) string { return fmt.Sprintf("%04d_%s", seq, name) }

// Write writes the bag for in as a profile ZIP into w (spec §2–§3). Each
// payload file is read exactly once; its hash is computed from the bytes
// as they are written. Size and mtime are compared before and after.
func Write(w io.WriteSeeker, name string, in *Input, m Meta) (*Written, error) {
	if why := ValidateComponent(name); why != "" {
		return nil, fmt.Errorf("%w: %q (%s)", ErrInvalidName, name, why)
	}
	if len(in.Files) > maxEntries-len(tagManifestOrder)-1 {
		return nil, fmt.Errorf("zu viele Dateien (%d)", len(in.Files))
	}
	if !reNonce.MatchString(m.SealNonce) {
		return nil, fmt.Errorf("bag: invalid seal nonce")
	}
	dir := DirName(m.Sequence, name)
	top := dir + "/"
	zw := newZipWriter(w)
	if err := zw.add(top+fileBagIt, int64(len(bagItContent)), bytes.NewReader([]byte(bagItContent)), io.Discard); err != nil {
		return nil, err
	}

	files := make([]FileEntry, 0, len(in.Files))
	manifest := make([]ManifestEntry, 0, len(in.Files))
	var octets int64
	for _, f := range in.Files {
		entry := top + "data/" + f.Path
		if len(entry) > maxEntryName {
			return nil, fmt.Errorf("%w: %q (Pfad zu lang)", ErrInvalidName, f.Path)
		}
		sum, err := writePayload(zw, entry, f)
		if err != nil {
			return nil, err
		}
		files = append(files, FileEntry{
			MTime: f.ModTime.UTC().Format(time.RFC3339), Path: "data/" + f.Path, SHA256: sum, Size: f.Size,
		})
		manifest = append(manifest, ManifestEntry{SHA256: sum, Path: "data/" + f.Path})
		octets += f.Size
	}

	info := BagInfo{
		SourceOrganization: m.Company,
		BaggingDate:        m.BaggingDate,
		PayloadOxum:        fmt.Sprintf("%d.%d", octets, len(files)),
		ExternalIdentifier: fmt.Sprintf("%s/%d", m.ArchiveID, m.Sequence),
		Belegjahr:          m.Belegjahr,
		RetentionYears:     m.RetentionYears,
		RetentionUntil:     RetentionUntil(m.Belegjahr, m.RetentionYears),
		ArchivedBy:         m.ArchivedBy,
		ToolVersion:        m.ToolVersion,
		SealNonce:          m.SealNonce,
	}
	chain := Chain{ArchiveID: m.ArchiveID, Format: FormatVersion, Previous: m.Previous, Sequence: m.Sequence}
	tags := map[string][]byte{fileBagIt: []byte(bagItContent), fileManifest: renderManifest(manifest),
		fileSkipped: renderSkipped(in.Skipped), fileVerify: []byte(verifyText)}
	var err error
	if tags[fileBagInfo], err = info.render(); err != nil {
		return nil, err
	}
	if tags[fileChain], err = chain.render(); err != nil {
		return nil, err
	}
	if tags[fileFiles], err = renderFiles(files); err != nil {
		return nil, err
	}
	// Re-parse what we render so the writer can never emit a bag its own
	// reader would refuse.
	if _, err := parseBagInfo(tags[fileBagInfo]); err != nil {
		return nil, err
	}
	if _, err := parseChain(tags[fileChain]); err != nil {
		return nil, err
	}
	tagManifest := renderTagManifest(tags)
	for _, n := range tagManifestOrder[1:] {
		if err := zw.add(top+n, int64(len(tags[n])), bytes.NewReader(tags[n]), io.Discard); err != nil {
			return nil, err
		}
	}
	if err := zw.add(top+fileTagManifest, int64(len(tagManifest)), bytes.NewReader(tagManifest), io.Discard); err != nil {
		return nil, err
	}
	if err := zw.close(); err != nil {
		return nil, err
	}
	return &Written{Dir: dir, Root: RootOf(tagManifest), Files: files, TagManifest: tagManifest}, nil
}

func writePayload(zw *zipWriter, entry string, f InputFile) (string, error) {
	fh, err := os.Open(f.OSPath)
	if err != nil {
		return "", err
	}
	defer fh.Close()
	same := func() error {
		fi, err := fh.Stat()
		if err != nil {
			return err
		}
		if fi.Size() != f.Size || !fi.ModTime().Equal(f.ModTime) {
			return fmt.Errorf("%w: %s", ErrChanged, f.Path)
		}
		return nil
	}
	if err := same(); err != nil {
		return "", err
	}
	h := sha256.New()
	if err := zw.add(entry, f.Size, fh, h); err != nil {
		if err == ErrChanged {
			err = fmt.Errorf("%w: %s", ErrChanged, f.Path)
		}
		return "", err
	}
	if err := same(); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
