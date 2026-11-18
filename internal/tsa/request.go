package tsa

import (
	"bytes"
	"crypto/rand"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"math/big"
)

// Request is an RFC 3161 TimeStampReq v1 over a 32-byte Prüfwert.
type Request struct {
	Imprint [32]byte              // the Prüfwert (SHA-256)
	Nonce   *big.Int              // 64-bit random nonce
	Policy  asn1.ObjectIdentifier // reqPolicy, may be nil
	DER     []byte                // exact bytes sent and stored as n.tsq
}

type messageImprint struct {
	HashAlgorithm pkix.AlgorithmIdentifier
	HashedMessage []byte
}

type timeStampReq struct {
	Version        int
	MessageImprint messageImprint
	ReqPolicy      asn1.ObjectIdentifier `asn1:"optional"`
	Nonce          *big.Int              `asn1:"optional"`
	CertReq        bool                  `asn1:"optional"`
	Extensions     []pkix.Extension      `asn1:"optional,tag:0"`
}

// NewRequest builds a TimeStampReq with certReq=TRUE and a fresh nonce.
// policy is the provider's request_policy (reqPolicy) or nil.
func NewRequest(pruefwert [32]byte, policy asn1.ObjectIdentifier) (*Request, error) {
	var nonce *big.Int
	for nonce == nil || nonce.Sign() == 0 {
		b := make([]byte, 8)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		nonce = new(big.Int).SetBytes(b)
	}
	return encodeRequest(pruefwert, policy, nonce)
}

// Canonical reports whether r is byte for byte the request NewRequest
// builds for its imprint, policy and nonce (64-bit nonce, certReq, no
// extensions). Only such a stored request may be sent again: anything
// else in n.tsq would leave the machine (AGENTS.md: only the Prüfwert).
func (r *Request) Canonical() bool {
	if r.Nonce == nil || r.Nonce.Sign() <= 0 || r.Nonce.BitLen() > 64 {
		return false
	}
	c, err := encodeRequest(r.Imprint, r.Policy, r.Nonce)
	return err == nil && bytes.Equal(c.DER, r.DER)
}

func encodeRequest(pruefwert [32]byte, policy asn1.ObjectIdentifier, nonce *big.Int) (*Request, error) {
	der, err := asn1.Marshal(timeStampReq{
		Version: 1,
		MessageImprint: messageImprint{
			// NULL parameters: what OpenSSL sends; some TSAs reject absent ones.
			HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: oidSHA256, Parameters: asn1.NullRawValue},
			HashedMessage: pruefwert[:],
		},
		ReqPolicy: policy,
		Nonce:     nonce,
		CertReq:   true,
	})
	if err != nil {
		return nil, err
	}
	return &Request{Imprint: pruefwert, Nonce: nonce, Policy: policy, DER: der}, nil
}

// ParseRequest parses a stored n.tsq. It accepts only requests this package
// would build: v1, SHA-256 imprint of 32 bytes, nonce present.
func ParseRequest(der []byte) (*Request, error) {
	var r timeStampReq
	rest, err := asn1.Unmarshal(der, &r)
	if err != nil {
		return nil, fmt.Errorf("tsq: %w", err)
	}
	if len(rest) != 0 {
		return nil, errors.New("tsq: trailing data")
	}
	if r.Version != 1 {
		return nil, fmt.Errorf("tsq: version %d", r.Version)
	}
	if !r.MessageImprint.HashAlgorithm.Algorithm.Equal(oidSHA256) || len(r.MessageImprint.HashedMessage) != 32 {
		return nil, errors.New("tsq: imprint is not SHA-256")
	}
	if r.Nonce == nil || r.Nonce.Sign() <= 0 {
		return nil, errors.New("tsq: no nonce")
	}
	req := &Request{Nonce: r.Nonce, Policy: r.ReqPolicy, DER: bytes.Clone(der)}
	copy(req.Imprint[:], r.MessageImprint.HashedMessage)
	return req, nil
}
