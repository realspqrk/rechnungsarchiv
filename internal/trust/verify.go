package trust

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

const (
	// maxListSize bounds every list we read; the largest current lists
	// (ES, IT, FR) are about 3.5 MB.
	maxListSize = 32 << 20
	// maxXMLDepth bounds element nesting; trusted lists need about 12 levels.
	maxXMLDepth = 64
)

// verifyLOTL verifies the LOTL signature against the pinned certificates and
// returns the parsed LOTL.
func verifyLOTL(raw []byte, pins []*x509.Certificate) (*List, error) {
	l, err := verifySigned(raw, pins)
	if err != nil {
		return nil, fmt.Errorf("LOTL: %w", err)
	}
	if l.Territory != territoryLOTL {
		return nil, fmt.Errorf("LOTL: list is for territory %q", l.Territory)
	}
	return l, nil
}

// verifyNational verifies a national list of territory against the signing
// certificates the LOTL names for it.
func verifyNational(raw []byte, lotl *List, territory string) (*List, error) {
	p, ok := lotl.pointer(territory)
	if !ok {
		return nil, fmt.Errorf("%s: LOTL %d has no pointer to an XML list", territory, lotl.Sequence)
	}
	l, err := verifySigned(raw, p.Certs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", territory, err)
	}
	if l.Territory != territory {
		return nil, fmt.Errorf("%s: list is for territory %q", territory, l.Territory)
	}
	return l, nil
}

// verifySigned checks the enveloped XMLDSig/XAdES signature of a trusted list
// against trusted signer certificates and decodes only the signed content.
//
// The signer certificate must be byte-identical to one of trusted and valid
// at the list's ListIssueDateTime. That time is read from the unsigned
// document first (goxmldsig needs a clock) and then must equal the signed one.
func verifySigned(raw []byte, trusted []*x509.Certificate) (*List, error) {
	if len(trusted) == 0 {
		return nil, errors.New("no trusted signer certificates")
	}
	doc, err := readXML(raw)
	if err != nil {
		return nil, err
	}
	issue := doc.Root().FindElement("./SchemeInformation/ListIssueDateTime")
	if issue == nil {
		return nil, errors.New("no ListIssueDateTime")
	}
	claimed, err := parseTime(issue.Text())
	if err != nil {
		return nil, fmt.Errorf("ListIssueDateTime: %w", err)
	}
	if err := moveSignatureFirst(doc.Root()); err != nil {
		return nil, err
	}
	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: trusted})
	ctx.Clock = dsig.NewFakeClockAt(claimed)
	signed, err := ctx.Validate(doc.Root())
	if err != nil {
		return nil, fmt.Errorf("signature: %w", err)
	}
	out := etree.NewDocument()
	out.SetRoot(signed)
	canonical, err := out.WriteToBytes()
	if err != nil {
		return nil, err
	}
	l, err := decodeList(canonical)
	if err != nil {
		return nil, err
	}
	if !l.IssueDate.Equal(claimed) {
		return nil, errors.New("signed ListIssueDateTime differs from the document")
	}
	l.Raw = raw
	return l, nil
}

// moveSignatureFirst makes the enveloped signature (a direct child of the
// root, last by convention) the root's first child.
//
// goxmldsig (>= 1.5) stops its depth-first signature search after 1000
// elements, and trusted lists have tens of thousands before the signature.
// The move does not change what is signed: the enveloped-signature transform
// removes the Signature element wherever it sits, and the surrounding text
// nodes stay where they were.
func moveSignatureFirst(root *etree.Element) error {
	var sig *etree.Element
	for _, c := range root.ChildElements() {
		if c.Tag == "Signature" && c.NamespaceURI() == dsig.Namespace {
			if sig != nil {
				return errors.New("more than one enveloped signature")
			}
			sig = c
		}
	}
	if sig == nil {
		return errors.New("no enveloped signature")
	}
	root.RemoveChild(sig)
	root.InsertChildAt(0, sig)
	return nil
}

// readXML parses untrusted XML with a size and depth bound. Documents with a
// DTD are refused: trusted lists never carry one, and refusing it rules out
// entity tricks.
func readXML(raw []byte) (*etree.Document, error) {
	if len(raw) == 0 {
		return nil, errors.New("empty document")
	}
	if len(raw) > maxListSize {
		return nil, fmt.Errorf("document larger than %d bytes", maxListSize)
	}
	if bytes.Contains(raw, []byte("<!DOCTYPE")) || bytes.Contains(raw, []byte("<!ENTITY")) {
		return nil, errors.New("document contains a DTD")
	}
	doc := etree.NewDocument()
	doc.ReadSettings.MaxDepth = maxXMLDepth
	if err := doc.ReadFromBytes(raw); err != nil {
		return nil, fmt.Errorf("parse XML: %w", err)
	}
	root := doc.Root()
	if root == nil || root.Tag != "TrustServiceStatusList" || root.NamespaceURI() != nsTSL {
		return nil, errors.New("not a trusted list")
	}
	return doc, nil
}
