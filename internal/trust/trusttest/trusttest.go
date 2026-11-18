// Package trusttest publishes synthetic EU trusted lists for tests: a LOTL
// and one national list, both really XML-signed, served by an in-memory web.
// Production code never imports it. Use it with trust.OpenTest (build tag
// testtsa), which trusts the fixture's LOTL signer instead of the pins.
package trusttest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// URIs used in the lists.
const (
	QTST       = "http://uri.etsi.org/TrstSvc/Svctype/TSA/QTST"
	Granted    = "http://uri.etsi.org/TrstSvc/TrustedList/Svcstatus/granted"
	Withdrawn  = "http://uri.etsi.org/TrstSvc/TrustedList/Svcstatus/withdrawn"
	LOTLURL    = "https://ec.europa.eu/tools/lotl/eu-lotl.xml"
	lotlType   = "http://uri.etsi.org/TrstSvc/TrustedList/TSLType/EUlistofthelists"
	genericTyp = "http://uri.etsi.org/TrstSvc/TrustedList/TSLType/EUgeneric"
)

// Service is a QTST entry of the national list.
type Service struct {
	TSP     string
	Name    string
	Certs   []*x509.Certificate // ServiceDigitalIdentity
	History []Status            // any order; the newest is the current status
}

// Status is one status of a service, valid from Start.
type Status struct {
	URI   string
	Start time.Time
}

// Fixture is a LOTL signer, a national list signer and the web serving both.
type Fixture struct {
	Territory   string
	NationalURL string
	LOTLSigner  *x509.Certificate // trust it via trust.TestConfig.Pins
	Web         *Web

	lotlKey, natKey *rsa.PrivateKey
	natCert         *x509.Certificate
	mu              sync.Mutex
	seq             int
}

// New creates a fixture for territory (e.g. "ZZ"). Nothing is published yet.
func New(tb testing.TB, territory string) *Fixture {
	tb.Helper()
	f := &Fixture{
		Territory:   territory,
		NationalURL: "https://tl.test.invalid/" + territory + ".xml",
		Web:         &Web{files: map[string][]byte{}},
	}
	f.LOTLSigner, f.lotlKey = signer(tb, "Test LOTL Signer")
	f.natCert, f.natKey = signer(tb, "Test "+territory+" TL Signer")
	return f
}

func signer(tb testing.TB, cn string) (*x509.Certificate, *rsa.PrivateKey) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		tb.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"Rechnungsarchiv Test"}},
		NotBefore:    time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2045, 1, 1, 0, 0, 0, 0, time.UTC),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		tb.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		tb.Fatal(err)
	}
	return c, key
}

// Publish signs a new LOTL and a new national list issued at issue (next
// update a month later) with the given services and serves them. It returns
// the national list's sequence number.
func (f *Fixture) Publish(tb testing.TB, issue time.Time, services ...Service) int {
	tb.Helper()
	f.mu.Lock()
	f.seq++
	seq := f.seq
	f.mu.Unlock()
	next := issue.AddDate(0, 1, 0)
	nat := f.national(seq, issue, next, services)
	lotl := f.lotl(seq, issue, next)
	f.Web.Set(f.NationalURL, sign(tb, nat, f.natKey, f.natCert))
	f.Web.Set(LOTLURL, sign(tb, lotl, f.lotlKey, f.LOTLSigner))
	return seq
}

// Client returns an HTTP client that is served by the fixture's web.
func (f *Fixture) Client() *http.Client { return &http.Client{Transport: f.Web} }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func b64(c *x509.Certificate) string { return base64.StdEncoding.EncodeToString(c.Raw) }

func header(seq int, typ, territory string, issue, next time.Time) string {
	return fmt.Sprintf(`<TSLVersionIdentifier>6</TSLVersionIdentifier>`+
		`<TSLSequenceNumber>%d</TSLSequenceNumber><TSLType>%s</TSLType>`+
		`<SchemeTerritory>%s</SchemeTerritory>`, seq, typ, territory) +
		"%s" + // pointers (LOTL only)
		fmt.Sprintf(`<ListIssueDateTime>%s</ListIssueDateTime><NextUpdate><dateTime>%s</dateTime></NextUpdate>`, ts(issue), ts(next))
}

func (f *Fixture) lotl(seq int, issue, next time.Time) string {
	ptr := `<PointersToOtherTSL><OtherTSLPointer><ServiceDigitalIdentities><ServiceDigitalIdentity><DigitalId>` +
		`<X509Certificate>` + b64(f.natCert) + `</X509Certificate></DigitalId></ServiceDigitalIdentity></ServiceDigitalIdentities>` +
		`<TSLLocation>` + f.NationalURL + `</TSLLocation><AdditionalInformation>` +
		`<OtherInformation><TSLType>` + genericTyp + `</TSLType></OtherInformation>` +
		`<OtherInformation><SchemeTerritory>` + f.Territory + `</SchemeTerritory></OtherInformation>` +
		`<OtherInformation><MimeType>application/vnd.etsi.tsl+xml</MimeType></OtherInformation>` +
		`</AdditionalInformation></OtherTSLPointer></PointersToOtherTSL>`
	return doc(fmt.Sprintf(header(seq, lotlType, "EU", issue, next), ptr), "")
}

func (f *Fixture) national(seq int, issue, next time.Time, services []Service) string {
	var b strings.Builder
	b.WriteString(`<TrustServiceProviderList>`)
	for _, s := range services {
		if len(s.History) == 0 {
			panic("trusttest: service without status")
		}
		hist := append([]Status(nil), s.History...)
		cur := 0
		for i, h := range hist {
			if h.Start.After(hist[cur].Start) {
				cur = i
			}
		}
		info := func(tag string, st Status) string {
			var certs string
			for _, c := range s.Certs {
				certs += `<DigitalId><X509Certificate>` + b64(c) + `</X509Certificate></DigitalId>`
			}
			return `<` + tag + `><ServiceTypeIdentifier>` + QTST + `</ServiceTypeIdentifier>` +
				`<ServiceName><Name xml:lang="en">` + html.EscapeString(s.Name) + `</Name></ServiceName>` +
				`<ServiceDigitalIdentity>` + certs + `</ServiceDigitalIdentity>` +
				`<ServiceStatus>` + st.URI + `</ServiceStatus><StatusStartingTime>` + ts(st.Start) + `</StatusStartingTime></` + tag + `>`
		}
		b.WriteString(`<TrustServiceProvider><TSPInformation><TSPName><Name xml:lang="en">` + html.EscapeString(s.TSP) +
			`</Name></TSPName></TSPInformation><TSPServices><TSPService>`)
		b.WriteString(info("ServiceInformation", hist[cur]))
		if len(hist) > 1 {
			b.WriteString(`<ServiceHistory>`)
			for i, h := range hist {
				if i != cur {
					b.WriteString(info("ServiceHistoryInstance", h))
				}
			}
			b.WriteString(`</ServiceHistory>`)
		}
		b.WriteString(`</TSPService></TSPServices></TrustServiceProvider>`)
	}
	b.WriteString(`</TrustServiceProviderList>`)
	return doc(fmt.Sprintf(header(seq, genericTyp, f.Territory, issue, next), ""), b.String())
}

func doc(scheme, body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
		`<TrustServiceStatusList xmlns="http://uri.etsi.org/02231/v2#" TSLTag="http://uri.etsi.org/19612/TSLTag">` +
		`<SchemeInformation>` + scheme + `</SchemeInformation>` + body + `</TrustServiceStatusList>`
}

func sign(tb testing.TB, xml string, key *rsa.PrivateKey, cert *x509.Certificate) []byte {
	tb.Helper()
	d := etree.NewDocument()
	if err := d.ReadFromString(xml); err != nil {
		tb.Fatal(err)
	}
	ctx, err := dsig.NewSigningContext(key, [][]byte{cert.Raw})
	if err != nil {
		tb.Fatal(err)
	}
	signed, err := ctx.SignEnveloped(d.Root())
	if err != nil {
		tb.Fatal(err)
	}
	d.SetRoot(signed)
	out, err := d.WriteToBytes()
	if err != nil {
		tb.Fatal(err)
	}
	return out
}

// Web serves fixed bytes by URL. Offline makes every request fail like a
// missing network.
type Web struct {
	mu      sync.Mutex
	files   map[string][]byte
	offline bool
	hits    int
}

// Set serves data at url.
func (w *Web) Set(url string, data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.files[url] = data
}

// SetOffline switches the simulated network off or on.
func (w *Web) SetOffline(off bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.offline = off
}

// Hits returns the number of requests served.
func (w *Web) Hits() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hits
}

// RoundTrip implements http.RoundTripper.
func (w *Web) RoundTrip(r *http.Request) (*http.Response, error) {
	w.mu.Lock()
	body, ok := w.files[r.URL.String()]
	off := w.offline
	w.hits++
	w.mu.Unlock()
	if off {
		return nil, errors.New("trusttest: offline")
	}
	status := http.StatusOK
	if !ok {
		status, body = http.StatusNotFound, []byte("not found")
	}
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}
