// Package tsatest provides a throw-away PKI and an RFC 3161 test TSA for
// tests. It is never imported by production code.
package tsatest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ocsp"
)

// Cert is a certificate with its private key.
type Cert struct {
	Cert *x509.Certificate
	Key  crypto.Signer
}

// CertOptions describes a certificate to issue.
type CertOptions struct {
	CN                  string
	Key                 crypto.Signer // default: new ECDSA P-256 key
	IsCA                bool
	NotBefore, NotAfter time.Time // default: now-1h .. now+1y
	EKU                 []asn1.ObjectIdentifier
	EKUCritical         bool
	KeyUsage            x509.KeyUsage // default: by role
	OCSP, CRL, Issuers  []string
	SigAlg              x509.SignatureAlgorithm
}

var (
	OIDTimeStamping = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	OIDOCSPSigning  = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 9}
	OIDCodeSigning  = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 3}
)

// ECKey returns a new P-256 key.
func ECKey(tb testing.TB) crypto.Signer {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatal(err)
	}
	return k
}

// RSAKey returns a new RSA key.
func RSAKey(tb testing.TB, bits int) crypto.Signer {
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		tb.Fatal(err)
	}
	return k
}

var serial struct {
	sync.Mutex
	n int64
}

func nextSerial() *big.Int {
	serial.Lock()
	defer serial.Unlock()
	serial.n++
	return big.NewInt(1000 + serial.n)
}

// SelfSigned creates a root CA.
func SelfSigned(tb testing.TB, o CertOptions) *Cert {
	o.IsCA = true
	return issue(tb, nil, o)
}

// Issue creates a certificate signed by ca.
func (ca *Cert) Issue(tb testing.TB, o CertOptions) *Cert {
	return issue(tb, ca, o)
}

func issue(tb testing.TB, parent *Cert, o CertOptions) *Cert {
	tb.Helper()
	if o.Key == nil {
		o.Key = ECKey(tb)
	}
	now := time.Now()
	if o.NotBefore.IsZero() {
		o.NotBefore = now.Add(-time.Hour)
	}
	if o.NotAfter.IsZero() {
		o.NotAfter = now.Add(365 * 24 * time.Hour)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          nextSerial(),
		Subject:               pkix.Name{CommonName: o.CN, Organization: []string{"Rechnungsarchiv Test"}},
		NotBefore:             o.NotBefore,
		NotAfter:              o.NotAfter,
		BasicConstraintsValid: true,
		IsCA:                  o.IsCA,
		KeyUsage:              o.KeyUsage,
		OCSPServer:            o.OCSP,
		CRLDistributionPoints: o.CRL,
		IssuingCertificateURL: o.Issuers,
		SignatureAlgorithm:    o.SigAlg,
	}
	if tmpl.KeyUsage == 0 {
		if o.IsCA {
			tmpl.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		} else {
			tmpl.KeyUsage = x509.KeyUsageDigitalSignature
		}
	}
	if len(o.EKU) > 0 {
		v, err := asn1.Marshal(o.EKU)
		if err != nil {
			tb.Fatal(err)
		}
		tmpl.ExtraExtensions = append(tmpl.ExtraExtensions, pkix.Extension{
			Id: asn1.ObjectIdentifier{2, 5, 29, 37}, Critical: o.EKUCritical, Value: v,
		})
	}
	signer, parentCert := o.Key, tmpl
	if parent != nil {
		signer, parentCert = parent.Key, parent.Cert
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, o.Key.Public(), signer)
	if err != nil {
		tb.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatal(err)
	}
	return &Cert{Cert: c, Key: o.Key}
}

// RevEntry marks a certificate as revoked in the PKI's responders.
type RevEntry struct {
	Reason int
	At     time.Time
}

// PKI is root → intermediate → TSU, with a delegated OCSP responder for the
// intermediate and CRLs for both CAs, served over HTTP. The TSU certificate
// names OCSP (and a CRL); the intermediate names only a CRL.
type PKI struct {
	Root, Inter, TSU, Responder *Cert
	URL                         string // base URL of the AIA/OCSP/CRL server

	mu      sync.Mutex
	revoked map[string]RevEntry
	// OCSPHook, if set, replaces the OCSP answer for a request.
	OCSPHook func(req *ocsp.Request) []byte
	// CRLHook, if set, replaces the CRL served at /crl/<name>.crl.
	CRLHook func(name string) []byte
	// Hits counts requests per path.
	Hits map[string]int
	down bool
}

// SetDown makes the AIA/OCSP/CRL server answer 503 (down=true) or serve
// normally again. Requests are still counted.
func (p *PKI) SetDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.down = down
}

// PKIOptions tweaks NewPKI.
type PKIOptions struct {
	RSA bool                 // RSA-2048 keys instead of P-256
	TSU func(o *CertOptions) // adjust the TSU certificate
}

// NewPKI builds the PKI and starts its HTTP server (closed via tb.Cleanup).
func NewPKI(tb testing.TB, opts PKIOptions) *PKI {
	tb.Helper()
	p := &PKI{revoked: map[string]RevEntry{}, Hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(p.serve))
	tb.Cleanup(srv.Close)
	p.URL = srv.URL
	key := func() crypto.Signer {
		if opts.RSA {
			return RSAKey(tb, 2048)
		}
		return ECKey(tb)
	}
	now := time.Now()
	p.Root = SelfSigned(tb, CertOptions{CN: "Test Root", Key: key(), NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(10 * 365 * 24 * time.Hour)})
	p.Inter = p.Root.Issue(tb, CertOptions{
		CN: "Test TSA CA", Key: key(), IsCA: true,
		NotBefore: now.Add(-24 * time.Hour), NotAfter: now.Add(5 * 365 * 24 * time.Hour),
		CRL: []string{p.URL + "/crl/root.crl"}, Issuers: []string{p.URL + "/root.cer"},
	})
	tsu := CertOptions{
		CN: "Test TSU", Key: key(),
		EKU: []asn1.ObjectIdentifier{OIDTimeStamping}, EKUCritical: true,
		OCSP: []string{p.URL + "/ocsp/inter"}, CRL: []string{p.URL + "/crl/inter.crl"},
		Issuers: []string{p.URL + "/inter.cer"},
	}
	if opts.TSU != nil {
		opts.TSU(&tsu)
	}
	p.TSU = p.Inter.Issue(tb, tsu)
	p.Responder = p.Inter.Issue(tb, CertOptions{CN: "Test OCSP Responder", EKU: []asn1.ObjectIdentifier{OIDOCSPSigning}})
	return p
}

// NewTSU issues another TSU certificate from the intermediate.
func (p *PKI) NewTSU(tb testing.TB, adjust func(o *CertOptions)) *Cert {
	o := CertOptions{
		CN: "Test TSU 2", EKU: []asn1.ObjectIdentifier{OIDTimeStamping}, EKUCritical: true,
		OCSP: []string{p.URL + "/ocsp/inter"}, Issuers: []string{p.URL + "/inter.cer"},
	}
	if adjust != nil {
		adjust(&o)
	}
	return p.Inter.Issue(tb, o)
}

// Revoke marks c revoked for both OCSP and CRL.
func (p *PKI) Revoke(c *x509.Certificate, reason int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.revoked[c.SerialNumber.String()] = RevEntry{Reason: reason, At: time.Now().Add(-time.Minute)}
}

func (p *PKI) serve(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	p.Hits[r.URL.Path]++
	ocspHook, crlHook, down := p.OCSPHook, p.CRLHook, p.down
	p.mu.Unlock()
	if down {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/root.cer":
		w.Write(p.Root.Cert.Raw)
	case "/inter.cer":
		w.Write(p.Inter.Cert.Raw)
	case "/ocsp/inter":
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		req, err := ocsp.ParseRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if ocspHook != nil {
			w.Write(ocspHook(req))
			return
		}
		w.Write(p.OCSP(req.SerialNumber, p.Inter, p.Responder, time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour)))
	case "/crl/root.crl", "/crl/inter.crl":
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/crl/"), ".crl")
		if crlHook != nil {
			w.Write(crlHook(name))
			return
		}
		ca := p.Root
		if name == "inter" {
			ca = p.Inter
		}
		w.Write(p.CRL(ca, time.Now().Add(-time.Hour), time.Now().Add(7*24*time.Hour), nil))
	default:
		http.NotFound(w, r)
	}
}

// OCSP builds a response for serial issued by issuer, signed by signer
// (the issuer itself or a delegated responder), using the PKI's revocation
// state.
func (p *PKI) OCSP(serial *big.Int, issuer, signer *Cert, thisUpdate, nextUpdate time.Time) []byte {
	tmpl := ocsp.Response{
		Status: ocsp.Good, SerialNumber: serial,
		ThisUpdate: thisUpdate, NextUpdate: nextUpdate,
	}
	p.mu.Lock()
	if e, ok := p.revoked[serial.String()]; ok {
		tmpl.Status, tmpl.RevocationReason, tmpl.RevokedAt = ocsp.Revoked, e.Reason, e.At
	}
	p.mu.Unlock()
	return SignOCSP(issuer, signer, tmpl)
}

// SignOCSP signs an OCSP response. If signer differs from issuer, its
// certificate is embedded.
func SignOCSP(issuer, signer *Cert, tmpl ocsp.Response) []byte {
	if !signer.Cert.Equal(issuer.Cert) {
		tmpl.Certificate = signer.Cert
	}
	der, err := ocsp.CreateResponse(issuer.Cert, signer.Cert, tmpl, signer.Key)
	if err != nil {
		panic(err)
	}
	return der
}

// CRL builds a CRL from ca with the PKI's revocation state for certificates
// ca issued. exts are added to the CRL (e.g. delta or IDP).
func (p *PKI) CRL(ca *Cert, thisUpdate, nextUpdate time.Time, exts []pkix.Extension) []byte {
	var entries []x509.RevocationListEntry
	p.mu.Lock()
	for s, e := range p.revoked {
		n, _ := new(big.Int).SetString(s, 10)
		for _, c := range []*Cert{p.Inter, p.TSU, p.Responder} {
			if c.Cert.SerialNumber.Cmp(n) == 0 && c.Cert.CheckSignatureFrom(ca.Cert) == nil {
				entries = append(entries, x509.RevocationListEntry{SerialNumber: n, RevocationTime: e.At, ReasonCode: e.Reason})
			}
		}
	}
	p.mu.Unlock()
	return SignCRL(ca, thisUpdate, nextUpdate, entries, exts)
}

var crlNumber struct {
	sync.Mutex
	n int64
}

// SignCRL signs a CRL.
func SignCRL(ca *Cert, thisUpdate, nextUpdate time.Time, entries []x509.RevocationListEntry, exts []pkix.Extension) []byte {
	crlNumber.Lock()
	crlNumber.n++
	num := big.NewInt(crlNumber.n)
	crlNumber.Unlock()
	der, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
		Number: num, ThisUpdate: thisUpdate, NextUpdate: nextUpdate,
		RevokedCertificateEntries: entries, ExtraExtensions: exts,
	}, ca.Cert, ca.Key)
	if err != nil {
		panic(err)
	}
	return der
}
