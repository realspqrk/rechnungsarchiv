package archive

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review 3 finding 5: rechnungsarchiv.json is read strictly: one canonical
// JSON object with every required field, nothing after it, no unknown key.
func TestConfigStrict(t *testing.T) {
	a, _ := setup(t)
	p := filepath.Join(a.Dir, configFile)
	good, _ := os.ReadFile(p)
	id := a.Config.ArchiveID
	canon := func(company, retention, tsa string) string {
		return `{"archive_id":"` + id + `","company":` + company + `,"format":1,"retention_default":` + retention + `,"tsa":` + tsa + `}`
	}
	if string(good) != canon(`"Alpenblick Tischlerei GmbH"`, "7", "null") {
		t.Fatalf("init wrote %s", good)
	}
	for name, data := range map[string]string{
		"trailing garbage":  string(good) + "x",
		"second object":     string(good) + `{"tsa":"mensatek"}`,
		"trailing newline":  string(good) + "\n",
		"leading space":     " " + string(good),
		"missing company":   `{"archive_id":"` + id + `","format":1,"retention_default":7,"tsa":null}`,
		"empty company":     canon(`""`, "7", "null"),
		"blank company":     canon(`"  "`, "7", "null"),
		"company newline":   canon(`"a\nb"`, "7", "null"),
		"missing tsa":       `{"archive_id":"` + id + `","company":"X","format":1,"retention_default":7}`,
		"unknown key":       `{"archive_id":"` + id + `","company":"X","extra":1,"format":1,"retention_default":7,"tsa":null}`,
		"duplicate key":     `{"archive_id":"` + id + `","company":"X","company":"Y","format":1,"retention_default":7,"tsa":null}`,
		"not canonical":     `{"company":"X","archive_id":"` + id + `","format":1,"retention_default":7,"tsa":null}`,
		"spaces":            strings.ReplaceAll(canon(`"X"`, "7", "null"), ",", ", "),
		"bad archive id":    strings.Replace(canon(`"X"`, "7", "null"), id, "kein-uuid", 1),
		"bad retention":     canon(`"X"`, "8", "null"),
		"float retention":   canon(`"X"`, "7.0", "null"),
		"empty tsa":         canon(`"X"`, "7", `""`),
		"not an object":     `[]`,
		"null":              `null`,
		"escaped non-ascii": canon(`"M`+string(rune(92))+`u00fcller"`, "7", "null"),
	} {
		os.WriteFile(p, []byte(data), 0o644)
		if _, err := OpenReadOnly(a.Dir); !errors.Is(err, ErrBadConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// Canonical variants are accepted.
	for _, data := range []string{canon(`"Müller & Söhne"`, "22", `"mensatek"`), string(good)} {
		os.WriteFile(p, []byte(data), 0o644)
		if _, err := OpenReadOnly(a.Dir); err != nil {
			t.Errorf("%s: %v", data, err)
		}
	}

	// A setter never rewrites a config that turned bad after Open.
	w, err := Open(a.Dir)
	if err != nil {
		t.Fatal(err)
	}
	bad := string(good) + `{"tsa":"mensatek"}`
	os.WriteFile(p, []byte(bad), 0o644)
	prov := "mensatek"
	for _, err := range []error{w.SetProvider(&prov), w.SetCompany("Neu GmbH"), w.SetRetentionDefault(10)} {
		if !errors.Is(err, ErrBadConfig) {
			t.Fatalf("setter on bad config: %v", err)
		}
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, []byte(bad)) {
		t.Fatalf("bad config rewritten: %s", b)
	}
}
