// Package trust fetches, verifies, caches and queries the EU trusted lists
// (ETSI TS 119 612): the EU list of trusted lists (LOTL) and the national
// trusted lists it points to. It answers one question (spec §6 step 6):
// was a timestamp's signer a qualified timestamp service at genTime?
package trust

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// URIs of ETSI TS 119 612 used in the EU trusted lists.
const (
	StatusGranted    = "http://uri.etsi.org/TrstSvc/TrustedList/Svcstatus/granted"
	StatusWithdrawn  = "http://uri.etsi.org/TrstSvc/TrustedList/Svcstatus/withdrawn"
	StatusAccredited = "http://uri.etsi.org/TrstSvc/TrustedList/Svcstatus/accredited"
	ServiceTypeQTST  = "http://uri.etsi.org/TrstSvc/Svctype/TSA/QTST"
)

const (
	typeLOTL          = "http://uri.etsi.org/TrstSvc/TrustedList/TSLType/EUlistofthelists"
	typeEUGeneric     = "http://uri.etsi.org/TrstSvc/TrustedList/TSLType/EUgeneric"
	mimeTSL           = "application/vnd.etsi.tsl+xml"
	nsTSL             = "http://uri.etsi.org/02231/v2#"
	territoryLOTL     = "EU"
	maxCertsPerEntity = 64
)

// List is a signature-verified trusted list (the LOTL or a national list).
type List struct {
	Territory  string    // SchemeTerritory: "EU" for the LOTL, else the country code
	Sequence   int       // TSLSequenceNumber
	IssueDate  time.Time // ListIssueDateTime, covered by the list's signature
	NextUpdate time.Time // zero if the list is closed
	Services   []Service // services that were QTST at some time (national lists)
	Raw        []byte    // the exact signed bytes

	pointers []pointer // LOTL only

	// fetchedOnline is set only when this process downloaded and verified the
	// list itself. It is never persisted: cached metadata is never evidence.
	fetchedOnline time.Time
}

// FetchedOnline reports when this process downloaded and verified the list,
// and false for lists loaded from a cache or the bootstrap snapshot.
func (l *List) FetchedOnline() (time.Time, bool) {
	return l.fetchedOnline, !l.fetchedOnline.IsZero()
}

// Service is a service entry of a national trusted list whose current
// information or history names the type QTST at least once.
type Service struct {
	TSPName string
	Name    string
	Certs   []*x509.Certificate // X509Certificate values of the current ServiceDigitalIdentity
	History []StatusInterval    // ascending, contiguous; the last one is open-ended
}

// StatusInterval is what the list states about a service from Start
// (inclusive) to End (exclusive): its status, its service type and its
// ServiceDigitalIdentity. A zero End means the entry is still current.
// ServiceHistoryInstance entries carry their own type and identity: a service
// that is QTST now may have been an ordinary TSA, or have had other
// certificates, before.
type StatusInterval struct {
	Status string
	Type   string // ServiceTypeIdentifier
	Start  time.Time
	End    time.Time
	// The identity: parsed X509Certificate values or, if the entry has none
	// (history instances usually carry only X509SKI), the X509SKI values.
	Certs []*x509.Certificate
	SKIs  [][]byte

	byCert bool // the entry has X509Certificate elements (parseable or not)
}

func (iv StatusInterval) contains(t time.Time) bool {
	return !t.Before(iv.Start) && (iv.End.IsZero() || t.Before(iv.End))
}

// at returns the entry that held at t.
func (s Service) at(t time.Time) (StatusInterval, bool) {
	for _, iv := range s.History {
		if iv.contains(t) {
			return iv, true
		}
	}
	return StatusInterval{}, false
}

// statusAt returns the status URI that held at t, or "" if none did.
func (s Service) statusAt(t time.Time) string {
	iv, _ := s.at(t)
	return iv.Status
}

// qualificationAt applies spec §6 step 6 to the history entry for t: Qualified
// if it has the type QTST, the status granted and an identity in path. All
// three come from the same entry. An otherwise qualifying entry that states
// no identity at all (an empty ServiceDigitalIdentity, seen in the FR list)
// cannot decide: Unconfirmed. Else NotQualified.
func (s Service) qualificationAt(path []*x509.Certificate, t time.Time) Result {
	iv, ok := s.at(t)
	switch {
	case !ok || iv.Type != ServiceTypeQTST || iv.Status != StatusGranted:
		return NotQualified
	case !iv.byCert && len(iv.SKIs) == 0:
		return Unconfirmed
	case iv.identifies(path):
		return Qualified
	}
	return NotQualified
}

// lists reports whether any entry of the service has an identity in path.
func (s Service) lists(path []*x509.Certificate) bool {
	for _, iv := range s.History {
		if iv.identifies(path) {
			return true
		}
	}
	return false
}

// identifies reports whether a certificate of path has the entry's identity.
//
// With certificates, identity is subject plus public key, so a re-issued CA
// certificate with the same key still matches (ETSI TS 119 615). Without
// certificates, an X509SKI matches a certificate whose subjectKeyIdentifier
// extension, or whose key hashed per RFC 5280 / RFC 7093 method 1, equals it.
// An entry whose certificates Go cannot parse never matches (fail closed).
func (iv StatusInterval) identifies(path []*x509.Certificate) bool {
	for _, c := range path {
		if iv.byCert {
			for _, sdi := range iv.Certs {
				if bytes.Equal(c.RawSubject, sdi.RawSubject) &&
					bytes.Equal(c.RawSubjectPublicKeyInfo, sdi.RawSubjectPublicKeyInfo) {
					return true
				}
			}
			continue
		}
		ids := keyIDs(c)
		for _, ski := range iv.SKIs {
			for _, id := range ids {
				if bytes.Equal(id, ski) {
					return true
				}
			}
		}
	}
	return false
}

// keyIDs returns the key identifiers a certificate may be listed under: its
// subjectKeyIdentifier extension and the SHA-1 (RFC 5280 §4.2.1.2) and the
// truncated SHA-256 (RFC 7093 §2 method 1) of its subjectPublicKey.
func keyIDs(c *x509.Certificate) [][]byte {
	var ids [][]byte
	if len(c.SubjectKeyId) > 0 {
		ids = append(ids, c.SubjectKeyId)
	}
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if rest, err := asn1.Unmarshal(c.RawSubjectPublicKeyInfo, &spki); err == nil && len(rest) == 0 {
		h1 := sha1.Sum(spki.PublicKey.Bytes)
		h256 := sha256.Sum256(spki.PublicKey.Bytes)
		ids = append(ids, h1[:], h256[:20])
	}
	return ids
}

// pointer is an OtherTSLPointer of the LOTL to a machine-readable list.
type pointer struct {
	Territory string
	Location  string
	Certs     []*x509.Certificate
}

// pointer returns the LOTL's pointer to the XML trusted list of territory.
func (l *List) pointer(territory string) (pointer, bool) {
	return findPointer(l.pointers, territory)
}

// XML shape of a trusted list (local names; the root namespace is checked).
type xmlTSL struct {
	XMLName xml.Name `xml:"TrustServiceStatusList"`
	Scheme  struct {
		Sequence   string       `xml:"TSLSequenceNumber"`
		Type       string       `xml:"TSLType"`
		Territory  string       `xml:"SchemeTerritory"`
		Pointers   []xmlPointer `xml:"PointersToOtherTSL>OtherTSLPointer"`
		IssueDate  string       `xml:"ListIssueDateTime"`
		NextUpdate string       `xml:"NextUpdate>dateTime"`
	} `xml:"SchemeInformation"`
	TSPs []struct {
		Names    []xmlName `xml:"TSPInformation>TSPName>Name"`
		Services []struct {
			Info    xmlServiceInfo   `xml:"ServiceInformation"`
			History []xmlServiceInfo `xml:"ServiceHistory>ServiceHistoryInstance"`
		} `xml:"TSPServices>TSPService"`
	} `xml:"TrustServiceProviderList>TrustServiceProvider"`
}

type xmlPointer struct {
	Certs    []string `xml:"ServiceDigitalIdentities>ServiceDigitalIdentity>DigitalId>X509Certificate"`
	Location string   `xml:"TSLLocation"`
	Info     []struct {
		TSLType   string `xml:"TSLType"`
		Territory string `xml:"SchemeTerritory"`
		MimeType  string `xml:"MimeType"`
	} `xml:"AdditionalInformation>OtherInformation"`
}

type xmlServiceInfo struct {
	Type   string    `xml:"ServiceTypeIdentifier"`
	Names  []xmlName `xml:"ServiceName>Name"`
	Certs  []string  `xml:"ServiceDigitalIdentity>DigitalId>X509Certificate"`
	SKIs   []string  `xml:"ServiceDigitalIdentity>DigitalId>X509SKI"`
	Status string    `xml:"ServiceStatus"`
	Start  string    `xml:"StatusStartingTime"`
}

type xmlName struct {
	Lang  string `xml:"lang,attr"`
	Value string `xml:",chardata"`
}

// decodeList builds a List from the verified, canonical XML of a list.
func decodeList(verified []byte) (*List, error) {
	var x xmlTSL
	if err := xml.Unmarshal(verified, &x); err != nil {
		return nil, fmt.Errorf("parse trusted list: %w", err)
	}
	if x.XMLName.Space != nsTSL {
		return nil, fmt.Errorf("unexpected root namespace %q", x.XMLName.Space)
	}
	l := &List{Territory: strings.TrimSpace(x.Scheme.Territory)}
	var err error
	if l.Sequence, err = parseSequence(x.Scheme.Sequence); err != nil {
		return nil, err
	}
	if l.IssueDate, err = parseTime(x.Scheme.IssueDate); err != nil {
		return nil, fmt.Errorf("ListIssueDateTime: %w", err)
	}
	if s := strings.TrimSpace(x.Scheme.NextUpdate); s != "" {
		if l.NextUpdate, err = parseTime(s); err != nil {
			return nil, fmt.Errorf("NextUpdate: %w", err)
		}
	}
	switch strings.TrimSpace(x.Scheme.Type) {
	case typeLOTL:
		if l.Territory != territoryLOTL {
			return nil, fmt.Errorf("LOTL with territory %q", l.Territory)
		}
		if l.pointers, err = decodePointers(x.Scheme.Pointers); err != nil {
			return nil, err
		}
	case typeEUGeneric:
		if l.Territory == territoryLOTL || l.Territory == "" {
			return nil, fmt.Errorf("national list with territory %q", l.Territory)
		}
		if l.Services, err = decodeServices(&x); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported TSLType %q", x.Scheme.Type)
	}
	return l, nil
}

func decodePointers(xs []xmlPointer) ([]pointer, error) {
	var out []pointer
	for _, xp := range xs {
		var p pointer
		var isXML bool
		for _, info := range xp.Info {
			if t := strings.TrimSpace(info.Territory); t != "" {
				p.Territory = t
			}
			if strings.TrimSpace(info.MimeType) == mimeTSL {
				isXML = true
			}
		}
		if !isXML || p.Territory == "" {
			continue
		}
		p.Location = strings.TrimSpace(xp.Location)
		certs, err := parseCerts(xp.Certs)
		if err != nil {
			return nil, fmt.Errorf("pointer %s: %w", p.Territory, err)
		}
		p.Certs = certs
		if _, dup := findPointer(out, p.Territory); !dup {
			out = append(out, p)
		}
	}
	return out, nil
}

func findPointer(ps []pointer, territory string) (pointer, bool) {
	for _, p := range ps {
		if p.Territory == territory {
			return p, true
		}
	}
	return pointer{}, false
}

func decodeServices(x *xmlTSL) ([]Service, error) {
	var out []Service
	for _, tsp := range x.TSPs {
		for _, xs := range tsp.Services {
			entries := append([]xmlServiceInfo{xs.Info}, xs.History...)
			if !slices.ContainsFunc(entries, func(e xmlServiceInfo) bool { return strings.TrimSpace(e.Type) == ServiceTypeQTST }) {
				continue
			}
			s := Service{TSPName: pickName(tsp.Names), Name: pickName(xs.Info.Names)}
			var err error
			if s.Certs, err = parseCerts(xs.Info.Certs); err != nil {
				return nil, fmt.Errorf("service %q: %w", s.Name, err)
			}
			s.History, err = buildHistory(entries)
			if errors.Is(err, errAmbiguousHistory) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("service %q: %w", s.Name, err)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// buildHistory turns the current information (entries[0]) and the
// ServiceHistory instances into contiguous intervals: each entry, with its
// status, type and identity, holds until the next StatusStartingTime.
func buildHistory(entries []xmlServiceInfo) ([]StatusInterval, error) {
	ivs := make([]StatusInterval, 0, len(entries))
	for _, e := range entries {
		start, err := parseTime(e.Start)
		if err != nil {
			return nil, fmt.Errorf("StatusStartingTime: %w", err)
		}
		iv := StatusInterval{Status: strings.TrimSpace(e.Status), Type: strings.TrimSpace(e.Type), Start: start}
		iv.Certs, iv.SKIs, iv.byCert = parseIdentity(e)
		ivs = append(ivs, iv)
	}
	// Stable: on equal starting times the current information stays first.
	slices.SortStableFunc(ivs, func(a, b StatusInterval) int { return a.Start.Compare(b.Start) })
	out := ivs[:0]
	for _, iv := range ivs {
		if n := len(out); n > 0 && out[n-1].Start.Equal(iv.Start) {
			// Real lists repeat an entry when only the name changed. The
			// first one is kept: the current information if it is among them.
			if out[n-1].Status != iv.Status || out[n-1].Type != iv.Type {
				return nil, errAmbiguousHistory
			}
			continue
		}
		out = append(out, iv)
	}
	for i := 1; i < len(out); i++ {
		out[i-1].End = out[i].Start
	}
	return out, nil
}

// errAmbiguousHistory marks a service with two different statuses or types
// starting at the same time. Such a service is skipped, so it can never
// decide.
var errAmbiguousHistory = errors.New("two different statuses or types with the same starting time")

// parseIdentity decodes the ServiceDigitalIdentity of one entry. Unlike the
// current certificates (parseCerts), bad values never reject the list here:
// they just cannot match.
func parseIdentity(e xmlServiceInfo) (certs []*x509.Certificate, skis [][]byte, byCert bool) {
	if len(e.Certs) > 0 {
		certs, _ = parseCerts(e.Certs)
		return certs, nil, true
	}
	for i, v := range e.SKIs {
		if i == maxCertsPerEntity {
			break
		}
		if b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(v), "")); err == nil && len(b) > 0 {
			skis = append(skis, b)
		}
	}
	return nil, skis, false
}

func pickName(names []xmlName) string {
	for _, n := range names {
		if n.Lang == "en" {
			return strings.TrimSpace(n.Value)
		}
	}
	if len(names) > 0 {
		return strings.TrimSpace(names[0].Value)
	}
	return ""
}

// parseCerts decodes X509Certificate values. Certificates that Go's x509
// cannot parse (some lists carry encodings it rejects) are skipped: such an
// identity can never match, which fails closed.
func parseCerts(b64s []string) ([]*x509.Certificate, error) {
	if len(b64s) > maxCertsPerEntity {
		return nil, errors.New("too many certificates")
	}
	certs := make([]*x509.Certificate, 0, len(b64s))
	for _, s := range b64s {
		der, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s), ""))
		if err != nil {
			return nil, fmt.Errorf("certificate base64: %w", err)
		}
		if c, err := x509.ParseCertificate(der); err == nil {
			certs = append(certs, c)
		}
	}
	return certs, nil
}

func parseSequence(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid TSLSequenceNumber %q", s)
	}
	return n, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return t.UTC(), nil
}
