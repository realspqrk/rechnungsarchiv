// Package creds keeps timestamp-provider credentials in the OS credential
// store (spec §1), per PC and keyed by archive_id and provider id. Nothing is
// ever written into the archive. Secrets never appear in String or %v output.
package creds

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Credential is what a provider needs on this PC.
type Credential struct {
	User     string `json:"user,omitempty"`     // HTTP Basic user
	Password string `json:"password,omitempty"` // HTTP Basic password, or the PKCS#12 password
	P12Path  string `json:"p12,omitempty"`      // client certificate: path to the PKCS#12 file
	URL      string `json:"url,omitempty"`      // endpoint from the contract (url_from_contract)
}

// String never shows the password.
func (c Credential) String() string {
	pw := ""
	if c.Password != "" {
		pw = "***"
	}
	return fmt.Sprintf("{User:%q Password:%s P12Path:%q URL:%q}", c.User, pw, c.P12Path, c.URL)
}

// GoString keeps %#v from printing the password.
func (c Credential) GoString() string { return "creds.Credential" + c.String() }

// Store is a credential store.
type Store interface {
	Set(archiveID, provider string, c Credential) error
	// Get returns ErrNotFound if nothing is stored.
	Get(archiveID, provider string) (Credential, error)
	Has(archiveID, provider string) (bool, error)
	Delete(archiveID, provider string) error
}

var (
	// ErrNotFound means no credential is stored for the key.
	ErrNotFound = errors.New("keine Zugangsdaten auf diesem PC gespeichert")
	// ErrUnsupported means this system has no supported credential store.
	ErrUnsupported = errors.New("Zugangsdaten können auf diesem System nicht sicher gespeichert werden (nur Windows wird unterstützt)")
)

// target is the store key: one entry per archive and provider.
func target(archiveID, provider string) (string, error) {
	for _, s := range []string{archiveID, provider} {
		if s == "" || strings.ContainsAny(s, "/\\ \t\r\n") {
			return "", fmt.Errorf("creds: invalid key part %q", s)
		}
	}
	return "rechnungsarchiv/" + archiveID + "/" + provider, nil
}

// System returns the OS credential store: Windows Credential Manager on
// Windows; elsewhere a store whose every call fails with ErrUnsupported.
func System() Store { return system{} }

// NewMemory returns an in-memory store for tests.
func NewMemory() Store { return &memory{m: map[string]Credential{}} }

type memory struct {
	mu sync.Mutex
	m  map[string]Credential
}

func (s *memory) Set(archiveID, provider string, c Credential) error {
	k, err := target(archiveID, provider)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = c
	return nil
}

func (s *memory) Get(archiveID, provider string) (Credential, error) {
	k, err := target(archiveID, provider)
	if err != nil {
		return Credential{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.m[k]
	if !ok {
		return Credential{}, ErrNotFound
	}
	return c, nil
}

func (s *memory) Has(archiveID, provider string) (bool, error) {
	_, err := s.Get(archiveID, provider)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (s *memory) Delete(archiveID, provider string) error {
	k, err := target(archiveID, provider)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
