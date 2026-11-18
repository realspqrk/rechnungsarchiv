//go:build testtsa

package main

// Test builds only (-tags testtsa): if RECHNUNGSARCHIV_TESTENV names a JSON
// file, the binary trusts the test PKI and the synthetic trusted lists of
// internal/testenv, registers the test provider and keeps credentials in a
// file instead of the Windows Credential Manager. Release builds do not
// contain this file.

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/realspqrk/rechnungsarchiv/internal/creds"
	"github.com/realspqrk/rechnungsarchiv/internal/trust"
	"github.com/realspqrk/rechnungsarchiv/internal/tsa"
)

const testEnvVar = "RECHNUNGSARCHIV_TESTENV"

// testConfig is written by the CLI tests.
type testConfig struct {
	Provider   tsa.Provider `json:"provider"`
	TLSRoots   string       `json:"tls_roots"`   // PEM: the test TSA's TLS certificate
	LOTLPins   string       `json:"lotl_pins"`   // PEM: the synthetic LOTL signer
	ListsProxy string       `json:"lists_proxy"` // serves trusted lists: GET <proxy>?u=<original URL>
	CredsFile  string       `json:"creds_file"`
	Cache      string       `json:"cache"` // read-only trusted-list cache
}

func init() {
	p := os.Getenv(testEnvVar)
	if p == "" {
		return
	}
	if err := loadTestEnv(p); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		os.Exit(1)
	}
}

func loadTestEnv(p string) error {
	data, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var c testConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return err
	}
	tsa.RegisterTestProvider(c.Provider)
	pins, err := pemCerts(c.LOTLPins)
	if err != nil {
		return err
	}
	roots, err := pemCerts(c.TLSRoots)
	if err != nil {
		return err
	}
	sys.rootCAs = x509.NewCertPool()
	for _, r := range roots {
		sys.rootCAs.AddCert(r)
	}
	client := &http.Client{Transport: proxy{c.ListsProxy}}
	open := func(write string, read ...string) (*trust.Store, error) {
		return trust.OpenTest(trust.TestConfig{WriteDir: write, ReadDirs: read, Pins: pins, Client: client})
	}
	sys.trust = func(dir string) (*trust.Store, error) {
		d := filepath.Join(dir, trust.DirName)
		return open(d, d)
	}
	sys.trustRead = func(dir string) (*trust.Store, error) {
		return open(c.Cache, filepath.Join(dir, trust.DirName), c.Cache)
	}
	sys.trustExport = func(dir string) (*trust.Store, error) {
		return trust.OpenTest(trust.TestConfig{ReadDirs: []string{filepath.Join(dir, trust.DirName)}, Pins: pins, Client: &http.Client{Transport: proxy{}}})
	}
	store := &fileCreds{path: c.CredsFile}
	sys.creds = func() creds.Store { return store }
	return nil
}

func pemCerts(s string) ([]*x509.Certificate, error) {
	var out []*x509.Certificate
	rest := []byte(s)
	for {
		var b *pem.Block
		b, rest = pem.Decode(rest)
		if b == nil {
			return out, nil
		}
		c, err := x509.ParseCertificate(b.Bytes)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
}

// proxy sends every request to the test's list server; an empty base is
// offline.
type proxy struct{ base string }

func (p proxy) RoundTrip(r *http.Request) (*http.Response, error) {
	if p.base == "" {
		return nil, fmt.Errorf("offline")
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, p.base+"?u="+url.QueryEscape(r.URL.String()), nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(req)
}

// fileCreds is a creds.Store in a JSON file.
type fileCreds struct {
	mu   sync.Mutex
	path string
}

func (f *fileCreds) load() map[string]creds.Credential {
	m := map[string]creds.Credential{}
	if data, err := os.ReadFile(f.path); err == nil {
		json.Unmarshal(data, &m)
	}
	return m
}

func (f *fileCreds) Set(archiveID, provider string, c creds.Credential) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.load()
	m[archiveID+"/"+provider] = c
	data, _ := json.Marshal(m)
	return os.WriteFile(f.path, data, 0o600)
}

func (f *fileCreds) Get(archiveID, provider string) (creds.Credential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.load()[archiveID+"/"+provider]
	if !ok {
		return creds.Credential{}, creds.ErrNotFound
	}
	return c, nil
}

func (f *fileCreds) Has(archiveID, provider string) (bool, error) {
	_, err := f.Get(archiveID, provider)
	return err == nil, nil
}

func (f *fileCreds) Delete(archiveID, provider string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.load()
	delete(m, archiveID+"/"+provider)
	data, _ := json.Marshal(m)
	return os.WriteFile(f.path, data, 0o600)
}
