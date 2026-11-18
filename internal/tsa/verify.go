package tsa

import (
	"bytes"
	"crypto"
	"crypto/sha1"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// Token is a time-stamp token that passed all acceptance checks (spec §5
// items 1–7).
type Token struct {
	GenTime  time.Time
	Accuracy *time.Duration // nil if the token states none
	Serial   *big.Int
	Policy   asn1.ObjectIdentifier
	Nonce    *big.Int
	Signer   *x509.Certificate
	// Chain is the validated path: Chain[0] is the signer, the last element
	// is the matching service-identity certificate (it may be the signer).
	Chain []*x509.Certificate
	// Certs holds every certificate available during verification (token
	// bag and caller-supplied), used to find issuers for revocation checks.
	Certs []*x509.Certificate
	DER   []byte // the TimeStampToken (CMS ContentInfo)
}

// VerifyOptions holds what a token is checked against.
type VerifyOptions struct {
	// Request is the stored request (n.tsq); imprint and nonce must match.
	Request *Request
	// Policy is the provider's expected policy OID; nil accepts any.
	Policy asn1.ObjectIdentifier
	// Anchors are the certificates the provider's trusted-list service
	// identity names. The path must end at one of them.
	Anchors []*x509.Certificate
	// Intermediates are additional certificates (n.chain.pem, AIA fetches).
	Intermediates []*x509.Certificate
}

// InvalidError reports a failed acceptance check. Item is the spec §5 item
// (1–7). A token failing any item is not acceptable.
type InvalidError struct {
	Item int
	Msg  string // short German text for the report
	Err  error
}

func (e *InvalidError) Error() string {
	s := fmt.Sprintf("Zeitstempel ungültig (§5.%d): %s", e.Item, e.Msg)
	if e.Err != nil {
		s += ": " + e.Err.Error()
	}
	return s
}

func (e *InvalidError) Unwrap() error { return e.Err }

func invalid(item int, msg string, err error) error {
	return &InvalidError{Item: item, Msg: msg, Err: err}
}

// StatusError is returned for a TimeStampResp without a granted token. It
// is a provider answer, not a token, so it never makes an Ablage Ungültig.
type StatusError struct {
	Status int
	Text   []string
	Fail   asn1.BitString
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Anbieter hat abgelehnt (Status %d) %q", e.Status, e.Text)
}

// ParseResponse splits a TimeStampResp into its token. A non-granted
// status yields *StatusError.
func ParseResponse(tsr []byte) ([]byte, error) {
	var resp timeStampResp
	if err := unmarshalAll(tsr, &resp, ""); err != nil {
		return nil, invalid(1, "Antwort nicht lesbar", err)
	}
	if s := resp.Status.Status; s != 0 && s != 1 {
		return nil, &StatusError{Status: s, Text: resp.Status.StatusString, Fail: resp.Status.FailInfo}
	}
	if len(resp.Token.FullBytes) == 0 {
		return nil, invalid(1, "Antwort enthält keinen Zeitstempel", nil)
	}
	return resp.Token.FullBytes, nil
}

// VerifyResponse verifies a stored or received TimeStampResp (n.tsr).
func VerifyResponse(tsr []byte, opts VerifyOptions) (*Token, error) {
	tok, err := ParseResponse(tsr)
	if err != nil {
		return nil, err
	}
	return VerifyToken(tok, opts)
}

// VerifyToken checks a TimeStampToken against spec §5 items 1–7. It works
// offline; it does not check revocation (see ValidateMaterial).
func VerifyToken(der []byte, opts VerifyOptions) (*Token, error) {
	tok, err := checkToken(der, opts)
	if err != nil {
		return nil, err
	}
	chain, err := verifyPath(tok.Signer, tok.Certs, opts.Anchors, tok.GenTime)
	if err != nil {
		return nil, err
	}
	tok.Chain = chain
	return tok, nil
}

// checkToken performs items 1–6 (everything except path validation).
func checkToken(der []byte, opts VerifyOptions) (*Token, error) {
	if opts.Request == nil {
		return nil, errors.New("tsa: VerifyOptions.Request is required")
	}
	// Item 1: CMS SignedData with id-ct-TSTInfo.
	var ci contentInfo
	if err := unmarshalAll(der, &ci, ""); err != nil || !ci.ContentType.Equal(oidSignedData) {
		return nil, invalid(1, "kein CMS SignedData", err)
	}
	sdRaw, err := explicitInner(ci.Content)
	if err != nil {
		return nil, invalid(1, "SignedData nicht lesbar", err)
	}
	var sd signedData
	if err := unmarshalAll(sdRaw.FullBytes, &sd, ""); err != nil {
		return nil, invalid(1, "SignedData nicht lesbar", err)
	}
	if !sd.EncapContentInfo.EContentType.Equal(oidTSTInfo) {
		return nil, invalid(1, "Inhalt ist kein TSTInfo", nil)
	}
	ec, err := explicitInner(sd.EncapContentInfo.EContent)
	if err != nil || !isUniversal(ec, asn1.TagOctetString, false) {
		return nil, invalid(1, "TSTInfo fehlt", nil)
	}
	content := ec.Bytes
	if len(sd.SignerInfos) != 1 {
		return nil, invalid(1, "nicht genau eine Signatur", nil)
	}
	si := sd.SignerInfos[0]

	// Item 6 (hash part): digest algorithm.
	h, ok := allowedHash(si.DigestAlgorithm.Algorithm)
	if !ok {
		return nil, invalid(6, "Hash-Verfahren nicht erlaubt", fmt.Errorf("%v", si.DigestAlgorithm.Algorithm))
	}

	// Item 2: signed attributes and messageDigest.
	if len(si.SignedAttrs.FullBytes) == 0 {
		return nil, invalid(2, "keine signierten Attribute", nil)
	}
	// The signature covers the attributes with an explicit SET OF tag.
	signed := bytes.Clone(si.SignedAttrs.FullBytes)
	signed[0] = 0x31
	attrs, err := parseAttributes(signed)
	if err != nil {
		return nil, invalid(2, "signierte Attribute nicht lesbar", err)
	}
	ct, ok := attrs.single(oidContentType)
	var ctOID asn1.ObjectIdentifier
	if !ok || unmarshalAll(ct.FullBytes, &ctOID, "") != nil || !ctOID.Equal(oidTSTInfo) {
		return nil, invalid(2, "Attribut contentType falsch", nil)
	}
	md, ok := attrs.single(oidMessageDigst)
	var digest []byte
	if !ok || unmarshalAll(md.FullBytes, &digest, "") != nil {
		return nil, invalid(2, "Attribut messageDigest fehlt", nil)
	}
	hh := h.New()
	hh.Write(content)
	if !bytes.Equal(digest, hh.Sum(nil)) {
		return nil, invalid(2, "messageDigest passt nicht zum Inhalt", nil)
	}

	// Item 3: the signer is the certificate ESSCertID(v2) identifies.
	bag, err := parseCertBag(sd.Certificates)
	if err != nil {
		return nil, invalid(1, "Zertifikat im Zeitstempel nicht lesbar", err)
	}
	pool := append(bag, opts.Intermediates...)
	signer, err := essSigner(attrs, pool)
	if err != nil {
		return nil, invalid(3, "Unterzeichner-Zertifikat nicht eindeutig gebunden", err)
	}
	if !sidMatches(si.SID, signer) {
		return nil, invalid(3, "SignerIdentifier passt nicht zum ESSCertID-Zertifikat", nil)
	}

	// Items 2 and 6: the signature itself.
	if err := verifySignature(signer, h, si.SignatureAlgorithm, signed, si.Signature); err != nil {
		var ie *InvalidError
		if errors.As(err, &ie) {
			return nil, err
		}
		return nil, invalid(2, "Signatur ungültig", err)
	}

	// Item 4: critical, sole id-kp-timeStamping.
	if err := checkTSUEKU(signer); err != nil {
		return nil, invalid(4, "Zertifikat ist nicht ausschließlich für Zeitstempel", err)
	}

	// Item 5: imprint, nonce, policy.
	info, err := parseTSTInfo(content)
	if err != nil {
		return nil, invalid(1, "TSTInfo nicht lesbar", err)
	}
	if !info.Imprint.HashAlgorithm.Algorithm.Equal(oidSHA256) || !bytes.Equal(info.Imprint.HashedMessage, opts.Request.Imprint[:]) {
		return nil, invalid(5, "Prüfwert im Zeitstempel passt nicht", nil)
	}
	if info.Nonce == nil || info.Nonce.Cmp(opts.Request.Nonce) != 0 {
		return nil, invalid(5, "Nonce passt nicht zur Anfrage", nil)
	}
	if opts.Policy != nil && !info.Policy.Equal(opts.Policy) {
		return nil, invalid(5, "Policy passt nicht", fmt.Errorf("got %v, want %v", info.Policy, opts.Policy))
	}

	return &Token{
		GenTime:  info.GenTime,
		Accuracy: info.Accuracy,
		Serial:   info.Serial,
		Policy:   info.Policy,
		Nonce:    info.Nonce,
		Signer:   signer,
		Certs:    dedupe(pool),
		DER:      bytes.Clone(der),
	}, nil
}

type attributes []attribute

func parseAttributes(set []byte) (attributes, error) {
	var attrs attributes
	if err := unmarshalAll(set, (*[]attribute)(&attrs), "set"); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, a := range attrs {
		k := a.Type.String()
		if seen[k] {
			return nil, fmt.Errorf("duplicate attribute %s", k)
		}
		seen[k] = true
	}
	return attrs, nil
}

// single returns the only value of an attribute.
func (as attributes) single(oid asn1.ObjectIdentifier) (asn1.RawValue, bool) {
	for _, a := range as {
		if a.Type.Equal(oid) {
			if len(a.Values) != 1 {
				return asn1.RawValue{}, false
			}
			return a.Values[0], true
		}
	}
	return asn1.RawValue{}, false
}

func (as attributes) has(oid asn1.ObjectIdentifier) bool {
	for _, a := range as {
		if a.Type.Equal(oid) {
			return true
		}
	}
	return false
}

// essCertID is the first entry of SigningCertificate(V2): it identifies
// the signer (RFC 2634 §5.4, RFC 5035).
type essCertID struct {
	hash         crypto.Hash
	certHash     []byte
	issuerSerial *issuerSerial
}

type issuerSerial struct {
	Issuer asn1.RawValue // GeneralNames
	Serial *big.Int
}

// essSigner finds the one certificate that every present ESSCertID /
// ESSCertIDv2 attribute identifies. A certificate merely present in the bag
// is never taken as the signer (CVE-2026-39984).
func essSigner(attrs attributes, pool []*x509.Certificate) (*x509.Certificate, error) {
	var ids []essCertID
	for _, v2 := range []bool{true, false} {
		oid := oidSigningCert
		if v2 {
			oid = oidSigningCert2
		}
		if !attrs.has(oid) {
			continue
		}
		val, ok := attrs.single(oid)
		if !ok {
			return nil, errors.New("signing certificate attribute has not exactly one value")
		}
		id, err := parseESSCertID(val, v2)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errors.New("no ESSCertID attribute")
	}
	var signer *x509.Certificate
	for _, c := range pool {
		if matchesAll(c, ids) {
			if signer != nil && !signer.Equal(c) {
				return nil, errors.New("several certificates match ESSCertID")
			}
			signer = c
		}
	}
	if signer == nil {
		return nil, errors.New("no certificate matches ESSCertID")
	}
	return signer, nil
}

func matchesAll(c *x509.Certificate, ids []essCertID) bool {
	for _, id := range ids {
		hh := id.hash.New()
		hh.Write(c.Raw)
		if !bytes.Equal(hh.Sum(nil), id.certHash) {
			return false
		}
		if id.issuerSerial != nil && !issuerSerialMatches(id.issuerSerial, c) {
			return false
		}
	}
	return true
}

func parseESSCertID(val asn1.RawValue, v2 bool) (essCertID, error) {
	var sc struct {
		Certs    []asn1.RawValue
		Policies asn1.RawValue `asn1:"optional"`
	}
	if err := unmarshalAll(val.FullBytes, &sc, ""); err != nil {
		return essCertID{}, err
	}
	if len(sc.Certs) == 0 {
		return essCertID{}, errors.New("empty ESSCertID list")
	}
	els, err := elements(sc.Certs[0].Bytes)
	if err != nil || len(els) == 0 {
		return essCertID{}, errors.New("bad ESSCertID")
	}
	id := essCertID{hash: crypto.SHA1} // ESSCertID v1 is SHA-1 by definition
	if v2 {
		id.hash = crypto.SHA256 // DEFAULT id-sha256
		if isUniversal(els[0], asn1.TagSequence, true) {
			var alg struct {
				Algorithm  asn1.ObjectIdentifier
				Parameters asn1.RawValue `asn1:"optional"`
			}
			if err := unmarshalAll(els[0].FullBytes, &alg, ""); err != nil {
				return essCertID{}, err
			}
			h, ok := allowedHash(alg.Algorithm)
			if !ok {
				return essCertID{}, fmt.Errorf("ESSCertIDv2 hash %v not allowed", alg.Algorithm)
			}
			id.hash = h
			els = els[1:]
		}
	}
	if len(els) == 0 || unmarshalAll(els[0].FullBytes, &id.certHash, "") != nil || len(id.certHash) != id.hash.Size() {
		return essCertID{}, errors.New("bad ESSCertID certHash")
	}
	els = els[1:]
	if len(els) > 0 {
		var is issuerSerial
		if err := unmarshalAll(els[0].FullBytes, &is, ""); err != nil {
			return essCertID{}, fmt.Errorf("issuerSerial: %w", err)
		}
		id.issuerSerial = &is
		els = els[1:]
	}
	if len(els) != 0 {
		return essCertID{}, errors.New("trailing ESSCertID fields")
	}
	return id, nil
}

// issuerSerialMatches requires a directoryName equal to the certificate's
// issuer and an equal serial number.
func issuerSerialMatches(is *issuerSerial, c *x509.Certificate) bool {
	if is.Serial == nil || is.Serial.Cmp(c.SerialNumber) != 0 {
		return false
	}
	names, err := elements(is.Issuer.Bytes)
	if err != nil {
		return false
	}
	for _, n := range names {
		if isContext(n, 4) && n.IsCompound && bytes.Equal(n.Bytes, c.RawIssuer) {
			return true
		}
	}
	return false
}

// sidMatches checks the CMS SignerIdentifier against the ESSCertID signer.
func sidMatches(sid asn1.RawValue, c *x509.Certificate) bool {
	switch {
	case isUniversal(sid, asn1.TagSequence, true):
		var ias issuerAndSerial
		if unmarshalAll(sid.FullBytes, &ias, "") != nil || ias.Serial == nil {
			return false
		}
		return bytes.Equal(ias.Issuer.FullBytes, c.RawIssuer) && ias.Serial.Cmp(c.SerialNumber) == 0
	case isContext(sid, 0) && !sid.IsCompound:
		return len(c.SubjectKeyId) > 0 && bytes.Equal(sid.Bytes, c.SubjectKeyId)
	}
	return false
}

// checkTSUEKU requires a critical extKeyUsage containing exactly
// id-kp-timeStamping (RFC 3161 §2.3).
func checkTSUEKU(c *x509.Certificate) error {
	for _, e := range c.Extensions {
		if e.Id.Equal(oidExtKeyUsage) {
			if !e.Critical {
				return errors.New("extKeyUsage not critical")
			}
			if len(c.UnknownExtKeyUsage) != 0 || len(c.ExtKeyUsage) != 1 || c.ExtKeyUsage[0] != x509.ExtKeyUsageTimeStamping {
				return errors.New("extKeyUsage is not solely timeStamping")
			}
			return nil
		}
	}
	return errors.New("no extKeyUsage")
}

func dedupe(certs []*x509.Certificate) []*x509.Certificate {
	var out []*x509.Certificate
	seen := map[[20]byte]bool{}
	for _, c := range certs {
		k := sha1.Sum(c.Raw)
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out
}
