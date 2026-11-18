package tsa

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
	"software.sslmate.com/src/go-pkcs12"
)

// tsaServer runs the test TSA over HTTPS with HTTP Basic auth.
func tsaServer(t *testing.T, e *env) (*tsatest.TSA, *httptest.Server, *Client) {
	t.Helper()
	ts := &tsatest.TSA{Signer: e.pki.TSU, Chain: []*x509.Certificate{e.pki.Inter.Cert}, Policy: tsatest.DefaultPolicy}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != "kunde" || p != "geheim" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		ts.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	c := &Client{URL: srv.URL, Auth: AuthBasic, Creds: Credentials{User: "kunde", Password: "geheim"}, RootCAs: pool, Timeout: 5 * time.Second}
	return ts, srv, c
}

func wantReason(t *testing.T, err error, r Reason) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Reason != r {
		t.Fatalf("want reason %s, got %v", r, err)
	}
	t.Log(err)
}

// TestStampEndToEnd runs the whole stamping flow against the local TSA and
// then verifies the stored attempt offline.
func TestStampEndToEnd(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, tsatest.PKIOptions{RSA: true})
	_, _, c := tsaServer(t, e)
	dir := filepath.Join(t.TempDir(), "0001_Test.nachweis")

	n := must(NextAttempt(dir))
	req := must(NewRequest(e.pw, nil))
	if err := WriteRequest(dir, n, req.DER); err != nil {
		t.Fatal(err)
	}
	tsr, err := c.Send(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteResponse(dir, n, tsr); err != nil {
		t.Fatal(err)
	}
	opts := e.opts()
	opts.Request = req
	var f Fetcher
	tok := must(f.Accept(ctx, tsr, opts))
	m, _, err := f.Collect(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteMaterial(dir, n, m); err != nil {
		t.Fatal(err)
	}

	// Offline: only files and the anchors.
	atts := MatchImprint(must(ReadAttempts(dir)), e.pw)
	if len(atts) != 1 || atts[0].Material == nil {
		t.Fatalf("attempts %+v", atts)
	}
	a := atts[0]
	off := VerifyOptions{Request: a.Request, Policy: tsatest.DefaultPolicy, Anchors: opts.Anchors, Intermediates: a.Material.Chain}
	tok2 := must(VerifyResponse(a.TSR, off))
	checks, err := ValidateMaterial(tok2, a.Material)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 2 || !tok2.GenTime.Equal(tok.GenTime) {
		t.Fatalf("%d checks", len(checks))
	}
}

func TestClientErrors(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, tsatest.PKIOptions{})
	ts, srv, c := tsaServer(t, e)
	reply := func(status int, body string) {
		ts.SetReply(func(w http.ResponseWriter, r *http.Request) bool {
			w.WriteHeader(status)
			w.Write([]byte(body))
			return true
		})
	}
	t.Run("wrong password", func(t *testing.T) {
		c2 := *c
		c2.Creds.Password = "falsch"
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonZugang)
	})
	t.Run("no credentials on this PC", func(t *testing.T) {
		c2 := *c
		c2.Creds = Credentials{}
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonZugang)
	})
	t.Run("403", func(t *testing.T) {
		reply(http.StatusForbidden, "Forbidden")
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonZugang)
	})
	t.Run("402 (Mensatek: no credit)", func(t *testing.T) {
		reply(http.StatusPaymentRequired, "")
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonGuthaben)
	})
	t.Run("rejection with quota text", func(t *testing.T) {
		reply(http.StatusOK, string(tsatest.Response(2, "Insufficient credit", nil)))
		tsr, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonGuthaben)
		if tsr == nil {
			t.Fatal("rejection body not returned")
		}
	})
	t.Run("plain rejection", func(t *testing.T) {
		reply(http.StatusOK, string(tsatest.Response(2, "badAlg", nil)))
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
	})
	t.Run("500", func(t *testing.T) {
		reply(http.StatusInternalServerError, "oops")
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
	})
	t.Run("HTML instead of a response", func(t *testing.T) {
		reply(http.StatusOK, "<html>maintenance</html>")
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
	})
	t.Run("redirect is not followed", func(t *testing.T) {
		hit := false
		target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
		defer target.Close()
		ts.SetReply(func(w http.ResponseWriter, r *http.Request) bool {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
			return true
		})
		_, err := c.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
		if hit {
			t.Fatal("redirect followed")
		}
	})
	ts.SetReply(nil)
	t.Run("plain HTTP refused", func(t *testing.T) {
		c2 := *c
		c2.URL = "http://" + srv.Listener.Addr().String()
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
	})
	t.Run("untrusted server certificate", func(t *testing.T) {
		c2 := *c
		c2.RootCAs = x509.NewCertPool()
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonAnbieter)
	})
	t.Run("offline", func(t *testing.T) {
		dead := httptest.NewTLSServer(http.NotFoundHandler())
		url := dead.URL
		dead.Close()
		c2 := *c
		c2.URL = url
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonOffline)
	})
	t.Run("timeout", func(t *testing.T) {
		ts.SetReply(func(w http.ResponseWriter, r *http.Request) bool {
			time.Sleep(300 * time.Millisecond)
			return false
		})
		defer ts.SetReply(nil)
		c2 := *c
		c2.Timeout = 50 * time.Millisecond
		_, err := c2.Send(ctx, e.req)
		wantReason(t, err, ReasonOffline)
	})
}

func TestClientCert(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, tsatest.PKIOptions{RSA: true})
	ts := &tsatest.TSA{Signer: e.pki.TSU, Chain: []*x509.Certificate{e.pki.Inter.Cert}}
	clientCA := tsatest.SelfSigned(t, tsatest.CertOptions{CN: "Client CA"})
	srv := httptest.NewUnstartedServer(ts)
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: x509.NewCertPool()}
	srv.TLS.ClientCAs.AddCert(clientCA.Cert)
	srv.StartTLS()
	defer srv.Close()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	p12 := func(ca *tsatest.Cert) []byte {
		cl := ca.Issue(t, tsatest.CertOptions{CN: "Kunde", Key: tsatest.RSAKey(t, 2048)})
		b, err := pkcs12.Modern.Encode(cl.Key, cl.Cert, nil, "pw")
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	good := p12(clientCA)
	if _, err := LoadClientCert(good, "falsch"); err == nil {
		t.Fatal("wrong PKCS#12 password accepted")
	}
	cert := must(LoadClientCert(good, "pw"))
	c := &Client{URL: srv.URL, Auth: AuthClientCert, Creds: Credentials{ClientCert: cert}, RootCAs: pool, Timeout: 5 * time.Second}
	if _, err := c.Send(ctx, e.req); err != nil {
		t.Fatal(err)
	}
	c.Creds.ClientCert = must(LoadClientCert(p12(tsatest.SelfSigned(t, tsatest.CertOptions{CN: "Other CA"})), "pw"))
	_, err := c.Send(ctx, e.req)
	wantReason(t, err, ReasonZugang)
}
