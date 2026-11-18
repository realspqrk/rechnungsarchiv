package tsa

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"

	"golang.org/x/crypto/ocsp"
)

// EvidenceKind is the type of a revocation evidence file.
type EvidenceKind string

const (
	KindOCSP EvidenceKind = "ocsp"
	KindCRL  EvidenceKind = "crl"
)

// RevStatus is a certificate's status according to one piece of evidence.
type RevStatus int

const (
	StatusGood RevStatus = iota
	StatusRevoked
	StatusUnknown
)

// RFC 5280 CRLReason codes needed by callers.
const (
	ReasonUnspecified     = 0
	ReasonKeyCompromise   = 1
	ReasonCACompromise    = 2
	ReasonCertificateHold = 6
)

// Revocation is the authenticated content of one OCSP response or CRL for
// one certificate.
type Revocation struct {
	Kind       EvidenceKind
	Status     RevStatus
	Reason     int       // CRLReason if revoked (absent = unspecified = 0)
	RevokedAt  time.Time // if revoked
	ThisUpdate time.Time
	NextUpdate time.Time // zero if absent
	// Covers reports that the evidence speaks for genTime (spec §5):
	// thisUpdate ≤ genTime ≤ nextUpdate, or thisUpdate within 24 h after
	// genTime while the certificate was still valid.
	Covers bool
}

// OK reports whether the evidence is acceptable as validation material.
func (r *Revocation) OK() bool { return r.Status == StatusGood && r.Covers }

// KeyCompromise reports a revocation that makes every token signed under
// this certificate Unbestimmt (spec §6 step 7).
func (r *Revocation) KeyCompromise() bool {
	if r.Status != StatusRevoked {
		return false
	}
	switch r.Reason {
	case ReasonUnspecified, ReasonKeyCompromise, ReasonCACompromise:
		return true
	}
	return false
}

// GracePeriod is how long after genTime fresh evidence is still accepted.
const GracePeriod = 24 * time.Hour

func covers(cert *x509.Certificate, thisUpdate, nextUpdate, genTime time.Time) bool {
	if !thisUpdate.After(genTime) && !nextUpdate.IsZero() && !genTime.After(nextUpdate) {
		return true
	}
	return !thisUpdate.Before(genTime) && !thisUpdate.After(genTime.Add(GracePeriod)) &&
		!thisUpdate.After(cert.NotAfter)
}

// CheckRevocation authenticates one OCSP response or CRL for cert, issued
// by issuer, and evaluates it for genTime. An error means the evidence is
// unusable (wrong signer, wrong certificate, delta/partitioned CRL, …); a
// usable but bad result is returned as *Revocation with OK() == false.
func CheckRevocation(kind EvidenceKind, raw []byte, cert, issuer *x509.Certificate, genTime time.Time) (*Revocation, error) {
	switch kind {
	case KindOCSP:
		return checkOCSP(raw, cert, issuer, genTime)
	case KindCRL:
		return checkCRL(raw, cert, issuer, genTime)
	}
	return nil, fmt.Errorf("unknown evidence kind %q", kind)
}

func checkOCSP(raw []byte, cert, issuer *x509.Certificate, genTime time.Time) (*Revocation, error) {
	// x/crypto/ocsp matches by serial only and accepts any embedded
	// certificate the issuer signed. We additionally check the certID
	// issuer hashes and the responder's authorization.
	resp, err := ocsp.ParseResponseForCert(raw, cert, issuer)
	if err != nil {
		return nil, fmt.Errorf("OCSP: %w", err)
	}
	if err := checkCertID(raw, cert, issuer); err != nil {
		return nil, err
	}
	if r := resp.Certificate; r != nil && !r.Equal(issuer) {
		if !bytes.Equal(r.RawIssuer, issuer.RawSubject) {
			return nil, errors.New("OCSP: responder not issued by the CA")
		}
		if !hasEKU(r, x509.ExtKeyUsageOCSPSigning) {
			return nil, errors.New("OCSP: responder not authorized (no id-kp-OCSPSigning)")
		}
		if resp.ProducedAt.Before(r.NotBefore) || resp.ProducedAt.After(r.NotAfter) {
			return nil, errors.New("OCSP: responder certificate not valid at producedAt")
		}
	}
	rev := &Revocation{Kind: KindOCSP, ThisUpdate: resp.ThisUpdate, NextUpdate: resp.NextUpdate}
	switch resp.Status {
	case ocsp.Good:
		rev.Status = StatusGood
	case ocsp.Revoked:
		rev.Status, rev.Reason, rev.RevokedAt = StatusRevoked, resp.RevocationReason, resp.RevokedAt
	default:
		rev.Status = StatusUnknown
	}
	rev.Covers = covers(cert, rev.ThisUpdate, rev.NextUpdate, genTime)
	return rev, nil
}

func hasEKU(c *x509.Certificate, want x509.ExtKeyUsage) bool {
	for _, u := range c.ExtKeyUsage {
		if u == want {
			return true
		}
	}
	return false
}

type ocspCertID struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	NameHash      []byte
	KeyHash       []byte
	Serial        *big.Int
}

// checkCertID requires exactly one SingleResponse for cert's serial and
// that its issuerNameHash / issuerKeyHash identify issuer.
func checkCertID(raw []byte, cert, issuer *x509.Certificate) error {
	var outer struct {
		Status asn1.Enumerated
		Bytes  struct {
			Type     asn1.ObjectIdentifier
			Response []byte
		} `asn1:"explicit,tag:0"`
	}
	var basic struct {
		TBS   asn1.RawValue
		Alg   pkix.AlgorithmIdentifier
		Sig   asn1.BitString
		Certs asn1.RawValue `asn1:"optional,explicit,tag:0"`
	}
	var tbs struct {
		Version     int `asn1:"optional,explicit,tag:0,default:0"`
		ResponderID asn1.RawValue
		ProducedAt  time.Time `asn1:"generalized"`
		Responses   []asn1.RawValue
		Exts        asn1.RawValue `asn1:"optional,explicit,tag:1"`
	}
	if unmarshalAll(raw, &outer, "") != nil || unmarshalAll(outer.Bytes.Response, &basic, "") != nil ||
		unmarshalAll(basic.TBS.FullBytes, &tbs, "") != nil {
		return errors.New("OCSP: response not parseable")
	}
	var spki struct {
		Algorithm pkix.AlgorithmIdentifier
		PublicKey asn1.BitString
	}
	if unmarshalAll(issuer.RawSubjectPublicKeyInfo, &spki, "") != nil {
		return errors.New("OCSP: issuer key not parseable")
	}
	found := 0
	for _, r := range tbs.Responses {
		// SingleResponse ::= SEQUENCE { certID, certStatus, thisUpdate, … }
		var id ocspCertID
		els, err := elements(r.Bytes)
		if err != nil || len(els) == 0 || unmarshalAll(els[0].FullBytes, &id, "") != nil {
			return errors.New("OCSP: certID not parseable")
		}
		if id.Serial == nil || id.Serial.Cmp(cert.SerialNumber) != 0 {
			continue
		}
		found++
		var h crypto.Hash
		switch {
		case id.HashAlgorithm.Algorithm.Equal(oidSHA1):
			h = crypto.SHA1 // RFC 6960 default; only identifies, the signature authenticates
		default:
			var ok bool
			if h, ok = allowedHash(id.HashAlgorithm.Algorithm); !ok {
				return errors.New("OCSP: certID hash not supported")
			}
		}
		nh, kh := h.New(), h.New()
		nh.Write(issuer.RawSubject)
		kh.Write(spki.PublicKey.RightAlign())
		if !bytes.Equal(nh.Sum(nil), id.NameHash) || !bytes.Equal(kh.Sum(nil), id.KeyHash) {
			return errors.New("OCSP: certID issuer does not match")
		}
	}
	if found != 1 {
		return errors.New("OCSP: not exactly one response for the certificate")
	}
	return nil
}

type idpExt struct {
	DistributionPoint          asn1.RawValue  `asn1:"optional,tag:0"`
	OnlyContainsUserCerts      bool           `asn1:"optional,tag:1"`
	OnlyContainsCACerts        bool           `asn1:"optional,tag:2"`
	OnlySomeReasons            asn1.BitString `asn1:"optional,tag:3"`
	IndirectCRL                bool           `asn1:"optional,tag:4"`
	OnlyContainsAttributeCerts bool           `asn1:"optional,tag:5"`
}

func checkCRL(raw []byte, cert, issuer *x509.Certificate, genTime time.Time) (*Revocation, error) {
	rl, err := x509.ParseRevocationList(raw)
	if err != nil {
		return nil, fmt.Errorf("CRL: %w", err)
	}
	if !bytes.Equal(rl.RawIssuer, cert.RawIssuer) || !bytes.Equal(rl.RawIssuer, issuer.RawSubject) {
		return nil, errors.New("CRL: issuer does not match")
	}
	// Checks the CA's signature, CA flag and cRLSign keyUsage.
	if err := rl.CheckSignatureFrom(issuer); err != nil {
		return nil, fmt.Errorf("CRL: %w", err)
	}
	for _, e := range rl.Extensions {
		switch {
		case e.Id.Equal(oidDeltaCRL):
			return nil, errors.New("CRL: delta CRL does not count")
		case e.Id.Equal(oidIssuingDistPt):
			if err := checkIDP(e.Value, cert); err != nil {
				return nil, err
			}
		case e.Critical && !e.Id.Equal(oidFreshestCRL):
			return nil, fmt.Errorf("CRL: unknown critical extension %v", e.Id)
		}
	}
	rev := &Revocation{Kind: KindCRL, Status: StatusGood, ThisUpdate: rl.ThisUpdate, NextUpdate: rl.NextUpdate}
	for _, entry := range rl.RevokedCertificateEntries {
		if entry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			for _, e := range entry.Extensions {
				if e.Critical { // e.g. certificateIssuer: indirect CRL
					return nil, errors.New("CRL: critical entry extension")
				}
			}
			rev.Status, rev.Reason, rev.RevokedAt = StatusRevoked, entry.ReasonCode, entry.RevocationTime
			break
		}
	}
	rev.Covers = covers(cert, rev.ThisUpdate, rev.NextUpdate, genTime)
	return rev, nil
}

// checkIDP rejects partitioned CRLs whose issuingDistributionPoint does not
// cover cert (RFC 5280 §5.2.5).
func checkIDP(der []byte, cert *x509.Certificate) error {
	var idp idpExt
	if err := unmarshalAll(der, &idp, ""); err != nil {
		return fmt.Errorf("CRL: IDP not parseable: %w", err)
	}
	switch {
	case idp.OnlySomeReasons.BitLength > 0:
		return errors.New("CRL: partitioned by reason")
	case idp.IndirectCRL, idp.OnlyContainsAttributeCerts:
		return errors.New("CRL: indirect or attribute CRL")
	case idp.OnlyContainsUserCerts && cert.IsCA, idp.OnlyContainsCACerts && !cert.IsCA:
		return errors.New("CRL: scope does not cover the certificate")
	}
	if len(idp.DistributionPoint.FullBytes) == 0 {
		return nil
	}
	// DistributionPointName: fullName [0] GeneralNames. Match a URI [6].
	els, err := elements(idp.DistributionPoint.Bytes)
	if err != nil || len(els) != 1 || !isContext(els[0], 0) {
		return errors.New("CRL: IDP name not supported")
	}
	names, err := elements(els[0].Bytes)
	if err != nil {
		return errors.New("CRL: IDP name not parseable")
	}
	for _, n := range names {
		if isContext(n, 6) {
			for _, dp := range cert.CRLDistributionPoints {
				if string(n.Bytes) == dp {
					return nil
				}
			}
		}
	}
	return errors.New("CRL: distribution point does not cover the certificate")
}
