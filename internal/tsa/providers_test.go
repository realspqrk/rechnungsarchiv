package tsa

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestProvidersShippedList(t *testing.T) {
	dec := json.NewDecoder(bytes.NewReader(providersJSON))
	dec.DisallowUnknownFields() // e.g. no "price"
	var ps []Provider
	if err := dec.Decode(&ps); err != nil {
		t.Fatal(err)
	}
	cc := regexp.MustCompile(`^[A-Z]{2}$`)
	ids := map[string]bool{}
	for _, p := range ps {
		if p.ID == "" || ids[p.ID] || p.Name == "" {
			t.Errorf("%q: missing or duplicate id/name", p.ID)
		}
		ids[p.ID] = true
		if !cc.MatchString(p.Country) || !cc.MatchString(p.TrustedList) {
			t.Errorf("%s: bad country codes", p.ID)
		}
		if p.Auth != AuthBasic && p.Auth != AuthClientCert {
			t.Errorf("%s: auth %q", p.ID, p.Auth)
		}
		if p.URL != "" && !strings.HasPrefix(p.URL, "https://") {
			t.Errorf("%s: URL not HTTPS", p.ID)
		}
		if _, err := p.RequestPolicyOID(); err != nil {
			t.Error(err)
		}
		if _, err := p.ExpectedPolicyOIDs(); err != nil {
			t.Error(err)
		}
		if p.URLFromContract != (p.URL == "") {
			t.Errorf("%s: url_from_contract must be set exactly when url is empty", p.ID)
		}
		if _, ok := p.DeclaredAccuracy(); p.Accuracy != "" && !ok {
			t.Errorf("%s: accuracy %q", p.ID, p.Accuracy)
		}
		// Display fields: the GUI's "Konto beim Anbieter anlegen" opens Website.
		if p.ShortName == "" || len(p.ShortName) > len(p.Name) {
			t.Errorf("%s: short_name %q", p.ID, p.ShortName)
		}
		if !strings.HasPrefix(p.Website, "https://") {
			t.Errorf("%s: website must be an https URL", p.ID)
		}
		if p.Recommended != (p.ID == "mensatek") {
			t.Errorf("%s: only mensatek is recommended (README)", p.ID)
		}
		// Only providers proven in real use are offered (README).
		if p.Hidden == (p.ID == "mensatek") {
			t.Errorf("%s: hidden=%v; only mensatek is offered", p.ID, p.Hidden)
		}
		if p.Hidden && (p.Offered("") || !p.Offered(p.ID)) {
			t.Errorf("%s: a hidden provider is offered only to an archive already using it", p.ID)
		}
	}
	if len(Providers()) != len(ps) {
		t.Error("test providers leak into a normal build")
	}
	m, ok := ProviderByID("mensatek")
	if !ok {
		t.Fatal("mensatek missing")
	}
	// Mensatek rejects any reqPolicy but issues tokens under these policies.
	if m.RequestPolicy != "" || len(m.ExpectedPolicies) != 2 {
		t.Errorf("mensatek policies: %q / %q", m.RequestPolicy, m.ExpectedPolicies)
	}
	if d, ok := ProviderByID("disig"); !ok || !d.URLFromContract {
		t.Error("disig must take its URL from the contract")
	}
}
