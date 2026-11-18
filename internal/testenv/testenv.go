//go:build testtsa

// Package testenv sets up a complete offline world for end-to-end tests: an
// archive with the demo invoices, a test PKI with an RFC 3161 TSA over
// HTTPS, a registered test provider, credentials, and synthetic signed EU
// trusted lists that name the test PKI as a QTST service. Only tests import
// it, and only with the testtsa build tag.
package testenv

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/realspqrk/rechnungsarchiv/internal/archive"
	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/stamp"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/trust/trusttest"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa/tsatest"
)

// Territory of the synthetic trusted list.
const Territory = "ZZ"

// Basic-auth credentials the test TSA accepts.
const (
	User     = "kunde"
	Password = "geheim"
)

// Env is one isolated test world.
type Env struct {
	T        testing.TB
	PKI      *tsatest.PKI
	TSA      *tsatest.TSA
	Server   *httptest.Server
	Lists    *trusttest.Fixture
	Creds    creds.Store
	Provider tsa.Provider
	Dir      string // archive folder
	Archive  *archive.Archive
}

var providerSeq atomic.Int64

// Mode switches the test TSA's HTTP behaviour.
type Mode int

const (
	Normal  Mode = iota
	Offline      // the connection is dropped (no network)
	Denied       // 401
	NoQuota      // 402
)

// New builds the world: an empty archive with the test provider set up.
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{T: t, PKI: tsatest.NewPKI(t, tsatest.PKIOptions{}), Creds: creds.NewMemory()}
	e.TSA = &tsatest.TSA{Signer: e.PKI.TSU, Chain: []*x509.Certificate{e.PKI.Inter.Cert}, Policy: tsatest.DefaultPolicy}
	e.TSA.SetEdit(WithAccuracy)
	e.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != User || p != Password {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		e.TSA.ServeHTTP(w, r)
	}))
	t.Cleanup(e.Server.Close)

	e.Lists = trusttest.New(t, Territory)
	e.PublishLists(time.Now().Add(-48 * time.Hour))

	id := fmt.Sprintf("test%d", providerSeq.Add(1))
	e.Provider = tsa.Provider{
		ID: id, Name: "Test-Zeitstempeldienst", Country: Territory, URL: e.Server.URL, Auth: tsa.AuthBasic,
		ExpectedPolicies: []string{tsatest.DefaultPolicy.String()}, Accuracy: "1s", TrustedList: Territory,
	}
	tsa.RegisterTestProvider(e.Provider)

	e.Dir = filepath.Join(t.TempDir(), "Archiv")
	a, err := archive.Init(e.Dir, "Muster Tischlerei GmbH")
	if err != nil {
		t.Fatal(err)
	}
	e.Archive = a
	e.SetProvider(id)
	if err := e.Creds.Set(a.Config.ArchiveID, id, creds.Credential{User: User, Password: Password}); err != nil {
		t.Fatal(err)
	}
	return e
}

// WithAccuracy is the default token edit: accuracy 1 s, as real QTSPs state.
func WithAccuracy(p *tsatest.TokenParams) { p.Accuracy = &tsatest.Accuracy{Seconds: 1} }

// PublishLists publishes a new trusted list issued at issue that names the
// test root as a QTST service granted for the last year.
func (e *Env) PublishLists(issue time.Time) {
	e.Lists.Publish(e.T, issue, trusttest.Service{
		TSP: "Test Trust Services", Name: "Test QTST", Certs: []*x509.Certificate{e.PKI.Root.Cert},
		History: []trusttest.Status{{URI: trusttest.Granted, Start: time.Now().AddDate(-1, 0, 0)}},
	})
}

// SetProvider sets the provider id in rechnungsarchiv.json ("" = none).
func (e *Env) SetProvider(id string) {
	e.T.Helper()
	var p *string
	if id != "" {
		p = &id
	}
	if err := e.Archive.SetProvider(p); err != nil {
		e.T.Fatal(err)
	}
}

// SetMode switches the TSA's behaviour.
func (e *Env) SetMode(m Mode) {
	if m == Normal {
		e.TSA.SetReply(nil)
		return
	}
	e.TSA.SetReply(func(w http.ResponseWriter, r *http.Request) bool {
		switch m {
		case Offline:
			if hj, ok := w.(http.Hijacker); ok {
				if c, _, err := hj.Hijack(); err == nil {
					c.Close()
				}
			}
		case Denied:
			http.Error(w, "invalid credentials", http.StatusUnauthorized)
		case NoQuota:
			http.Error(w, "payment required", http.StatusPaymentRequired)
		}
		return true
	})
}

// Demo returns the path of a demo invoice folder.
func Demo(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "demo", name)
}

// Folders of testdata/demo.
const (
	Incoming = "2026-10 Eingangsrechnungen"
	Outgoing = "2026-10 Ausgangsrechnungen"
)

// Add archives a demo folder as a new Ablage.
func (e *Env) Add(folder string, opts archive.Options) *archive.Deposit {
	e.T.Helper()
	d, err := e.Archive.Archive(Demo(folder), opts)
	if err != nil {
		e.T.Fatal(err)
	}
	return d
}

// Trust opens the archive's trusted-list store with network access to the
// synthetic lists (normal mode: refreshed lists go into the archive).
func (e *Env) Trust() *trust.Store {
	dir := filepath.Join(e.Dir, trust.DirName)
	return e.TrustAt([]string{dir}, dir)
}

// OfflineTrust opens a store over the given list caches without network.
func (e *Env) OfflineTrust(dirs ...string) *trust.Store {
	return e.TrustAt(dirs, "")
}

// TrustAt opens a store that reads dirs and stores refreshed lists in
// write ("" = no network at all).
func (e *Env) TrustAt(dirs []string, write string) *trust.Store {
	e.T.Helper()
	cfg := trust.TestConfig{WriteDir: write, ReadDirs: dirs, Pins: []*x509.Certificate{e.Lists.LOTLSigner}}
	if write != "" {
		cfg.Client = e.Lists.Client()
	} else {
		cfg.Client = &http.Client{Transport: offline{}}
	}
	s, err := trust.OpenTest(cfg)
	if err != nil {
		e.T.Fatal(err)
	}
	return s
}

type offline struct{}

func (offline) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("offline")}
}

// RootCAs trusts the test TSA's TLS certificate.
func (e *Env) RootCAs() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(e.Server.Certificate())
	return p
}

// StampOptions returns ready stamping options.
func (e *Env) StampOptions(s *trust.Store) stamp.Options {
	return stamp.Options{Trust: s, Creds: e.Creds, Lock: e.Archive.Lock, RootCAs: e.RootCAs()}
}

// Stamp runs stamp.Run with a fresh normal-mode trust store.
func (e *Env) Stamp() *stamp.Result {
	e.T.Helper()
	r, err := stamp.Run(context.Background(), e.Archive, e.StampOptions(e.Trust()))
	if err != nil {
		e.T.Fatal(err)
	}
	return r
}

// JSON renders v for failure messages.
func JSON(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
