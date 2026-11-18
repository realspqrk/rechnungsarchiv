package tsatest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

var (
	oidSHA1       = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512     = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}
	oidRSA        = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSAPSS     = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidMGF1       = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	oidSignedData = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidCT         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMD         = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigTime    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 5}
	oidSigCert    = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	oidSigCert2   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}

	// DefaultPolicy is the policy the test TSA uses unless told otherwise.
	DefaultPolicy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 99999, 1}
)

var hashOID = map[crypto.Hash]asn1.ObjectIdentifier{
	crypto.SHA1: oidSHA1, crypto.SHA256: oidSHA256, crypto.SHA384: oidSHA384, crypto.SHA512: oidSHA512,
}

var ecdsaOID = map[crypto.Hash]asn1.ObjectIdentifier{
	crypto.SHA1:   {1, 2, 840, 10045, 4, 1},
	crypto.SHA256: {1, 2, 840, 10045, 4, 3, 2},
	crypto.SHA384: {1, 2, 840, 10045, 4, 3, 3},
	crypto.SHA512: {1, 2, 840, 10045, 4, 3, 4},
}

// Accuracy is the TSTInfo accuracy.
type Accuracy struct{ Seconds, Millis, Micros int }

// TokenParams describes a token. Zero fields get sane defaults; the other
// fields exist to build broken tokens for negative tests.
type TokenParams struct {
	Imprint    []byte
	ImprintAlg asn1.ObjectIdentifier // default SHA-256
	Nonce      *big.Int
	Policy     asn1.ObjectIdentifier // default DefaultPolicy
	GenTime    time.Time             // default now
	Serial     *big.Int
	Accuracy   *Accuracy
	Hash       crypto.Hash // CMS digest; default SHA-256

	Signer  *Cert
	SignKey crypto.Signer       // default Signer.Key (set to forge)
	Certs   []*x509.Certificate // certificate bag
	PSS     bool

	CertIDCert     *x509.Certificate // ESSCertID target; default signer
	CertIDv1       bool              // SigningCertificate (SHA-1) instead of V2
	CertIDHash     crypto.Hash       // V2 hash; default SHA-256 (omitted)
	NoCertID       bool
	NoIssuerSerial bool
	SIDCert        *x509.Certificate // SignerIdentifier target; default signer

	WrongMessageDigest bool // messageDigest over other content
	TamperSignedAttrs  bool // change an attribute after signing
}

type attr struct {
	Type   asn1.ObjectIdentifier
	Values []asn1.RawValue `asn1:"set"`
}

func raw(v any) asn1.RawValue {
	b, err := asn1.Marshal(v)
	if err != nil {
		panic(err)
	}
	return asn1.RawValue{FullBytes: b}
}

func tagged(class, tag int, compound bool, content []byte) asn1.RawValue {
	b, err := asn1.Marshal(asn1.RawValue{Class: class, Tag: tag, IsCompound: compound, Bytes: content})
	if err != nil {
		panic(err)
	}
	return asn1.RawValue{FullBytes: b}
}

type issuerSerial struct {
	Issuer []asn1.RawValue
	Serial *big.Int
}

func essCertID(p TokenParams, c *x509.Certificate) asn1.RawValue {
	is := issuerSerial{Issuer: []asn1.RawValue{tagged(2, 4, true, c.RawIssuer)}, Serial: c.SerialNumber}
	var fields []asn1.RawValue
	h := crypto.SHA1
	if !p.CertIDv1 {
		h = p.CertIDHash
		if h == 0 {
			h = crypto.SHA256
		}
		if h != crypto.SHA256 {
			fields = append(fields, raw(pkix.AlgorithmIdentifier{Algorithm: hashOID[h]}))
		}
	}
	hh := h.New()
	hh.Write(c.Raw)
	fields = append(fields, raw(hh.Sum(nil)))
	if !p.NoIssuerSerial {
		fields = append(fields, raw(is))
	}
	id := raw(fields) // SEQUENCE of the fields
	return raw(struct{ Certs []asn1.RawValue }{[]asn1.RawValue{id}})
}

type tstInfo struct {
	Version  int
	Policy   asn1.ObjectIdentifier
	Imprint  imprint
	Serial   *big.Int
	GenTime  time.Time     `asn1:"generalized"`
	Accuracy asn1.RawValue `asn1:"optional"`
	Nonce    *big.Int      `asn1:"optional"`
}

type imprint struct {
	Alg  pkix.AlgorithmIdentifier
	Hash []byte
}

type accuracyOut struct {
	Seconds int `asn1:"optional"`
	Millis  int `asn1:"optional,tag:0"`
	Micros  int `asn1:"optional,tag:1"`
}

type signerInfo struct {
	Version            int
	SID                asn1.RawValue
	DigestAlgorithm    pkix.AlgorithmIdentifier
	SignedAttrs        asn1.RawValue
	SignatureAlgorithm pkix.AlgorithmIdentifier
	Signature          []byte
}

// RawValue fields with FullBytes are written verbatim (struct tags are
// ignored), so explicit [0] wrappers are built with tagged().
type encap struct {
	Type    asn1.ObjectIdentifier
	Content asn1.RawValue
}

type signedData struct {
	Version          int
	DigestAlgorithms []pkix.AlgorithmIdentifier `asn1:"set"`
	Encap            encap
	Certificates     asn1.RawValue `asn1:"optional"`
	SignerInfos      []signerInfo  `asn1:"set"`
}

type contentInfo struct {
	Type    asn1.ObjectIdentifier
	Content asn1.RawValue
}

// Token builds a TimeStampToken (CMS ContentInfo).
func Token(p TokenParams) ([]byte, error) {
	if p.Signer == nil {
		return nil, errors.New("tsatest: no signer")
	}
	if p.Hash == 0 {
		p.Hash = crypto.SHA256
	}
	if p.ImprintAlg == nil {
		p.ImprintAlg = oidSHA256
	}
	if p.Policy == nil {
		p.Policy = DefaultPolicy
	}
	if p.GenTime.IsZero() {
		p.GenTime = time.Now()
	}
	if p.Serial == nil {
		p.Serial = nextSerial()
	}
	key := p.SignKey
	if key == nil {
		key = p.Signer.Key
	}
	info := tstInfo{
		Version: 1, Policy: p.Policy,
		Imprint: imprint{Alg: pkix.AlgorithmIdentifier{Algorithm: p.ImprintAlg}, Hash: p.Imprint},
		Serial:  p.Serial, GenTime: p.GenTime.UTC().Truncate(time.Second), Nonce: p.Nonce,
	}
	if a := p.Accuracy; a != nil {
		info.Accuracy = raw(accuracyOut{a.Seconds, a.Millis, a.Micros})
	}
	content, err := asn1.Marshal(info)
	if err != nil {
		return nil, err
	}

	h := p.Hash.New()
	h.Write(content)
	if p.WrongMessageDigest {
		h.Write([]byte("x"))
	}
	sigTime := p.GenTime.UTC().Truncate(time.Second)
	attrs := func(t time.Time) []byte {
		as := []attr{
			{oidCT, []asn1.RawValue{raw(oidTSTInfo)}},
			{oidMD, []asn1.RawValue{raw(h.Sum(nil))}},
			{oidSigTime, []asn1.RawValue{raw(t)}},
		}
		if !p.NoCertID {
			c := p.CertIDCert
			if c == nil {
				c = p.Signer.Cert
			}
			oid := oidSigCert2
			if p.CertIDv1 {
				oid = oidSigCert
			}
			as = append(as, attr{oid, []asn1.RawValue{essCertID(p, c)}})
		}
		b, err := asn1.MarshalWithParams(as, "set")
		if err != nil {
			panic(err)
		}
		return b
	}
	signed := attrs(sigTime)
	ah := p.Hash.New()
	ah.Write(signed)
	digest := ah.Sum(nil)

	var sigAlg pkix.AlgorithmIdentifier
	var sig []byte
	switch key.Public().(type) {
	case *ecdsa.PublicKey:
		sigAlg = pkix.AlgorithmIdentifier{Algorithm: ecdsaOID[p.Hash]}
		sig, err = key.Sign(rand.Reader, digest, p.Hash)
	case *rsa.PublicKey:
		if p.PSS {
			hashAlg := pkix.AlgorithmIdentifier{Algorithm: hashOID[p.Hash], Parameters: asn1.NullRawValue}
			params := struct {
				Hash pkix.AlgorithmIdentifier `asn1:"explicit,tag:0"`
				MGF  pkix.AlgorithmIdentifier `asn1:"explicit,tag:1"`
				Salt int                      `asn1:"explicit,tag:2"`
			}{hashAlg, pkix.AlgorithmIdentifier{Algorithm: oidMGF1, Parameters: raw(hashAlg)}, p.Hash.Size()}
			sigAlg = pkix.AlgorithmIdentifier{Algorithm: oidRSAPSS, Parameters: raw(params)}
			sig, err = key.Sign(rand.Reader, digest, &rsa.PSSOptions{SaltLength: p.Hash.Size(), Hash: p.Hash})
		} else {
			sigAlg = pkix.AlgorithmIdentifier{Algorithm: oidRSA, Parameters: asn1.NullRawValue}
			sig, err = key.Sign(rand.Reader, digest, p.Hash)
		}
	default:
		return nil, errors.New("tsatest: unsupported key")
	}
	if err != nil {
		return nil, err
	}
	if p.TamperSignedAttrs {
		signed = attrs(sigTime.Add(time.Second))
	}
	signedImplicit := append([]byte{0xA0}, signed[1:]...)

	sidCert := p.SIDCert
	if sidCert == nil {
		sidCert = p.Signer.Cert
	}
	sid := raw(struct {
		Issuer asn1.RawValue
		Serial *big.Int
	}{asn1.RawValue{FullBytes: sidCert.RawIssuer}, sidCert.SerialNumber})

	var bag []byte
	for _, c := range p.Certs {
		bag = append(bag, c.Raw...)
	}
	sd := signedData{
		Version:          3,
		DigestAlgorithms: []pkix.AlgorithmIdentifier{{Algorithm: hashOID[p.Hash]}},
		Encap:            encap{Type: oidTSTInfo, Content: tagged(2, 0, true, tagged(0, asn1.TagOctetString, false, content).FullBytes)},
		SignerInfos: []signerInfo{{
			Version: 1, SID: sid,
			DigestAlgorithm:    pkix.AlgorithmIdentifier{Algorithm: hashOID[p.Hash]},
			SignedAttrs:        asn1.RawValue{FullBytes: signedImplicit},
			SignatureAlgorithm: sigAlg,
			Signature:          sig,
		}},
	}
	if len(p.Certs) > 0 {
		sd.Certificates = tagged(2, 0, true, bag)
	}
	return asn1.Marshal(contentInfo{Type: oidSignedData, Content: tagged(2, 0, true, raw(sd).FullBytes)})
}

// Response wraps a token into a TimeStampResp. status 0 = granted.
func Response(status int, text string, token []byte) []byte {
	type statusInfo struct {
		Status int
		Text   []string `asn1:"optional"`
	}
	si := statusInfo{Status: status}
	if text != "" {
		si.Text = []string{text}
	}
	resp := struct {
		Status statusInfo
		Token  asn1.RawValue `asn1:"optional"`
	}{Status: si}
	if token != nil {
		resp.Token = asn1.RawValue{FullBytes: token}
	}
	b, err := asn1.Marshal(resp)
	if err != nil {
		panic(err)
	}
	return b
}

// Request is the parsed part of a TimeStampReq the test TSA needs.
type Request struct {
	Version int
	Imprint imprint
	Policy  asn1.ObjectIdentifier `asn1:"optional"`
	Nonce   *big.Int              `asn1:"optional"`
	CertReq bool                  `asn1:"optional"`
}

// TSA is an RFC 3161 server over HTTP.
type TSA struct {
	Signer *Cert
	Chain  []*x509.Certificate // added to the bag after the signer when certReq
	Policy asn1.ObjectIdentifier

	mu       sync.Mutex
	edit     func(p *TokenParams)
	reply    func(w http.ResponseWriter, r *http.Request) bool
	requests int
}

// SetEdit installs a hook that adjusts each token before it is built.
func (s *TSA) SetEdit(f func(p *TokenParams)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.edit = f
}

// SetReply installs a hook that may take over the HTTP answer (return
// true) before the request is processed. nil removes it.
func (s *TSA) SetReply(f func(w http.ResponseWriter, r *http.Request) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reply = f
}

// Requests returns the number of tokens issued so far.
func (s *TSA) Requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

// Respond answers a DER TimeStampReq with a TimeStampResp.
func (s *TSA) Respond(tsq []byte) ([]byte, error) {
	var req Request
	if _, err := asn1.Unmarshal(tsq, &req); err != nil {
		return nil, err
	}
	p := TokenParams{
		Imprint: req.Imprint.Hash, ImprintAlg: req.Imprint.Alg.Algorithm,
		Nonce: req.Nonce, Policy: s.Policy, Signer: s.Signer,
	}
	if req.CertReq {
		p.Certs = append([]*x509.Certificate{s.Signer.Cert}, s.Chain...)
	}
	s.mu.Lock()
	edit := s.edit
	s.requests++
	s.mu.Unlock()
	if edit != nil {
		edit(&p)
	}
	tok, err := Token(p)
	if err != nil {
		return nil, err
	}
	return Response(0, "", tok), nil
}

func (s *TSA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	reply := s.reply
	s.mu.Unlock()
	if reply != nil && reply(w, r) {
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/timestamp-query" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := s.Respond(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/timestamp-reply")
	w.Write(resp)
}
