package tsa

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// Evidence files of attempt n inside <Ablage>.nachweis/ (spec §1):
//
//	n.tsq          request, written before sending
//	n.tsr          response
//	n.chain.pem    chain certificates
//	n.rev/<CertKey>.ocsp|.crl
//
// Every file is created once and never replaced.

// ErrExists is returned when an evidence file already exists with
// different content.
var ErrExists = errors.New("Nachweisdatei existiert bereits")

// Attempt is one stored attempt.
type Attempt struct {
	N        int
	TSQ      []byte
	Request  *Request  // parsed TSQ; nil if unparseable
	TSR      []byte    // nil if no response stored
	Material *Material // nil if no chain stored
	// MaterialErr is set when n.chain.pem exists but cannot be parsed. The
	// attempt then lacks usable material; other attempts stay readable.
	MaterialErr error
}

// NextAttempt returns the number for a new attempt in dir.
func NextAttempt(dir string) (int, error) {
	atts, err := ReadAttempts(dir)
	if err != nil {
		return 0, err
	}
	n := 1
	for _, a := range atts {
		if a.N >= n {
			n = a.N + 1
		}
	}
	return n, nil
}

// WriteRequest stores n.tsq. Call it before sending the request.
func WriteRequest(dir string, n int, tsq []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return writeNew(filepath.Join(dir, fmt.Sprintf("%d.tsq", n)), tsq)
}

// WriteResponse stores n.tsr.
func WriteResponse(dir string, n int, tsr []byte) error {
	return writeNew(filepath.Join(dir, fmt.Sprintf("%d.tsr", n)), tsr)
}

// WriteMaterial stores n.rev/ and then n.chain.pem.
func WriteMaterial(dir string, n int, m *Material) error {
	rev := filepath.Join(dir, fmt.Sprintf("%d.rev", n))
	if err := os.MkdirAll(rev, 0o755); err != nil {
		return err
	}
	keys := make([]string, 0, len(m.Evidence))
	for k := range m.Evidence {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ev := m.Evidence[k]
		if !certKeyRE.MatchString(k) || (ev.Kind != KindOCSP && ev.Kind != KindCRL) {
			return fmt.Errorf("bad evidence name %q.%s", k, ev.Kind)
		}
		if err := writeNew(filepath.Join(rev, k+"."+string(ev.Kind)), ev.Raw); err != nil {
			return err
		}
	}
	return writeNew(filepath.Join(dir, fmt.Sprintf("%d.chain.pem", n)), EncodePEM(m.Chain))
}

var (
	certKeyRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fileRE    = regexp.MustCompile(`^([1-9][0-9]{0,8})\.(tsq|tsr|chain\.pem|rev)$`)
	revRE     = regexp.MustCompile(`^([0-9a-f]{64})\.(ocsp|crl)$`)
)

// ReadAttempts loads all attempts of a .nachweis folder, ordered by n. A
// missing folder yields no attempts. Unknown files are ignored.
func ReadAttempts(dir string) ([]Attempt, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	byN := map[int]*Attempt{}
	get := func(n int) *Attempt {
		if byN[n] == nil {
			byN[n] = &Attempt{N: n}
		}
		return byN[n]
	}
	for _, e := range entries {
		m := fileRE.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		n, _ := strconv.Atoi(m[1])
		p := filepath.Join(dir, e.Name())
		a := get(n)
		switch m[2] {
		case "tsq":
			if a.TSQ, err = os.ReadFile(p); err != nil {
				return nil, err
			}
			a.Request, _ = ParseRequest(a.TSQ)
		case "tsr":
			if a.TSR, err = os.ReadFile(p); err != nil {
				return nil, err
			}
		case "chain.pem":
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, err
			}
			certs, err := ParsePEM(b)
			if err != nil {
				a.MaterialErr = fmt.Errorf("%s: %w", e.Name(), err)
				continue
			}
			mat := material(a)
			mat.Chain = certs
		case "rev":
			if err := readRev(p, material(a)); err != nil {
				return nil, err
			}
		}
	}
	out := make([]Attempt, 0, len(byN))
	for _, a := range byN {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out, nil
}

func material(a *Attempt) *Material {
	if a.Material == nil {
		a.Material = &Material{Evidence: map[string]Evidence{}}
	}
	return a.Material
}

func readRev(dir string, m *Material) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		r := revRE.FindStringSubmatch(e.Name())
		if r == nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		m.Evidence[r[1]] = Evidence{Kind: EvidenceKind(r[2]), Raw: b}
	}
	return nil
}

// MatchImprint returns the attempts whose stored request carries the
// Prüfwert (spec §1 pairing). File names are only a hint.
func MatchImprint(attempts []Attempt, pruefwert [32]byte) []Attempt {
	var out []Attempt
	for _, a := range attempts {
		if a.Request != nil && a.Request.Imprint == pruefwert {
			out = append(out, a)
		}
	}
	return out
}

// EncodePEM encodes certificates as concatenated PEM blocks.
func EncodePEM(certs []*x509.Certificate) []byte {
	var b bytes.Buffer
	for _, c := range certs {
		pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return b.Bytes()
}

// ParsePEM parses concatenated PEM CERTIFICATE blocks.
func ParsePEM(b []byte) ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	for {
		var blk *pem.Block
		blk, b = pem.Decode(b)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(bytes.TrimSpace(b)) != 0 {
		return nil, errors.New("trailing data after PEM")
	}
	return certs, nil
}

// WriteNew creates path with data: temp file, fsync, then a rename that
// fails if path exists (MoveFileEx without REPLACE_EXISTING on Windows,
// link + unlink + directory fsync elsewhere). An existing file with
// identical content counts as written (idempotent retry); different content
// yields ErrExists. internal/trust stores its list cache with it.
func WriteNew(path string, data []byte) error { return writeNew(path, data) }

// tempPattern names the temp file of path ".<name>.<random>.tmp": hidden,
// never matched by the evidence file names, and removed by archive recovery,
// which deletes *.tmp.
func tempPattern(path string) string { return "." + filepath.Base(path) + ".*.tmp" }

func writeNew(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, tempPattern(path))
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = renameNoReplace(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
		if errors.Is(err, fs.ErrExist) {
			if old, rerr := os.ReadFile(path); rerr == nil && bytes.Equal(old, data) {
				return nil
			}
			return fmt.Errorf("%w: %s", ErrExists, path)
		}
		return err
	}
	return nil
}
