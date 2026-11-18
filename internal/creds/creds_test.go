package creds

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func exercise(t *testing.T, s Store) {
	t.Helper()
	b := make([]byte, 8)
	rand.Read(b)
	id := "test-" + hex.EncodeToString(b) // never collides with a real archive
	t.Cleanup(func() { s.Delete(id, "mensatek") })

	if ok, err := s.Has(id, "mensatek"); err != nil || ok {
		t.Fatalf("Has before Set: %v %v", ok, err)
	}
	if _, err := s.Get(id, "mensatek"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set: %v", err)
	}
	c := Credential{User: "kunde", Password: "geheim-ä€"}
	if err := s.Set(id, "mensatek", c); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get(id, "mensatek"); err != nil || got != c {
		t.Fatalf("Get: %v %v", got, err)
	}
	// Keyed per archive and provider.
	if ok, _ := s.Has(id, "disig"); ok {
		t.Fatal("other provider sees the credential")
	}
	if ok, _ := s.Has(id+"x", "mensatek"); ok {
		t.Fatal("other archive sees the credential")
	}
	c2 := Credential{Password: "p12pw", P12Path: `C:\Zert\disig.p12`, URL: "https://tsa.example/x"}
	if err := s.Set(id, "mensatek", c2); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(id, "mensatek"); got != c2 {
		t.Fatalf("overwrite: %v", got)
	}
	if err := s.Delete(id, "mensatek"); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.Has(id, "mensatek"); err != nil || ok {
		t.Fatalf("Has after Delete: %v %v", ok, err)
	}
	if err := s.Delete(id, "mensatek"); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if err := s.Set("a/b", "mensatek", c); err == nil {
		t.Fatal("key with separator accepted")
	}
}

func TestMemory(t *testing.T) { exercise(t, NewMemory()) }

func TestSystem(t *testing.T) {
	if runtime.GOOS != "windows" {
		if err := System().Set("x", "y", Credential{}); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("want ErrUnsupported, got %v", err)
		}
		return
	}
	exercise(t, System())
}

func TestNoSecretInOutput(t *testing.T) {
	c := Credential{User: "kunde", Password: "streng-geheim"}
	for _, s := range []string{fmt.Sprint(c), fmt.Sprintf("%v %+v %#v %s", c, c, c, c), fmt.Sprint(&c)} {
		if strings.Contains(s, "streng-geheim") {
			t.Fatalf("secret in output: %s", s)
		}
	}
}
