package tsa

import (
	"crypto"
	"encoding/asn1"
)

// Object identifiers used by RFC 3161, RFC 5652 (CMS), RFC 2634/5035 (ESS)
// and RFC 5280.
var (
	oidSHA1   = asn1.ObjectIdentifier{1, 3, 14, 3, 2, 26}
	oidSHA256 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 1}
	oidSHA384 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 2}
	oidSHA512 = asn1.ObjectIdentifier{2, 16, 840, 1, 101, 3, 4, 2, 3}

	oidRSAEncryption   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 1}
	oidRSAPSS          = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 10}
	oidSHA256WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 11}
	oidSHA384WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 12}
	oidSHA512WithRSA   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 13}
	oidMGF1            = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 1, 8}
	oidECPublicKey     = asn1.ObjectIdentifier{1, 2, 840, 10045, 2, 1}
	oidECDSAWithSHA256 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 2}
	oidECDSAWithSHA384 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 3}
	oidECDSAWithSHA512 = asn1.ObjectIdentifier{1, 2, 840, 10045, 4, 3, 4}

	oidSignedData   = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 7, 2}
	oidTSTInfo      = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4}
	oidContentType  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 3}
	oidMessageDigst = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 4}
	oidSigningCert  = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 12}
	oidSigningCert2 = asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 2, 47}

	oidExtKeyUsage    = asn1.ObjectIdentifier{2, 5, 29, 37}
	oidDeltaCRL       = asn1.ObjectIdentifier{2, 5, 29, 27}
	oidIssuingDistPt  = asn1.ObjectIdentifier{2, 5, 29, 28}
	oidFreshestCRL    = asn1.ObjectIdentifier{2, 5, 29, 46}
	oidKPTimeStamping = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
)

// allowedHash maps the hash OIDs on the spec §5 item 6 allowlist.
func allowedHash(oid asn1.ObjectIdentifier) (crypto.Hash, bool) {
	switch {
	case oid.Equal(oidSHA256):
		return crypto.SHA256, true
	case oid.Equal(oidSHA384):
		return crypto.SHA384, true
	case oid.Equal(oidSHA512):
		return crypto.SHA512, true
	}
	return 0, false
}
