package trust

import (
	"bytes"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures are real lists downloaded on 2026-11-18 (see testdata/trust).
const (
	fixtureLOTL = "EU-398.xml"
	fixtureAT   = "AT-75.xml"
	fixtureSK   = "SK-146.xml"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "trust", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixtureLOTLList(t *testing.T) *List {
	t.Helper()
	l, err := verifyLOTL(readFixture(t, fixtureLOTL), lotlPins)
	if err != nil {
		t.Fatalf("verify LOTL: %v", err)
	}
	return l
}

func fixtureNational(t *testing.T, name, territory string) *List {
	t.Helper()
	l, err := verifyNational(readFixture(t, name), fixtureLOTLList(t), territory)
	if err != nil {
		t.Fatalf("verify %s: %v", name, err)
	}
	return l
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVerifyLOTLAgainstPins(t *testing.T) {
	l := fixtureLOTLList(t)
	if l.Territory != "EU" || l.Sequence != 398 {
		t.Fatalf("got %s-%d, want EU-398", l.Territory, l.Sequence)
	}
	if want := mustTime(t, "2026-11-09T13:00:00Z"); !l.IssueDate.Equal(want) {
		t.Errorf("IssueDate = %v, want %v", l.IssueDate, want)
	}
	if want := mustTime(t, "2027-05-02T12:00:00Z"); !l.NextUpdate.Equal(want) {
		t.Errorf("NextUpdate = %v, want %v", l.NextUpdate, want)
	}
	for _, cc := range []string{"AT", "ES", "SK", "IT", "FR"} {
		p, ok := l.pointer(cc)
		if !ok {
			t.Errorf("no XML pointer for %s", cc)
			continue
		}
		if len(p.Certs) == 0 || !strings.HasPrefix(p.Location, "http") {
			t.Errorf("%s pointer incomplete: %+v", cc, p)
		}
	}
	if p, _ := l.pointer("AT"); p.Location != "https://www.signatur.rtr.at/vertrauensliste.xml" {
		t.Errorf("AT location = %q", p.Location)
	}
	if !bytes.Equal(l.Raw, readFixture(t, fixtureLOTL)) {
		t.Error("Raw must be the exact signed bytes")
	}
}

func TestVerifyNationalAgainstLOTLPointer(t *testing.T) {
	at := fixtureNational(t, fixtureAT, "AT")
	if at.Territory != "AT" || at.Sequence != 75 {
		t.Fatalf("got %s-%d, want AT-75", at.Territory, at.Sequence)
	}
	if want := mustTime(t, "2026-06-15T14:44:12Z"); !at.IssueDate.Equal(want) {
		t.Errorf("IssueDate = %v, want %v", at.IssueDate, want)
	}
	sk := fixtureNational(t, fixtureSK, "SK")
	if sk.Sequence != 146 {
		t.Errorf("SK sequence = %d", sk.Sequence)
	}
}

func TestVerifyRejectsTamperedByte(t *testing.T) {
	raw := readFixture(t, fixtureAT)
	i := bytes.Index(raw, []byte("GLOBALTRUST QUALIFIED TIMESTAMP 1"))
	if i < 0 {
		t.Fatal("marker not found")
	}
	tampered := bytes.Clone(raw)
	tampered[i] = 'X'
	if _, err := verifyNational(tampered, fixtureLOTLList(t), "AT"); err == nil {
		t.Fatal("tampered list verified")
	}

	lotl := bytes.Clone(readFixture(t, fixtureLOTL))
	j := bytes.Index(lotl, []byte("https://www.signatur.rtr.at/vertrauensliste.xml"))
	copy(lotl[j:], "https://www.signatur.rtr.ax")
	if _, err := verifyLOTL(lotl, lotlPins); err == nil {
		t.Fatal("tampered LOTL verified")
	}
}

func TestVerifyRejectsTamperedIssueDate(t *testing.T) {
	raw := bytes.Clone(readFixture(t, fixtureAT))
	old := []byte("<ListIssueDateTime>2026-06-15T14:44:12Z")
	i := bytes.Index(raw, old)
	if i < 0 {
		t.Fatal("marker not found")
	}
	copy(raw[i:], "<ListIssueDateTime>2026-12-15T14:44:12Z")
	if _, err := verifyNational(raw, fixtureLOTLList(t), "AT"); err == nil {
		t.Fatal("list with moved issue date verified")
	}
}

func TestVerifyRejectsWrongSigner(t *testing.T) {
	lotl := fixtureLOTLList(t)
	// AT list checked against the SK signing certificates.
	if _, err := verifyNational(readFixture(t, fixtureAT), lotl, "SK"); err == nil {
		t.Fatal("AT list accepted with SK signer certificates")
	}
	// LOTL checked against national signer certificates instead of the pins.
	p, _ := lotl.pointer("AT")
	if _, err := verifyLOTL(readFixture(t, fixtureLOTL), p.Certs); err == nil {
		t.Fatal("LOTL accepted with unpinned certificates")
	}
	if _, err := verifyLOTL(readFixture(t, fixtureLOTL), nil); err == nil {
		t.Fatal("LOTL accepted without pins")
	}
	// A national list is never accepted as the LOTL, even when its signer were pinned.
	if _, err := verifyLOTL(readFixture(t, fixtureAT), p.Certs); err == nil {
		t.Fatal("national list accepted as LOTL")
	}
}

func TestVerifyRejectsMalformedInput(t *testing.T) {
	lotl := fixtureLOTLList(t)
	cases := map[string][]byte{
		"empty":     nil,
		"not xml":   []byte("hello"),
		"html":      []byte("<html><body>moved</body></html>"),
		"doctype":   []byte(`<?xml version="1.0"?><!DOCTYPE x [<!ENTITY a "aaaa">]><TrustServiceStatusList xmlns="http://uri.etsi.org/02231/v2#">&a;</TrustServiceStatusList>`),
		"unsigned":  []byte(`<TrustServiceStatusList xmlns="http://uri.etsi.org/02231/v2#"><SchemeInformation><TSLSequenceNumber>1</TSLSequenceNumber><SchemeTerritory>AT</SchemeTerritory><ListIssueDateTime>2030-01-01T00:00:00Z</ListIssueDateTime></SchemeInformation></TrustServiceStatusList>`),
		"too deep":  []byte(strings.Repeat("<a>", 5000) + strings.Repeat("</a>", 5000)),
		"truncated": readFixture(t, fixtureAT)[:100000],
	}
	for name, raw := range cases {
		if _, err := verifyNational(raw, lotl, "AT"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	big := make([]byte, maxListSize+1)
	if _, err := verifyNational(big, lotl, "AT"); err == nil {
		t.Error("oversized input accepted")
	}
}

func findService(t *testing.T, l *List, name string) Service {
	t.Helper()
	for _, s := range l.Services {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("%s: service %q not found", l.Territory, name)
	return Service{}
}

func TestParseQTSTServices(t *testing.T) {
	at := fixtureNational(t, fixtureAT, "AT")
	sk := fixtureNational(t, fixtureSK, "SK")
	for _, l := range []*List{at, sk} {
		if len(l.Services) == 0 {
			t.Fatalf("%s: no QTST services", l.Territory)
		}
		for _, s := range l.Services {
			if s.TSPName == "" || s.Name == "" || len(s.History) == 0 {
				t.Errorf("%s: incomplete service %+v", l.Territory, s)
			}
			for i, iv := range s.History {
				if i > 0 && !s.History[i-1].End.Equal(iv.Start) {
					t.Errorf("%s/%s: intervals not contiguous", l.Territory, s.Name)
				}
			}
			if last := s.History[len(s.History)-1]; !last.End.IsZero() {
				t.Errorf("%s/%s: current interval must be open", l.Territory, s.Name)
			}
		}
	}

	gt := findService(t, at, "GLOBALTRUST 2015 QUALIFIED TIMESTAMP 1")
	if !strings.Contains(gt.TSPName, "e-commerce monitoring") {
		t.Errorf("TSPName = %q", gt.TSPName)
	}
	if len(gt.Certs) != 1 || !strings.Contains(gt.Certs[0].Subject.CommonName, "GLOBALTRUST 2015 QUALIFIED TIMESTAMP 1") {
		t.Errorf("unexpected certs: %v", gt.Certs)
	}
	want := []StatusInterval{
		{Status: StatusAccredited, Start: mustTime(t, "2015-06-29T13:00:00Z"), End: mustTime(t, "2016-06-30T22:00:00Z")},
		{Status: StatusWithdrawn, Start: mustTime(t, "2016-06-30T22:00:00Z"), End: mustTime(t, "2018-09-18T22:00:00Z")},
		{Status: StatusGranted, Start: mustTime(t, "2018-09-18T22:00:00Z")},
	}
	if len(gt.History) != len(want) {
		t.Fatalf("history = %+v", gt.History)
	}
	for i := range want {
		g := gt.History[i]
		if g.Status != want[i].Status || !g.Start.Equal(want[i].Start) || !g.End.Equal(want[i].End) {
			t.Errorf("interval %d = %+v, want %+v", i, g, want[i])
		}
	}

	var aTrust, disig bool
	for _, s := range at.Services {
		aTrust = aTrust || strings.Contains(s.TSPName, "A-Trust")
	}
	for _, s := range sk.Services {
		if strings.Contains(s.TSPName, "Disig") && s.statusAt(time.Now()) == StatusGranted {
			disig = true
		}
	}
	if !aTrust {
		t.Error("no A-Trust QTST service in AT list")
	}
	if !disig {
		t.Error("no granted Disig QTST service in SK list")
	}
}

func TestBuildHistoryDuplicates(t *testing.T) {
	// Repeated entry with the same status (seen in the FR list): merged.
	h, err := buildHistory([]xmlServiceInfo{
		{Status: StatusWithdrawn, Start: "2016-06-30T22:00:00Z"},
		{Status: StatusWithdrawn, Start: "2016-06-30T22:00:00Z"},
		{Status: StatusAccredited, Start: "2012-06-19T22:00:00Z"},
	})
	if err != nil || len(h) != 2 || h[1].Status != StatusWithdrawn || !h[0].End.Equal(h[1].Start) {
		t.Fatalf("got %+v, %v", h, err)
	}
	// Conflicting statuses at one instant: the service must never decide.
	_, err = buildHistory([]xmlServiceInfo{
		{Status: StatusGranted, Start: "2020-01-01T00:00:00Z"},
		{Status: StatusWithdrawn, Start: "2020-01-01T00:00:00Z"},
	})
	if err != errAmbiguousHistory {
		t.Fatalf("err = %v", err)
	}
}

// certsOf returns the SDI certificates of the named service.
func certsOf(t *testing.T, l *List, name string) []*x509.Certificate {
	t.Helper()
	s := findService(t, l, name)
	if len(s.Certs) == 0 {
		t.Fatalf("%s has no SDI certificate", name)
	}
	return s.Certs
}
