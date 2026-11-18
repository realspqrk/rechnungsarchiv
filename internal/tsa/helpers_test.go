package tsa

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"testing"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

type env struct {
	pki *tsatest.PKI
	pw  [32]byte
	req *Request
}

func newEnv(t testing.TB, o tsatest.PKIOptions) *env {
	t.Helper()
	e := &env{pki: tsatest.NewPKI(t, o), pw: sha256.Sum256([]byte("Ablage 0001"))}
	var err error
	if e.req, err = NewRequest(e.pw, nil); err != nil {
		t.Fatal(err)
	}
	return e
}

// params returns a token for e.req as the honest TSU would issue it.
func (e *env) params() tsatest.TokenParams {
	return tsatest.TokenParams{
		Imprint: e.pw[:], Nonce: e.req.Nonce, Signer: e.pki.TSU,
		Certs: []*x509.Certificate{e.pki.TSU.Cert, e.pki.Inter.Cert},
	}
}

func (e *env) token(t testing.TB, edit func(p *tsatest.TokenParams)) []byte {
	t.Helper()
	p := e.params()
	if edit != nil {
		edit(&p)
	}
	tok, err := tsatest.Token(p)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (e *env) opts() VerifyOptions {
	return VerifyOptions{Request: e.req, Policies: []asn1.ObjectIdentifier{tsatest.DefaultPolicy}, Anchors: []*x509.Certificate{e.pki.Root.Cert}}
}

func (e *env) verify(t testing.TB, edit func(p *tsatest.TokenParams)) (*Token, error) {
	t.Helper()
	return VerifyResponse(tsatest.Response(0, "", e.token(t, edit)), e.opts())
}

func wantItem(t *testing.T, err error, item int) {
	t.Helper()
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("want InvalidError item %d, got %v", item, err)
	}
	if ie.Item != item {
		t.Fatalf("want item %d, got %v", item, err)
	}
}

// must panics on error; in a test that shows up as a failure with stack.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
