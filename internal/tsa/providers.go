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
	ID      string `json:"id"`
	Name    string `json:"name"`
	Country string `json:"country"` // ISO 3166-1 alpha-2
	URL     string `json:"url"`
	Auth    string `json:"auth"` // AuthBasic or AuthClientCert
	// URLFromContract is true when the endpoint comes with the contract
	// (e.g. Disig); URL is then empty and the user enters it.
	URLFromContract bool `json:"url_from_contract,omitempty"`
	// RequestPolicy is sent as reqPolicy if set. Some providers (Mensatek)
	// reject any reqPolicy, so it is usually empty.
	RequestPolicy string `json:"request_policy,omitempty"`
	// ExpectedPolicies are the token policies accepted at stamping time
	// (spec §5 item 5); empty means not checked.
	ExpectedPolicies []string `json:"expected_policies,omitempty"`
	Accuracy         string   `json:"accuracy,omitempty"`
	TrustedList      string   `json:"trusted_list"` // EU trusted list country code

	// Display fields for the GUI. No prices, ever (they live in the README).
	ShortName   string `json:"short_name,omitempty"`  // e.g. "Mensatek"
	Website     string `json:"website,omitempty"`     // official page where an account is opened
	Recommended bool   `json:"recommended,omitempty"` // the README's recommendation
	// Hidden providers are not offered for new setups (not proven in real
	// use yet). They stay known: existing tokens verify, and an archive
	// already set up for one keeps working and can update its credentials.
	Hidden bool `json:"hidden,omitempty"`
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

// Offered reports whether p may be chosen for an archive whose provider is
// current ("" if none): every provider that is not hidden, and the one
// already set up.
func (p Provider) Offered(current string) bool { return !p.Hidden || p.ID == current }

// ProviderByID looks up a provider.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers() {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// RequestPolicyOID returns the reqPolicy to send, or nil if none is set.
func (p Provider) RequestPolicyOID() (asn1.ObjectIdentifier, error) {
	if p.RequestPolicy == "" {
		return nil, nil
	}
	return parseOID(p.ID, p.RequestPolicy)
}

// ExpectedPolicyOIDs returns the accepted token policies; nil accepts any.
func (p Provider) ExpectedPolicyOIDs() ([]asn1.ObjectIdentifier, error) {
	var out []asn1.ObjectIdentifier
	for _, s := range p.ExpectedPolicies {
		oid, err := parseOID(p.ID, s)
		if err != nil {
			return nil, err
		}
		out = append(out, oid)
	}
	return out, nil
}

func parseOID(id, s string) (asn1.ObjectIdentifier, error) {
	var oid asn1.ObjectIdentifier
	for _, part := range strings.Split(s, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("provider %s: bad policy OID %q", id, s)
		}
		oid = append(oid, n)
	}
	if len(oid) < 2 {
		return nil, fmt.Errorf("provider %s: bad policy OID %q", id, s)
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
