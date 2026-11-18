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
		if _, err := p.PolicyOID(); err != nil {
			t.Error(err)
		}
		if _, ok := p.DeclaredAccuracy(); p.Accuracy != "" && !ok {
			t.Errorf("%s: accuracy %q", p.ID, p.Accuracy)
		}
	}
	if len(Providers()) != len(ps) {
		t.Error("test providers leak into a normal build")
	}
	if _, ok := ProviderByID("mensatek"); !ok {
		t.Error("mensatek missing")
	}
}
