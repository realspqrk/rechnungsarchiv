package tsa

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/ocsp"
)

// Evidence is one stored revocation file (n.rev/<CertKey>.<kind>).
type Evidence struct {
	Kind EvidenceKind
	Raw  []byte
}

// Material is the validation material of one attempt: n.chain.pem and
// n.rev/.
type Material struct {
	Chain    []*x509.Certificate
	Evidence map[string]Evidence // key: CertKey of the certificate
}

// CertKey names a certificate's evidence file: hex SHA-256 of its DER.
func CertKey(c *x509.Certificate) string {
	s := sha256.Sum256(c.Raw)
	return hex.EncodeToString(s[:])
}

// CertCheck is the revocation result for one certificate of the path.
type CertCheck struct {
	Cert       *x509.Certificate
	Revocation *Revocation // nil if no usable evidence
	Err        error       // why the evidence is missing or unusable
}

// ErrMaterial marks missing or unacceptable validation material (reason
// prüfdaten). Use errors.Is.
var ErrMaterial = errors.New("Prüfdaten fehlen oder sind ungültig")

// needsEvidence returns the certificates that need revocation evidence and
// their issuers: the signer and every CA strictly between it and the
// anchor. The anchor CA itself is governed by the trusted list.
func needsEvidence(tok *Token, pool []*x509.Certificate) (certs, issuers []*x509.Certificate, err error) {
	ch := tok.Chain
	n := len(ch) - 1
	if n == 0 {
		n = 1 // the signer is the anchor: still needs evidence
	}
	for i := 0; i < n; i++ {
		var iss *x509.Certificate
		if i+1 < len(ch) {
			iss = ch[i+1]
		} else if iss = findIssuer(ch[i], pool); iss == nil {
			return nil, nil, fmt.Errorf("%w: Aussteller von %s fehlt", ErrMaterial, ch[i].Subject)
		}
		certs, issuers = append(certs, ch[i]), append(issuers, iss)
	}
	return certs, issuers, nil
}

func findIssuer(c *x509.Certificate, pool []*x509.Certificate) *x509.Certificate {
	for _, p := range pool {
		if bytes.Equal(p.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(p) == nil {
			return p
		}
	}
	return nil
}

// ValidateMaterial checks stored validation material for a verified token
// (offline). It returns one CertCheck per certificate that needs evidence;
// the error wraps ErrMaterial unless every check is OK(). Revoked results
// are returned even when they do not cover genTime, so callers can apply
// the key-compromise rule (spec §6 step 7).
func ValidateMaterial(tok *Token, m *Material) ([]CertCheck, error) {
	pool := append(append([]*x509.Certificate{}, tok.Chain...), m.Chain...)
	certs, issuers, err := needsEvidence(tok, pool)
	if err != nil {
		return nil, err
	}
	var checks []CertCheck
	var bad error
	for i, c := range certs {
		cc := CertCheck{Cert: c}
		ev, ok := m.Evidence[CertKey(c)]
		if !ok {
			cc.Err = errors.New("keine Sperrinformation")
		} else {
			cc.Revocation, cc.Err = CheckRevocation(ev.Kind, ev.Raw, c, issuers[i], tok.GenTime)
		}
		if cc.Err == nil && !cc.Revocation.OK() {
			cc.Err = describe(cc.Revocation)
		}
		if cc.Err != nil && bad == nil {
			bad = fmt.Errorf("%w: %s: %v", ErrMaterial, c.Subject.CommonName, cc.Err)
		}
		checks = append(checks, cc)
	}
	return checks, bad
}

func describe(r *Revocation) error {
	switch {
	case r.Status == StatusRevoked && r.Reason == ReasonCertificateHold:
		return errors.New("Zertifikat gesperrt (certificateHold)")
	case r.Status == StatusRevoked:
		return fmt.Errorf("Zertifikat widerrufen (Grund %d)", r.Reason)
	case r.Status == StatusUnknown:
		return errors.New("Status unbekannt")
	}
	return errors.New("Sperrinformation deckt den Zeitpunkt nicht ab")
}

// Fetcher retrieves certificates (AIA) and revocation evidence over the
// network. The zero value uses a client with a 30 s timeout.
type Fetcher struct {
	HTTP *http.Client
}

const maxFetch = 10 << 20 // CRLs can be large

func (f *Fetcher) client() *http.Client {
	if f != nil && f.HTTP != nil {
		return f.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (f *Fetcher) do(ctx context.Context, method, url, ctype string, body []byte) ([]byte, error) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return nil, fmt.Errorf("unsupported URL %q", url)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFetch {
		return nil, fmt.Errorf("%s: response too large", url)
	}
	return b, nil
}

// Accept verifies a freshly received response like VerifyResponse, first
// completing the chain from the signer via AIA caIssuers where certificates
// are missing.
func (f *Fetcher) Accept(ctx context.Context, tsr []byte, opts VerifyOptions) (*Token, error) {
	der, err := ParseResponse(tsr)
	if err != nil {
		return nil, err
	}
	tok, err := checkToken(der, opts)
	if err != nil {
		return nil, err
	}
	tok.Certs = f.completeChain(ctx, tok.Signer, tok.Certs, opts.Anchors)
	chain, err := verifyPath(tok.Signer, tok.Certs, opts.Anchors, tok.GenTime)
	if err != nil {
		return nil, err
	}
	tok.Chain = chain
	return tok, nil
}

// completeChain walks issuers upwards and fetches the missing ones. Errors
// are not fatal here: path validation reports what is still missing.
func (f *Fetcher) completeChain(ctx context.Context, c *x509.Certificate, pool, anchors []*x509.Certificate) []*x509.Certificate {
	for depth := 0; depth < 8; depth++ {
		for _, a := range anchors {
			if a.Equal(c) || (bytes.Equal(a.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(a) == nil) {
				return pool
			}
		}
		if iss := findIssuer(c, pool); iss != nil {
			c = iss
			continue
		}
		var iss *x509.Certificate
		for _, url := range c.IssuingCertificateURL {
			b, err := f.do(ctx, http.MethodGet, url, "", nil)
			if err != nil {
				continue
			}
			for _, cand := range parseCertsLoose(b) {
				if bytes.Equal(cand.RawSubject, c.RawIssuer) && c.CheckSignatureFrom(cand) == nil {
					iss = cand
				}
			}
			if iss != nil {
				break
			}
		}
		if iss == nil {
			return pool
		}
		pool = append(pool, iss)
		c = iss
	}
	return pool
}

// parseCertsLoose reads a caIssuers payload: DER, PEM or a certs-only
// PKCS#7 (.p7c).
func parseCertsLoose(b []byte) []*x509.Certificate {
	if c, err := x509.ParseCertificate(b); err == nil {
		return []*x509.Certificate{c}
	}
	if certs, err := ParsePEM(b); err == nil && len(certs) > 0 {
		return certs
	}
	var ci contentInfo
	if unmarshalAll(b, &ci, "") == nil && ci.ContentType.Equal(oidSignedData) {
		var sd signedData
		if in, err := explicitInner(ci.Content); err == nil && unmarshalAll(in.FullBytes, &sd, "") == nil {
			certs, _ := parseCertBag(sd.Certificates)
			return certs
		}
	}
	return nil
}

// Collect fetches validation material for an accepted token: OCSP where the
// certificate names a responder, else (or if OCSP fails) the full CRL from
// its CDP. The result has passed ValidateMaterial; on error the returned
// checks explain each certificate.
func (f *Fetcher) Collect(ctx context.Context, tok *Token) (*Material, []CertCheck, error) {
	certs, issuers, err := needsEvidence(tok, tok.Certs)
	if err != nil {
		return nil, nil, err
	}
	m := &Material{Evidence: map[string]Evidence{}}
	m.Chain = append(m.Chain, tok.Chain...)
	for i, c := range certs {
		if !containsCert(m.Chain, issuers[i]) {
			m.Chain = append(m.Chain, issuers[i])
		}
		if ev, ok := f.fetchEvidence(ctx, c, issuers[i], tok.GenTime); ok {
			m.Evidence[CertKey(c)] = ev
		}
	}
	checks, err := ValidateMaterial(tok, m)
	if err != nil {
		return nil, checks, err
	}
	return m, checks, nil
}

// fetchEvidence returns the first authentic evidence. Authentic evidence
// that is not OK (revoked, stale) is returned as well, so the caller sees
// the real status; inauthentic responses are skipped.
func (f *Fetcher) fetchEvidence(ctx context.Context, c, issuer *x509.Certificate, genTime time.Time) (Evidence, bool) {
	var fallback *Evidence
	try := func(kind EvidenceKind, raw []byte) bool {
		rev, err := CheckRevocation(kind, raw, c, issuer, genTime)
		if err != nil {
			return false
		}
		ev := Evidence{Kind: kind, Raw: raw}
		if rev.OK() || rev.Status == StatusRevoked {
			fallback = &ev
			return true
		}
		if fallback == nil {
			fallback = &ev
		}
		return false
	}
	if len(c.OCSPServer) > 0 {
		// SHA-1 certID: the RFC 5019 profile many responders insist on. It only
		// identifies; the response signature authenticates.
		req, err := ocsp.CreateRequest(c, issuer, nil)
		if err == nil {
			for _, url := range c.OCSPServer {
				if raw, err := f.do(ctx, http.MethodPost, url, "application/ocsp-request", req); err == nil && try(KindOCSP, raw) {
					return *fallback, true
				}
			}
		}
	}
	for _, url := range c.CRLDistributionPoints {
		if raw, err := f.do(ctx, http.MethodGet, url, "", nil); err == nil && try(KindCRL, raw) {
			return *fallback, true
		}
	}
	if fallback != nil {
		return *fallback, true
	}
	return Evidence{}, false
}

func containsCert(list []*x509.Certificate, c *x509.Certificate) bool {
	for _, x := range list {
		if x.Equal(c) {
			return true
		}
	}
	return false
}
