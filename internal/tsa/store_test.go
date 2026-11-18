package tsa

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

func TestWriteNeverOverwrites(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0001_Test.nachweis")
	if err := WriteRequest(dir, 1, []byte("first")); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(dir, 1, []byte("second")); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	if err := WriteRequest(dir, 1, []byte("first")); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	if err := WriteResponse(dir, 1, []byte("tsr")); err != nil {
		t.Fatal(err)
	}
	if err := WriteResponse(dir, 1, []byte("other")); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	m := &Material{Evidence: map[string]Evidence{strings.Repeat("a", 64): {Kind: KindOCSP, Raw: []byte("o")}}}
	if err := WriteMaterial(dir, 1, m); err != nil {
		t.Fatal(err)
	}
	m.Evidence[strings.Repeat("a", 64)] = Evidence{Kind: KindOCSP, Raw: []byte("changed")}
	if err := WriteMaterial(dir, 1, m); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	for f, want := range map[string]string{"1.tsq": "first", "1.tsr": "tsr", "1.rev/" + strings.Repeat("a", 64) + ".ocsp": "o"} {
		if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != want {
			t.Errorf("%s = %q, want %q", f, b, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
	if err := WriteMaterial(dir, 1, &Material{Evidence: map[string]Evidence{"../x": {Kind: KindCRL}}}); err == nil {
		t.Fatal("path traversal in evidence key accepted")
	}
}

// TestTempFilesEndInTmp: a crash between creating and publishing leaves the
// temp file behind. Archive recovery deletes *.tmp in a .nachweis folder
// (under the writer lock), so temp names must end in .tmp, and ReadAttempts
// must ignore them.
func TestTempFilesEndInTmp(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"1.tsq", "1.chain.pem", filepath.Join("1.rev", strings.Repeat("a", 64)+".ocsp")} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		f, err := os.CreateTemp(filepath.Dir(p), tempPattern(p))
		if err != nil {
			t.Fatal(err)
		}
		f.Write([]byte("partial"))
		f.Close() // the crash: never published
		base := filepath.Base(f.Name())
		if !strings.HasSuffix(base, ".tmp") || !strings.HasPrefix(base, ".") {
			t.Errorf("%s: temp file %q does not end in .tmp", name, base)
		}
	}
	// Only the (empty) 1.rev folder counts; no temp file is read as evidence.
	atts := must(ReadAttempts(dir))
	if len(atts) != 1 || atts[0].TSQ != nil || atts[0].Material == nil || len(atts[0].Material.Evidence) != 0 || atts[0].Material.Chain != nil {
		t.Fatalf("temp files read as evidence: %+v", atts)
	}
}

// TestWriteNewLongPath: evidence paths beyond MAX_PATH must publish (the raw
// MoveFileEx call needs the \\?\ prefix on Windows).
func TestWriteNewLongPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("d", 100), strings.Repeat("e", 100), "0001_Test.nachweis", "1.rev")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, strings.Repeat("a", 64)+".ocsp")
	if err := WriteNew(p, []byte("o")); err != nil {
		t.Fatal(err)
	}
	if err := WriteNew(p, []byte("x")); !errors.Is(err, ErrExists) {
		t.Fatalf("overwrite: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "o" {
		t.Fatalf("content %q", b)
	}
}

func TestReadAttemptsPairsByImprint(t *testing.T) {
	dir := t.TempDir()
	pw := sha256.Sum256([]byte("a"))
	other := sha256.Sum256([]byte("b"))
	r1 := must(NewRequest(pw, nil))
	r2 := must(NewRequest(other, nil)) // a misfiled attempt: name says nothing
	r3 := must(NewRequest(pw, nil))
	for n, r := range map[int]*Request{1: r1, 2: r2, 7: r3} {
		if err := WriteRequest(dir, n, r.DER); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	atts := must(ReadAttempts(dir))
	if len(atts) != 3 || atts[2].N != 7 {
		t.Fatalf("attempts %+v", atts)
	}
	got := MatchImprint(atts, pw)
	if len(got) != 2 || got[0].N != 1 || got[1].N != 7 || !bytes.Equal(got[1].TSQ, r3.DER) {
		t.Fatalf("matched %+v", got)
	}
	// A corrupt chain of one attempt must not hide the others.
	os.WriteFile(filepath.Join(dir, "7.chain.pem"), []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), 0o644)
	atts = must(ReadAttempts(dir))
	if len(atts) != 3 || atts[2].MaterialErr == nil || atts[2].Material != nil || atts[0].MaterialErr != nil {
		t.Fatalf("corrupt chain: %+v", atts)
	}
	if n := must(NextAttempt(dir)); n != 8 {
		t.Fatalf("next %d", n)
	}
	if n := must(NextAttempt(filepath.Join(dir, "missing"))); n != 1 {
		t.Fatalf("next in empty %d", n)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	pw := sha256.Sum256([]byte("x"))
	policy := []int{0, 4, 0, 2023, 1, 1}
	r := must(NewRequest(pw, policy))
	p := must(ParseRequest(r.DER))
	if p.Imprint != pw || p.Nonce.Cmp(r.Nonce) != 0 || !p.Policy.Equal(policy) {
		t.Fatalf("%+v", p)
	}
	if r.Nonce.BitLen() > 64 {
		t.Fatal("nonce longer than 64 bit")
	}
	r2 := must(NewRequest(pw, nil))
	if r2.Nonce.Cmp(r.Nonce) == 0 {
		t.Fatal("nonce reused")
	}
	var raw timeStampReq
	if err := unmarshalAll(r2.DER, &raw, ""); err != nil || !raw.CertReq || raw.ReqPolicy != nil {
		t.Fatalf("certReq/policy: %+v %v", raw, err)
	}
}

// TestChainExtendsAppendOnly: n.chain.pem may only grow by appending
// certificates (an issuer obtained later, spec §5); the stored certificates
// must stay the same DER in the same order. Any other change is refused and
// leaves the file as it was.
func TestChainExtendsAppendOnly(t *testing.T) {
	pki := tsatest.NewPKI(t, tsatest.PKIOptions{})
	tsu, inter, root := pki.TSU.Cert, pki.Inter.Cert, pki.Root.Cert
	other := pki.NewTSU(t, nil).Cert
	dir := t.TempDir()
	path := filepath.Join(dir, "1.chain.pem")
	write := func(certs ...*x509.Certificate) error {
		return WriteMaterial(dir, 1, &Material{Chain: certs, Evidence: map[string]Evidence{}})
	}
	stored := func() []byte { b, _ := os.ReadFile(path); return b }

	if err := write(tsu); err != nil { // written by an older version
		t.Fatal(err)
	}
	if err := write(tsu, inter); err != nil {
		t.Fatalf("append the issuer: %v", err)
	}
	if !bytes.Equal(stored(), EncodePEM([]*x509.Certificate{tsu, inter})) {
		t.Fatal("chain not extended")
	}
	if err := write(tsu, inter); err != nil {
		t.Fatalf("identical retry: %v", err)
	}
	for name, certs := range map[string][]*x509.Certificate{
		"shorter":        {tsu},
		"prefix altered": {other, inter, root},
		"reordered":      {inter, tsu, root},
		"inserted":       {tsu, root, inter},
		"empty":          {},
	} {
		if err := write(certs...); !errors.Is(err, ErrExists) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if !bytes.Equal(stored(), EncodePEM([]*x509.Certificate{tsu, inter})) {
		t.Fatal("a refused write changed the file")
	}
	if err := write(tsu, inter, root); err != nil {
		t.Fatalf("append again: %v", err)
	}

	// A malformed stored chain is never replaced.
	bad := []byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n")
	os.WriteFile(filepath.Join(dir, "2.chain.pem"), bad, 0o644)
	if err := WriteMaterial(dir, 2, &Material{Chain: []*x509.Certificate{tsu, inter}}); !errors.Is(err, ErrExists) {
		t.Fatalf("malformed chain replaced: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "2.chain.pem")); !bytes.Equal(b, bad) {
		t.Fatal("malformed chain changed")
	}
	// Code review 12: pem.Decode skips a block with broken base64, so the
	// rest parses as just the TSU and an extension would delete the broken
	// block. Only a byte-wise append is allowed.
	hidden := append([]byte("-----BEGIN CERTIFICATE-----\n!invalid-base64!\n-----END CERTIFICATE-----\n"), EncodePEM([]*x509.Certificate{tsu})...)
	os.WriteFile(filepath.Join(dir, "3.chain.pem"), hidden, 0o644)
	if err := WriteMaterial(dir, 3, &Material{Chain: []*x509.Certificate{tsu, inter}}); !errors.Is(err, ErrExists) {
		t.Fatalf("chain with a skipped block replaced: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "3.chain.pem")); !bytes.Equal(b, hidden) {
		t.Fatal("chain with a skipped block changed")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
}

// Code review 19: only a request this package builds may be resent; extra
// content in a stored n.tsq would otherwise leave the machine.
func TestCanonicalRequest(t *testing.T) {
	pw := sha256.Sum256([]byte("x"))
	r := must(NewRequest(pw, nil))
	if !must(ParseRequest(r.DER)).Canonical() {
		t.Fatal("own request not canonical")
	}
	withExt, _ := asn1.Marshal(timeStampReq{Version: 1,
		MessageImprint: messageImprint{HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue}, HashedMessage: pw[:]},
		Nonce:          r.Nonce, CertReq: true,
		Extensions: []pkix.Extension{{Id: asn1.ObjectIdentifier{1, 2, 3}, Value: []byte("Rechnung Muster GmbH.pdf")}}})
	huge := new(big.Int).Lsh(big.NewInt(1), 70)
	bigNonce, _ := asn1.Marshal(timeStampReq{Version: 1,
		MessageImprint: messageImprint{HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue}, HashedMessage: pw[:]},
		Nonce:          huge, CertReq: true})
	noCert, _ := asn1.Marshal(timeStampReq{Version: 1,
		MessageImprint: messageImprint{HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue}, HashedMessage: pw[:]},
		Nonce:          r.Nonce})
	for name, der := range map[string][]byte{"extension": withExt, "oversized nonce": bigNonce, "certReq false": noCert} {
		p, err := ParseRequest(der)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if p.Canonical() {
			t.Errorf("%s accepted as canonical", name)
		}
	}
}
