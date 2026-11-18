package tsa

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left: %s", e.Name())
		}
	}
	if err := WriteMaterial(dir, 1, &Material{Evidence: map[string]Evidence{"../x": {Kind: KindCRL}}}); err == nil {
		t.Fatal("path traversal in evidence key accepted")
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
