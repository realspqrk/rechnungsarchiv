package tsa

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Minimal, strict CMS / RFC 3161 structures. Parsing is DER only: BER
// (indefinite lengths, constructed strings) is rejected by encoding/asn1.

type contentInfo struct {
	ContentType asn1.ObjectIdentifier
	Content     asn1.RawValue `asn1:"explicit,tag:0"`
}

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	EncapContentInfo encapContentInfo
	Certificates     asn1.RawValue `asn1:"optional,tag:0"`
	CRLs             asn1.RawValue `asn1:"optional,tag:1"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type encapContentInfo struct {
	EContentType asn1.ObjectIdentifier
	EContent     asn1.RawValue `asn1:"explicit,optional,tag:0"`
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue `asn1:"optional,tag:0"`
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
	UnsignedAttrs      asn1.RawValue `asn1:"optional,tag:1"`
}

type attribute struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

type issuerAndSerial struct {
	Issuer asn1.RawValue
	Serial *big.Int
}

type pkiStatusInfo struct {
	Status       int
	StatusString []string       `asn1:"optional"`
	FailInfo     asn1.BitString `asn1:"optional"`
}

type timeStampResp struct {
	Status pkiStatusInfo
	Token  asn1.RawValue `asn1:"optional"`
}

type accuracy struct {
	Seconds int `asn1:"optional"`
	Millis  int `asn1:"optional,tag:0"`
	Micros  int `asn1:"optional,tag:1"`
}

type tstInfo struct {
	Version  int
	Policy   asn1.ObjectIdentifier
	Imprint  messageImprint
	Serial   *big.Int
	GenTime  time.Time
	Accuracy *time.Duration
	Nonce    *big.Int
}

// unmarshalAll unmarshals exactly one DER element, rejecting trailing data.
func unmarshalAll(b []byte, v any, params string) error {
	rest, err := asn1.UnmarshalWithParams(b, v, params)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("trailing data")
	}
	return nil
}

// elements splits the contents of a constructed element into its children.
func elements(b []byte) ([]asn1.RawValue, error) {
	var out []asn1.RawValue
	for len(b) > 0 {
		var v asn1.RawValue
		rest, err := asn1.Unmarshal(b, &v)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		b = rest
	}
	return out, nil
}

// explicitInner returns the element inside an EXPLICIT tag. encoding/asn1
// hands RawValue fields with "explicit" to us still wrapped.
func explicitInner(v asn1.RawValue) (asn1.RawValue, error) {
	if v.Class != asn1.ClassContextSpecific {
		return v, nil
	}
	var inner asn1.RawValue
	if err := unmarshalAll(v.Bytes, &inner, ""); err != nil {
		return asn1.RawValue{}, err
	}
	return inner, nil
}

func isUniversal(v asn1.RawValue, tag int, compound bool) bool {
	return v.Class == asn1.ClassUniversal && v.Tag == tag && v.IsCompound == compound
}

func isContext(v asn1.RawValue, tag int) bool {
	return v.Class == asn1.ClassContextSpecific && v.Tag == tag
}

// parseCertBag parses the SignedData certificates field. Non-X.509 choices
// (attribute certificates, other) are ignored.
func parseCertBag(raw asn1.RawValue) ([]*x509.Certificate, error) {
	if len(raw.FullBytes) == 0 {
		return nil, nil
	}
	els, err := elements(raw.Bytes)
	if err != nil {
		return nil, err
	}
	var certs []*x509.Certificate
	for _, e := range els {
		if !isUniversal(e, asn1.TagSequence, true) {
			continue
		}
		c, err := x509.ParseCertificate(e.FullBytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	return certs, nil
}

// parseTSTInfo parses TSTInfo by walking its fields, so that an absent
// Accuracy can be told apart from a zero one.
func parseTSTInfo(b []byte) (*tstInfo, error) {
	var seq asn1.RawValue
	if err := unmarshalAll(b, &seq, ""); err != nil {
		return nil, err
	}
	if !isUniversal(seq, asn1.TagSequence, true) {
		return nil, errors.New("TSTInfo is not a SEQUENCE")
	}
	els, err := elements(seq.Bytes)
	if err != nil {
		return nil, err
	}
	if len(els) < 5 {
		return nil, errors.New("TSTInfo too short")
	}
	var t tstInfo
	if err := unmarshalAll(els[0].FullBytes, &t.Version, ""); err != nil || t.Version != 1 {
		return nil, errors.New("TSTInfo version is not 1")
	}
	if err := unmarshalAll(els[1].FullBytes, &t.Policy, ""); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	if err := unmarshalAll(els[2].FullBytes, &t.Imprint, ""); err != nil {
		return nil, fmt.Errorf("messageImprint: %w", err)
	}
	if err := unmarshalAll(els[3].FullBytes, &t.Serial, ""); err != nil {
		return nil, fmt.Errorf("serialNumber: %w", err)
	}
	if t.GenTime, err = parseGeneralizedTime(els[4]); err != nil {
		return nil, err
	}
	rest := els[5:]
	if len(rest) > 0 && isUniversal(rest[0], asn1.TagSequence, true) {
		var a accuracy
		if err := unmarshalAll(rest[0].FullBytes, &a, ""); err != nil {
			return nil, fmt.Errorf("accuracy: %w", err)
		}
		if a.Seconds < 0 || a.Millis < 0 || a.Millis > 999 || a.Micros < 0 || a.Micros > 999 {
			return nil, errors.New("accuracy out of range")
		}
		d := time.Duration(a.Seconds)*time.Second + time.Duration(a.Millis)*time.Millisecond + time.Duration(a.Micros)*time.Microsecond
		t.Accuracy = &d
		rest = rest[1:]
	}
	if len(rest) > 0 && isUniversal(rest[0], asn1.TagBoolean, false) {
		rest = rest[1:] // ordering: irrelevant for a single stamp
	}
	if len(rest) > 0 && isUniversal(rest[0], asn1.TagInteger, false) {
		if err := unmarshalAll(rest[0].FullBytes, &t.Nonce, ""); err != nil {
			return nil, fmt.Errorf("nonce: %w", err)
		}
		rest = rest[1:]
	}
	if len(rest) > 0 && isContext(rest[0], 0) {
		rest = rest[1:] // tsa name: informational
	}
	if len(rest) > 0 && isContext(rest[0], 1) {
		var exts []pkix.Extension
		if err := unmarshalAll(rest[0].FullBytes, &exts, "tag:1"); err != nil {
			return nil, fmt.Errorf("extensions: %w", err)
		}
		for _, e := range exts {
			if e.Critical {
				return nil, fmt.Errorf("unknown critical TSTInfo extension %v", e.Id)
			}
		}
		rest = rest[1:]
	}
	if len(rest) != 0 {
		return nil, errors.New("unexpected TSTInfo field")
	}
	return &t, nil
}

// parseGeneralizedTime accepts YYYYMMDDHHMMSS[.f*]Z. It is more lenient
// than encoding/asn1 about trailing zeros in the fraction, which some TSAs
// emit, but insists on UTC as RFC 3161 requires.
func parseGeneralizedTime(v asn1.RawValue) (time.Time, error) {
	if !isUniversal(v, asn1.TagGeneralizedTime, false) {
		return time.Time{}, errors.New("genTime is not a GeneralizedTime")
	}
	s := string(v.Bytes)
	if len(s) < 15 || s[len(s)-1] != 'Z' {
		return time.Time{}, errors.New("genTime is not UTC")
	}
	t, err := time.Parse("20060102150405", s[:len(s)-1])
	if err != nil {
		return time.Time{}, fmt.Errorf("genTime: %w", err)
	}
	return t.UTC(), nil
}
