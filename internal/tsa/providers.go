package tsa

import (
	_ "embed"
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Provider is one entry of the built-in provider list (spec §7). It holds
// connection data only; trust comes from the EU trusted lists.
type Provider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Country     string `json:"country"` // ISO 3166-1 alpha-2
	URL         string `json:"url"`
	Auth        string `json:"auth"`             // AuthBasic or AuthClientCert
	Policy      string `json:"policy,omitempty"` // expected TSA policy OID, dotted
	Accuracy    string `json:"accuracy,omitempty"`
	TrustedList string `json:"trusted_list"` // EU trusted list country code
}

//go:embed providers.json
var providersJSON []byte

// testProviders is only filled by RegisterTestProvider (build tag testtsa).
var testProviders []Provider

// Providers returns the built-in list.
func Providers() []Provider {
	var ps []Provider
	if err := json.Unmarshal(providersJSON, &ps); err != nil {
		panic("tsa: providers.json: " + err.Error())
	}
	return append(ps, testProviders...)
}

// ProviderByID looks up a provider.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// PolicyOID returns the expected policy, or nil if none is listed.
func (p Provider) PolicyOID() (asn1.ObjectIdentifier, error) {
	if p.Policy == "" {
		return nil, nil
	}
	var oid asn1.ObjectIdentifier
	for _, part := range strings.Split(p.Policy, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("provider %s: bad policy OID %q", p.ID, p.Policy)
		}
		oid = append(oid, n)
	}
	if len(oid) < 2 {
		return nil, fmt.Errorf("provider %s: bad policy OID %q", p.ID, p.Policy)
	}
	return oid, nil
}

// DeclaredAccuracy returns the provider's declared accuracy, used when a
// token states none (spec §6 step 8).
func (p Provider) DeclaredAccuracy() (time.Duration, bool) {
	if p.Accuracy == "" {
		return 0, false
	}
	d, err := time.ParseDuration(p.Accuracy)
	return d, err == nil && d >= 0
}
