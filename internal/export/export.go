// Package export writes the export for a tax audit (spec §8). It reads the
// source archive and never writes into it.
package export

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/present"
	"github.com/realspqrk/rechnungsarchiv/internal/report"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/verify"
)

// Names inside an export folder.
const (
	ExeName      = "rechnungsarchiv.exe"
	ProtocolsDir = "Archivprotokolle"
	FileListName = "DATEIVERZEICHNIS.csv"
	GuideName    = "PRUEFANLEITUNG.txt"
)

// Options configures Run.
type Options struct {
	From, To int // Belegjahr range, inclusive
	// SourceTrust opens the trusted lists for the source archive and is
	// refreshed if possible. Default trust.OpenReadOnly: refreshed lists go
	// to a cache outside the archive.
	SourceTrust func(archiveDir string) (*trust.Store, error)
	// ExportTrust opens the trusted lists inside the export for its own
	// verification. Default trust.Open (it is never refreshed).
	ExportTrust func(exportDir string) (*trust.Store, error)
	// Executable returns the verifier to bundle. Default os.Executable.
	Executable func() (string, error)
	Now        func() time.Time
	Version    string // for the Archivprotokolle; default archive.Version
}

// Result describes a written export.
type Result struct {
	Dir      string         `json:"dir"`
	Exported []int          `json:"exported"`
	Bridged  []int          `json:"bridged,omitempty"`
	Report   *verify.Report `json:"report"`
	Warnings []string       `json:"warnings,omitempty"` // German
}

// Run writes the export of the Ablagen with Belegjahr From..To of the
// archive src into the new folder dst.
func Run(ctx context.Context, src, dst string, opts Options) (*Result, error) {
	if opts.From > opts.To || opts.From == 0 {
		return nil, fmt.Errorf("Belegjahr-Bereich %d–%d ist ungültig", opts.From, opts.To)
	}
	setDefaults(&opts)
	a, err := archive.OpenReadOnly(src)
	if err != nil {
		return nil, err
	}
	g, err := checkTarget(src, dst)
	if err != nil {
		return nil, err
	}
	deps, err := a.Deposits()
	if err != nil {
		return nil, err
	}
	res := &Result{Dir: dst}
	var selected []archive.Deposit
	for _, d := range deps {
		if d.Err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Ablage %s ist nicht lesbar und wurde nicht exportiert: %v", filepath.Base(d.Path), d.Err))
			continue
		}
		if y := d.Bag.Info.Belegjahr; y >= opts.From && y <= opts.To {
			selected = append(selected, d)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("keine Ablage mit Belegjahr %d bis %d", opts.From, opts.To)
	}
	// Nachweise pair with Ablagen by imprint, whatever their names (spec §1).
	nachweise, err := verify.Nachweise(src)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return nil, err
	}
	if err := g.check(); err != nil { // again, now that dst exists
		return nil, err
	}

	// The Ablagen and their Nachweise, unchanged.
	copied := map[string]bool{}
	for _, d := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := copyFile(d.Path, filepath.Join(dst, filepath.Base(d.Path)), 0o444); err != nil {
			return nil, err
		}
		for _, n := range nachweise[d.Bag.Root] {
			if copied[n] {
				continue
			}
			copied[n] = true
			if err := copyTree(filepath.Join(src, n), filepath.Join(dst, n)); err != nil {
				return nil, err
			}
		}
		res.Exported = append(res.Exported, d.Sequence)
	}

	// Brückendaten for excluded Ablagen between the first and last one. Only
	// the numbers present are visited, however far apart they are.
	first, last := selected[0].Sequence, selected[len(selected)-1].Sequence
	inSel := map[string]bool{}
	for _, d := range selected {
		inSel[d.Path] = true
	}
	bySeq := map[int][]archive.Deposit{}
	for _, d := range deps {
		if !inSel[d.Path] && d.Sequence > first && d.Sequence < last {
			bySeq[d.Sequence] = append(bySeq[d.Sequence], d)
		}
	}
	bridged := false
	for _, seq := range slices.Sorted(maps.Keys(bySeq)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ds := bySeq[seq]
		if len(ds) > 1 || ds[0].Err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("Für Ablage %s können keine Brückendaten geschrieben werden (nicht lesbar oder Nummer doppelt vergeben).", present.Number(seq)))
			continue
		}
		if !bridged {
			if err := mkNew(filepath.Join(dst, verify.BridgeDir)); err != nil {
				return nil, err
			}
			bridged = true
		}
		dir := filepath.Join(dst, verify.BridgeDir, present.Number(seq))
		if err := mkNew(dir); err != nil {
			return nil, err
		}
		if err := writeNew(filepath.Join(dir, verify.ChainFile), ds[0].Bag.ChainJSON); err != nil {
			return nil, err
		}
		if err := writeNew(filepath.Join(dir, verify.TagManifestFile), ds[0].Bag.TagManifest); err != nil {
			return nil, err
		}
		res.Bridged = append(res.Bridged, seq)
	}

	// Trusted lists: refreshed if possible, then all cached ones.
	provider := ""
	if a.Config.TSA != nil {
		provider = *a.Config.TSA
	}
	if err := copyLists(ctx, src, dst, provider, opts, res); err != nil {
		return nil, err
	}

	// The verifier itself.
	if exe, err := opts.Executable(); err != nil {
		res.Warnings = append(res.Warnings, "Prüfprogramm konnte nicht beigelegt werden: "+err.Error())
	} else if err := copyFile(exe, filepath.Join(dst, ExeName), 0o755); err != nil {
		res.Warnings = append(res.Warnings, "Prüfprogramm konnte nicht beigelegt werden: "+err.Error())
	}

	// The report is computed only from what the export contains.
	es, err := opts.ExportTrust(dst)
	if err != nil {
		return nil, err
	}
	rep, err := verify.Export(ctx, dst, verify.Options{Trust: es, Now: opts.Now})
	if err != nil {
		return nil, err
	}
	res.Report = rep

	if err := g.check(); err != nil { // right before writing the generated files
		return nil, err
	}
	if err := mkNew(filepath.Join(dst, ProtocolsDir)); err != nil {
		return nil, err
	}
	for i := range rep.Deposits {
		d := &rep.Deposits[i]
		pdf, err := report.PDF(rep, d, report.Options{Version: opts.Version})
		if err != nil {
			return nil, err
		}
		if err := writeNew(filepath.Join(dst, ProtocolsDir, report.FileName(d)), pdf); err != nil {
			return nil, err
		}
	}
	csvData, err := fileList(rep)
	if err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(dst, FileListName), csvData); err != nil {
		return nil, err
	}
	if err := writeNew(filepath.Join(dst, GuideName), guide(rep)); err != nil {
		return nil, err
	}
	return res, nil
}

func setDefaults(o *Options) {
	if o.SourceTrust == nil {
		o.SourceTrust = trust.OpenReadOnly
	}
	if o.ExportTrust == nil {
		o.ExportTrust = trust.Open
	}
	if o.Executable == nil {
		o.Executable = os.Executable
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

var reList = regexp.MustCompile(`^[A-Z]{2}-[0-9]{1,9}(-[0-9]{1,3})?\.xml$`)

// copyLists refreshes the source's trusted lists (never into the archive)
// and writes every cached and every loaded list into the export.
func copyLists(ctx context.Context, src, dst, provider string, opts Options, res *Result) error {
	out := filepath.Join(dst, trust.DirName)
	if err := mkNew(out); err != nil {
		return err
	}
	s, err := opts.SourceTrust(src)
	if err != nil {
		return err
	}
	if err := s.Refresh(ctx, verify.Territories(s, provider)...); err != nil {
		res.Warnings = append(res.Warnings, "Vertrauenslisten konnten nicht aktualisiert werden; der Export enthält die gespeicherten: "+err.Error())
	}
	des, err := os.ReadDir(filepath.Join(src, trust.DirName))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, de := range des {
		if de.Type().IsRegular() && reList.MatchString(de.Name()) {
			if err := copyFile(filepath.Join(src, trust.DirName, de.Name()), filepath.Join(out, de.Name()), 0o644); err != nil {
				return err
			}
		}
	}
	for _, l := range s.Lists() {
		if err := addList(out, fmt.Sprintf("%s-%d", l.Territory, l.Sequence), l.Raw); err != nil {
			return err
		}
	}
	return nil
}

// addList stores a list unless identical bytes are already there; a
// different list with the same sequence gets a -2, -3 … suffix.
func addList(dir, base string, raw []byte) error {
	for n := 1; n < 1000; n++ {
		name := base + ".xml"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.xml", base, n)
		}
		old, err := os.ReadFile(filepath.Join(dir, name))
		switch {
		case err == nil && bytes.Equal(old, raw):
			return nil
		case err == nil:
			continue
		case errors.Is(err, fs.ErrNotExist):
			return writeNew(filepath.Join(dir, name), raw)
		default:
			return err
		}
	}
	return fmt.Errorf("%s: too many versions", base)
}

// fileList is DATEIVERZEICHNIS.csv: UTF-8 with BOM, ";" separated, CRLF, so
// Excel opens it directly.
func fileList(rep *verify.Report) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(utf8BOM)
	w := csv.NewWriter(&b)
	w.Comma, w.UseCRLF = ';', true
	w.Write([]string{"Ablage", "Pfad", "Größe (Byte)", "SHA-256", "Belegjahr", "belegt spätestens"})
	for _, d := range rep.Deposits {
		proven := present.Proven(d.Proven)
		if d.Proven == nil || d.Status == verify.StatusChanged || d.Status == verify.StatusInvalid {
			proven = "nicht belegt (" + present.Label(d) + ")"
		}
		for _, f := range d.Files {
			w.Write([]string{present.Number(d.Sequence) + "_" + d.Name, f.Path, fmt.Sprint(f.Size), f.SHA256, fmt.Sprint(d.Belegjahr), proven})
		}
	}
	w.Flush()
	return b.Bytes(), w.Error()
}

// copyFile copies src to a new file dst (never replacing one).
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	return os.Chmod(dst, mode)
}

// copyTree copies a Nachweis folder; leftover temp files are skipped. A
// missing folder (no attempt yet) is fine.
func copyTree(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return filepath.WalkDir(src, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case de.IsDir():
			return mkNew(target)
		case strings.HasPrefix(de.Name(), ".tmp-") || strings.HasSuffix(de.Name(), ".tmp"):
			return nil
		case !de.Type().IsRegular():
			return fmt.Errorf("%s: keine normale Datei", p)
		}
		return copyFile(p, target, 0o444)
	})
}

func writeNew(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// utf8BOM makes Excel read the CSV as UTF-8.
const utf8BOM = "\xef\xbb\xbf"
