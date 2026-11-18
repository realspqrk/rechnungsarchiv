package tsa

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// Reason is an Ausstehend reason from spec §5 that this package produces.
type Reason string

const (
	ReasonOffline    Reason = "offline"
	ReasonZugang     Reason = "zugang"
	ReasonGuthaben   Reason = "guthaben"
	ReasonPruefdaten Reason = "prüfdaten"
	ReasonAnbieter   Reason = "anbieter"
)

// Error is a failed stamping step with its spec §5 reason.
type Error struct {
	Reason Reason
	Msg    string // short German text
	Err    error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return e.Msg + ": " + e.Err.Error()
	}
	return e.Msg
}

func (e *Error) Unwrap() error { return e.Err }

// Auth types in providers.json.
const (
	AuthBasic      = "basic"
	AuthClientCert = "clientcert"
)

// Credentials are read from the OS credential store by the caller.
type Credentials struct {
	User, Password string           // AuthBasic
	ClientCert     *tls.Certificate // AuthClientCert, see LoadClientCert
}

// LoadClientCert decodes a PKCS#12 file into a TLS client certificate.
func LoadClientCert(p12 []byte, password string) (*tls.Certificate, error) {
	key, cert, chain, err := pkcs12.DecodeChain(p12, password)
	if err != nil {
		return nil, &Error{Reason: ReasonZugang, Msg: "Zertifikatsdatei oder Passwort ungültig", Err: err}
	}
	tc := &tls.Certificate{PrivateKey: key, Leaf: cert, Certificate: [][]byte{cert.Raw}}
	for _, c := range chain {
		tc.Certificate = append(tc.Certificate, c.Raw)
	}
	return tc, nil
}

// Client sends requests to one TSA.
type Client struct {
	URL     string
	Auth    string // AuthBasic or AuthClientCert
	Creds   Credentials
	Timeout time.Duration  // whole exchange; default 60 s
	RootCAs *x509.CertPool // nil: system roots

	allowHTTP bool // tests and the online interop test only
}

const maxResponse = 1 << 20

// Send POSTs the request and returns the raw TimeStampResp. It does not
// verify the token (see Fetcher.Accept). On a provider rejection the raw
// body is returned together with the *Error.
func (c *Client) Send(ctx context.Context, req *Request) ([]byte, error) {
	if !strings.HasPrefix(c.URL, "https://") && !(c.allowHTTP && strings.HasPrefix(c.URL, "http://")) {
		return nil, &Error{Reason: ReasonAnbieter, Msg: "Anbieter-Adresse ist nicht HTTPS"}
	}
	tlsCfg := &tls.Config{RootCAs: c.RootCAs, MinVersion: tls.VersionTLS12}
	switch c.Auth {
	case AuthBasic:
		if c.Creds.User == "" {
			return nil, &Error{Reason: ReasonZugang, Msg: "Keine Zugangsdaten auf diesem PC"}
		}
	case AuthClientCert:
		if c.Creds.ClientCert == nil {
			return nil, &Error{Reason: ReasonZugang, Msg: "Kein Zugangszertifikat auf diesem PC"}
		}
		tlsCfg.Certificates = []tls.Certificate{*c.Creds.ClientCert}
	case "":
	default:
		return nil, fmt.Errorf("tsa: unknown auth type %q", c.Auth)
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	hc := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			TLSClientConfig:       tlsCfg,
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 45 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer hc.CloseIdleConnections()

	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(req.DER))
	if err != nil {
		return nil, &Error{Reason: ReasonAnbieter, Msg: "Anbieter-Adresse ungültig", Err: err}
	}
	hr.Header.Set("Content-Type", "application/timestamp-query")
	hr.Header.Set("Accept", "application/timestamp-reply")
	if c.Auth == AuthBasic {
		hr.SetBasicAuth(c.Creds.User, c.Creds.Password)
	}
	resp, err := hc.Do(hr)
	if err != nil {
		return nil, transportError(err, c.Auth == AuthClientCert)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, &Error{Reason: ReasonOffline, Msg: "Verbindung abgebrochen", Err: err}
	}
	if len(body) > maxResponse {
		return nil, &Error{Reason: ReasonAnbieter, Msg: "Antwort zu groß"}
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		if quotaText(string(body)) {
			return nil, &Error{Reason: ReasonGuthaben, Msg: "Guthaben beim Anbieter aufgebraucht", Err: httpErr(resp, body)}
		}
		return nil, &Error{Reason: ReasonZugang, Msg: "Zugang abgelehnt", Err: httpErr(resp, body)}
	case resp.StatusCode == http.StatusPaymentRequired:
		return nil, &Error{Reason: ReasonGuthaben, Msg: "Guthaben beim Anbieter aufgebraucht", Err: httpErr(resp, body)}
	case resp.StatusCode != http.StatusOK:
		if quotaText(string(body)) {
			return nil, &Error{Reason: ReasonGuthaben, Msg: "Guthaben beim Anbieter aufgebraucht", Err: httpErr(resp, body)}
		}
		return nil, &Error{Reason: ReasonAnbieter, Msg: "Anbieter antwortet mit Fehler", Err: httpErr(resp, body)}
	}
	if _, err := ParseResponse(body); err != nil {
		var se *StatusError
		if errors.As(err, &se) {
			r, msg := ReasonAnbieter, "Anbieter hat die Anfrage abgelehnt"
			if quotaText(strings.Join(se.Text, " ")) {
				r, msg = ReasonGuthaben, "Guthaben beim Anbieter aufgebraucht"
			}
			return body, &Error{Reason: r, Msg: msg, Err: err}
		}
		if quotaText(string(body)) {
			return nil, &Error{Reason: ReasonGuthaben, Msg: "Guthaben beim Anbieter aufgebraucht", Err: httpErr(resp, body)}
		}
		return nil, &Error{Reason: ReasonAnbieter, Msg: "Antwort ist kein Zeitstempel", Err: err}
	}
	return body, nil
}

func httpErr(resp *http.Response, body []byte) error {
	s := string(body)
	if len(s) > 200 {
		s = s[:200]
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(s))
}

// quotaText is the heuristic for "quota exhausted" (reason guthaben). No
// provider documents a machine-readable signal; we match words providers
// use in error texts (EN/DE/ES/IT/SK). Assumption, see providers.json notes.
func quotaText(s string) bool {
	s = strings.ToLower(s)
	for _, w := range []string{
		"credit", "quota", "balance", "insufficient", "exhausted", "no stamps", "out of stamps",
		"guthaben", "kontingent",
		"crédito", "credito", "saldo", "sin sellos",
		"esaurit",
		"kredit", "vyčerpan",
	} {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// transportError maps a failed HTTP exchange: rejected client certificates
// are zugang, server certificate problems anbieter, everything else offline.
func transportError(err error, clientCert bool) error {
	// Over TCP, crypto/tls returns remote alerts as an unexported type
	// (tls.AlertError is QUIC only), so match the text.
	if msg := err.Error(); strings.Contains(msg, "remote error: tls: ") {
		for _, a := range []string{"bad certificate", "unsupported certificate", "certificate revoked",
			"certificate expired", "certificate unknown", "unknown certificate authority",
			"access denied", "certificate required"} {
			if clientCert && strings.Contains(msg, a) {
				return &Error{Reason: ReasonZugang, Msg: "Zugangszertifikat abgelehnt", Err: err}
			}
		}
		return &Error{Reason: ReasonAnbieter, Msg: "TLS-Fehler beim Anbieter", Err: err}
	}
	var ua x509.UnknownAuthorityError
	var ci x509.CertificateInvalidError
	var hn x509.HostnameError
	var ve *tls.CertificateVerificationError
	if errors.As(err, &ua) || errors.As(err, &ci) || errors.As(err, &hn) || errors.As(err, &ve) {
		return &Error{Reason: ReasonAnbieter, Msg: "Server-Zertifikat des Anbieters ungültig", Err: err}
	}
	return &Error{Reason: ReasonOffline, Msg: "Anbieter nicht erreichbar", Err: err}
}
