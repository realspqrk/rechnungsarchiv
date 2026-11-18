package tsa

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"errors"
	"fmt"
	"time"
)

// checkKey enforces the key part of the allowlist (spec §5 item 6).
func checkKey(pub any) error {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 2048 {
			return fmt.Errorf("RSA key of %d bits", k.N.BitLen())
		}
		return nil
	case *ecdsa.PublicKey:
		if k.Curve == elliptic.P256() || k.Curve == elliptic.P384() {
			return nil
		}
		return fmt.Errorf("curve %s", k.Curve.Params().Name)
	}
	return fmt.Errorf("key type %T", pub)
}

type pssParams struct {
	Hash         pkix.AlgorithmIdentifier `asn1:"explicit,tag:0,optional"`
	MGF          pkix.AlgorithmIdentifier `asn1:"explicit,tag:1,optional"`
	SaltLength   int                      `asn1:"explicit,tag:2,optional,default:20"`
	TrailerField int                      `asn1:"explicit,tag:3,optional,default:1"`
}

// verifySignature verifies the CMS signature over the signed attributes
// with the signer's key, restricted to the allowlist.
func verifySignature(signer *x509.Certificate, h crypto.Hash, alg pkix.AlgorithmIdentifier, signed, sig []byte) error {
	if err := checkKey(signer.PublicKey); err != nil {
		return invalid(6, "Schlüssel nicht erlaubt", err)
	}
	hh := h.New()
	hh.Write(signed)
	digest := hh.Sum(nil)

	sameHash := func(oid asn1.ObjectIdentifier) error {
		if got, ok := allowedHash(oid); !ok || got != h {
			return invalid(6, "Signaturverfahren nicht erlaubt", fmt.Errorf("%v with digest %v", alg.Algorithm, h))
		}
		return nil
	}
	a := alg.Algorithm
	switch {
	case a.Equal(oidRSAEncryption), a.Equal(oidSHA256WithRSA), a.Equal(oidSHA384WithRSA), a.Equal(oidSHA512WithRSA):
		if !a.Equal(oidRSAEncryption) {
			if err := sameHash(map[string]asn1.ObjectIdentifier{
				oidSHA256WithRSA.String(): oidSHA256,
				oidSHA384WithRSA.String(): oidSHA384,
				oidSHA512WithRSA.String(): oidSHA512,
			}[a.String()]); err != nil {
				return err
			}
		}
		pub, ok := signer.PublicKey.(*rsa.PublicKey)
		if !ok {
			return invalid(6, "Schlüsseltyp passt nicht zum Verfahren", nil)
		}
		return rsa.VerifyPKCS1v15(pub, h, digest, sig)
	case a.Equal(oidRSAPSS):
		var p pssParams
		if err := unmarshalAll(alg.Parameters.FullBytes, &p, ""); err != nil {
			return invalid(6, "PSS-Parameter nicht lesbar", err)
		}
		if err := sameHash(p.Hash.Algorithm); err != nil {
			return err
		}
		var mgfHash pkix.AlgorithmIdentifier
		if !p.MGF.Algorithm.Equal(oidMGF1) || unmarshalAll(p.MGF.Parameters.FullBytes, &mgfHash, "") != nil {
			return invalid(6, "PSS-MGF nicht erlaubt", nil)
		}
		if err := sameHash(mgfHash.Algorithm); err != nil {
			return err
		}
		if p.TrailerField != 1 || p.SaltLength < 0 {
			return invalid(6, "PSS-Parameter nicht erlaubt", nil)
		}
		pub, ok := signer.PublicKey.(*rsa.PublicKey)
		if !ok {
			return invalid(6, "Schlüsseltyp passt nicht zum Verfahren", nil)
		}
		return rsa.VerifyPSS(pub, h, digest, sig, &rsa.PSSOptions{SaltLength: p.SaltLength, Hash: h})
	case a.Equal(oidECPublicKey), a.Equal(oidECDSAWithSHA256), a.Equal(oidECDSAWithSHA384), a.Equal(oidECDSAWithSHA512):
		if !a.Equal(oidECPublicKey) {
			if err := sameHash(map[string]asn1.ObjectIdentifier{
				oidECDSAWithSHA256.String(): oidSHA256,
				oidECDSAWithSHA384.String(): oidSHA384,
				oidECDSAWithSHA512.String(): oidSHA512,
			}[a.String()]); err != nil {
				return err
			}
		}
		pub, ok := signer.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return invalid(6, "Schlüsseltyp passt nicht zum Verfahren", nil)
		}
		if !ecdsa.VerifyASN1(pub, digest, sig) {
			return errors.New("ECDSA verification failed")
		}
		return nil
	}
	return invalid(6, "Signaturverfahren nicht erlaubt", fmt.Errorf("%v", a))
}

var allowedCertSigs = map[x509.SignatureAlgorithm]bool{
	x509.SHA256WithRSA: true, x509.SHA384WithRSA: true, x509.SHA512WithRSA: true,
	x509.SHA256WithRSAPSS: true, x509.SHA384WithRSAPSS: true, x509.SHA512WithRSAPSS: true,
	x509.ECDSAWithSHA256: true, x509.ECDSAWithSHA384: true, x509.ECDSAWithSHA512: true,
}

// verifyPath validates the signer at genTime (RFC 5280, via crypto/x509)
// up to one of the anchors, which may be a CA or the signer itself.
func verifyPath(signer *x509.Certificate, pool, anchors []*x509.Certificate, at time.Time) ([]*x509.Certificate, error) {
	if len(anchors) == 0 {
		return nil, invalid(7, "keine Vertrauensanker angegeben", nil)
	}
	roots := x509.NewCertPool()
	for _, a := range anchors {
		roots.AddCert(a)
	}
	inter := x509.NewCertPool()
	for _, c := range pool {
		inter.AddCert(c)
	}
	chains, err := signer.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: inter,
		CurrentTime:   at,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
	})
	if err != nil {
		return nil, invalid(7, "Zertifikatspfad zum Zeitpunkt des Zeitstempels ungültig", err)
	}
	var last error
	for _, ch := range chains {
		if last = checkChain(ch); last == nil {
			return ch, nil
		}
	}
	return nil, invalid(7, "Zertifikatspfad nicht erlaubt", last)
}

// checkChain adds the checks crypto/x509 leaves out: algorithm allowlist on
// every certificate in the path and keyUsage (RFC 5280 §6.1.4 (n)).
func checkChain(ch []*x509.Certificate) error {
	for i, c := range ch {
		if err := checkKey(c.PublicKey); err != nil {
			return fmt.Errorf("%s: %w", c.Subject, err)
		}
		if i < len(ch)-1 && !allowedCertSigs[c.SignatureAlgorithm] {
			return fmt.Errorf("%s: signature algorithm %v", c.Subject, c.SignatureAlgorithm)
		}
		if i > 0 && c.KeyUsage != 0 && c.KeyUsage&x509.KeyUsageCertSign == 0 {
			return fmt.Errorf("%s: keyUsage lacks keyCertSign", c.Subject)
		}
	}
	tsu := ch[0]
	if tsu.KeyUsage != 0 && tsu.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		return errors.New("TSU keyUsage lacks digitalSignature")
	}
	return nil
}
