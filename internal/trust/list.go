// Package trust fetches, verifies, caches and queries the EU trusted lists
// (ETSI TS 119 612): the EU list of trusted lists (LOTL) and the national
// trusted lists it points to. It answers one question (spec §6 step 6):
// was a timestamp's signer a qualified timestamp service at genTime?
package trust

import (
	"crypto/x509"
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
	Services   []Service // QTST services only (national lists)
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

// Service is a QTST service entry of a national trusted list.
type Service struct {
	TSPName string
	Name    string
	Certs   []*x509.Certificate // X509Certificate values of the ServiceDigitalIdentity
	History []StatusInterval    // ascending, contiguous; the last one is open-ended
}

// StatusInterval is a status that held from Start (inclusive) to End
// (exclusive). A zero End means the status is still current.
type StatusInterval struct {
	Status string
	Start  time.Time
	End    time.Time
}

func (iv StatusInterval) contains(t time.Time) bool {
	return !t.Before(iv.Start) && (iv.End.IsZero() || t.Before(iv.End))
}

// statusAt returns the status URI that held at t, or "" if none did.
func (s Service) statusAt(t time.Time) string {
	for _, iv := range s.History {
		if iv.contains(t) {
			return iv.Status
		}
	}
	return ""
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
			if strings.TrimSpace(xs.Info.Type) != ServiceTypeQTST {
				continue
			}
			s := Service{TSPName: pickName(tsp.Names), Name: pickName(xs.Info.Names)}
			var err error
			if s.Certs, err = parseCerts(xs.Info.Certs); err != nil {
				return nil, fmt.Errorf("service %q: %w", s.Name, err)
			}
			s.History, err = buildHistory(append([]xmlServiceInfo{xs.Info}, xs.History...))
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

// buildHistory turns the current status and the ServiceHistory instances into
// contiguous intervals: each status holds until the next StatusStartingTime.
func buildHistory(entries []xmlServiceInfo) ([]StatusInterval, error) {
	ivs := make([]StatusInterval, 0, len(entries))
	for _, e := range entries {
		start, err := parseTime(e.Start)
		if err != nil {
			return nil, fmt.Errorf("StatusStartingTime: %w", err)
		}
		ivs = append(ivs, StatusInterval{Status: strings.TrimSpace(e.Status), Start: start})
	}
	slices.SortStableFunc(ivs, func(a, b StatusInterval) int { return a.Start.Compare(b.Start) })
	out := ivs[:0]
	for _, iv := range ivs {
		if n := len(out); n > 0 && out[n-1].Start.Equal(iv.Start) {
			// Real lists repeat an entry when only the name changed.
			if out[n-1].Status != iv.Status {
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

// errAmbiguousHistory marks a service with two different statuses starting at
// the same time. Such a service is skipped, so it can never decide.
var errAmbiguousHistory = errors.New("two different statuses with the same starting time")

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
